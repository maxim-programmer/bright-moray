package db

import (
	"auction/model"
	"context"
	"time"
)

// GetLotLive возвращает публичное состояние лота (без данных, зависящих от зрителя).
// Считается теми же выражениями, что и обычный Lot, поэтому цена, minBid и статус
// всегда совпадают с REST.
func (q *Queries) GetLotLive(ctx context.Context, id int64) (model.LotLive, error) {
	l, err := scanLot(q.db.QueryRowContext(ctx, lotSelect+" WHERE l.id = $2", int64(0), id))
	if err != nil {
		return model.LotLive{}, mapErr(err)
	}
	return l.Live(), nil
}

// LotRemaining возвращает «сырой» статус лота в БД и сколько осталось до конца
// по часам БД (≤ 0 — срок вышел). Нужен планировщику завершения аукционов.
func (q *Queries) LotRemaining(ctx context.Context, id int64) (string, time.Duration, error) {
	var status string
	var secs float64
	err := q.db.QueryRowContext(ctx,
		`SELECT status, EXTRACT(EPOCH FROM (ends_at - clock_timestamp()))::float8 FROM lots WHERE id = $1`, id).
		Scan(&status, &secs)
	if err != nil {
		return "", 0, mapErr(err)
	}
	return status, time.Duration(secs * float64(time.Second)), nil
}

// LotEnd — активный лот и время до его окончания.
type LotEnd struct {
	ID   int64
	Left time.Duration
}

// ActiveLotEnds возвращает все активные лоты, срок которых ещё не вышел.
func (q *Queries) ActiveLotEnds(ctx context.Context) ([]LotEnd, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, EXTRACT(EPOCH FROM (ends_at - clock_timestamp()))::float8
		 FROM lots WHERE status = 'active' AND ends_at > clock_timestamp()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LotEnd
	for rows.Next() {
		var e LotEnd
		var secs float64
		if err := rows.Scan(&e.ID, &secs); err != nil {
			return nil, err
		}
		e.Left = time.Duration(secs * float64(time.Second))
		out = append(out, e)
	}
	return out, rows.Err()
}
