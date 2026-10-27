package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"auction/auth"
	"auction/config"
	"auction/db"
	"auction/model"
)

const (
	sessionCookie = "session"
	sessionTTL    = 7 * 24 * time.Hour
	maxJSONBody   = 1 << 20
)

type Handler struct {
	q         *db.Queries
	cfg       config.Config
	secret    []byte
	loginRate *limiter
	// хеш-пустышка: сравниваем с ним, когда пользователь не найден,
	// чтобы время ответа не выдавало существование никнейма
	dummyHash []byte
}

func New(q *db.Queries, cfg config.Config) *Handler {
	return &Handler{
		q:         q,
		cfg:       cfg,
		secret:    []byte(cfg.JWTSecret),
		loginRate: newLimiter(10, 5*time.Minute),
		dummyHash: mustHash("dummy-password"),
	}
}

/* ---------- ответы ---------- */

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// fail переводит ошибку слоя данных в HTTP-ответ.
func fail(w http.ResponseWriter, err error) {
	var ae *model.APIError
	switch {
	case errors.As(err, &ae):
		writeError(w, ae.Status, ae.Code, ae.Message)
	case errors.Is(err, db.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Не найдено")
	case errors.Is(err, context.Canceled):
		// клиент ушёл — отвечать некому
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "Некорректное тело запроса")
		return false
	}
	return true
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

/* ---------- сессии ---------- */

func (h *Handler) setSession(w http.ResponseWriter, userID int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    auth.Sign(h.secret, userID, sessionTTL),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

// userID возвращает id авторизованного пользователя или 0.
// Токен берётся из cookie, а при её отсутствии — из заголовка Authorization: Bearer.
func (h *Handler) userID(r *http.Request) int64 {
	token := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		token = c.Value
	} else if a := r.Header.Get("Authorization"); len(a) > 7 && a[:7] == "Bearer " {
		token = a[7:]
	}
	if token == "" {
		return 0
	}
	id, err := auth.Parse(h.secret, token)
	if err != nil {
		return 0
	}
	return id
}

func (h *Handler) authed(fn func(w http.ResponseWriter, r *http.Request, uid int64)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := h.userID(r)
		if uid == 0 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Войдите в аккаунт")
			return
		}
		fn(w, r, uid)
	}
}

/* ---------- ограничитель попыток входа ---------- */

type limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	max    int
	window time.Duration
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{hits: map[string][]time.Time{}, max: max, window: window}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	if len(l.hits) > 10000 { // защита от разрастания карты
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	return true
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

/* ---------- middleware ---------- */

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Middleware: восстановление после паники, лог запросов, базовые заголовки безопасности.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if p := recover(); p != nil {
				log.Printf("panic: %v\n%s", p, debug.Stack())
				writeError(rec, http.StatusInternalServerError, "internal", "Внутренняя ошибка сервера")
			}
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(rec, r)
	})
}

/* ---------- маршруты ---------- */

func SetupRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /api/time", h.ServerTime)

	mux.HandleFunc("POST /api/auth/register", h.Register)
	mux.HandleFunc("POST /api/auth/login", h.Login)
	mux.HandleFunc("POST /api/auth/logout", h.Logout)
	mux.HandleFunc("GET /api/auth/me", h.authed(h.Me))

	mux.HandleFunc("PUT /api/me/profile", h.authed(h.UpdateProfile))
	mux.HandleFunc("PUT /api/me/username", h.authed(h.ChangeUsername))
	mux.HandleFunc("PUT /api/me/password", h.authed(h.ChangePassword))
	mux.HandleFunc("GET /api/me/stats", h.authed(h.MyStats))
	mux.HandleFunc("GET /api/me/lots", h.authed(h.MyLots))
	mux.HandleFunc("GET /api/me/bids", h.authed(h.MyBidLots))

	mux.HandleFunc("GET /api/lots", h.ListLots)
	mux.HandleFunc("POST /api/lots", h.authed(h.CreateLot))
	mux.HandleFunc("GET /api/lots/random", h.RandomLot)
	mux.HandleFunc("GET /api/lots/{id}", h.GetLot)
	mux.HandleFunc("DELETE /api/lots/{id}", h.authed(h.CancelLot))
	mux.HandleFunc("GET /api/lots/{id}/similar", h.SimilarLots)
	mux.HandleFunc("GET /api/lots/{id}/bids", h.GetLotBids)
	mux.HandleFunc("POST /api/lots/{id}/bids", h.authed(h.PlaceBid))

	mux.HandleFunc("POST /api/uploads", h.authed(h.Upload))
	mux.HandleFunc("GET /uploads/{name}", h.ServeUpload)

	mux.HandleFunc("GET /api/categories", h.GetCategories)
	mux.HandleFunc("GET /api/dorms", h.GetDorms)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Такого метода нет")
	})
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.q.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ServerTime нужен фронтенду, чтобы считать таймеры по серверным часам, а не по часам клиента.
func (h *Handler) ServerTime(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int64{"now": time.Now().UnixMilli()})
}

func (h *Handler) GetCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.q.GetCategories(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cats)
}

func (h *Handler) GetDorms(w http.ResponseWriter, r *http.Request) {
	dorms, err := h.q.GetDorms(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dorms)
}
