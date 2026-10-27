package db

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"auction/model"
)

const PageSize = 20

// Статус с учётом срока: пока ничто не «закрывает» лоты фоновой задачей,
// просроченный активный лот считается завершённым прямо в запросе.
const effStatus = `(CASE WHEN l.status = 'active' AND l.ends_at <= now() THEN 'finished' ELSE l.status END)`

// Текущая цена: лидирующая ставка или стартовая цена.
const curPrice = `COALESCE(top.amount, l.start_price)`

const lotFromBase = `
FROM lots l
JOIN users au ON au.id = l.author_id
LEFT JOIN LATERAL (
	SELECT user_id, amount FROM bids WHERE lot_id = l.id ORDER BY amount DESC, id ASC LIMIT 1
) top ON true`

// $1 в полном запросе — id текущего пользователя (0 — аноним).
const lotFromFull = lotFromBase + `
LEFT JOIN LATERAL (SELECT COUNT(*) AS n FROM bids WHERE lot_id = l.id) cnt ON true
LEFT JOIN LATERAL (SELECT MAX(amount) AS amount FROM bids WHERE lot_id = l.id AND user_id = $1) mine ON true`

const lotSelect = `SELECT l.id, l.title, l.description, l.image, l.category_id, l.subcategory_id,
	l.author_id, au.username, au.dorm_id, l.start_price, l.step,
	` + curPrice + `, cnt.n, top.user_id, COALESCE(mine.amount, 0),
	l.created_at, l.ends_at, ` + effStatus + ` ` + lotFromFull

func scanLot(row interface{ Scan(...any) error }) (model.Lot, error) {
	var l model.Lot
	var leader sql.NullInt64
	var created, ends time.Time
	err := row.Scan(&l.ID, &l.Title, &l.Description, &l.Image, &l.Category, &l.Subcategory,
		&l.AuthorID, &l.AuthorUsername, &l.DormID, &l.StartPrice, &l.Step,
		&l.CurrentPrice, &l.BidsCount, &leader, &l.MyMaxBid,
		&created, &ends, &l.Status)
	if err != nil {
		return l, err
	}
	if leader.Valid {
		id := leader.Int64
		l.LeaderID = &id
	}
	l.MinBid = l.CurrentPrice + l.Step
	l.CreatedAt = ms(created)
	l.EndsAt = ms(ends)
	return l, nil
}

func buildWhere(f model.LotFilter, start int) (string, []any) {
	var conds []string
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(start+len(args)-1)
	}
	switch f.Status {
	case "active", "finished", "cancelled":
		conds = append(conds, effStatus+" = "+next(f.Status))
	}
	if f.Category != "" {
		conds = append(conds, "l.category_id = "+next(f.Category))
	}
	if f.Sub != "" {
		conds = append(conds, "l.subcategory_id = "+next(f.Sub))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
		p := next("%" + r.Replace(q) + "%")
		conds = append(conds, fmt.Sprintf("(l.title ILIKE %s OR l.description ILIKE %s)", p, p))
	}
	if f.PriceMin > 0 {
		conds = append(conds, curPrice+" >= "+next(f.PriceMin))
	}
	if f.PriceMax > 0 {
		conds = append(conds, curPrice+" <= "+next(f.PriceMax))
	}
	if f.DormID > 0 {
		conds = append(conds, "au.dorm_id = "+next(f.DormID))
	}
	if f.AuthorID > 0 {
		conds = append(conds, "l.author_id = "+next(f.AuthorID))
	}
	if f.BidderID > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM bids bb WHERE bb.lot_id = l.id AND bb.user_id = "+next(f.BidderID)+")")
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func orderBy(sort string) string {
	switch sort {
	case "new":
		return "l.created_at DESC, l.id DESC"
	case "price_asc":
		return curPrice + " ASC, l.id DESC"
	case "price_desc":
		return curPrice + " DESC, l.id DESC"
	case "title_asc":
		return "lower(l.title) ASC, l.id DESC"
	case "title_desc":
		return "lower(l.title) DESC, l.id DESC"
	case "bids":
		return "cnt.n DESC, l.id DESC"
	default: // ending: активные по близости окончания, остальные — после них
		return "(CASE WHEN " + effStatus + " = 'active' THEN 0 ELSE 1 END), l.ends_at ASC, l.id DESC"
	}
}

// ListLots возвращает лоты по фильтру; limit = 0 — без ограничения.
func (q *Queries) ListLots(ctx context.Context, viewer int64, f model.LotFilter, limit, offset int) ([]model.Lot, error) {
	where, args := buildWhere(f, 2)
	query := lotSelect + where + " ORDER BY " + orderBy(f.Sort)
	all := append([]any{viewer}, args...)
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(all)+1, len(all)+2)
		all = append(all, limit, offset)
	}
	return q.queryLots(ctx, query, all...)
}

func (q *Queries) CountLots(ctx context.Context, f model.LotFilter) (int, error) {
	where, args := buildWhere(f, 1)
	var n int
	err := q.db.QueryRowContext(ctx, "SELECT COUNT(*) "+lotFromBase+where, args...).Scan(&n)
	return n, err
}

func (q *Queries) queryLots(ctx context.Context, query string, args ...any) ([]model.Lot, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lots := make([]model.Lot, 0)
	for rows.Next() {
		l, err := scanLot(rows)
		if err != nil {
			return nil, err
		}
		lots = append(lots, l)
	}
	return lots, rows.Err()
}

// GetLot возвращает лот; контакты второй стороны добавляются только продавцу и победителю
// завершённого лота со ставками.
func (q *Queries) GetLot(ctx context.Context, viewer, id int64) (model.Lot, error) {
	l, err := scanLot(q.db.QueryRowContext(ctx, lotSelect+" WHERE l.id = $2", viewer, id))
	if err != nil {
		return l, mapErr(err)
	}
	if viewer > 0 && l.Status == "finished" && l.BidsCount > 0 && l.LeaderID != nil {
		var otherID int64
		var role string
		switch viewer {
		case l.AuthorID:
			otherID, role = *l.LeaderID, "Победитель"
		case *l.LeaderID:
			otherID, role = l.AuthorID, "Продавец"
		}
		if otherID > 0 {
			c := model.Contact{Role: role}
			err := q.db.QueryRowContext(ctx, `SELECT name, telegram FROM users WHERE id = $1`, otherID).
				Scan(&c.Name, &c.Telegram)
			if err == nil {
				l.Contact = &c
			}
		}
	}
	return l, nil
}

type NewLot struct {
	Title, Description, Image, Category, Subcategory string
	StartPrice, Step                                 int
	Hours                                            float64
}

func (q *Queries) CreateLot(ctx context.Context, authorID int64, n NewLot) (model.Lot, error) {
	endsAt := time.Now().Add(time.Duration(n.Hours * float64(time.Hour)))
	var id int64
	err := q.db.QueryRowContext(ctx,
		`INSERT INTO lots (title, description, image, category_id, subcategory_id, author_id, start_price, step, ends_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		n.Title, n.Description, n.Image, n.Category, n.Subcategory, authorID, n.StartPrice, n.Step, endsAt).Scan(&id)
	if err != nil {
		return model.Lot{}, mapErr(err)
	}
	return q.GetLot(ctx, authorID, id)
}

// CancelLot отменяет свой активный лот без ставок. Проверки и обновление — в одной
// транзакции под блокировкой строки лота, чтобы ставка не проскочила между ними.
func (q *Queries) CancelLot(ctx context.Context, lotID, userID int64) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var authorID int64
	var status string
	var ended bool
	err = tx.QueryRowContext(ctx,
		`SELECT author_id, status, ends_at <= now() FROM lots WHERE id = $1 FOR UPDATE`, lotID).
		Scan(&authorID, &status, &ended)
	if err != nil {
		return mapErr(err)
	}
	if authorID != userID {
		return model.Errf(403, "forbidden", "Отменить лот может только его автор")
	}
	if status != "active" || ended {
		return model.Errf(409, "lot_closed", "Аукцион уже закрыт")
	}
	var bids int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bids WHERE lot_id = $1`, lotID).Scan(&bids); err != nil {
		return err
	}
	if bids > 0 {
		return model.Errf(409, "has_bids", "Нельзя отменить лот, на который уже сделаны ставки")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lots SET status = 'cancelled' WHERE id = $1`, lotID); err != nil {
		return err
	}
	return tx.Commit()
}

// SimilarLots: активные лоты той же категории, сначала из той же подкатегории.
func (q *Queries) SimilarLots(ctx context.Context, viewer, lotID int64, limit int) ([]model.Lot, error) {
	query := lotSelect + ` WHERE l.id <> $2 AND ` + effStatus + ` = 'active'
		AND l.category_id = (SELECT category_id FROM lots WHERE id = $2)
		ORDER BY (l.subcategory_id <> (SELECT subcategory_id FROM lots WHERE id = $2)), l.ends_at ASC, l.id DESC
		LIMIT $3`
	return q.queryLots(ctx, query, viewer, lotID, limit)
}

// RandomLotID возвращает id случайного активного лота (0 — если таких нет).
func (q *Queries) RandomLotID(ctx context.Context, category, sub string) (int64, error) {
	var id int64
	err := q.db.QueryRowContext(ctx,
		`SELECT l.id FROM lots l
		 WHERE l.status = 'active' AND l.ends_at > now()
		   AND ($1 = '' OR l.category_id = $1) AND ($2 = '' OR l.subcategory_id = $2)
		 ORDER BY random() LIMIT 1`, category, sub).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}
