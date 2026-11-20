package handler

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"auction/logging"
)

// requestIDHeader — заголовок, которым request_id приходит от прокси (nginx) и
// возвращается клиенту: по нему обращение пользователя находится в логах.
const requestIDHeader = "X-Request-Id"

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap позволяет http.ResponseController добраться до настоящего ResponseWriter.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Hijack нужен для апгрейда до WebSocket: без него обёртка скрывала бы http.Hijacker.
// В лог при этом попадает 101, а не 200.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(s.ResponseWriter).Hijack()
	if err == nil {
		s.status = http.StatusSwitchingProtocols
	}
	return conn, rw, err
}

// Middleware: request_id, восстановление после паники, access-лог, базовые заголовки
// безопасности. Логгер берётся из logging.Default() (его задаёт main).
//
// В контекст запроса кладётся состояние логирования (см. пакет logging): дальше по
// цепочке handler → db → live любой код, у которого есть ctx, пишет записи с тем же
// request_id. Access-лог — одна запись на запрос, она пишется в конце.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Идентификатор от прокси принимаем, только если он безопасен для лога.
		id := r.Header.Get(requestIDHeader)
		if !logging.ValidRequestID(id) {
			id = logging.NewRequestID()
		}
		ctx, _ := logging.WithRequest(r.Context(), logging.Default(), id)
		r = r.WithContext(ctx)
		w.Header().Set(requestIDHeader, id)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			p := recover()
			// http.ErrAbortHandler — штатный способ оборвать ответ: это не авария,
			// и пробрасываем его дальше, как того требует net/http.
			if p != nil && p != http.ErrAbortHandler {
				err := fmt.Errorf("panic: %v", p)
				logging.SetError(ctx, err)
				logPanic(ctx, err)
				writeError(rec, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
			}
			logAccess(ctx, r, rec, start)
			if p == http.ErrAbortHandler {
				panic(p)
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(rec, r)
	})
}

// logPanic пишет ERROR со стеком. Стек добавляется только к этой записи: в обычных
// ERROR-записях он не нужен и лишь раздувал бы лог.
func logPanic(ctx context.Context, err error) {
	logging.FromContext(ctx, logging.ComponentMiddleware).
		WithOptions(zap.AddStacktrace(zapcore.ErrorLevel)).
		Error("panic в обработчике", zap.Error(err))
}

// logAccess пишет единую запись о запросе:
//
//	request_id, user_id (если известен), method, path, status_code, duration_ms, client_ip, error.
//
// Уровень: 5xx — ERROR, /health — DEBUG (его опрашивает Docker каждые несколько секунд),
// остальное — INFO. 4xx — тоже INFO: для этого приложения 401 нормален (access-токен живёт
// 15 минут, после него клиент обновляет сессию), а опасные события пишутся на WARN
// в месте обнаружения.
func logAccess(ctx context.Context, r *http.Request, rec *statusRecorder, start time.Time) {
	lvl := zapcore.InfoLevel
	switch {
	case rec.status >= http.StatusInternalServerError:
		lvl = zapcore.ErrorLevel
	case r.URL.Path == "/health":
		lvl = zapcore.DebugLevel
	}
	ce := logging.FromContext(ctx, logging.ComponentMiddleware).Check(lvl, "http request")
	if ce == nil {
		return
	}
	fields := []zap.Field{
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.Int("status_code", rec.status),
		zap.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		zap.String("client_ip", clientIP(r)),
	}
	if st := logging.RequestFrom(ctx); st != nil {
		if err := st.Err(); err != nil {
			fields = append(fields, zap.Error(err))
		}
	}
	ce.Write(fields...)
}
