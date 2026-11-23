package handler

import (
	"auction/model"
	"net/http"
)

// Статистика и аналитика.

// AdminStats — общая статистика за период (from, to; без них — за всё время).
func (h *Handler) AdminStats(w http.ResponseWriter, r *http.Request, _ int64) {
	from, to, msg := periodFromQuery(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	s, err := h.q.GetOverallStats(r.Context(), model.StatsFilter{From: from, To: to})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// AdminUserStats — статистика конкретного пользователя за период.
func (h *Handler) AdminUserStats(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	from, to, msg := periodFromQuery(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	s, err := h.q.GetUserPeriodStats(r.Context(), id, model.StatsFilter{From: from, To: to})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func init() {
	adminRoutes = append(adminRoutes, func(mux *http.ServeMux, h *Handler) {
		a := h.adminOnly
		mux.HandleFunc("GET /api/admin/stats", a(h.AdminStats))
		mux.HandleFunc("GET /api/admin/stats/users/{id}", a(h.AdminUserStats))
	})
}
