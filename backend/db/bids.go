package db

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"auction/model"
)

const MaxAmount = 2_000_000_000

// formatRub: 12500 -> «12 500 ₽» (как на фронтенде).
func formatRub(v int) string {
	s := strconv.Itoa(v)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString("\u00a0")
		}
		b.WriteRune(r)
	}
	return b.String() + "\u00a0₽"
}

func (q *Queries) GetLotBids(ctx context.Context, lotID int64) ([]model.Bid, error) {
	var exists bool
	if err := q.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM lots WHERE id = $1)`, lotID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := q.db.QueryContext(ctx,
		`SELECT b.id, b.lot_id, b.user_id, u.username, b.amount, b.created_at
		 FROM bids b JOIN users u ON u.id = b.user_id
		 WHERE b.lot_id = $1 ORDER BY b.created_at DESC, b.id DESC`, lotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bids := make([]model.Bid, 0)
	for rows.Next() {
		var b model.Bid
		var created time.Time
		if err := rows.Scan(&b.ID, &b.LotID, &b.UserID, &b.Username, &b.Amount, &created); err != nil {
			return nil, err
		}
		b.CreatedAt = ms(created)
		bids = append(bids, b)
	}
	return bids, rows.Err()
}

// PlaceBid проверяет все правила аукциона и пишет ставку в одной транзакции
// под блокировкой строки лота — параллельные ставки выстраиваются в очередь.
func (q *Queries) PlaceBid(ctx context.Context, lotID, userID int64, amount int) (model.Bid, error) {
	var bid model.Bid
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return bid, err
	}
	defer tx.Rollback()

	var authorID int64
	var status string
	var ended bool
	var startPrice, step int
	err = tx.QueryRowContext(ctx,
		`SELECT author_id, status, ends_at <= now(), start_price, step FROM lots WHERE id = $1 FOR UPDATE`, lotID).
		Scan(&authorID, &status, &ended, &startPrice, &step)
	if err != nil {
		return bid, mapErr(err)
	}
	if authorID == userID {
		return bid, model.Errf(403, "own_lot", "Нельзя делать ставку на свой лот")
	}
	if status != "active" || ended {
		return bid, model.Errf(409, "lot_closed", "Аукцион уже закрыт")
	}

	current := startPrice
	var topUser, topAmount int64
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, amount FROM bids WHERE lot_id = $1 ORDER BY amount DESC, id ASC LIMIT 1`, lotID).
		Scan(&topUser, &topAmount)
	switch {
	case err == sql.ErrNoRows:
	case err != nil:
		return bid, err
	default:
		current = int(topAmount)
		if topUser == userID {
			return bid, model.Errf(409, "already_leading", "Ваша ставка и так лидирует")
		}
	}

	minBid := current + step
	if amount < minBid {
		return bid, model.Errf(400, "bid_too_low", "Минимальная ставка: %s", formatRub(minBid))
	}
	if amount > MaxAmount {
		return bid, model.Errf(400, "bid_too_high", "Слишком большая ставка")
	}

	var created time.Time
	err = tx.QueryRowContext(ctx,
		`INSERT INTO bids (lot_id, user_id, amount) VALUES ($1, $2, $3) RETURNING id, created_at`,
		lotID, userID, amount).Scan(&bid.ID, &created)
	if err != nil {
		return bid, mapErr(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&bid.Username); err != nil {
		return bid, mapErr(err)
	}
	if err := tx.Commit(); err != nil {
		return bid, err
	}
	bid.LotID, bid.UserID, bid.Amount, bid.CreatedAt = lotID, userID, amount, ms(created)
	return bid, nil
}

// GetUserStats: «выиграно» — завершённые лоты, где пользователь лидер;
// «продано» — свои завершённые лоты со ставками; рейтинг = выиграно + продано.
func (q *Queries) GetUserStats(ctx context.Context, userID int64) (model.Stats, error) {
	var s model.Stats
	const finished = `(l.status = 'finished' OR (l.status = 'active' AND l.ends_at <= now()))`
	err := q.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM lots WHERE author_id = $1),
		(SELECT COUNT(DISTINCT lot_id) FROM bids WHERE user_id = $1),
		(SELECT COUNT(*) FROM lots l WHERE `+finished+`
			AND (SELECT user_id FROM bids WHERE lot_id = l.id ORDER BY amount DESC, id ASC LIMIT 1) = $1),
		(SELECT COUNT(*) FROM lots l WHERE l.author_id = $1 AND `+finished+`
			AND EXISTS (SELECT 1 FROM bids WHERE lot_id = l.id))`, userID).
		Scan(&s.Created, &s.Participated, &s.Won, &s.Sold)
	s.Rating = s.Won + s.Sold
	return s, err
}
