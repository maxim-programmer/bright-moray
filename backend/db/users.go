package db

import (
	"context"
	"time"

	"auction/model"
)

const userCols = `id, username, password, name, telegram, dorm_id, created_at`

func scanUser(row interface{ Scan(...any) error }) (model.User, error) {
	var u model.User
	var created time.Time
	err := row.Scan(&u.ID, &u.Username, &u.Password, &u.Name, &u.Telegram, &u.DormID, &created)
	u.CreatedAt = ms(created)
	return u, mapErr(err)
}

func (q *Queries) CreateUser(ctx context.Context, username, passwordHash, name, telegram string, dormID int16) (model.User, error) {
	return scanUser(q.db.QueryRowContext(ctx,
		`INSERT INTO users (username, password, name, telegram, dorm_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING `+userCols,
		username, passwordHash, name, telegram, dormID))
}

// GetUserByUsername ищет без учёта регистра.
func (q *Queries) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	return scanUser(q.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE lower(username) = lower($1)`, username))
}

func (q *Queries) GetUserByID(ctx context.Context, id int64) (model.User, error) {
	return scanUser(q.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (q *Queries) UpdateProfile(ctx context.Context, id int64, name, telegram string, dormID int16) (model.User, error) {
	return scanUser(q.db.QueryRowContext(ctx,
		`UPDATE users SET name = $1, telegram = $2, dorm_id = $3 WHERE id = $4 RETURNING `+userCols,
		name, telegram, dormID, id))
}

func (q *Queries) ChangeUsername(ctx context.Context, id int64, newUsername string) (model.User, error) {
	return scanUser(q.db.QueryRowContext(ctx,
		`UPDATE users SET username = $1 WHERE id = $2 RETURNING `+userCols, newUsername, id))
}

func (q *Queries) ChangePassword(ctx context.Context, id int64, passwordHash string) error {
	res, err := q.db.ExecContext(ctx, `UPDATE users SET password = $1 WHERE id = $2`, passwordHash, id)
	if err != nil {
		return mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
