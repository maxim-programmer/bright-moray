package handler

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"auction/db"
	"auction/model"
)

const (
	maxPrice = 100_000_000
	maxHours = 24 * 30
	minHours = 1.0 / 60
)

var imageRe = regexp.MustCompile(`^/uploads/[a-f0-9]{32}\.(jpg|png|gif|webp)$`)

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func (h *Handler) ListLots(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	f := model.LotFilter{
		Category: qs.Get("category"),
		Sub:      qs.Get("sub"),
		Q:        qs.Get("q"),
		Status:   qs.Get("status"),
		Sort:     qs.Get("sort"),
		PriceMin: atoi(qs.Get("priceMin")),
		PriceMax: atoi(qs.Get("priceMax")),
		DormID:   atoi(qs.Get("dormId")),
	}
	switch f.Status {
	case "":
		f.Status = "active"
	case "active", "finished", "all":
	default:
		writeError(w, http.StatusBadRequest, "validation", "status: active, finished или all")
		return
	}

	total, err := h.q.CountLots(r.Context(), f)
	if err != nil {
		fail(w, err)
		return
	}
	pages := (total + db.PageSize - 1) / db.PageSize
	if pages < 1 {
		pages = 1
	}
	page := atoi(qs.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	lots, err := h.q.ListLots(r.Context(), h.userID(r), f, db.PageSize, (page-1)*db.PageSize)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lots": lots, "page": page, "totalPages": pages, "total": total,
	})
}

type createLotReq struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Image       string  `json:"image"` // url из POST /api/uploads
	Category    string  `json:"category"`
	Subcategory string  `json:"subcategory"`
	StartPrice  int     `json:"startPrice"`
	Step        int     `json:"step"`
	Hours       float64 `json:"hours"`
}

func (h *Handler) CreateLot(w http.ResponseWriter, r *http.Request, uid int64) {
	var req createLotReq
	if !decode(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)

	bad := func(msg string) { writeError(w, http.StatusBadRequest, "validation", msg) }
	switch {
	case req.Title == "" || req.Description == "" || req.Image == "" ||
		req.Category == "" || req.Subcategory == "":
		bad("Заполните все поля")
		return
	case utf8.RuneCountInString(req.Title) > 300:
		bad("Название не длиннее 300 символов")
		return
	case utf8.RuneCountInString(req.Description) > 5000:
		bad("Описание не длиннее 5000 символов")
		return
	case !imageRe.MatchString(req.Image):
		bad("Сначала загрузите картинку через /api/uploads")
		return
	case req.StartPrice < 1 || req.StartPrice > maxPrice:
		bad("Стартовая цена: от 1 до 100 000 000")
		return
	case req.Step < 1 || req.Step > maxPrice:
		bad("Шаг ставки: от 1 до 100 000 000")
		return
	case req.Hours < minHours || req.Hours > maxHours:
		bad("Длительность аукциона: от 1 минуты до 30 дней")
		return
	}
	ok, err := h.q.SubcategoryExists(r.Context(), req.Category, req.Subcategory)
	if err != nil {
		fail(w, err)
		return
	}
	if !ok {
		bad("Подкатегория не принадлежит выбранной категории")
		return
	}

	lot, err := h.q.CreateLot(r.Context(), uid, db.NewLot{
		Title: req.Title, Description: req.Description, Image: req.Image,
		Category: req.Category, Subcategory: req.Subcategory,
		StartPrice: req.StartPrice, Step: req.Step, Hours: req.Hours,
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, lot)
}

func (h *Handler) GetLot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
		return
	}
	lot, err := h.q.GetLot(r.Context(), h.userID(r), id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lot)
}

func (h *Handler) CancelLot(w http.ResponseWriter, r *http.Request, uid int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
		return
	}
	if err := h.q.CancelLot(r.Context(), id, uid); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
			return
		}
		fail(w, err)
		return
	}
	lot, err := h.q.GetLot(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lot)
}

func (h *Handler) SimilarLots(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
		return
	}
	lots, err := h.q.SimilarLots(r.Context(), h.userID(r), id, 4)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lots)
}

func (h *Handler) RandomLot(w http.ResponseWriter, r *http.Request) {
	id, err := h.q.RandomLotID(r.Context(), r.URL.Query().Get("category"), r.URL.Query().Get("sub"))
	if err != nil {
		fail(w, err)
		return
	}
	if id == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"id": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

/* ---------- мои лоты и ставки ---------- */

func (h *Handler) MyLots(w http.ResponseWriter, r *http.Request, uid int64) {
	lots, err := h.q.ListLots(r.Context(), uid, model.LotFilter{Status: "all", AuthorID: uid}, 0, 0)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lots)
}

func (h *Handler) MyBidLots(w http.ResponseWriter, r *http.Request, uid int64) {
	lots, err := h.q.ListLots(r.Context(), uid, model.LotFilter{Status: "all", BidderID: uid}, 0, 0)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lots)
}

func (h *Handler) MyStats(w http.ResponseWriter, r *http.Request, uid int64) {
	stats, err := h.q.GetUserStats(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

/* ---------- ставки ---------- */

func (h *Handler) GetLotBids(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
		return
	}
	bids, err := h.q.GetLotBids(r.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bids)
}

type placeBidReq struct {
	Amount int `json:"amount"`
}

func (h *Handler) PlaceBid(w http.ResponseWriter, r *http.Request, uid int64) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
		return
	}
	var req placeBidReq
	if !decode(w, r, &req) {
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "validation", "Укажите сумму ставки")
		return
	}
	bid, err := h.q.PlaceBid(r.Context(), id, uid, req.Amount)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Лот не найден")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	lot, err := h.q.GetLot(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bid": bid, "lot": lot})
}
