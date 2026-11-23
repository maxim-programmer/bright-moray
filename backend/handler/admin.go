package handler

import (
	"auction/db"
	"auction/logging"
	"auction/model"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
)

const (
	msgBlocked = "Ваш аккаунт заблокирован администратором"

	adminPageSize    = 20
	adminMaxPageSize = 100
	maxReasonLen     = 1000
)

/* ---------- маршруты ---------- */

// adminRoutes — регистраторы маршрутов админ-API. Каждый файл admin_*.go добавляет свои в init(),
// поэтому список маршрутов лежит рядом с обработчиками, а не в одном длинном перечне.
var adminRoutes []func(mux *http.ServeMux, h *Handler)

func setupAdminRoutes(mux *http.ServeMux, h *Handler) {
	for _, register := range adminRoutes {
		register(mux, h)
	}
}

/* ---------- доступ ---------- */

// adminOnly пропускает только администратора. Роль берётся из БД при каждом запросе,
// поэтому снятие роли и блокировка действуют сразу, а не после истечения access-токена.
func (h *Handler) adminOnly(fn func(w http.ResponseWriter, r *http.Request, adminID int64)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := h.userID(r)
		if uid == 0 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Войдите в аккаунт")
			return
		}
		role, blocked, err := h.q.UserAccess(r.Context(), uid)
		switch {
		case errors.Is(err, db.ErrNotFound):
			h.clearAuthCookies(w)
			writeError(w, http.StatusUnauthorized, "unauthorized", "Войдите в аккаунт")
			return
		case err != nil:
			fail(w, r, err)
			return
		case blocked:
			h.clearAuthCookies(w)
			writeError(w, http.StatusForbidden, "account_blocked", msgBlocked)
			return
		case role != model.RoleAdmin:
			writeError(w, http.StatusForbidden, "forbidden", "Недостаточно прав")
			return
		}
		fn(w, r, uid)
	}
}

/* ---------- журнал ---------- */

// logEvent пишет событие в журнал. Сбой журнала не должен ломать само действие пользователя
// (оно уже выполнено), поэтому ошибка только попадает в лог.
func (h *Handler) logEvent(r *http.Request, e db.EventInput) {
	if h.q == nil {
		return
	}
	if err := h.q.LogEvent(context.WithoutCancel(r.Context()), e); err != nil {
		logging.FromContext(r.Context(), logging.ComponentHandler).
			Warn("failed to write event", zap.String("type", e.Type), zap.Error(err))
	}
}

/* ---------- настройки ---------- */

// defaultSettings — значения, пока администратор их не менял. Правила продления берутся
// из переменных окружения (ANTI_SNIPE_*), остальные совпадают с прежними жёсткими ограничениями.
func (h *Handler) defaultSettings() model.Settings {
	return model.Settings{
		MinBidStep:             model.DefaultMinBidStep,
		MinDurationMinutes:     model.DefaultMinMinutes,
		MaxDurationMinutes:     model.DefaultMaxMinutes,
		DefaultDurationMinutes: model.DefaultDurationMin,
		AntiSnipeWindowSec:     int(h.cfg.AntiSnipeWindow / time.Second),
		AntiSnipeExtendSec:     int(h.cfg.AntiSnipeExtend / time.Second),
	}
}

func (h *Handler) settings(ctx context.Context) (model.Settings, error) {
	return h.q.GetSettings(ctx, h.defaultSettings())
}

// antiSnipeNow — действующие правила продления аукциона (для хаба).
func (h *Handler) antiSnipeNow(ctx context.Context) db.AntiSnipe {
	static := db.AntiSnipe{Window: h.cfg.AntiSnipeWindow, Extend: h.cfg.AntiSnipeExtend}
	if h.q == nil {
		return static
	}
	s, err := h.settings(ctx)
	if err != nil {
		h.logger().Warn("failed to load settings, using static anti-snipe", zap.Error(err))
		return static
	}
	return db.AntiSnipe{
		Window: time.Duration(s.AntiSnipeWindowSec) * time.Second,
		Extend: time.Duration(s.AntiSnipeExtendSec) * time.Second,
	}
}

/* ---------- завершение аукциона ---------- */

// onAuctionFinished фиксирует завершение аукциона по таймеру (статус finished и запись журнала).
func (h *Handler) onAuctionFinished(ctx context.Context, lotID int64) {
	if h.q == nil {
		return
	}
	if err := h.q.MarkFinished(ctx, lotID); err != nil {
		h.logger().With(zap.String("component", logging.ComponentLive)).
			Error("failed to record auction finish", zap.Int64("lot_id", lotID), zap.Error(err))
	}
}

/* ---------- разбор запросов ---------- */

// eventFilterFromQuery разбирает фильтры журнала: type, byAdmin, from, to и (для общего журнала) userId, lotId.
func eventFilterFromQuery(r *http.Request) (model.EventFilter, string) {
	qs := r.URL.Query()
	from, to, msg := periodFromQuery(r)
	if msg != "" {
		return model.EventFilter{}, msg
	}
	t := strings.TrimSpace(qs.Get("type"))
	if len(t) > 64 {
		return model.EventFilter{}, "type слишком длинный"
	}
	return model.EventFilter{
		Type:    t,
		ByAdmin: qs.Get("byAdmin") == "true",
		From:    from,
		To:      to,
	}, ""
}

func (h *Handler) writeEvents(w http.ResponseWriter, r *http.Request, f model.EventFilter) {
	page, size, offset := adminPaging(r)
	events, total, err := h.q.ListEvents(r.Context(), f, size, offset)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageResponse("events", events, total, page, size))
}

// validLotStatusFilter: "" и "all" — без фильтра по статусу.
func validLotStatusFilter(s string) bool {
	switch s {
	case "", "all", model.StatusPending, model.StatusApproved, model.StatusRejected,
		model.StatusActive, model.StatusFinished, model.StatusCancelled:
		return true
	}
	return false
}

// adminPaging читает page и pageSize из запроса.
func adminPaging(r *http.Request) (page, size, offset int) {
	qs := r.URL.Query()
	page = atoi(qs.Get("page"))
	if page < 1 {
		page = 1
	}
	size = atoi(qs.Get("pageSize"))
	if size < 1 {
		size = adminPageSize
	}
	if size > adminMaxPageSize {
		size = adminMaxPageSize
	}
	return page, size, (page - 1) * size
}

func pageResponse(key string, items any, total, page, size int) map[string]any {
	pages := (total + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	return map[string]any{key: items, "total": total, "page": page, "pageSize": size, "totalPages": pages}
}

// parseTimeParam принимает миллисекунды Unix или дату YYYY-MM-DD (UTC). Для верхней границы
// дата означает конец этого дня (граница исключается), поэтому добавляются сутки.
func parseTimeParam(raw string, upper bool) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return n, n > 0
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return 0, false
	}
	if upper {
		t = t.AddDate(0, 0, 1)
	}
	return t.UnixMilli(), true
}

// periodFromQuery читает from и to; to не включается в период.
func periodFromQuery(r *http.Request) (from, to int64, msg string) {
	qs := r.URL.Query()
	var ok bool
	if from, ok = parseTimeParam(qs.Get("from"), false); !ok {
		return 0, 0, "from: миллисекунды Unix или дата ГГГГ-ММ-ДД"
	}
	if to, ok = parseTimeParam(qs.Get("to"), true); !ok {
		return 0, 0, "to: миллисекунды Unix или дата ГГГГ-ММ-ДД"
	}
	if from > 0 && to > 0 && to <= from {
		return 0, 0, "Конец периода должен быть позже начала"
	}
	return from, to, ""
}

// readReason читает и проверяет причину действия. required — без неё действие недопустимо.
func readReason(w http.ResponseWriter, reason string, required bool) (string, bool) {
	reason = strings.TrimSpace(reason)
	if required && reason == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите причину")
		return "", false
	}
	if utf8.RuneCountInString(reason) > maxReasonLen {
		writeError(w, http.StatusBadRequest, "validation", "Причина не длиннее 1000 символов")
		return "", false
	}
	return reason, true
}

// decodeOptional разбирает тело, которое может отсутствовать (например, причина необязательна).
func decodeOptional(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	return decode(w, r, v)
}

/* ---------- сообщения о допустимых значениях ---------- */

func stepRangeMessage(min int) string {
	return fmt.Sprintf("Шаг ставки: от %s до 100 000 000", spaced(min))
}

func spaced(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(" ")
		}
		b.WriteRune(c)
	}
	return b.String()
}

// durationAllowed: длительность в часах укладывается в настройки (с запасом на ошибку округления).
func durationAllowed(hours float64, s model.Settings) bool {
	minutes := hours * 60
	return minutes >= float64(s.MinDurationMinutes)-1e-6 && minutes <= float64(s.MaxDurationMinutes)+1e-6
}

func durationRangeMessage(s model.Settings) string {
	return "Длительность аукциона: от " + ruDuration(s.MinDurationMinutes) + " до " + ruDuration(s.MaxDurationMinutes)
}

// ruDuration записывает длительность в родительном падеже: «1 минуты», «2 часов», «30 дней».
func ruDuration(minutes int) string {
	n, forms := minutes, [3]string{"минуты", "минут", "минут"}
	switch {
	case minutes%1440 == 0:
		n, forms = minutes/1440, [3]string{"дня", "дней", "дней"}
	case minutes%60 == 0:
		n, forms = minutes/60, [3]string{"часа", "часов", "часов"}
	}
	form := forms[1]
	if n%10 == 1 && n%100 != 11 {
		form = forms[0]
	}
	return fmt.Sprintf("%d %s", n, form)
}
