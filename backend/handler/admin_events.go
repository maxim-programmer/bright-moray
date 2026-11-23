package handler

import (
	"auction/model"
	"net/http"
)

// Журнал событий и статистика.

// AdminListEvents — журнал всех событий системы с фильтрами по пользователю, лоту, типу и периоду.
func (h *Handler) AdminListEvents(w http.ResponseWriter, r *http.Request, _ int64) {
	f, msg := eventFilterFromQuery(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	f.UserID = int64(atoi(r.URL.Query().Get("userId")))
	f.LotID = int64(atoi(r.URL.Query().Get("lotId")))
	h.writeEvents(w, r, f)
}

// AdminEventTypes — справочник типов событий для фильтра.
func (h *Handler) AdminEventTypes(w http.ResponseWriter, r *http.Request, _ int64) {
	type item struct {
		Type  string `json:"type"`
		Label string `json:"label"`
	}
	types := make([]item, 0, len(model.EventLabels))
	for _, e := range model.EventLabels {
		types = append(types, item{e.Type, e.Label})
	}
	groups := []item{
		{"user.", "Пользователи"}, {"lot.", "Лоты"}, {"bid.", "Ставки"}, {"auction.", "Аукционы"},
		{"settings.", "Настройки"}, {"category.", "Категории"}, {"subcategory.", "Подкатегории"},
		{"dorm.", "Общежития"}, {"admin.", "Команды терминала"},
	}
	writeJSON(w, http.StatusOK, map[string]any{"types": types, "groups": groups})
}

func init() {
	adminRoutes = append(adminRoutes, func(mux *http.ServeMux, h *Handler) {
		a := h.adminOnly
		mux.HandleFunc("GET /api/admin/events", a(h.AdminListEvents))
		mux.HandleFunc("GET /api/admin/events/types", a(h.AdminEventTypes))
	})
}
