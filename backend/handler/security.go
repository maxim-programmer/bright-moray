package handler

import (
	"net/http"
	"net/url"
	"strings"

	"auction/config"
)

// Guard — защита запросов браузера: CORS с credentials для доверенных Origin и проверка
// источника (CSRF) для запросов, изменяющих состояние. Ставится вокруг mux внутри Middleware.
//
// Cookie выдаются с SameSite=Lax, и Guard — вторая линия обороны, не зависящая от
// браузерных настроек cookie:
//   - Origin доверенного фронтенда (ALLOWED_ORIGINS) получает CORS-заголовки с
//     Access-Control-Allow-Credentials; «*» не используется никогда;
//   - POST/PUT/PATCH/DELETE принимаются только с того же сайта (Sec-Fetch-Site: same-origin
//     или Origin с тем же хостом, что у запроса) либо с доверенного Origin;
//   - запросы без Origin и Sec-Fetch-Site (curl, мобильные и серверные клиенты) пропускаются:
//     CSRF — атака браузера, а браузер эти заголовки на изменяющих запросах всегда шлёт.
func (h *Handler) Guard(next http.Handler) http.Handler {
	trusted := map[string]bool{}
	for _, o := range h.cfg.AllowedOrigins {
		if n, err := config.NormalizeOrigin(o); err == nil {
			trusted[n] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := false
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if n, err := config.NormalizeOrigin(origin); err == nil && trusted[n] {
				allowed = true
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}

		// Предзапрос CORS: отвечаем только доверенным источникам.
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if !allowed {
				writeError(w, http.StatusForbidden, "bad_origin", "Источник запроса не разрешён")
				return
			}
			hd := w.Header()
			hd.Add("Vary", "Access-Control-Request-Method")
			hd.Add("Vary", "Access-Control-Request-Headers")
			hd.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			hd.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			hd.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if !safeMethod(r.Method) && !allowed && !sameSite(r) {
			writeError(w, http.StatusForbidden, "bad_origin", "Источник запроса не разрешён")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// sameSite сообщает, что изменяющий запрос пришёл со страницы этого же сайта.
// Хосты сравниваются без порта — так же, как для WebSocket (см. live.Hub.checkOrigin):
// cookie не различает порты, а за nginx Host приходит без порта.
func sameSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin":
		return true
	case "":
		// заголовка нет (старый браузер, не-браузер) — решает Origin
	default: // cross-site, same-site (другой поддомен), none
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	ou, err := url.Parse(origin)
	if err != nil || ou.Hostname() == "" { // в том числе «Origin: null»
		return false
	}
	ru, err := url.Parse("//" + r.Host)
	return err == nil && strings.EqualFold(ou.Hostname(), ru.Hostname())
}
