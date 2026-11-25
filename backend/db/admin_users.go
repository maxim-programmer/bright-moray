package db

import (
	"auction/model"
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UserAccess возвращает роль пользователя и признак блокировки (ErrNotFound — пользователя нет).
func (q *Queries) UserAccess(ctx context.Context, id int64) (role string, blocked bool, err error) {
	err = q.db.QueryRowContext(ctx,
		`SELECT role, blocked_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&role, &blocked)
	return role, blocked, mapErr(err)
}

const adminUserSelect = `SELECT u.id, u.username, u.name, COALESCE(u.telegram, ''), COALESCE(u.dorm_id, 0), u.created_at,
	u.role, u.blocked_at, COALESCE(u.blocked_reason, ''), u.blocked_by,
	(SELECT COUNT(*) FROM lots WHERE author_id = u.id),
	(SELECT COUNT(*) FROM bids WHERE user_id = u.id),
	(SELECT MAX(e.created_at) FROM events e WHERE e.user_id = u.id OR e.actor_id = u.id)
	FROM users u`

func scanAdminUser(row interface{ Scan(...any) error }) (model.AdminUser, error) {
	var u model.AdminUser
	var created time.Time
	var blockedAt, last sql.NullTime
	var blockedBy sql.NullInt64
	err := row.Scan(&u.ID, &u.Username, &u.Name, &u.Telegram, &u.DormID, &created,
		&u.Role, &blockedAt, &u.BlockedReason, &blockedBy, &u.LotsCount, &u.BidsCount, &last)
	if err != nil {
		return u, err
	}
	u.CreatedAt = ms(created)
	u.IsBlocked = blockedAt.Valid
	u.Blocked = blockedAt.Valid
	if blockedAt.Valid {
		v := ms(blockedAt.Time)
		u.BlockedAt = &v
	}
	if blockedBy.Valid {
		v := blockedBy.Int64
		u.BlockedBy = &v
	}
	if last.Valid {
		v := ms(last.Time)
		u.LastActivity = &v
	}
	return u, nil
}

func userWhere(f model.UserFilter) (string, []any) {
	var conds []string
	var args []any
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
		p := next("%" + r.Replace(q) + "%")
		cond := fmt.Sprintf("(u.username ILIKE %s OR u.name ILIKE %s OR u.telegram ILIKE %s", p, p, p)
		if id, err := strconv.ParseInt(q, 10, 64); err == nil && id > 0 {
			cond += " OR u.id = " + next(id)
		}
		conds = append(conds, cond+")")
	}
	switch f.Role {
	case model.RoleUser, model.RoleAdmin:
		conds = append(conds, "u.role = "+next(f.Role))
	}
	switch f.Blocked {
	case "true":
		conds = append(conds, "u.blocked_at IS NOT NULL")
	case "false":
		conds = append(conds, "u.blocked_at IS NULL")
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListUsers возвращает страницу пользователей (новые сверху) и общее число по фильтру.
func (q *Queries) ListUsers(ctx context.Context, f model.UserFilter, limit, offset int) ([]model.AdminUser, int, error) {
	where, args := userWhere(f)
	var total int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := adminUserSelect + where +
		fmt.Sprintf(" ORDER BY u.id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := make([]model.AdminUser, 0)
	for rows.Next() {
		u, err := scanAdminUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

func (q *Queries) GetAdminUser(ctx context.Context, id int64) (model.AdminUser, error) {
	u, err := scanAdminUser(q.db.QueryRowContext(ctx, adminUserSelect+` WHERE u.id = $1`, id))
	return u, mapErr(err)
}

// BlockUser блокирует пользователя и отзывает все его сессии. Администратора заблокировать нельзя.
func (q *Queries) BlockUser(ctx context.Context, adminID, userID int64, reason string) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var role string
	var blocked bool
	err = tx.QueryRowContext(ctx,
		`SELECT role, blocked_at IS NOT NULL FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&role, &blocked)
	if err != nil {
		return mapErr(err)
	}
	switch {
	case role == model.RoleAdmin:
		return model.Errf(409, "forbidden_target", "Администратора заблокировать нельзя")
	case blocked:
		return model.Errf(409, "already_blocked", "Пользователь уже заблокирован")
	}
	r := sql.NullString{String: reason, Valid: reason != ""}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET blocked_at = now(), blocked_reason = $2, blocked_by = $3 WHERE id = $1`,
		userID, r, adminID); err != nil {
		return mapErr(err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = clock_timestamp(), revoked_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL`, userID, RevokeBlocked); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvUserBlocked, UserID: userID, ActorID: adminID, ByAdmin: true,
		Details: map[string]any{"reason": reason},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// UnblockUser снимает блокировку.
func (q *Queries) UnblockUser(ctx context.Context, adminID, userID int64) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var blocked bool
	err = tx.QueryRowContext(ctx,
		`SELECT blocked_at IS NOT NULL FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&blocked)
	if err != nil {
		return mapErr(err)
	}
	if !blocked {
		return model.Errf(409, "not_blocked", "Пользователь не заблокирован")
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET blocked_at = NULL, blocked_reason = NULL, blocked_by = NULL WHERE id = $1`, userID); err != nil {
		return mapErr(err)
	}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvUserUnblocked, UserID: userID, ActorID: adminID, ByAdmin: true,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// UserBids — история ставок пользователя (новые сверху) и общее число ставок.
func (q *Queries) UserBids(ctx context.Context, userID int64, limit, offset int) ([]model.UserBid, int, error) {
	var exists bool
	if err := q.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, ErrNotFound
	}
	var total int
	if err := q.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bids WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.db.QueryContext(ctx, `
		SELECT b.id, b.lot_id, l.title,
		       (CASE WHEN l.status = 'active' AND l.ends_at <= now() THEN 'finished' ELSE l.status END),
		       b.amount, b.created_at,
		       b.id = (SELECT id FROM bids WHERE lot_id = l.id ORDER BY amount DESC, id ASC LIMIT 1)
		FROM bids b JOIN lots l ON l.id = b.lot_id
		WHERE b.user_id = $1
		ORDER BY b.created_at DESC, b.id DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]model.UserBid, 0)
	for rows.Next() {
		var b model.UserBid
		var created time.Time
		if err := rows.Scan(&b.ID, &b.LotID, &b.LotTitle, &b.LotStatus, &b.Amount, &created, &b.Leading); err != nil {
			return nil, 0, err
		}
		b.CreatedAt = ms(created)
		out = append(out, b)
	}
	return out, total, rows.Err()
}

func itoa(n int) string { return strconv.Itoa(n) }
