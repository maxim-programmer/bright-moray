package handler

import (
	"auction/db"
	"auction/model"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Системные настройки и справочники (категории, подкатегории, общежития).

func (h *Handler) AdminGetSettings(w http.ResponseWriter, r *http.Request, _ int64) {
	s, err := h.settings(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s, "defaults": h.defaultSettings()})
}

type settingsReq struct {
	MinBidStep             *int `json:"minBidStep"`
	MinDurationMinutes     *int `json:"minDurationMinutes"`
	MaxDurationMinutes     *int `json:"maxDurationMinutes"`
	DefaultDurationMinutes *int `json:"defaultDurationMinutes"`
	AntiSnipeWindowSec     *int `json:"antiSnipeWindowSec"`
	AntiSnipeExtendSec     *int `json:"antiSnipeExtendSec"`
}

// AdminUpdateSettings меняет присланные настройки; остальные остаются прежними.
// Изменения действуют на новые лоты и ставки, уже идущие аукционы не пересчитываются
// (правила продления применяются к следующей ставке).
func (h *Handler) AdminUpdateSettings(w http.ResponseWriter, r *http.Request, adminID int64) {
	var req settingsReq
	if !decode(w, r, &req) {
		return
	}
	cur, err := h.settings(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	next := cur
	changes := map[string]any{}
	apply := func(name string, dst *int, v *int) {
		if v != nil && *v != *dst {
			changes[name] = map[string]any{"from": *dst, "to": *v}
			*dst = *v
		}
	}
	apply("minBidStep", &next.MinBidStep, req.MinBidStep)
	apply("minDurationMinutes", &next.MinDurationMinutes, req.MinDurationMinutes)
	apply("maxDurationMinutes", &next.MaxDurationMinutes, req.MaxDurationMinutes)
	apply("defaultDurationMinutes", &next.DefaultDurationMinutes, req.DefaultDurationMinutes)
	apply("antiSnipeWindowSec", &next.AntiSnipeWindowSec, req.AntiSnipeWindowSec)
	apply("antiSnipeExtendSec", &next.AntiSnipeExtendSec, req.AntiSnipeExtendSec)
	if msg := next.Validate(); msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	if len(changes) > 0 {
		if err := h.q.SaveSettings(r.Context(), adminID, next, map[string]any{"changes": changes}); err != nil {
			fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": next, "defaults": h.defaultSettings()})
}

/* ---------- категории ---------- */

type categoryReq struct {
	ID          string `json:"id"` // необязателен: по умолчанию строится из названия
	Name        string `json:"name"`
	Description string `json:"description"`
}

// cleanName проверяет название справочника.
func cleanName(w http.ResponseWriter, name string, max int) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > max {
		writeError(w, http.StatusBadRequest, "validation", fmt.Sprintf("Название: от 1 до %d символов", max))
		return "", false
	}
	return name, true
}

// uniqueID подбирает свободный id: base, base-2, base-3, … Если администратор задал id сам,
// подмена невозможна — занятый id это ошибка (explicit = true).
func uniqueID(explicit bool, base string, try func(id string) error) (string, error) {
	if explicit {
		return base, try(base)
	}
	id := base
	for i := 2; i < 50; i++ {
		err := try(id)
		if !errors.Is(err, db.ErrConflict) {
			return id, err
		}
		id = base + "-" + strconv.Itoa(i)
	}
	return id, db.ErrConflict
}

func (h *Handler) AdminCreateCategory(w http.ResponseWriter, r *http.Request, adminID int64) {
	var req categoryReq
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanName(w, req.Name, 200)
	if !ok {
		return
	}
	desc := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(desc) > 2000 {
		writeError(w, http.StatusBadRequest, "validation", "Описание не длиннее 2000 символов")
		return
	}
	explicit := strings.TrimSpace(req.ID) != ""
	base := strings.TrimSpace(req.ID)
	if !explicit {
		base = slugify(name, "category")
	}
	if !slugRe.MatchString(base) {
		writeError(w, http.StatusBadRequest, "validation", "id: латиница, цифры и дефис, до 63 символов")
		return
	}
	id, err := uniqueID(explicit, base, func(id string) error {
		return h.q.CreateCategory(r.Context(), adminID, id, name, desc)
	})
	if errors.Is(err, db.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "Категория с таким id уже есть")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": name, "description": desc, "subs": []any{}})
}

func (h *Handler) AdminUpdateCategory(w http.ResponseWriter, r *http.Request, adminID int64) {
	var req categoryReq
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanName(w, req.Name, 200)
	if !ok {
		return
	}
	desc := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(desc) > 2000 {
		writeError(w, http.StatusBadRequest, "validation", "Описание не длиннее 2000 символов")
		return
	}
	id := r.PathValue("id")
	if err := h.q.UpdateCategory(r.Context(), adminID, id, name, desc); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name, "description": desc})
}

func (h *Handler) AdminDeleteCategory(w http.ResponseWriter, r *http.Request, adminID int64) {
	err := h.q.DeleteCategory(r.Context(), adminID, r.PathValue("id"))
	if errors.Is(err, db.ErrBadRef) {
		writeError(w, http.StatusConflict, "in_use", "В этой категории есть лоты — удалить её нельзя")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) AdminCreateSubcategory(w http.ResponseWriter, r *http.Request, adminID int64) {
	var req categoryReq
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanName(w, req.Name, 200)
	if !ok {
		return
	}
	categoryID := r.PathValue("id")
	explicit := strings.TrimSpace(req.ID) != ""
	base := strings.TrimSpace(req.ID)
	if !explicit {
		base = slugify(name, "subcategory")
	}
	if !slugRe.MatchString(base) {
		writeError(w, http.StatusBadRequest, "validation", "id: латиница, цифры и дефис, до 63 символов")
		return
	}
	id, err := uniqueID(explicit, base, func(id string) error {
		return h.q.CreateSubcategory(r.Context(), adminID, categoryID, id, name)
	})
	if errors.Is(err, db.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "Подкатегория с таким id уже есть")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": name})
}

func (h *Handler) AdminUpdateSubcategory(w http.ResponseWriter, r *http.Request, adminID int64) {
	var req categoryReq
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanName(w, req.Name, 200)
	if !ok {
		return
	}
	id := r.PathValue("sub")
	if err := h.q.UpdateSubcategory(r.Context(), adminID, r.PathValue("id"), id, name); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name})
}

func (h *Handler) AdminDeleteSubcategory(w http.ResponseWriter, r *http.Request, adminID int64) {
	err := h.q.DeleteSubcategory(r.Context(), adminID, r.PathValue("id"), r.PathValue("sub"))
	if errors.Is(err, db.ErrBadRef) {
		writeError(w, http.StatusConflict, "in_use", "В этой подкатегории есть лоты — удалить её нельзя")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/* ---------- общежития ---------- */

type dormReq struct {
	Name string `json:"name"`
}

func dormID(r *http.Request) (int16, bool) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 16)
	return int16(n), err == nil && n > 0
}

func (h *Handler) AdminCreateDorm(w http.ResponseWriter, r *http.Request, adminID int64) {
	var req dormReq
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanName(w, req.Name, 100)
	if !ok {
		return
	}
	d, err := h.q.CreateDorm(r.Context(), adminID, name)
	if errors.Is(err, db.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "Общежитие с таким названием уже есть")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (h *Handler) AdminUpdateDorm(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := dormID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	var req dormReq
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanName(w, req.Name, 100)
	if !ok {
		return
	}
	err := h.q.UpdateDorm(r.Context(), adminID, id, name)
	if errors.Is(err, db.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "Общежитие с таким названием уже есть")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, model.Dorm{ID: id, Name: name})
}

func (h *Handler) AdminDeleteDorm(w http.ResponseWriter, r *http.Request, adminID int64) {
	id, ok := dormID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный id")
		return
	}
	err := h.q.DeleteDorm(r.Context(), adminID, id)
	if errors.Is(err, db.ErrBadRef) {
		writeError(w, http.StatusConflict, "in_use", "В этом общежитии есть пользователи — удалить его нельзя")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func init() {
	adminRoutes = append(adminRoutes, func(mux *http.ServeMux, h *Handler) {
		a := h.adminOnly
		mux.HandleFunc("GET /api/admin/settings", a(h.AdminGetSettings))
		mux.HandleFunc("PUT /api/admin/settings", a(h.AdminUpdateSettings))
		mux.HandleFunc("POST /api/admin/categories", a(h.AdminCreateCategory))
		mux.HandleFunc("PUT /api/admin/categories/{id}", a(h.AdminUpdateCategory))
		mux.HandleFunc("DELETE /api/admin/categories/{id}", a(h.AdminDeleteCategory))
		mux.HandleFunc("POST /api/admin/categories/{id}/subcategories", a(h.AdminCreateSubcategory))
		mux.HandleFunc("PUT /api/admin/categories/{id}/subcategories/{sub}", a(h.AdminUpdateSubcategory))
		mux.HandleFunc("DELETE /api/admin/categories/{id}/subcategories/{sub}", a(h.AdminDeleteSubcategory))
		mux.HandleFunc("POST /api/admin/dorms", a(h.AdminCreateDorm))
		mux.HandleFunc("PUT /api/admin/dorms/{id}", a(h.AdminUpdateDorm))
		mux.HandleFunc("DELETE /api/admin/dorms/{id}", a(h.AdminDeleteDorm))
	})
}
