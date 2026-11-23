package handler

import (
	"auction/db"
	"auction/model"
	"net/http"
	"strings"
)

// Аукционы в админ-панели: просмотр, принудительное завершение и отмена, споры.

// AdminListAuctions — аукционы по статусу: active (по умолчанию), finished, cancelled или all.
func (h *Handler) AdminListAuctions(w http.ResponseWriter, r *http.Request, _ int64) {
	qs := r.URL.Query()
	status := qs.Get("status")
	switch status {
	case "":
		status = model.StatusActive
	case model.StatusActive, model.StatusFinished, model.StatusCancelled, "all":
	default:
		writeError(w, http.StatusBadRequest, "validation", "status: active, finished, cancelled или all")
		return
	}
	sort := qs.Get("sort")
	if sort == "" && status != model.StatusActive {
		sort = "new"
	}
	f := model.LotFilter{
		Category: qs.Get("category"),
		Sub:      qs.Get("sub"),
		Q:        strings.TrimSpace(qs.Get("q")),
		Status:   status,
		Sort:     sort,
		Started:  true,
	}
	page, size, offset := adminPaging(r)
	lots, total, err := h.q.AdminListLots(r.Context(), f, size, offset)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageResponse("auctions", lots, total, page, size))
}

// AdminGetAuction — аукцион целиком: лот, ставки, автор, победитель и итоговая цена.
func (h *Handler) AdminGetAuction(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	d, err := h.q.GetAuctionDetail(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// AdminAuctionBids — история ставок аукциона (новые сверху).
func (h *Handler) AdminAuctionBids(w http.ResponseWriter, r *http.Request, _ int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	bids, err := h.q.GetLotBids(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bids": bids, "total": len(bids)})
}

// AdminFinishAuction досрочно завершает идущий аукцион; победитель — текущий лидер.
func (h *Handler) AdminFinishAuction(w http.ResponseWriter, r *http.Request, adminID int64) {
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
	if _, err := h.q.ForceFinish(r.Context(), adminID, id, reason); err != nil {
		fail(w, r, err)
		return
	}
	h.hub.LotClosed(r.Context(), id, true)
	h.respondAuction(w, r, id)
}

// AdminCancelAuction отменяет идущий аукцион; причина обязательна.
func (h *Handler) AdminCancelAuction(w http.ResponseWriter, r *http.Request, adminID int64) {
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
	if err := h.q.CancelAuction(r.Context(), adminID, id, reason); err != nil {
		fail(w, r, err)
		return
	}
	h.hub.LotClosed(r.Context(), id, false)
	h.respondAuction(w, r, id)
}

type resolveReq struct {
	Action string `json:"action"` // dismiss | remove_bid | annul
	BidID  int64  `json:"bidId"`
	Reason string `json:"reason"`
}

// AdminResolveDispute разрешает спорную ситуацию по завершённому аукциону:
// оставить результат (dismiss), удалить спорную ставку (remove_bid) или аннулировать
// результат, отменив аукцион (annul).
func (h *Handler) AdminResolveDispute(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	var req resolveReq
	if !decode(w, r, &req) {
		return
	}
	reason, ok := readReason(w, req.Reason, true)
	if !ok {
		return
	}
	switch req.Action {
	case db.DisputeDismiss, db.DisputeAnnul:
	case db.DisputeRemoveBid:
		if req.BidID < 1 {
			writeError(w, http.StatusBadRequest, "validation", "Укажите bidId — ставку, которую нужно удалить")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "validation", "action: dismiss, remove_bid или annul")
		return
	}
	res, err := h.q.ResolveDispute(r.Context(), adminID, id, req.Action, req.BidID, reason)
	if err != nil {
		fail(w, r, err)
		return
	}
	if res.Annulled {
		h.hub.LotClosed(r.Context(), id, false)
	}
	h.respondAuction(w, r, id)
}

func (h *Handler) respondAuction(w http.ResponseWriter, r *http.Request, id int64) {
	d, err := h.q.GetAuctionDetail(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func init() {
	adminRoutes = append(adminRoutes, func(mux *http.ServeMux, h *Handler) {
		a := h.adminOnly
		mux.HandleFunc("GET /api/admin/auctions", a(h.AdminListAuctions))
		mux.HandleFunc("GET /api/admin/auctions/{id}", a(h.AdminGetAuction))
		mux.HandleFunc("GET /api/admin/auctions/{id}/bids", a(h.AdminAuctionBids))
		mux.HandleFunc("POST /api/admin/auctions/{id}/finish", a(h.AdminFinishAuction))
		mux.HandleFunc("POST /api/admin/auctions/{id}/cancel", a(h.AdminCancelAuction))
		mux.HandleFunc("POST /api/admin/auctions/{id}/resolve", a(h.AdminResolveDispute))
	})
}
