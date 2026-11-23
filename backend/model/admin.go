package model

import "encoding/json"

// AdminUser — пользователь глазами администратора.
type AdminUser struct {
	User
	Email         string `json:"email"` // необязательна: пустая строка, если не задана
	IsBlocked     bool   `json:"blocked"`
	BlockedAt     *int64 `json:"blockedAt"`
	BlockedReason string `json:"blockedReason,omitempty"`
	BlockedBy     *int64 `json:"blockedBy,omitempty"`
	LotsCount     int    `json:"lotsCount"`
	BidsCount     int    `json:"bidsCount"`
	LastActivity  *int64 `json:"lastActivity"`
}

// UserFilter — фильтр списка пользователей в админ-панели.
type UserFilter struct {
	Q       string // ID, имя, никнейм, почта или Telegram
	Role    string // user | admin | ""
	Blocked string // "true" | "false" | ""
}

// UserBid — ставка пользователя вместе с лотом (для истории ставок пользователя).
type UserBid struct {
	ID        int64  `json:"id"`
	LotID     int64  `json:"lotId"`
	LotTitle  string `json:"lotTitle"`
	LotStatus string `json:"lotStatus"`
	Amount    int    `json:"amount"`
	CreatedAt int64  `json:"createdAt"`
	Leading   bool   `json:"leading"` // эта ставка сейчас лидирует (или выиграла)
}

// Event — запись журнала событий.
type Event struct {
	ID            int64           `json:"id"`
	CreatedAt     int64           `json:"createdAt"`
	Type          string          `json:"type"`
	Label         string          `json:"label"`
	UserID        *int64          `json:"userId"`
	Username      string          `json:"username,omitempty"`
	ActorID       *int64          `json:"actorId"`
	ActorUsername string          `json:"actorUsername,omitempty"`
	ByAdmin       bool            `json:"byAdmin"`
	LotID         *int64          `json:"lotId"`
	Details       json.RawMessage `json:"details"`
}

// EventFilter — фильтр журнала. Нулевые значения не ограничивают выборку.
type EventFilter struct {
	UserID  int64 // события, где пользователь — субъект или исполнитель
	LotID   int64
	Type    string // точный тип или префикс до точки, например "lot."
	ByAdmin bool   // только действия администраторов
	From    int64  // мс Unix, включительно
	To      int64  // мс Unix, не включая
	// ExcludeTypes — типы, которые нужно скрыть (например, ставки в истории изменений лота).
	ExcludeTypes []string
}

// Settings — системные настройки аукциона, которыми управляет администратор.
type Settings struct {
	MinBidStep             int `json:"minBidStep"`             // минимальный шаг ставки, ₽
	MinDurationMinutes     int `json:"minDurationMinutes"`     // минимальная длительность аукциона
	MaxDurationMinutes     int `json:"maxDurationMinutes"`     // максимальная длительность аукциона
	DefaultDurationMinutes int `json:"defaultDurationMinutes"` // длительность, если автор её не указал (автозавершение)
	AntiSnipeWindowSec     int `json:"antiSnipeWindowSec"`     // ставка в последние N секунд продлевает аукцион; 0 — выключено
	AntiSnipeExtendSec     int `json:"antiSnipeExtendSec"`     // …так, чтобы до конца оставалось не меньше N секунд
}

// Границы значений настроек.
const (
	MaxSettingStep     = 100_000_000
	MaxSettingMinutes  = 365 * 24 * 60
	MaxSettingSeconds  = 24 * 60 * 60
	DefaultMinBidStep  = 1
	DefaultMinMinutes  = 1
	DefaultMaxMinutes  = 30 * 24 * 60
	DefaultDurationMin = 24 * 60
)

// Validate проверяет согласованность настроек; возвращает текст ошибки или "".
func (s Settings) Validate() string {
	switch {
	case s.MinBidStep < 1 || s.MinBidStep > MaxSettingStep:
		return "Минимальный шаг ставки: от 1 до 100 000 000"
	case s.MinDurationMinutes < 1 || s.MinDurationMinutes > MaxSettingMinutes:
		return "Минимальная длительность: от 1 минуты до 365 дней"
	case s.MaxDurationMinutes < 1 || s.MaxDurationMinutes > MaxSettingMinutes:
		return "Максимальная длительность: от 1 минуты до 365 дней"
	case s.MinDurationMinutes > s.MaxDurationMinutes:
		return "Минимальная длительность не может быть больше максимальной"
	case s.DefaultDurationMinutes < s.MinDurationMinutes || s.DefaultDurationMinutes > s.MaxDurationMinutes:
		return "Время автозавершения по умолчанию должно быть между минимальной и максимальной длительностью"
	case s.AntiSnipeWindowSec < 0 || s.AntiSnipeWindowSec > MaxSettingSeconds ||
		s.AntiSnipeExtendSec < 0 || s.AntiSnipeExtendSec > MaxSettingSeconds:
		return "Правила продления: от 0 до 86 400 секунд"
	case s.AntiSnipeWindowSec > 0 && s.AntiSnipeExtendSec == 0:
		return "Укажите, на сколько секунд продлевать аукцион, или отключите продление (окно = 0)"
	}
	return ""
}

// StatsFilter — период статистики (мс Unix; 0 — без границы).
type StatsFilter struct {
	From int64
	To   int64
}

// CategoryStat — востребованность категории.
type CategoryStat struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Lots int    `json:"lots"`
	Bids int    `json:"bids"`
}

// UserActivityStat — строка рейтинга самых активных пользователей.
type UserActivityStat struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Lots     int    `json:"lots"`
	Bids     int    `json:"bids"`
	Score    int    `json:"score"` // lots + bids
}

// OverallStats — общая статистика системы.
type OverallStats struct {
	From                int64              `json:"from"`
	To                  int64              `json:"to"`
	UsersTotal          int                `json:"usersTotal"`      // всего зарегистрировано на конец периода
	UsersRegistered     int                `json:"usersRegistered"` // зарегистрировалось за период
	UsersActive         int                `json:"usersActive"`     // что-то делали в системе за период
	UsersBlocked        int                `json:"usersBlocked"`
	LotsCreated         int                `json:"lotsCreated"`
	LotsPending         int                `json:"lotsPending"`
	AuctionsActive      int                `json:"auctionsActive"`
	AuctionsFinished    int                `json:"auctionsFinished"`
	AuctionsCancelled   int                `json:"auctionsCancelled"`
	BidsTotal           int                `json:"bidsTotal"`
	AvgBidsPerAuction   float64            `json:"avgBidsPerAuction"`
	TopCategories       []CategoryStat     `json:"topCategories"`
	TopUsers            []UserActivityStat `json:"topUsers"`
	ActiveWindowDefault bool               `json:"activeWindowDefault"` // период не задан: активность считалась за 30 суток
}

// UserStats — статистика одного пользователя за период.
type UserStats struct {
	UserID       int64  `json:"userId"`
	From         int64  `json:"from"`
	To           int64  `json:"to"`
	LotsCreated  int    `json:"lotsCreated"`
	BidsPlaced   int    `json:"bidsPlaced"`
	Participated int    `json:"participated"` // в скольких аукционах делал ставки
	Won          int    `json:"won"`
	Sold         int    `json:"sold"`
	SpentTotal   int64  `json:"spentTotal"`  // сумма выигранных лотов
	EarnedTotal  int64  `json:"earnedTotal"` // сумма проданных лотов
	Events       int    `json:"events"`
	RegisteredAt int64  `json:"registeredAt"`
	LastActivity *int64 `json:"lastActivity"`
}

// AuctionDetail — подробности аукциона для администратора.
type AuctionDetail struct {
	Lot        Lot        `json:"lot"`
	Bids       []Bid      `json:"bids"`
	Author     *PublicRef `json:"author"`
	Winner     *PublicRef `json:"winner"`     // nil — ставок нет
	FinalPrice *int       `json:"finalPrice"` // nil — ставок нет
	Closed     bool       `json:"closed"`     // торги завершены или отменены
}

// PublicRef — краткие сведения о пользователе (с контактами: администратору нужны для споров).
type PublicRef struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Telegram string `json:"telegram"`
}
