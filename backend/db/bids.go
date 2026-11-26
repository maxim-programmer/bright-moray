package db

import (
	"auction/model"
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
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

// GetLotBidsFor — история ставок с проверкой видимости лота для зрителя viewer (0 — аноним).
// Лот, не прошедший модерацию или снятый с публикации, виден только автору, администратору
// и пользователю, делавшему на него ставки; для остальных его «нет».
func (q *Queries) GetLotBidsFor(ctx context.Context, viewer, lotID int64) ([]model.Bid, error) {
	var author int64
	var hidden bool
	var status string
	err := q.db.QueryRowContext(ctx, `SELECT author_id, hidden, status FROM lots WHERE id = $1`, lotID).
		Scan(&author, &hidden, &status)
	if err != nil {
		return nil, mapErr(err)
	}
	public := !hidden && (status == model.StatusActive || status == model.StatusFinished || status == model.StatusCancelled)
	if !public && viewer != author {
		ok := false
		if viewer > 0 {
			if err := q.db.QueryRowContext(ctx, `
				SELECT EXISTS (SELECT 1 FROM bids WHERE lot_id = $1 AND user_id = $2)
				    OR EXISTS (SELECT 1 FROM users WHERE id = $2 AND role = 'admin')`, lotID, viewer).Scan(&ok); err != nil {
				return nil, err
			}
		}
		if !ok {
			return nil, ErrNotFound
		}
	}
	return q.GetLotBids(ctx, lotID)
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

// AntiSnipe — защита от «снайперских» ставок в последние секунды аукциона.
// Если ставка принята, когда до конца осталось не больше Window, конец аукциона
// переносится на «сейчас + Extend» (но никогда не приближается). Window = 0 или
// Extend = 0 выключают защиту.
type AntiSnipe struct {
	Window time.Duration
	Extend time.Duration
}

func (a AntiSnipe) Enabled() bool { return a.Window > 0 && a.Extend > 0 }

// PlacedBid — результат успешной ставки.
type PlacedBid struct {
	Bid      model.Bid
	Extended bool          // сработал Anti-snipe: конец аукциона перенесён
	EndsAt   time.Time     // конец аукциона после ставки
	Left     time.Duration // сколько осталось до конца после ставки (по часам БД)
}

// PlaceBid проверяет все правила аукциона и пишет ставку в одной транзакции
// под блокировкой строки лота — параллельные ставки выстраиваются в очередь.
// Время сверяется по часам БД (clock_timestamp), а не по времени начала транзакции:
// иначе ставка, прождавшая блокировку, могла бы пройти уже после окончания аукциона.
func (q *Queries) PlaceBid(ctx context.Context, lotID, userID int64, amount int, as AntiSnipe) (PlacedBid, error) {
	var res PlacedBid
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	var authorID int64
	var status string
	var endsAt, dbNow time.Time
	var startPrice, step int
	err = tx.QueryRowContext(ctx,
		`SELECT author_id, status, ends_at, clock_timestamp(), start_price, step FROM lots WHERE id = $1 FOR UPDATE`, lotID).
		Scan(&authorID, &status, &endsAt, &dbNow, &startPrice, &step)
	if err != nil {
		return res, mapErr(err)
	}
	// Заблокированный пользователь не может делать ставки, даже если его access-токен ещё жив
	// или он подключён по WebSocket. Несуществующего пользователя отвергнет внешний ключ ниже.
	var blocked bool
	var blockedReason string
	switch err := tx.QueryRowContext(ctx,
		`SELECT blocked_at IS NOT NULL, COALESCE(blocked_reason, '') FROM users WHERE id = $1`, userID).
		Scan(&blocked, &blockedReason); {
	case err == sql.ErrNoRows:
	case err != nil:
		return res, err
	case blocked:
		return res, model.Errf(403, "account_blocked", "%s", model.BlockedMessage(blockedReason))
	}
	if authorID == userID {
		return res, model.Errf(403, "own_lot", "Нельзя делать ставку на свой лот")
	}
	switch status {
	case model.StatusPending, model.StatusApproved, model.StatusRejected:
		return res, ErrNotFound // лот ещё не опубликован: для остальных его не существует
	}
	if status != model.StatusActive || !endsAt.After(dbNow) {
		return res, model.Errf(409, "lot_closed", "Аукцион уже закрыт")
	}

	// Первая ставка может быть равна стартовой цене (без шага);
	// каждая следующая — не меньше текущей цены + шаг.
	minBid := startPrice
	var topUser, topAmount int64
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, amount FROM bids WHERE lot_id = $1 ORDER BY amount DESC, id ASC LIMIT 1`, lotID).
		Scan(&topUser, &topAmount)
	switch {
	case err == sql.ErrNoRows:
	case err != nil:
		return res, err
	default:
		minBid = int(topAmount) + step
		if topUser == userID {
			return res, model.Errf(409, "already_leading", "Ваша ставка и так лидирует")
		}
	}

	if amount < minBid {
		return res, model.Errf(400, "bid_too_low", "Минимальная ставка: %s", formatRub(minBid))
	}
	if amount > MaxAmount {
		return res, model.Errf(400, "bid_too_high", "Слишком большая ставка")
	}

	var created time.Time
	err = tx.QueryRowContext(ctx,
		`INSERT INTO bids (lot_id, user_id, amount) VALUES ($1, $2, $3) RETURNING id, created_at`,
		lotID, userID, amount).Scan(&res.Bid.ID, &created)
	if err != nil {
		return res, mapErr(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&res.Bid.Username); err != nil {
		return res, mapErr(err)
	}

	// Anti-snipe: ставка в последние Window секунд переносит конец аукциона.
	left := endsAt.Sub(dbNow)
	if as.Enabled() && left <= as.Window {
		if newEnds := endsAt.Add(as.Extend); newEnds.After(endsAt) {
			if _, err := tx.ExecContext(ctx, `UPDATE lots SET ends_at = $2 WHERE id = $1`, lotID, newEnds); err != nil {
				return res, mapErr(err)
			}
			endsAt, left, res.Extended = newEnds, as.Extend, true
		}
	}

	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvBidPlaced, UserID: userID, ActorID: userID, LotID: lotID,
		Details: map[string]any{"amount": amount, "bidId": res.Bid.ID, "extended": res.Extended},
	}); err != nil {
		return res, err
	}

	if err := tx.Commit(); err != nil {
		return res, err
	}
	res.Bid.LotID, res.Bid.UserID, res.Bid.Amount, res.Bid.CreatedAt = lotID, userID, amount, ms(created)
	res.EndsAt, res.Left = endsAt, left
	return res, nil
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
