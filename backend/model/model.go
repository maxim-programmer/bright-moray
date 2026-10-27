package model

import "fmt"

// Все JSON-поля — camelCase, время — миллисекунды Unix (как в моках фронтенда).

type Dorm struct {
	ID   int16  `json:"id"`
	Name string `json:"name"`
}

// User — полный профиль, отдаётся только самому пользователю.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Password  string `json:"-"`
	Name      string `json:"name"`
	Telegram  string `json:"telegram"`
	DormID    int16  `json:"dormId"`
	CreatedAt int64  `json:"createdAt"`
}

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
	Status         string   `json:"status"` // active | finished | cancelled (уже с учётом ends_at)
	Contact        *Contact `json:"contact,omitempty"`
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
