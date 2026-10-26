package model

import "time"

type Dorm struct {
	ID   int16  `json:"id"`
	Name string `json:"name"`
}

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	Name      string    `json:"name"`
	Telegram  string    `json:"telegram"`
	DormID    int16     `json:"dorm_id"`
	CreatedAt time.Time `json:"created_at"`
}

type Category struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Subs        []Subcategory  `json:"subs,omitempty"`
}

type Subcategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Lot struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Image        string     `json:"image"`
	CategoryID   string     `json:"category"`
	SubcategoryID string    `json:"subcategory"`
	AuthorID     int64      `json:"author_id"`
	StartPrice   int        `json:"start_price"`
	Step         int        `json:"step"`
	CreatedAt    time.Time  `json:"created_at"`
	EndsAt       time.Time  `json:"ends_at"`
	Status       string     `json:"status"`
}

type Bid struct {
	ID        int64     `json:"id"`
	LotID     int64     `json:"lot_id"`
	UserID    int64     `json:"user_id"`
	Amount    int        `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

type RegisterRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Telegram string `json:"telegram"`
	DormID   int16  `json:"dorm_id"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateLotRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Image       string  `json:"image"`
	Category    string  `json:"category"`
	Subcategory string  `json:"subcategory"`
	StartPrice  int     `json:"start_price"`
	Step        int     `json:"step"`
	Hours       float64 `json:"hours"`
}

type PlaceBidRequest struct {
	Amount int `json:"amount"`
}

type UpdateProfileRequest struct {
	Name     string `json:"name"`
	Telegram string `json:"telegram"`
}

type ChangePasswordRequest struct {
	Current     string `json:"current"`
	NewPassword string `json:"new_password"`
	Confirm     string `json:"confirm"`
}

type ChangeUsernameRequest struct {
	NewUsername string `json:"new_username"`
}

type StatsResponse struct {
	LotsCreated int `json:"lots_created"`
	LotsWon     int `json:"lots_won"`
	LotsLost    int `json:"lots_lost"`
	TotalBids   int `json:"total_bids"`
}
