package model

import "fmt"

// Все JSON-поля — camelCase, время — миллисекунды Unix (как в моках фронтенда).

type Dorm struct {
	ID   int16  `json:"id"`
	Name string `json:"name"`
}

// Роли пользователей.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// User — полный профиль, отдаётся только самому пользователю.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Password  string `json:"-"`
	Name      string `json:"name"`
	Telegram  string `json:"telegram"` // у администратора, созданного через терминал, может быть пустым
	DormID    int16  `json:"dormId"`   // 0 — общежитие не выбрано
	CreatedAt int64  `json:"createdAt"`
	Role      string `json:"role"` // user | admin
	Blocked   bool   `json:"-"`    // заблокированный пользователь не может войти и пользоваться сервисом
}

// IsAdmin сообщает, что у пользователя роль администратора.
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// Статусы лота (колонка lots.status).
const (
	StatusPending   = "pending"   // ожидает одобрения администратора
	StatusApproved  = "approved"  // одобрен, ожидает начала аукциона
	StatusRejected  = "rejected"  // отклонён администратором
	StatusActive    = "active"    // участвует в аукционе
	StatusFinished  = "finished"  // аукцион завершён
	StatusCancelled = "cancelled" // аукцион отменён
)

type Category struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Subs        []Subcategory `json:"subs"`
}

type Subcategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Contact struct {
	Role     string `json:"role"` // «Победитель» или «Продавец»
	Name     string `json:"name"`
	Telegram string `json:"telegram"`
}

type Lot struct {
	ID             int64    `json:"id"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Image          string   `json:"image"`
	Category       string   `json:"category"`
	Subcategory    string   `json:"subcategory"`
	AuthorID       int64    `json:"authorId"`
	AuthorUsername string   `json:"authorUsername"`
	DormID         int16    `json:"dormId"` // общежитие автора
	StartPrice     int      `json:"startPrice"`
	Step           int      `json:"step"`
	CurrentPrice   int      `json:"currentPrice"`
	MinBid         int      `json:"minBid"`
	BidsCount      int      `json:"bidsCount"`
	LeaderID       *int64   `json:"leaderId"`
	MyMaxBid       int      `json:"myMaxBid"` // 0 — если ставок нет или не авторизован
	CreatedAt      int64    `json:"createdAt"`
	EndsAt         int64    `json:"endsAt"`
	Status         string   `json:"status"` // pending | approved | rejected | active | finished | cancelled (active — уже с учётом ends_at)
	Contact        *Contact `json:"contact,omitempty"`

	// Модерация. Причину (отказа, снятия, отмены) видят автор лота и администраторы.
	DurationSec      int64  `json:"durationSec"`      // заявленная длительность аукциона
	StartsAt         *int64 `json:"startsAt"`         // начало аукциона (мс Unix); nil — ещё не назначено
	Hidden           bool   `json:"hidden,omitempty"` // снят с публикации администратором
	ModerationReason string `json:"moderationReason,omitempty"`
	ModeratedBy      *int64 `json:"moderatedBy,omitempty"` // только в ответах для администратора
	ModeratedAt      *int64 `json:"moderatedAt,omitempty"` // только в ответах для администратора
}

type Bid struct {
	ID        int64  `json:"id"`
	LotID     int64  `json:"lotId"`
	UserID    int64  `json:"userId"`
	Username  string `json:"username"`
	Amount    int    `json:"amount"`
	CreatedAt int64  `json:"createdAt"`
}

type Stats struct {
	Created      int `json:"created"`
	Participated int `json:"participated"`
	Won          int `json:"won"`
	Sold         int `json:"sold"`
	Rating       int `json:"rating"` // won + sold
	Complaints   int `json:"complaints"`
}

type LotFilter struct {
	Category, Sub, Q, Status, Sort string
	PriceMin, PriceMax             int
	DormID                         int
	AuthorID, BidderID             int64
	// All отключает ограничение публичной видимости (лоты на модерации, отклонённые,
	// снятые с публикации). Включается только для «моих» списков и административных запросов.
	All bool
	// Started оставляет только лоты, прошедшие модерацию и дошедшие до аукциона
	// (active, finished, cancelled) — «аукционы» в админ-панели.
	Started bool
}

// APIError — ожидаемая ошибка бизнес-правил: хэндлер отдаёт её как есть.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

func Errf(status int, code, format string, a ...any) *APIError {
	return &APIError{Status: status, Code: code, Message: fmt.Sprintf(format, a...)}
}
