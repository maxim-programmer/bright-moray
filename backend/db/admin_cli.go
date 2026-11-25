package db

import (
	"auction/model"
	"context"
	"database/sql"
)

// Что сделали команды make admin-create / make admin-delete.
const (
	CLICreated  = "created"  // создан новый пользователь с ролью admin
	CLIPromoted = "promoted" // существующий пользователь стал администратором
	CLIRefresh  = "updated"  // уже был администратором: обновлён пароль
	CLIDemoted  = "demoted"  // роль снята, аккаунт остался обычным пользователем
	CLIDeleted  = "deleted"  // аккаунт, созданный командой, удалён
)

// CLIResult — итог команды администрирования из терминала.
type CLIResult struct {
	Action   string
	UserID   int64
	Username string
	Unblock  bool // у пользователя снята блокировка (администратор не может быть заблокирован)
}

// CLIProfile — необязательные данные нового администратора. Пустые поля не сохраняются
// (имя заменяется значением по умолчанию).
type CLIProfile struct {
	Name     string
	Telegram string // без «@»
}

// CLIUserExists сообщает, есть ли пользователь с таким логином (без учёта регистра).
func (q *Queries) CLIUserExists(ctx context.Context, username string) (bool, error) {
	var ok bool
	err := q.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE lower(username) = lower($1))`, username).Scan(&ok)
	return ok, err
}

// CLIGrantAdmin реализует «make admin-create»: создаёт пользователя с ролью admin или, если
// пользователь с таким логином уже есть, повышает его до администратора. В обоих случаях
// сохраняется переданный хеш пароля; у существующего аккаунта при этом отзываются все сессии,
// а profile не используется (его данные не трогаем).
func (q *Queries) CLIGrantAdmin(ctx context.Context, username, passwordHash string, profile CLIProfile) (CLIResult, error) {
	var res CLIResult
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	var id int64
	var role, stored string
	var blocked bool
	err = tx.QueryRowContext(ctx,
		`SELECT id, role, username, blocked_at IS NOT NULL FROM users WHERE lower(username) = lower($1) FOR UPDATE`, username).
		Scan(&id, &role, &stored, &blocked)
	switch {
	case err == sql.ErrNoRows:
		name := profile.Name
		if name == "" {
			name = "Администратор"
		}
		var taken bool
		if profile.Telegram != "" {
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(telegram) = lower($1))`,
				profile.Telegram).Scan(&taken); err != nil {
				return res, err
			}
			if taken {
				return res, model.Errf(409, "conflict", "Этот Telegram уже занят другим пользователем")
			}
		}
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO users (username, password, name, telegram, role) VALUES ($1, $2, $3, $4, 'admin') RETURNING id`,
			username, passwordHash, name, nullString(profile.Telegram)).Scan(&id); err != nil {
			return res, mapErr(err)
		}
		res = CLIResult{Action: CLICreated, UserID: id, Username: username}
		if err := insertEvent(ctx, tx, EventInput{
			Type: model.EvAdminCLICreated, UserID: id, ByAdmin: true,
			Details: map[string]any{"username": username, "name": name, "telegram": profile.Telegram},
		}); err != nil {
			return res, err
		}
	case err != nil:
		return res, err
	default:
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET password = $2, role = 'admin', blocked_at = NULL, blocked_reason = NULL, blocked_by = NULL
			WHERE id = $1`, id, passwordHash); err != nil {
			return res, mapErr(err)
		}
		// пароль изменён: украденные сессии не должны его пережить
		if _, err := tx.ExecContext(ctx, `
			UPDATE refresh_tokens SET revoked_at = clock_timestamp(), revoked_reason = $2
			WHERE user_id = $1 AND revoked_at IS NULL`, id, RevokePasswordChange); err != nil {
			return res, err
		}
		res = CLIResult{Action: CLIPromoted, UserID: id, Username: stored, Unblock: blocked}
		if role == model.RoleAdmin {
			res.Action = CLIRefresh
		}
		if err := insertEvent(ctx, tx, EventInput{
			Type: model.EvAdminCLIPromoted, UserID: id, ByAdmin: true,
			Details: map[string]any{"username": stored, "previousRole": role, "unblocked": blocked},
		}); err != nil {
			return res, err
		}
	}
	return res, tx.Commit()
}

// CLIRevokeAdmin реализует «make admin-delete» — откат admin-create. Аккаунт, который создала
// команда и которым не пользовались как обычным (нет лотов и ставок), удаляется целиком; во всех
// остальных случаях у пользователя снимается только роль admin.
func (q *Queries) CLIRevokeAdmin(ctx context.Context, username string) (CLIResult, error) {
	var res CLIResult
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	var id int64
	var role, stored string
	err = tx.QueryRowContext(ctx,
		`SELECT id, role, username FROM users WHERE lower(username) = lower($1) FOR UPDATE`, username).
		Scan(&id, &role, &stored)
	if err != nil {
		return res, mapErr(err)
	}
	if role != model.RoleAdmin {
		return res, model.Errf(409, "not_admin", "Пользователь %q не является администратором", stored)
	}

	var createdByCLI bool
	var lots, bids int
	if err := tx.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM events WHERE type = $2 AND user_id = $1),
		(SELECT COUNT(*) FROM lots WHERE author_id = $1),
		(SELECT COUNT(*) FROM bids WHERE user_id = $1)`, id, model.EvAdminCLICreated).
		Scan(&createdByCLI, &lots, &bids); err != nil {
		return res, err
	}

	if createdByCLI && lots == 0 && bids == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
			return res, mapErr(err)
		}
		res = CLIResult{Action: CLIDeleted, UserID: id, Username: stored}
		if err := insertEvent(ctx, tx, EventInput{
			Type: model.EvAdminCLIDeleted, ByAdmin: true, Details: map[string]any{"username": stored, "userId": id},
		}); err != nil {
			return res, err
		}
		return res, tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = 'user' WHERE id = $1`, id); err != nil {
		return res, mapErr(err)
	}
	res = CLIResult{Action: CLIDemoted, UserID: id, Username: stored}
	if err := insertEvent(ctx, tx, EventInput{
		Type: model.EvAdminCLIDemoted, UserID: id, ByAdmin: true, Details: map[string]any{"username": stored},
	}); err != nil {
		return res, err
	}
	return res, tx.Commit()
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
