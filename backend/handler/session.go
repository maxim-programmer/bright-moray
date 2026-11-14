package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"auction/auth"
	"auction/db"
)

const (
	// Access-токен (JWT) остаётся в cookie с прежним именем и Path=/: его читает и
	// WebSocket-рукопожатие. Refresh-токен виден только маршрутам авторизации.
	sessionCookie     = "session"
	refreshCookie     = "refresh_token"
	refreshCookiePath = "/api/auth"

	// Сколько хранится история токенов после предела жизни семейства (для аудита).
	sessionHistoryRetention = 7 * 24 * time.Hour
	sessionCleanupEvery     = time.Hour
)

/* ---------- cookies ---------- */

func (h *Handler) cookie(name, value, path string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// noStore запрещает кэшировать ответы, в которых выдаются или отзываются токены.
func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

// setAuthCookies выдаёт пару cookie: свежий access-токен и переданный refresh-токен.
// refreshTTL — оставшаяся жизнь refresh-токена, и cookie живёт ровно столько же.
// Возвращает момент истечения выданного access-токена.
func (h *Handler) setAuthCookies(w http.ResponseWriter, userID int64, refreshToken string, refreshTTL time.Duration) (time.Time, error) {
	access, claims, err := h.jwt.Issue(userID)
	if err != nil {
		return time.Time{}, err
	}
	noStore(w)
	http.SetCookie(w, h.cookie(sessionCookie, access, "/", int(h.jwt.TTL().Seconds())))
	http.SetCookie(w, h.cookie(refreshCookie, refreshToken, refreshCookiePath, int(refreshTTL.Seconds())))
	return claims.ExpiresAt, nil
}

// clearAuthCookies удаляет обе cookie. Атрибуты Path и Secure должны совпадать с теми,
// с которыми cookie выдавались, иначе браузер её не удалит.
func (h *Handler) clearAuthCookies(w http.ResponseWriter) {
	noStore(w)
	http.SetCookie(w, h.cookie(sessionCookie, "", "/", -1))
	http.SetCookie(w, h.cookie(refreshCookie, "", refreshCookiePath, -1))
}

// refreshHash достаёт хеш refresh-токена из cookie запроса.
func refreshHash(r *http.Request) ([]byte, bool) {
	c, err := r.Cookie(refreshCookie)
	if err != nil {
		return nil, false
	}
	return auth.HashRefreshToken(c.Value)
}

/* ---------- создание сессии ---------- */

// startSession открывает новую сессию пользователя и выдаёт cookie.
// Если в запросе уже была сессия этого браузера, она закрывается — чтобы повторный вход
// не оставлял за собой «осиротевших» токенов.
func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	if old, ok := refreshHash(r); ok {
		if err := h.q.RevokeSessionByRefreshHash(r.Context(), old, db.RevokeRelogin); err != nil {
			log.Printf("auth: не удалось закрыть прежнюю сессию: %v", err)
		}
	}
	token, hash, err := auth.NewRefreshToken()
	if err != nil {
		return err
	}
	st, err := h.q.CreateSession(r.Context(), userID, hash, h.cfg.RefreshIdleTTL, h.cfg.SessionMaxTTL)
	if err != nil {
		return err
	}
	_, err = h.setAuthCookies(w, userID, token, st.TTL)
	return err
}

/* ---------- проверка access-токена ---------- */

// userID возвращает id авторизованного пользователя или 0.
// Access-токен берётся из cookie, а если там его нет или он негоден — из заголовка
// Authorization: Bearer (для не-браузерных клиентов). Refresh-токен здесь не годится:
// это не JWT и проверку подписи он не пройдёт. БД не используется.
func (h *Handler) userID(r *http.Request) int64 {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if cl, err := h.jwt.Verify(c.Value); err == nil {
			return cl.Subject
		}
	}
	if a := r.Header.Get("Authorization"); len(a) > 7 && strings.EqualFold(a[:7], "Bearer ") {
		if cl, err := h.jwt.Verify(strings.TrimSpace(a[7:])); err == nil {
			return cl.Subject
		}
	}
	return 0
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

/* ---------- /auth/refresh, /auth/logout, /auth/logout-all ---------- */

// Refresh обменивает действующий refresh-токен (cookie) на новую пару токенов.
// Старый refresh-токен при этом перестаёт действовать (ротация).
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	old, ok := refreshHash(r)
	if !ok {
		h.clearAuthCookies(w)
		writeError(w, http.StatusUnauthorized, "invalid_refresh", "Сессия недействительна, войдите заново")
		return
	}
	token, hash, err := auth.NewRefreshToken()
	if err != nil {
		fail(w, err)
		return
	}

	st, err := h.q.RotateRefreshToken(r.Context(), old, hash, h.cfg.RefreshIdleTTL, h.cfg.RefreshReuseGap)
	switch {
	case errors.Is(err, db.ErrRefreshRace):
		// Параллельный запрос (другая вкладка) только что обновил этот же токен и уже
		// выдал свежие cookie. Ничего не трогаем: клиент повторит запрос с новыми cookie.
		writeError(w, http.StatusConflict, "refresh_conflict", "Сессия только что обновлена, повторите запрос")
		return
	case errors.Is(err, db.ErrRefreshReused):
		log.Printf("auth: повторное использование refresh-токена, сессии отозваны user=%d", st.UserID)
		h.clearAuthCookies(w)
		writeError(w, http.StatusUnauthorized, "refresh_reused", "Сессия завершена из соображений безопасности, войдите заново")
		return
	case errors.Is(err, db.ErrRefreshInvalid):
		h.clearAuthCookies(w)
		writeError(w, http.StatusUnauthorized, "invalid_refresh", "Сессия недействительна, войдите заново")
		return
	case err != nil:
		fail(w, err)
		return
	}

	accessExp, err := h.setAuthCookies(w, st.UserID, token, st.TTL)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"accessExpiresAt": accessExp.UnixMilli(),
	})
}

// Logout завершает текущую сессию (всё семейство её токенов) и стирает cookie.
// Нужен только refresh-cookie: выйти можно и с просроченным access-токеном.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if old, ok := refreshHash(r); ok {
		if err := h.q.RevokeSessionByRefreshHash(r.Context(), old, db.RevokeLogout); err != nil {
			fail(w, err) // cookie не стираем: токен ещё жив, пусть клиент повторит выход
			return
		}
	}
	h.clearAuthCookies(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LogoutAll завершает все сессии пользователя на всех устройствах.
// Пользователь определяется по access-токену, а при его истечении — по действующему
// refresh-токену: оба доказывают владение аккаунтом.
func (h *Handler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	uid := h.userID(r)
	if uid == 0 {
		if old, ok := refreshHash(r); ok {
			var err error
			if uid, err = h.q.UserIDByActiveRefreshHash(r.Context(), old); err != nil && !errors.Is(err, db.ErrNotFound) {
				fail(w, err)
				return
			}
		}
	}
	if uid == 0 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Войдите в аккаунт")
		return
	}
	if _, err := h.q.RevokeUserSessions(r.Context(), uid, db.RevokeLogoutAll); err != nil {
		fail(w, err)
		return
	}
	h.clearAuthCookies(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

/* ---------- очистка истории ---------- */

// StartSessionCleanup раз в час удаляет историю токенов давно истёкших сессий.
// Работает до отмены ctx. Вызывать один раз при старте.
func (h *Handler) StartSessionCleanup(ctx context.Context) {
	go func() {
		t := time.NewTicker(sessionCleanupEvery)
		defer t.Stop()
		for {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			n, err := h.q.DeleteExpiredSessions(cctx, sessionHistoryRetention)
			cancel()
			switch {
			case err != nil && ctx.Err() == nil:
				log.Printf("auth: очистка сессий: %v", err)
			case n > 0:
				log.Printf("auth: удалено токенов истёкших сессий: %d", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
