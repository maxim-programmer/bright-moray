package handler

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"auction/db"
)

var (
	usernameRe = regexp.MustCompile(`^[\p{L}\p{N}_.-]{3,32}$`)
	telegramRe = regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`)
)

func mustHash(p string) []byte {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return b
}

func normTelegram(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "@")
}

func validUsername(s string) string {
	if !usernameRe.MatchString(s) {
		return "Никнейм: 3–32 символа, буквы, цифры, «_», «.» или «-»"
	}
	return ""
}

func validTelegram(s string) string {
	if !telegramRe.MatchString(s) {
		return "Telegram: 5–32 символа, латинские буквы, цифры и «_»"
	}
	return ""
}

func validName(s string) string {
	if n := utf8.RuneCountInString(s); n < 1 || n > 100 {
		return "Имя: от 1 до 100 символов"
	}
	return ""
}

// bcrypt учитывает только первые 72 байта — длиннее не принимаем.
func validPassword(s string) string {
	if len(s) < 6 {
		return "Пароль должен быть не короче 6 символов"
	}
	if len(s) > 72 {
		return "Пароль не должен быть длиннее 72 байт"
	}
	return ""
}

type registerReq struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Telegram string `json:"telegram"`
	DormID   int16  `json:"dormId"`
	Password string `json:"password"`
	Confirm  string `json:"confirm"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Username = strings.TrimSpace(req.Username)
	req.Telegram = normTelegram(req.Telegram)

	if req.Name == "" || req.Username == "" || req.Telegram == "" || req.Password == "" || req.DormID == 0 {
		writeError(w, http.StatusBadRequest, "validation", "Заполните все поля")
		return
	}
	for _, msg := range []string{validName(req.Name), validUsername(req.Username), validTelegram(req.Telegram), validPassword(req.Password)} {
		if msg != "" {
			writeError(w, http.StatusBadRequest, "validation", msg)
			return
		}
	}
	if req.Confirm != "" && req.Confirm != req.Password {
		writeError(w, http.StatusBadRequest, "validation", "Пароли не совпадают")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, err)
		return
	}
	user, err := h.q.CreateUser(r.Context(), req.Username, string(hash), req.Name, req.Telegram, req.DormID)
	switch {
	case errors.Is(err, db.ErrConflict):
		if _, e := h.q.GetUserByUsername(r.Context(), req.Username); e == nil {
			writeError(w, http.StatusConflict, "username_taken", "Такой никнейм уже занят")
		} else {
			writeError(w, http.StatusConflict, "telegram_taken", "Этот Telegram уже используется")
		}
		return
	case errors.Is(err, db.ErrBadRef):
		writeError(w, http.StatusBadRequest, "validation", "Выберите общежитие из списка")
		return
	case err != nil:
		fail(w, err)
		return
	}
	h.setSession(w, user.ID)
	writeJSON(w, http.StatusCreated, user)
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decode(w, r, &req) {
		return
	}
	ip := clientIP(r)
	if !h.loginRate.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Слишком много попыток входа, попробуйте позже")
		return
	}

	user, err := h.q.GetUserByUsername(r.Context(), strings.TrimSpace(req.Username))
	hash := h.dummyHash
	if err == nil {
		hash = []byte(user.Password)
	}
	okPass := bcrypt.CompareHashAndPassword(hash, []byte(req.Password)) == nil
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		fail(w, err)
		return
	}
	if err != nil || !okPass {
		writeError(w, http.StatusUnauthorized, "bad_credentials", "Неверный никнейм или пароль")
		return
	}

	h.loginRate.reset(ip)
	h.setSession(w, user.ID)
	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.clearSession(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request, uid int64) {
	user, err := h.q.GetUserByID(r.Context(), uid)
	if errors.Is(err, db.ErrNotFound) { // пользователь удалён, а токен ещё жив
		h.clearSession(w)
		writeError(w, http.StatusUnauthorized, "unauthorized", "Войдите в аккаунт")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

type profileReq struct {
	Name     string `json:"name"`
	Telegram string `json:"telegram"`
	DormID   int16  `json:"dormId"`
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request, uid int64) {
	var req profileReq
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Telegram = normTelegram(req.Telegram)
	if req.Name == "" || req.Telegram == "" || req.DormID == 0 {
		writeError(w, http.StatusBadRequest, "validation", "Заполните все поля")
		return
	}
	for _, msg := range []string{validName(req.Name), validTelegram(req.Telegram)} {
		if msg != "" {
			writeError(w, http.StatusBadRequest, "validation", msg)
			return
		}
	}
	user, err := h.q.UpdateProfile(r.Context(), uid, req.Name, req.Telegram, req.DormID)
	switch {
	case errors.Is(err, db.ErrConflict):
		writeError(w, http.StatusConflict, "telegram_taken", "Этот Telegram уже используется")
	case errors.Is(err, db.ErrBadRef):
		writeError(w, http.StatusBadRequest, "validation", "Выберите общежитие из списка")
	case err != nil:
		fail(w, err)
	default:
		writeJSON(w, http.StatusOK, user)
	}
}

type usernameReq struct {
	NewUsername string `json:"newUsername"`
}

func (h *Handler) ChangeUsername(w http.ResponseWriter, r *http.Request, uid int64) {
	var req usernameReq
	if !decode(w, r, &req) {
		return
	}
	req.NewUsername = strings.TrimSpace(req.NewUsername)
	if req.NewUsername == "" {
		writeError(w, http.StatusBadRequest, "validation", "Введите новый никнейм")
		return
	}
	if msg := validUsername(req.NewUsername); msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	user, err := h.q.ChangeUsername(r.Context(), uid, req.NewUsername)
	if errors.Is(err, db.ErrConflict) {
		writeError(w, http.StatusConflict, "username_taken", "Такой никнейм уже занят")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

type passwordReq struct {
	Current     string `json:"current"`
	NewPassword string `json:"newPassword"`
	Confirm     string `json:"confirm"`
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request, uid int64) {
	var req passwordReq
	if !decode(w, r, &req) {
		return
	}
	if req.Current == "" || req.NewPassword == "" || req.Confirm == "" {
		writeError(w, http.StatusBadRequest, "validation", "Заполните все поля")
		return
	}
	user, err := h.q.GetUserByID(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Current)) != nil {
		writeError(w, http.StatusBadRequest, "wrong_password", "Текущий пароль указан неверно")
		return
	}
	if msg := validPassword(req.NewPassword); msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	if req.NewPassword != req.Confirm {
		writeError(w, http.StatusBadRequest, "validation", "Пароли не совпадают")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		fail(w, err)
		return
	}
	if err := h.q.ChangePassword(r.Context(), uid, string(hash)); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
