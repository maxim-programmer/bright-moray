package handler

import (
	"bright-moray/db"
	"bright-moray/model"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	q *db.Queries
}

func New(q *db.Queries) *Handler {
	return &Handler{q: q}
}

func sendJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func sendError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req model.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.Username == "" || req.Telegram == "" || req.Password == "" {
		sendError(w, http.StatusBadRequest, "all fields are required")
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	user, err := h.q.CreateUser(req.Username, string(hashedPassword), req.Name, req.Telegram, req.DormID)
	if err != nil {
		sendError(w, http.StatusConflict, "username or telegram already exists")
		return
	}

	sendJSON(w, http.StatusCreated, user)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.q.LoginByUsername(req.Username)
	if err == nil && bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) == nil {
		sendJSON(w, http.StatusOK, model.User{
			ID: user.ID, Username: user.Username, Name: user.Name,
			Telegram: user.Telegram, DormID: user.DormID, CreatedAt: user.CreatedAt,
		})
		return
	}

	sendError(w, http.StatusUnauthorized, "invalid credentials")
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	id := extractID(r, "id")
	user, err := h.q.GetUserByID(id)
	if err != nil {
		sendError(w, http.StatusNotFound, "user not found")
		return
	}
	sendJSON(w, http.StatusOK, user)
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	id := extractID(r, "id")
	var req model.UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.q.UpdateProfile(id, req.Name, req.Telegram)
	if err != nil {
		sendError(w, http.StatusNotFound, "user not found")
		return
	}
	sendJSON(w, http.StatusOK, user)
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	id := extractID(r, "id")
	var req model.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	if err := h.q.ChangePassword(id, string(hashedPassword)); err != nil {
		sendError(w, http.StatusInternalServerError, "failed to change password")
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"message": "password changed"})
}

func (h *Handler) ChangeUsername(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	id := extractID(r, "id")
	var req model.ChangeUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.q.ChangeUsername(id, req.NewUsername)
	if err != nil {
		sendError(w, http.StatusConflict, "username already taken")
		return
	}
	sendJSON(w, http.StatusOK, user)
}

func (h *Handler) CreateLot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req model.CreateLotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	authorID := req.AuthorID
	if authorID == 0 {
		authorID = extractID(r, "author_id")
	}
	if authorID == 0 {
		sendError(w, http.StatusBadRequest, "author_id is required")
		return
	}

	lot, err := h.q.CreateLot(req, authorID)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to create lot")
		return
	}
	sendJSON(w, http.StatusCreated, lot)
}

func (h *Handler) GetLot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	id := extractID(r, "id")
	lot, err := h.q.GetLotByID(id)
	if err != nil {
		sendError(w, http.StatusNotFound, "lot not found")
		return
	}
	sendJSON(w, http.StatusOK, lot)
}

func (h *Handler) ListLots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := 20
	offset := (page - 1) * pageSize

	lots, err := h.q.ListLots(status, pageSize, offset)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to list lots")
		return
	}

	total, _ := h.q.CountLots(status)
	pages := (total + pageSize - 1) / pageSize

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"lots":      lots,
		"page":      page,
		"totalPages": pages,
	})
}

func (h *Handler) CancelLot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	lotID := extractID(r, "id")
	authorID := extractID(r, "author_id")

	if err := h.q.CancelLot(lotID, authorID); err != nil {
		sendError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) GetMyLots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	authorID := extractID(r, "author_id")
	lots, err := h.q.GetMyLots(authorID)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to get lots")
		return
	}
	sendJSON(w, http.StatusOK, lots)
}

func (h *Handler) GetMyBidLots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := extractID(r, "user_id")
	lots, err := h.q.GetMyBidLots(userID)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to get lots")
		return
	}
	sendJSON(w, http.StatusOK, lots)
}

func (h *Handler) GetLotBids(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	lotID := extractID(r, "lot_id")
	bids, err := h.q.GetLotBids(lotID)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to get bids")
		return
	}
	sendJSON(w, http.StatusOK, bids)
}

func (h *Handler) PlaceBid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req model.PlaceBidRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	lotID := req.LotID
	userID := req.UserID
	if lotID == 0 || userID == 0 {
		lotID = extractID(r, "lot_id")
		userID = extractID(r, "user_id")
	}
	if lotID == 0 || userID == 0 {
		sendError(w, http.StatusBadRequest, "lot_id and user_id are required")
		return
	}

	bid, err := h.q.PlaceBid(lotID, userID, req.Amount)
	if err != nil {
		sendError(w, http.StatusBadRequest, "failed to place bid")
		return
	}
	sendJSON(w, http.StatusCreated, bid)
}

func (h *Handler) GetCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cats, err := h.q.GetCategories()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to get categories")
		return
	}
	sendJSON(w, http.StatusOK, cats)
}

func (h *Handler) GetDorms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	dorms, err := h.q.GetDorms()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to get dorms")
		return
	}
	sendJSON(w, http.StatusOK, dorms)
}

func (h *Handler) GetUserStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := extractID(r, "user_id")
	stats, err := h.q.GetUserStats(userID)
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}
	sendJSON(w, http.StatusOK, stats)
}

func extractID(r *http.Request, param string) int64 {
	idStr := r.URL.Query().Get(param)
	if idStr == "" {
		return 0
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func SetupRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("/health", h.Health)

	mux.HandleFunc("/api/auth/register", h.Register)
	mux.HandleFunc("/api/auth/login", h.Login)

	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.GetProfile(w, r)
		case http.MethodPut:
			h.UpdateProfile(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/users/change-password", h.ChangePassword)
	mux.HandleFunc("/api/users/change-username", h.ChangeUsername)

	mux.HandleFunc("/api/lots", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListLots(w, r)
		case http.MethodPost:
			h.CreateLot(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/lots/my", h.GetMyLots)
	mux.HandleFunc("/api/lots/my-bids", h.GetMyBidLots)

	mux.HandleFunc("/api/lots/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/lots/")
		q := r.URL.Query()
		q.Set("id", idStr)
		r.URL.RawQuery = q.Encode()
		switch r.Method {
		case http.MethodGet:
			h.GetLot(w, r)
		case http.MethodDelete:
			h.CancelLot(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/lots/bids", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.GetLotBids(w, r)
		case http.MethodPost:
			h.PlaceBid(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/lots/bids/", func(w http.ResponseWriter, r *http.Request) {
		lotID := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/lots/bids/"), "/")[0]
		r.URL.Query().Set("lot_id", lotID)
		switch r.Method {
		case http.MethodGet:
			h.GetLotBids(w, r)
		case http.MethodPost:
			h.PlaceBid(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/categories", h.GetCategories)
	mux.HandleFunc("/api/dorms", h.GetDorms)
	mux.HandleFunc("/api/stats", h.GetUserStats)
}
