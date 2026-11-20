package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Request — состояние логирования одного HTTP-запроса. Создаётся в Middleware
// (WithRequest) и лежит в context.Context, поэтому доходит до handler, db и live без
// изменения сигнатур: везде, где есть ctx, есть и request_id.
//
// Состояние изменяемое, потому что user_id становится известен не в начале запроса,
// а когда проверен access-токен. Все методы безопасны для параллельного вызова.
type Request struct {
	id   string
	base *zap.Logger

	mu  sync.Mutex
	log *zap.Logger // base + request_id (+ user_id, когда известен)
	uid int64
	err error
}

type ctxKey struct{}

// WithRequest кладёт в контекст состояние нового запроса с логгером base и
// идентификатором id (см. NewRequestID, ValidRequestID).
func WithRequest(ctx context.Context, base *zap.Logger, id string) (context.Context, *Request) {
	r := &Request{
		id:   id,
		base: base,
		log:  base.With(zap.String("request_id", id)),
	}
	return context.WithValue(ctx, ctxKey{}, r), r
}

// RequestFrom возвращает состояние запроса или nil, если контекст не запросный.
func RequestFrom(ctx context.Context) *Request {
	r, _ := ctx.Value(ctxKey{}).(*Request)
	return r
}

// FromContext возвращает логгер для компонента component (ComponentHandler и т. п.).
// В запросе это логгер с request_id и, если пользователь уже известен, с user_id;
// вне запроса — логгер по умолчанию. Поле component есть всегда: обязательный
// параметр не даёт про него забыть.
func FromContext(ctx context.Context, component string) *zap.Logger {
	base := Default()
	if r := RequestFrom(ctx); r != nil {
		base = r.logger()
	}
	return base.With(zap.String("component", component))
}

// RequestID возвращает идентификатор запроса или "" вне запроса.
func RequestID(ctx context.Context) string {
	if r := RequestFrom(ctx); r != nil {
		return r.id
	}
	return ""
}

// SetUser сообщает, что запрос выполняется от имени пользователя uid. Дальнейшие записи
// этого запроса получают поле user_id. Вызывается там, где проверяется access-токен.
func SetUser(ctx context.Context, uid int64) {
	r := RequestFrom(ctx)
	if r == nil || uid <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.uid == uid {
		return
	}
	r.uid = uid
	r.log = r.base.With(zap.String("request_id", r.id), zap.Int64("user_id", uid))
}

// SetError запоминает ошибку, определившую неуспешный ответ. Запись о ней пишет
// Middleware — одна строка на запрос вместе со статусом и временем, поэтому ошибка не
// дублируется. Сохраняется первая ошибка: она ближе к причине.
func SetError(ctx context.Context, err error) {
	r := RequestFrom(ctx)
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}

// ID — идентификатор запроса.
func (r *Request) ID() string { return r.id }

// UserID — id пользователя или 0, если запрос не авторизован.
func (r *Request) UserID() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.uid
}

// Err — ошибка, запомненная через SetError, или nil.
func (r *Request) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *Request) logger() *zap.Logger {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.log
}

/* ---------- идентификатор запроса ---------- */

// NewRequestID создаёт случайный идентификатор: 16 байт в hex (32 символа).
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand не должен отказывать; идентификатор нужен только для корреляции
		// логов, поэтому не падаем, а берём время.
		return fmt.Sprintf("t%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// ValidRequestID сообщает, годится ли идентификатор, пришедший от клиента или прокси.
// Принимаем только 8–64 символа [A-Za-z0-9_-]: иначе клиент мог бы подсунуть в лог
// произвольный текст.
func ValidRequestID(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
