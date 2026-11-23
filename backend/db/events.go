package db

import (
	"auction/model"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// execer — то, что умеют и *sql.DB, и *sql.Tx: событие можно записать как отдельным
// запросом, так и в той же транзакции, что и само действие.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// EventInput — новая запись журнала. Нулевые идентификаторы сохраняются как NULL.
type EventInput struct {
	Type    string
	UserID  int64 // о ком событие
	ActorID int64 // кто совершил действие
	LotID   int64
	ByAdmin bool
	Details map[string]any
}

func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func insertEvent(ctx context.Context, ex execer, e EventInput) error {
	details := "{}"
	if len(e.Details) > 0 {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return fmt.Errorf("event details: %w", err)
		}
		details = string(b)
	}
	_, err := ex.ExecContext(ctx,
		`INSERT INTO events (type, user_id, actor_id, by_admin, lot_id, details)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		e.Type, nullID(e.UserID), nullID(e.ActorID), e.ByAdmin, nullID(e.LotID), details)
	return mapErr(err)
}

// LogEvent пишет событие в журнал отдельным запросом.
func (q *Queries) LogEvent(ctx context.Context, e EventInput) error {
	return insertEvent(ctx, q.db, e)
}

func eventWhere(f model.EventFilter) (string, []any) {
	var conds []string
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.UserID > 0 {
		p := next(f.UserID)
		conds = append(conds, "(e.user_id = "+p+" OR e.actor_id = "+p+")")
	}
	if f.LotID > 0 {
		conds = append(conds, "e.lot_id = "+next(f.LotID))
	}
	if t := strings.TrimSpace(f.Type); t != "" {
		if strings.HasSuffix(t, ".") { // группа событий: «lot.» — все события лотов
			r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
			conds = append(conds, "e.type LIKE "+next(r.Replace(t)+"%"))
		} else {
			conds = append(conds, "e.type = "+next(t))
		}
	}
	for _, t := range f.ExcludeTypes {
		conds = append(conds, "e.type <> "+next(t))
	}
	if f.ByAdmin {
		conds = append(conds, "e.by_admin")
	}
	if f.From > 0 {
		conds = append(conds, "e.created_at >= "+next(time.UnixMilli(f.From)))
	}
	if f.To > 0 {
		conds = append(conds, "e.created_at < "+next(time.UnixMilli(f.To)))
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListEvents возвращает страницу журнала (новые сверху) и общее число записей по фильтру.
func (q *Queries) ListEvents(ctx context.Context, f model.EventFilter, limit, offset int) ([]model.Event, int, error) {
	where, args := eventWhere(f)
	var total int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events e`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT e.id, e.created_at, e.type, e.user_id, COALESCE(u.username, ''), e.actor_id,
		COALESCE(a.username, ''), e.by_admin, e.lot_id, e.details
		FROM events e
		LEFT JOIN users u ON u.id = e.user_id
		LEFT JOIN users a ON a.id = e.actor_id` + where +
		fmt.Sprintf(" ORDER BY e.created_at DESC, e.id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	events := make([]model.Event, 0)
	for rows.Next() {
		var e model.Event
		var created time.Time
		var userID, actorID, lotID sql.NullInt64
		var details []byte
		if err := rows.Scan(&e.ID, &created, &e.Type, &userID, &e.Username, &actorID,
			&e.ActorUsername, &e.ByAdmin, &lotID, &details); err != nil {
			return nil, 0, err
		}
		e.CreatedAt = ms(created)
		e.Label = model.EventLabel(e.Type)
		if userID.Valid {
			v := userID.Int64
			e.UserID = &v
		}
		if actorID.Valid {
			v := actorID.Int64
			e.ActorID = &v
		}
		if lotID.Valid {
			v := lotID.Int64
			e.LotID = &v
		}
		e.Details = json.RawMessage(details)
		if len(e.Details) == 0 {
			e.Details = json.RawMessage("{}")
		}
		events = append(events, e)
	}
	return events, total, rows.Err()
}
