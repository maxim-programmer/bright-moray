package db

import (
	"auction/model"
	"context"
	"database/sql"
	"time"
)

// ActiveWindowDefault — за какой срок считаются активные пользователи, если период не задан.
const ActiveWindowDefault = 30 * 24 * time.Hour

// Условие «значение col попадает в период»: $1 — начало (включительно), $2 — конец (не включая);
// NULL — граница не задана.
func inRange(col string) string {
	return `($1::timestamptz IS NULL OR ` + col + ` >= $1::timestamptz) AND ($2::timestamptz IS NULL OR ` + col + ` < $2::timestamptz)`
}

func rangeArgs(f model.StatsFilter) (from, to sql.NullTime) {
	if f.From > 0 {
		from = sql.NullTime{Time: time.UnixMilli(f.From), Valid: true}
	}
	if f.To > 0 {
		to = sql.NullTime{Time: time.UnixMilli(f.To), Valid: true}
	}
	return
}

const (
	startedStatuses = `('active', 'finished', 'cancelled')`
	finishedLot     = `(l.status = 'finished' OR (l.status = 'active' AND l.ends_at <= now()))`
)

// GetOverallStats — общая статистика по всем пользователям и аукционам за период.
func (q *Queries) GetOverallStats(ctx context.Context, f model.StatsFilter) (model.OverallStats, error) {
	var s model.OverallStats
	s.From, s.To = f.From, f.To
	from, to := rangeArgs(f)

	// Активные пользователи: без периода — за последние 30 суток.
	actFrom, actTo := from, to
	if !actFrom.Valid {
		actFrom = sql.NullTime{Time: time.Now().Add(-ActiveWindowDefault), Valid: true}
		s.ActiveWindowDefault = !to.Valid
	}
	if !actTo.Valid {
		actTo = sql.NullTime{Time: time.Now().Add(time.Second), Valid: true}
	}

	err := q.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM users WHERE $2::timestamptz IS NULL OR created_at < $2::timestamptz),
		(SELECT COUNT(*) FROM users WHERE `+inRange("created_at")+`),
		(SELECT COUNT(*) FROM users WHERE blocked_at IS NOT NULL),
		(SELECT COUNT(DISTINCT actor_id) FROM events WHERE actor_id IS NOT NULL AND NOT by_admin
			AND created_at >= $3::timestamptz AND created_at < $4::timestamptz),
		(SELECT COUNT(*) FROM lots WHERE `+inRange("created_at")+`),
		(SELECT COUNT(*) FROM lots WHERE status = 'pending'),
		(SELECT COUNT(*) FROM lots WHERE status = 'active' AND ends_at > now()),
		(SELECT COUNT(*) FROM lots l WHERE `+finishedLot+` AND `+inRange("l.ends_at")+`),
		(SELECT COUNT(*) FROM lots WHERE status = 'cancelled' AND `+inRange("COALESCE(cancelled_at, created_at)")+`),
		(SELECT COUNT(*) FROM bids WHERE `+inRange("created_at")+`)`,
		from, to, actFrom, actTo).
		Scan(&s.UsersTotal, &s.UsersRegistered, &s.UsersBlocked, &s.UsersActive,
			&s.LotsCreated, &s.LotsPending, &s.AuctionsActive, &s.AuctionsFinished, &s.AuctionsCancelled, &s.BidsTotal)
	if err != nil {
		return s, err
	}

	// Среднее число ставок на аукцион: по аукционам, начавшимся в период, считаем все их ставки.
	var auctions, bids int
	if err := q.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM((SELECT COUNT(*) FROM bids b WHERE b.lot_id = l.id)), 0)
		FROM lots l WHERE l.status IN `+startedStatuses+` AND `+inRange("l.starts_at"), from, to).
		Scan(&auctions, &bids); err != nil {
		return s, err
	}
	if auctions > 0 {
		s.AvgBidsPerAuction = float64(bids) / float64(auctions)
	}

	// Самые востребованные категории: больше ставок, затем больше лотов.
	s.TopCategories = make([]model.CategoryStat, 0)
	rows, err := q.db.QueryContext(ctx, `
		WITH lc AS (
			SELECT category_id AS cid, COUNT(*) AS n FROM lots
			WHERE status IN `+startedStatuses+` AND `+inRange("created_at")+` GROUP BY 1),
		bc AS (
			SELECT l.category_id AS cid, COUNT(*) AS n FROM bids b JOIN lots l ON l.id = b.lot_id
			WHERE `+inRange("b.created_at")+` GROUP BY 1)
		SELECT c.id, c.name, COALESCE(lc.n, 0), COALESCE(bc.n, 0)
		FROM categories c LEFT JOIN lc ON lc.cid = c.id LEFT JOIN bc ON bc.cid = c.id
		WHERE COALESCE(lc.n, 0) + COALESCE(bc.n, 0) > 0
		ORDER BY COALESCE(bc.n, 0) DESC, COALESCE(lc.n, 0) DESC, c.name LIMIT 10`, from, to)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var c model.CategoryStat
		if err := rows.Scan(&c.ID, &c.Name, &c.Lots, &c.Bids); err != nil {
			rows.Close()
			return s, err
		}
		s.TopCategories = append(s.TopCategories, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, err
	}

	// Самые активные пользователи: лоты + ставки за период.
	s.TopUsers = make([]model.UserActivityStat, 0)
	rows, err = q.db.QueryContext(ctx, `
		WITH lc AS (SELECT author_id AS uid, COUNT(*) AS n FROM lots WHERE `+inRange("created_at")+` GROUP BY 1),
		bc AS (SELECT user_id AS uid, COUNT(*) AS n FROM bids WHERE `+inRange("created_at")+` GROUP BY 1)
		SELECT u.id, u.username, u.name, COALESCE(lc.n, 0), COALESCE(bc.n, 0)
		FROM users u LEFT JOIN lc ON lc.uid = u.id LEFT JOIN bc ON bc.uid = u.id
		WHERE COALESCE(lc.n, 0) + COALESCE(bc.n, 0) > 0
		ORDER BY COALESCE(lc.n, 0) + COALESCE(bc.n, 0) DESC, u.id LIMIT 10`, from, to)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var u model.UserActivityStat
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Lots, &u.Bids); err != nil {
			rows.Close()
			return s, err
		}
		u.Score = u.Lots + u.Bids
		s.TopUsers = append(s.TopUsers, u)
	}
	rows.Close()
	return s, rows.Err()
}

// GetUserPeriodStats — статистика одного пользователя за период.
func (q *Queries) GetUserPeriodStats(ctx context.Context, userID int64, f model.StatsFilter) (model.UserStats, error) {
	s := model.UserStats{UserID: userID, From: f.From, To: f.To}
	from, to := rangeArgs(f)

	var created time.Time
	if err := q.db.QueryRowContext(ctx, `SELECT created_at FROM users WHERE id = $1`, userID).Scan(&created); err != nil {
		return s, mapErr(err)
	}
	s.RegisteredAt = ms(created)

	var last sql.NullTime
	err := q.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM lots WHERE author_id = $3 AND `+inRange("created_at")+`),
		(SELECT COUNT(*) FROM bids WHERE user_id = $3 AND `+inRange("created_at")+`),
		(SELECT COUNT(DISTINCT lot_id) FROM bids WHERE user_id = $3 AND `+inRange("created_at")+`),
		(SELECT COUNT(*) FROM events WHERE (user_id = $3 OR actor_id = $3) AND `+inRange("created_at")+`),
		(SELECT MAX(created_at) FROM events WHERE user_id = $3 OR actor_id = $3)`,
		from, to, userID).
		Scan(&s.LotsCreated, &s.BidsPlaced, &s.Participated, &s.Events, &last)
	if err != nil {
		return s, err
	}
	if last.Valid {
		v := ms(last.Time)
		s.LastActivity = &v
	}

	err = q.db.QueryRowContext(ctx, `
		WITH fin AS (
			SELECT l.author_id, top.user_id AS winner, top.amount
			FROM lots l
			JOIN LATERAL (SELECT user_id, amount FROM bids WHERE lot_id = l.id ORDER BY amount DESC, id ASC LIMIT 1) top ON true
			WHERE `+finishedLot+` AND `+inRange("l.ends_at")+`
		)
		SELECT COUNT(*) FILTER (WHERE winner = $3), COALESCE(SUM(amount) FILTER (WHERE winner = $3), 0),
		       COUNT(*) FILTER (WHERE author_id = $3), COALESCE(SUM(amount) FILTER (WHERE author_id = $3), 0)
		FROM fin`, from, to, userID).
		Scan(&s.Won, &s.SpentTotal, &s.Sold, &s.EarnedTotal)
	return s, err
}
