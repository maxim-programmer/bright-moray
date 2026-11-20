package handler

import (
	"auction/config"
	"auction/db"
	"auction/logging"
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// reqlogObserve подменяет логгер по умолчанию на наблюдателя и возвращает его записи.
func reqlogObserve(t *testing.T, lvl zapcore.Level) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(lvl)
	logging.SetDefault(zap.New(core))
	t.Cleanup(func() { logging.SetDefault(nil) })
	return logs
}

func reqlogServe(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestReqLogAccessLog(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logging.SetUser(r.Context(), 7) // как это делает h.userID после проверки токена
		w.WriteHeader(http.StatusCreated)
	}))

	rec := reqlogServe(h, "POST", "/api/lots/17/bids?token=SECRET", map[string]string{"X-Real-IP": "203.0.113.5"})

	id := rec.Header().Get("X-Request-Id")
	if len(id) != 32 {
		t.Fatalf("X-Request-Id в ответе: %q", id)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("заголовки безопасности пропали")
	}
	if logs.Len() != 1 {
		t.Fatalf("записей: %d (ожидалась одна на запрос)", logs.Len())
	}
	e := logs.All()[0]
	if e.Message != "http request" || e.Level != zapcore.InfoLevel {
		t.Errorf("msg=%q level=%v", e.Message, e.Level)
	}
	m := e.ContextMap()
	want := map[string]any{
		"component": "middleware", "request_id": id, "user_id": int64(7),
		"method": "POST", "path": "/api/lots/17/bids", "status_code": int64(201), "client_ip": "203.0.113.5",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, ожидалось %v", k, m[k], v)
		}
	}
	if d, ok := m["duration_ms"].(float64); !ok || d < 0 {
		t.Errorf("duration_ms = %v", m["duration_ms"])
	}
	if _, ok := m["error"]; ok {
		t.Errorf("у успешного запроса не должно быть error: %v", m)
	}
	for k, v := range m { // query-строка (там могут быть токены) в лог не попадает
		if s, ok := v.(string); ok && strings.Contains(s, "SECRET") {
			t.Errorf("в поле %s попала query-строка: %q", k, s)
		}
	}
}

func TestReqLogRequestID(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	var inHandler string
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inHandler = logging.RequestID(r.Context())
		logging.FromContext(r.Context(), logging.ComponentHandler).Info("внутри")
	}))

	// безопасный идентификатор от прокси принимается
	rec := reqlogServe(h, "GET", "/api/x", map[string]string{"X-Request-Id": "nginx-abc12345"})
	if got := rec.Header().Get("X-Request-Id"); got != "nginx-abc12345" || inHandler != got {
		t.Errorf("принят %q, в обработчике %q", got, inHandler)
	}
	// записи обработчика и access-запись делят один request_id
	for _, e := range logs.All() {
		if e.ContextMap()["request_id"] != "nginx-abc12345" {
			t.Errorf("%q: request_id = %v", e.Message, e.ContextMap()["request_id"])
		}
	}

	// небезопасный идентификатор заменяется на свой
	for _, bad := range []string{"short", "with space and more", "inject\"quote\"1234", strings.Repeat("a", 65)} {
		rec := reqlogServe(h, "GET", "/api/x", map[string]string{"X-Request-Id": bad})
		got := rec.Header().Get("X-Request-Id")
		if got == bad || !logging.ValidRequestID(got) {
			t.Errorf("идентификатор %q не заменён: %q", bad, got)
		}
	}
}

func TestReqLogLevels(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	status := http.StatusOK
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))

	cases := []struct {
		path   string
		status int
		level  zapcore.Level
	}{
		{"/api/lots", 200, zapcore.InfoLevel},
		{"/api/auth/me", 401, zapcore.InfoLevel}, // нормальный сценарий до refresh
		{"/api/lots/1", 404, zapcore.InfoLevel},
		{"/api/lots", 500, zapcore.ErrorLevel},
		{"/api/lots", 503, zapcore.ErrorLevel},
		{"/health", 200, zapcore.DebugLevel},
		{"/health", 503, zapcore.ErrorLevel}, // упавшая проверка — не шум
	}
	for _, c := range cases {
		logs.TakeAll()
		status = c.status
		reqlogServe(h, "GET", c.path, nil)
		all := logs.All()
		if len(all) != 1 || all[0].Level != c.level {
			t.Errorf("%s %d: записи %v, ожидался уровень %v", c.path, c.status, all, c.level)
		}
	}

	// на уровне info health-проверки не попадают в лог совсем
	logs = reqlogObserve(t, zapcore.InfoLevel)
	status = 200
	reqlogServe(h, "GET", "/health", nil)
	if logs.Len() != 0 {
		t.Errorf("/health при info: %d записей", logs.Len())
	}
}

func TestReqLogFailRecordsErrorOnceInAccessLine(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail(w, r, errors.New("db down"))
	}))
	rec := reqlogServe(h, "GET", "/api/lots", nil)

	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"internal"`) {
		t.Fatalf("ответ: %d %s", rec.Code, rec.Body.String())
	}
	if logs.Len() != 1 {
		t.Fatalf("ошибка должна дать одну запись, а не %d: %v", logs.Len(), logs.All())
	}
	e := logs.All()[0]
	if e.Level != zapcore.ErrorLevel || e.ContextMap()["error"] != "db down" || e.ContextMap()["status_code"] != int64(500) {
		t.Errorf("запись: %v %v", e.Level, e.ContextMap())
	}
}

func TestReqLogFailExpectedErrorsAreNotLoggedAsErrors(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail(w, r, db.ErrNotFound)
	}))
	rec := reqlogServe(h, "GET", "/api/lots/9", nil)
	if rec.Code != 404 {
		t.Fatalf("код %d", rec.Code)
	}
	e := logs.All()[0]
	if e.Level != zapcore.InfoLevel {
		t.Errorf("404 — INFO, получено %v", e.Level)
	}
	if _, ok := e.ContextMap()["error"]; ok {
		t.Errorf("ожидаемый исход не должен нести error: %v", e.ContextMap())
	}
}

func TestReqLogFailWithoutMiddlewareLogsDirectly(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	rec := httptest.NewRecorder()
	fail(rec, httptest.NewRequest("GET", "/x", nil), errors.New("прямой вызов"))
	if rec.Code != 500 || logs.Len() != 1 {
		t.Fatalf("код %d, записей %d", rec.Code, logs.Len())
	}
	e := logs.All()[0]
	if e.Level != zapcore.ErrorLevel || e.ContextMap()["component"] != "handler" || e.ContextMap()["error"] != "прямой вызов" {
		t.Errorf("запись: %v %v", e.Level, e.ContextMap())
	}
}

func TestReqLogPanic(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logging.SetUser(r.Context(), 3)
		reqlogExplode()
	}))
	rec := reqlogServe(h, "GET", "/api/boom", nil)

	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"internal"`) {
		t.Fatalf("ответ: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("текст panic утёк клиенту: %s", rec.Body.String())
	}
	all := logs.All()
	if len(all) != 2 {
		t.Fatalf("ожидались запись о panic и access-запись, получено %d: %v", len(all), all)
	}
	p, acc := all[0], all[1]
	if p.Message != "panic in handler" || p.Level != zapcore.ErrorLevel {
		t.Errorf("panic: %q %v", p.Message, p.Level)
	}
	if p.ContextMap()["error"] != "panic: boom" || p.ContextMap()["user_id"] != int64(3) {
		t.Errorf("поля panic: %v", p.ContextMap())
	}
	if !strings.Contains(p.Stack, "reqlogExplode") {
		t.Errorf("стек не содержит место panic:\n%s", p.Stack)
	}
	if acc.Message != "http request" || acc.Level != zapcore.ErrorLevel ||
		acc.ContextMap()["status_code"] != int64(500) || acc.ContextMap()["error"] != "panic: boom" {
		t.Errorf("access: %v %v", acc.Level, acc.ContextMap())
	}
	if acc.Stack != "" {
		t.Error("стек должен быть только у записи о panic")
	}
}

func reqlogExplode() { panic("boom") }

func TestReqLogRepanicsErrAbortHandler(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	var got any
	func() {
		defer func() { got = recover() }()
		reqlogServe(h, "GET", "/api/abort", nil)
	}()
	if got != http.ErrAbortHandler {
		t.Errorf("ErrAbortHandler должен идти дальше, получено %v", got)
	}
	// это не авария: записи о panic нет, есть только access-запись
	if logs.Len() != 1 || logs.All()[0].Message != "http request" {
		t.Errorf("записи: %v", logs.All())
	}
}

// Hijack нужен WebSocket: обёртка не должна его скрывать, а в лог должен идти статус 101.
func TestReqLogKeepsHijackForWebSocket(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	srv := httptest.NewServer(Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("Hijack через обёртку: %v", err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\n")
		_ = rw.Flush()
	})))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("GET /api/ws HTTP/1.1\r\nHost: x\r\n\r\n"))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, _ := bufio.NewReader(conn).ReadString('\n')
	if !strings.Contains(line, "101") {
		t.Fatalf("ответ: %q", line)
	}

	deadline := time.Now().Add(3 * time.Second)
	for logs.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if logs.Len() != 1 || logs.All()[0].ContextMap()["status_code"] != int64(101) {
		t.Errorf("записи: %v", logs.All())
	}
}

// Сквозная проверка без БД: Middleware → Guard → mux → authed → обработчик.
// Маршрут PUT /api/me/password отвечает 400 на пустое тело до обращения к базе.
func TestReqLogAccessLogGetsUserIDAfterAuth(t *testing.T) {
	logs := reqlogObserve(t, zapcore.DebugLevel)
	h := New(nil, config.Config{JWTSecret: "0123456789abcdef0123456789abcdef"})
	t.Cleanup(h.StopLive)
	mux := http.NewServeMux()
	SetupRoutes(mux, h)
	srv := Middleware(h.Guard(mux))

	access, _, err := h.jwt.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	do := func(cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "http://example.com/api/me/password", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.com")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(access); rec.Code != http.StatusBadRequest {
		t.Fatalf("с токеном: %d %s", rec.Code, rec.Body.String())
	}
	m := logs.TakeAll()[0].ContextMap()
	if m["user_id"] != int64(42) || m["status_code"] != int64(400) || m["path"] != "/api/me/password" {
		t.Errorf("с токеном: %v", m)
	}

	if rec := do(""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("без токена: %d", rec.Code)
	}
	m = logs.TakeAll()[0].ContextMap()
	if _, ok := m["user_id"]; ok || m["status_code"] != int64(401) {
		t.Errorf("без токена user_id быть не должно: %v", m)
	}
}
