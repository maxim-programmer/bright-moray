package handler

import (
	"auction/db"
	"auction/model"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Модерация лотов: очередь заявок, одобрение и отказ, правка, снятие с публикации, удаление.

func (h *Handler) AdminListLots(w http.ResponseWriter, r *http.Request, _ int64) {
	qs := r.URL.Query()
	status := qs.Get("status")
	if !validLotStatusFilter(status) {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный status")
		return
	}
	sort := qs.Get("sort")
	if sort == "" {
		sort = "new"
		if status == model.StatusPending {
			sort = "old"
		}
	}
	f := model.LotFilter{
		Category: qs.Get("category"),
		Sub:      qs.Get("sub"),
		Q:        strings.TrimSpace(qs.Get("q")),
		Status:   status,
		Sort:     sort,
		AuthorID: int64(atoi(qs.Get("authorId"))),
	}
	page, size, offset := adminPaging(r)
	lots, total, err := h.q.AdminListLots(r.Context(), f, size, offset)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageResponse("lots", lots, total, page, size))
}

func (h *Handler) AdminGetLot(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	lot, err := h.q.AdminGetLot(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	author, err := h.q.GetAdminUser(r.Context(), lot.AuthorID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lot": lot, "author": author})
}

// AdminLotHistory — история изменений лота: всё, что с ним делали (без ставок).
func (h *Handler) AdminLotHistory(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	// лота может уже не быть (удалён), а история в журнале остаётся — поэтому 404 не отдаём
	page, size, offset := adminPaging(r)
	events, total, err := h.q.ListEvents(r.Context(),
		model.EventFilter{LotID: id, ExcludeTypes: []string{model.EvBidPlaced}}, size, offset)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageResponse("events", events, total, page, size))
}

type approveReq struct {
	// StartsAt — когда начать аукцион (мс Unix). Не указано или уже прошло — начать сразу.
	StartsAt *int64 `json:"startsAt"`
}

func (h *Handler) AdminApproveLot(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	var req approveReq
	if !decodeOptional(w, r, &req) {
		return
	}
	var startAt time.Time
	if req.StartsAt != nil && *req.StartsAt > 0 {
		startAt = time.UnixMilli(*req.StartsAt)
		if startAt.After(time.Now().AddDate(1, 0, 0)) {
			writeError(w, http.StatusBadRequest, "validation", "Начало аукциона — не позже чем через год")
			return
		}
	}
	res, err := h.q.ApproveLot(r.Context(), adminID, id, startAt)
	if err != nil {
		fail(w, r, err)
		return
	}
	if res.Started {
		h.hub.LotCreated(id, res.EndsAt) // таймер завершения аукциона
	}
	h.respondAdminLot(w, r, id)
}

func (h *Handler) AdminRejectLot(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	var req reasonReq
	if !decode(w, r, &req) {
		return
	}
	reason, ok := readReason(w, req.Reason, true)
	if !ok {
		return
	}
	if err := h.q.RejectLot(r.Context(), adminID, id, reason); err != nil {
		fail(w, r, err)
		return
	}
	h.respondAdminLot(w, r, id)
}

func (h *Handler) AdminUnpublishLot(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	var req reasonReq
	if !decode(w, r, &req) {
		return
	}
	reason, ok := readReason(w, req.Reason, true)
	if !ok {
		return
	}
	res, err := h.q.UnpublishLot(r.Context(), adminID, id, reason)
	if err != nil {
		fail(w, r, err)
		return
	}
	if res.WasActive {
		h.hub.LotClosed(r.Context(), id, false)
	}
	h.respondAdminLot(w, r, id)
}

func (h *Handler) AdminDeleteLot(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	var req reasonReq
	if !decode(w, r, &req) {
		return
	}
	reason, ok := readReason(w, req.Reason, true)
	if !ok {
		return
	}
	if _, err := h.q.DeleteLot(r.Context(), adminID, id, reason); err != nil {
		fail(w, r, err)
		return
	}
	h.hub.LotRemoved(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type editLotReq struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Image       *string  `json:"image"`
	Category    *string  `json:"category"`
	Subcategory *string  `json:"subcategory"`
	StartPrice  *int     `json:"startPrice"`
	Step        *int     `json:"step"`
	Hours       *float64 `json:"hours"`
}

// AdminEditLot правит лот. Присланные поля проверяются по тем же правилам, что и при создании.
func (h *Handler) AdminEditLot(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	var req editLotReq
	if !decode(w, r, &req) {
		return
	}
	bad := func(msg string) { writeError(w, http.StatusBadRequest, "validation", msg) }

	e := db.LotEdit{
		Title: req.Title, Description: req.Description, Image: req.Image,
		Category: req.Category, Subcategory: req.Subcategory,
		StartPrice: req.StartPrice, Step: req.Step,
	}
	if e.Title != nil {
		t := strings.TrimSpace(*e.Title)
		e.Title = &t
		if t == "" || utf8.RuneCountInString(t) > 300 {
			bad("Название: от 1 до 300 символов")
			return
		}
	}
	if e.Description != nil {
		d := strings.TrimSpace(*e.Description)
		e.Description = &d
		if d == "" || utf8.RuneCountInString(d) > 5000 {
			bad("Описание: от 1 до 5000 символов")
			return
		}
	}
	if e.Image != nil && !imageRe.MatchString(*e.Image) {
		bad("Картинка должна быть загружена через /api/uploads")
		return
	}
	if (e.Category != nil) != (e.Subcategory != nil) && e.Category != nil {
		bad("Укажите подкатегорию вместе с категорией")
		return
	}
	if e.StartPrice != nil && (*e.StartPrice < 1 || *e.StartPrice > maxPrice) {
		bad("Стартовая цена: от 1 до 100 000 000")
		return
	}
	settings, err := h.settings(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	if e.Step != nil && (*e.Step < settings.MinBidStep || *e.Step > maxPrice) {
		bad(stepRangeMessage(settings.MinBidStep))
		return
	}
	if req.Hours != nil {
		if !durationAllowed(*req.Hours, settings) {
			bad(durationRangeMessage(settings))
			return
		}
		sec := int64(*req.Hours*3600 + 0.5)
		if sec < 1 {
			sec = 1
		}
		e.DurationSec = &sec
	}

	lot, changed, err := h.q.EditLot(r.Context(), adminID, id, e)
	switch {
	case errors.Is(err, db.ErrBadRef):
		bad("Подкатегория не принадлежит выбранной категории")
		return
	case err != nil:
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lot": lot, "changed": changed})
}

func (h *Handler) respondAdminLot(w http.ResponseWriter, r *http.Request, id int64) {
	lot, err := h.q.AdminGetLot(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, lot)
}

func init() {
	adminRoutes = append(adminRoutes, func(mux *http.ServeMux, h *Handler) {
		a := h.adminOnly
		mux.HandleFunc("GET /api/admin/lots", a(h.AdminListLots))
		mux.HandleFunc("GET /api/admin/lots/{id}", a(h.AdminGetLot))
		mux.HandleFunc("PUT /api/admin/lots/{id}", a(h.AdminEditLot))
		mux.HandleFunc("DELETE /api/admin/lots/{id}", a(h.AdminDeleteLot))
		mux.HandleFunc("GET /api/admin/lots/{id}/history", a(h.AdminLotHistory))
		mux.HandleFunc("POST /api/admin/lots/{id}/approve", a(h.AdminApproveLot))
		mux.HandleFunc("POST /api/admin/lots/{id}/reject", a(h.AdminRejectLot))
		mux.HandleFunc("POST /api/admin/lots/{id}/unpublish", a(h.AdminUnpublishLot))
	})
}
