package handler

import (
	"auction/model"
	"net/http"
	"strings"
)

// Пользователи в админ-панели: список, поиск, карточка, история, блокировка.

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request, _ int64) {
	qs := r.URL.Query()
	f := model.UserFilter{
		Q:       strings.TrimSpace(qs.Get("q")),
		Role:    qs.Get("role"),
		Blocked: qs.Get("blocked"),
	}
	switch f.Role {
	case "", model.RoleUser, model.RoleAdmin:
	default:
		writeError(w, http.StatusBadRequest, "validation", "role: user или admin")
		return
	}
	switch f.Blocked {
	case "", "true", "false":
	default:
		writeError(w, http.StatusBadRequest, "validation", "blocked: true или false")
		return
	}
	page, size, offset := adminPaging(r)
	users, total, err := h.q.ListUsers(r.Context(), f, size, offset)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageResponse("users", users, total, page, size))
}

// AdminGetUser — подробная информация об отдельном пользователе и его статистика за всё время.
func (h *Handler) AdminGetUser(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	u, err := h.q.GetAdminUser(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	stats, err := h.q.GetUserPeriodStats(r.Context(), id, model.StatsFilter{})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "stats": stats})
}

// AdminUserActivity — история активности пользователя (его действия и действия над ним).
func (h *Handler) AdminUserActivity(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	if _, err := h.q.GetAdminUser(r.Context(), id); err != nil {
		fail(w, r, err)
		return
	}
	f, msg := eventFilterFromQuery(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	f.UserID = id
	h.writeEvents(w, r, f)
}

// AdminUserLots — все лоты пользователя, любых статусов.
func (h *Handler) AdminUserLots(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	if _, err := h.q.GetAdminUser(r.Context(), id); err != nil {
		fail(w, r, err)
		return
	}
	status := r.URL.Query().Get("status")
	if !validLotStatusFilter(status) {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный status")
		return
	}
	page, size, offset := adminPaging(r)
	f := model.LotFilter{AuthorID: id, Status: status, Sort: "new"}
	lots, total, err := h.q.AdminListLots(r.Context(), f, size, offset)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageResponse("lots", lots, total, page, size))
}

// AdminUserBids — история ставок пользователя.
func (h *Handler) AdminUserBids(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	page, size, offset := adminPaging(r)
	bids, total, err := h.q.UserBids(r.Context(), id, size, offset)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageResponse("bids", bids, total, page, size))
}

func (h *Handler) AdminBlockUser(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	var req reasonReq
	if !decodeOptional(w, r, &req) {
		return
	}
	reason, ok := readReason(w, req.Reason, false)
	if !ok {
		return
	}
	if id == adminID {
		writeError(w, http.StatusConflict, "forbidden_target", "Нельзя заблокировать самого себя")
		return
	}
	if err := h.q.BlockUser(r.Context(), adminID, id, reason); err != nil {
		fail(w, r, err)
		return
	}
	h.respondUser(w, r, id)
}

func (h *Handler) AdminUnblockUser(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	if err := h.q.UnblockUser(r.Context(), adminID, id); err != nil {
		fail(w, r, err)
		return
	}
	h.respondUser(w, r, id)
}

func (h *Handler) respondUser(w http.ResponseWriter, r *http.Request, id int64) {
	u, err := h.q.GetAdminUser(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func init() {
	adminRoutes = append(adminRoutes, func(mux *http.ServeMux, h *Handler) {
		a := h.adminOnly
		mux.HandleFunc("GET /api/admin/users", a(h.AdminListUsers))
		mux.HandleFunc("GET /api/admin/users/{id}", a(h.AdminGetUser))
		mux.HandleFunc("GET /api/admin/users/{id}/activity", a(h.AdminUserActivity))
		mux.HandleFunc("GET /api/admin/users/{id}/lots", a(h.AdminUserLots))
		mux.HandleFunc("GET /api/admin/users/{id}/bids", a(h.AdminUserBids))
		mux.HandleFunc("POST /api/admin/users/{id}/block", a(h.AdminBlockUser))
		mux.HandleFunc("POST /api/admin/users/{id}/unblock", a(h.AdminUnblockUser))
	})
}
