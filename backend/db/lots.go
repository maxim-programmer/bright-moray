package db

import (
	"context"
	"database/sql"
	"fmt"
	"math"
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
	l.author_id, au.username, COALESCE(au.dorm_id, 0), l.start_price, l.step,
	` + curPrice + `, cnt.n, top.user_id, COALESCE(mine.amount, 0),
	l.created_at, l.ends_at, ` + effStatus + `,
	COALESCE(l.duration_seconds, GREATEST(1, EXTRACT(EPOCH FROM (l.ends_at - l.created_at))::bigint)),
	l.starts_at, l.hidden, COALESCE(l.moderation_reason, ''), l.moderated_by, l.moderated_at ` + lotFromFull

func scanLot(row interface{ Scan(...any) error }) (model.Lot, error) {
	var l model.Lot
	var leader, moderatedBy sql.NullInt64
	var created, ends time.Time
	var startsAt, moderatedAt sql.NullTime
	err := row.Scan(&l.ID, &l.Title, &l.Description, &l.Image, &l.Category, &l.Subcategory,
		&l.AuthorID, &l.AuthorUsername, &l.DormID, &l.StartPrice, &l.Step,
		&l.CurrentPrice, &l.BidsCount, &leader, &l.MyMaxBid,
		&created, &ends, &l.Status,
		&l.DurationSec, &startsAt, &l.Hidden, &l.ModerationReason, &moderatedBy, &moderatedAt)
	if err != nil {
		return l, err
	}
	if startsAt.Valid {
		v := ms(startsAt.Time)
		l.StartsAt = &v
	}
	if moderatedBy.Valid {
		v := moderatedBy.Int64
		l.ModeratedBy = &v
	}
	if moderatedAt.Valid {
		v := ms(moderatedAt.Time)
		l.ModeratedAt = &v
	}
	if leader.Valid {
		id := leader.Int64
		l.LeaderID = &id
	}
	// первая ставка может быть равна стартовой цене, дальше — цена + шаг
	if l.BidsCount > 0 {
		l.MinBid = l.CurrentPrice + l.Step
	} else {
		l.MinBid = l.StartPrice
	}
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
	if !f.All { // публично видны только опубликованные лоты
		conds = append(conds, "(l.hidden = false AND l.status IN ('active', 'finished', 'cancelled'))")
	}
	if f.Started {
		conds = append(conds, "l.status IN ('active', 'finished', 'cancelled')")
	}
	switch f.Status {
	case "active", "finished", "cancelled", "pending", "approved", "rejected":
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
	case "old": // очередь модерации: давно ждущие — первыми
		return "l.created_at ASC, l.id ASC"
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

// isPublic: лот виден всем (прошёл модерацию и не снят с публикации).
func isPublic(l model.Lot) bool {
	if l.Hidden {
		return false
	}
	switch l.Status {
	case model.StatusActive, model.StatusFinished, model.StatusCancelled:
		return true
	}
	return false
}

// forViewer убирает служебные поля модерации: причину видит только автор лота,
// сведения о модераторе — только администратор (см. adminLot).
func forViewer(l model.Lot, viewer int64) model.Lot {
	if l.AuthorID != viewer {
		l.ModerationReason = ""
	}
	l.ModeratedBy, l.ModeratedAt = nil, nil
	return l
}

// ListLots возвращает лоты по фильтру; limit = 0 — без ограничения.
func (q *Queries) ListLots(ctx context.Context, viewer int64, f model.LotFilter, limit, offset int) ([]model.Lot, error) {
	lots, err := q.listLotsRaw(ctx, viewer, f, limit, offset)
	for i := range lots {
		lots[i] = forViewer(lots[i], viewer)
	}
	return lots, err
}

// listLotsRaw — как ListLots, но с полными служебными полями (для администратора).
func (q *Queries) listLotsRaw(ctx context.Context, viewer int64, f model.LotFilter, limit, offset int) ([]model.Lot, error) {
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
//
// Лот, не прошедший модерацию или снятый с публикации, виден только автору, администратору
// и пользователю, который уже делал на него ставки; для остальных его «нет».
func (q *Queries) GetLot(ctx context.Context, viewer, id int64) (model.Lot, error) {
	l, err := q.getLotRaw(ctx, viewer, id)
	if err != nil {
		return l, err
	}
	admin := false
	if viewer > 0 && viewer != l.AuthorID && (!isPublic(l) || l.ModerationReason != "") {
		if role, _, err := q.UserAccess(ctx, viewer); err == nil {
			admin = role == model.RoleAdmin
		}
	}
	if !isPublic(l) && viewer != l.AuthorID && !admin {
		var bidder bool
		if viewer > 0 {
			if err := q.db.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM bids WHERE lot_id = $1 AND user_id = $2)`, id, viewer).Scan(&bidder); err != nil {
				return model.Lot{}, err
			}
		}
		if !bidder {
			return model.Lot{}, ErrNotFound
		}
	}
	if admin {
		l.ModeratedBy, l.ModeratedAt = nil, nil // подробности модерации — в админ-запросах
	} else {
		l = forViewer(l, viewer)
	}
	return l, nil
}

// getLotRaw читает лот без проверок видимости; контакты второй стороны добавляются как в GetLot.
func (q *Queries) getLotRaw(ctx context.Context, viewer, id int64) (model.Lot, error) {
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
			err := q.db.QueryRowContext(ctx, `SELECT name, COALESCE(telegram, '') FROM users WHERE id = $1`, otherID).
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

// CreateLot создаёт заявку на лот: он получает статус pending и попадает в публичный каталог
// только после одобрения администратором. ends_at пока расчётный (создание + длительность);
// настоящий срок аукцион получит в момент старта.
func (q *Queries) CreateLot(ctx context.Context, authorID int64, n NewLot) (model.Lot, error) {
	durSec := int64(math.Round(n.Hours * 3600))
	if durSec < 1 {
		durSec = 1
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Lot{}, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO lots (title, description, image, category_id, subcategory_id, author_id, start_price, step,
		                   status, duration_seconds, ends_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9, clock_timestamp() + make_interval(secs => $10::float8))
		 RETURNING id`,
		n.Title, n.Description, n.Image, n.Category, n.Subcategory, authorID, n.StartPrice, n.Step, durSec, float64(durSec)).Scan(&id)
	if err != nil {
		return model.Lot{}, mapErr(err)
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvLotCreated, UserID: authorID, ActorID: authorID, LotID: id,
		Details: map[string]any{"title": n.Title, "startPrice": n.StartPrice, "step": n.Step, "durationSec": durSec},
	}); err != nil {
		return model.Lot{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Lot{}, err
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
	if _, err := tx.ExecContext(ctx,
		`UPDATE lots SET status = 'cancelled', cancelled_at = clock_timestamp() WHERE id = $1`, lotID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvLotCancelled, UserID: userID, ActorID: userID, LotID: lotID,
	}); err != nil {
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
	lots, err := q.queryLots(ctx, query, viewer, lotID, limit)
	for i := range lots {
		lots[i] = forViewer(lots[i], viewer)
	}
	return lots, err
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
