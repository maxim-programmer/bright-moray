package model

// LotLive — публичная часть состояния лота, одинаковая для всех зрителей.
// Именно она рассылается по WebSocket (в отличие от Lot, где есть поля вида
// myMaxBid и contact, зависящие от того, кто смотрит).
type LotLive struct {
	ID           int64  `json:"id"`
	Status       string `json:"status"` // active | finished | cancelled (с учётом endsAt)
	CurrentPrice int    `json:"currentPrice"`
	MinBid       int    `json:"minBid"`
	BidsCount    int    `json:"bidsCount"`
	LeaderID     *int64 `json:"leaderId"`
	EndsAt       int64  `json:"endsAt"` // мс Unix; меняется при срабатывании Anti-snipe
}

// Live выделяет из лота его публичное состояние.
func (l Lot) Live() LotLive {
	return LotLive{
		ID:           l.ID,
		Status:       l.Status,
		CurrentPrice: l.CurrentPrice,
		MinBid:       l.MinBid,
		BidsCount:    l.BidsCount,
		LeaderID:     l.LeaderID,
		EndsAt:       l.EndsAt,
	}
}
