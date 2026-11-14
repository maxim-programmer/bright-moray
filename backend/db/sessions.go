package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Ошибки refresh-токенов. Вызывающий код различает их, чтобы выбрать ответ и реакцию.
var (
	// ErrRefreshInvalid — токен неизвестен, просрочен или его семейство уже отозвано.
	ErrRefreshInvalid = errors.New("refresh token invalid")
	// ErrRefreshReused — предъявлен уже использованный токен; всё семейство отозвано.
	ErrRefreshReused = errors.New("refresh token reused")
	// ErrRefreshRace — токен только что был обновлён параллельным запросом (в пределах
	// допустимого окна). Ничего не изменено и не отозвано; клиент должен повторить запрос
	// уже с новым cookie.
	ErrRefreshRace = errors.New("refresh token rotated concurrently")
)

// Причины отзыва (совпадают с CHECK в миграции 004).
const (
	RevokeLogout         = "logout"
	RevokeLogoutAll      = "logout_all"
	RevokePasswordChange = "password_change"
	RevokeReuse          = "reuse_detected"
	RevokeRelogin        = "relogin"
)

// SessionToken — только что выпущенный refresh-токен (его хеш уже в БД).
type SessionToken struct {
	UserID int64
	// TTL — сколько токен ещё может бездействовать, считая по часам БД.
	TTL time.Duration
}

func secs(d time.Duration) float64 { return d.Seconds() }

// CreateSession начинает новую сессию (новое семейство токенов) и сохраняет хеш refresh-токена.
// Срок токена — idle, но не позже maxAge с момента входа.
func (q *Queries) CreateSession(ctx context.Context, userID int64, tokenHash []byte, idle, maxAge time.Duration) (SessionToken, error) {
	var ttl float64
	err := q.db.QueryRowContext(ctx, `
		WITH t AS (SELECT clock_timestamp() AS now)
		INSERT INTO refresh_tokens (user_id, family_id, token_hash, created_at, expires_at, family_expires_at)
		SELECT $1, gen_random_uuid(), $2, t.now,
		       LEAST(t.now + make_interval(secs => $3::float8), t.now + make_interval(secs => $4::float8)),
		       t.now + make_interval(secs => $4::float8)
		FROM t
		RETURNING EXTRACT(EPOCH FROM (expires_at - created_at))::float8`,
		userID, tokenHash, secs(idle), secs(maxAge)).Scan(&ttl)
	if err != nil {
		return SessionToken{}, mapErr(err)
	}
	return SessionToken{UserID: userID, TTL: time.Duration(ttl * float64(time.Second))}, nil
}

// RotateRefreshToken атомарно заменяет предъявленный refresh-токен новым.
//
// Всё выполняется в одной транзакции под блокировкой строки токена (SELECT … FOR UPDATE),
// поэтому два параллельных обновления одного токена не могут оба успеть: второе дождётся
// первого и увидит токен уже использованным.
//
//   - неизвестный, просроченный или отозванный токен → ErrRefreshInvalid;
//   - уже использованный токен → всё семейство отзывается (и это сохраняется, транзакция
//     фиксируется), результат ErrRefreshReused; в SessionToken.UserID — владелец, для лога;
//   - уже использованный токен в пределах reuseGap после использования → ErrRefreshRace
//     без побочных эффектов (две вкладки обновились одновременно).
//
// Срок нового токена — idle от сейчас, но не дальше предела семейства.
func (q *Queries) RotateRefreshToken(ctx context.Context, oldHash, newHash []byte, idle, reuseGap time.Duration) (SessionToken, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionToken{}, err
	}
	defer tx.Rollback() // после Commit — no-op

	var (
		id, userID             int64
		family                 string
		expires, familyExpires time.Time
		now                    time.Time
		used, revoked          sql.NullTime
		sinceUsed              sql.NullFloat64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, user_id, family_id, expires_at, family_expires_at, used_at, revoked_at,
		       clock_timestamp(), EXTRACT(EPOCH FROM (clock_timestamp() - used_at))::float8
		FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, oldHash).
		Scan(&id, &userID, &family, &expires, &familyExpires, &used, &revoked, &now, &sinceUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionToken{}, ErrRefreshInvalid
	}
	if err != nil {
		return SessionToken{}, err
	}

	switch {
	case revoked.Valid:
		return SessionToken{}, ErrRefreshInvalid

	case used.Valid:
		if reuseGap > 0 && sinceUsed.Valid && time.Duration(sinceUsed.Float64*float64(time.Second)) <= reuseGap {
			return SessionToken{UserID: userID}, ErrRefreshRace
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE refresh_tokens SET revoked_at = clock_timestamp(), revoked_reason = $2
			WHERE family_id = $1 AND revoked_at IS NULL`, family, RevokeReuse); err != nil {
			return SessionToken{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionToken{}, err
		}
		return SessionToken{UserID: userID}, ErrRefreshReused

	case !now.Before(expires) || !now.Before(familyExpires):
		return SessionToken{}, ErrRefreshInvalid
	}

	var newID int64
	var ttl float64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO refresh_tokens (user_id, family_id, token_hash, created_at, expires_at, family_expires_at)
		VALUES ($1, $2, $3, clock_timestamp(),
		        LEAST(clock_timestamp() + make_interval(secs => $4::float8), $5), $5)
		RETURNING id, EXTRACT(EPOCH FROM (expires_at - created_at))::float8`,
		userID, family, newHash, secs(idle), familyExpires).Scan(&newID, &ttl)
	if err != nil {
		return SessionToken{}, mapErr(err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE refresh_tokens SET used_at = clock_timestamp(), replaced_by = $2 WHERE id = $1`, id, newID); err != nil {
		return SessionToken{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionToken{}, err
	}
	return SessionToken{UserID: userID, TTL: time.Duration(ttl * float64(time.Second))}, nil
}

// RevokeSessionByRefreshHash отзывает всё семейство, которому принадлежит токен
// (в любом его состоянии). Неизвестный токен — не ошибка.
func (q *Queries) RevokeSessionByRefreshHash(ctx context.Context, tokenHash []byte, reason string) error {
	_, err := q.db.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = clock_timestamp(), revoked_reason = $2
		WHERE revoked_at IS NULL
		  AND family_id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)`,
		tokenHash, reason)
	return err
}

// RevokeUserSessions отзывает все сессии пользователя и возвращает число отозванных токенов.
func (q *Queries) RevokeUserSessions(ctx context.Context, userID int64, reason string) (int64, error) {
	res, err := q.db.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = clock_timestamp(), revoked_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL`, userID, reason)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UserIDByActiveRefreshHash возвращает владельца действующего (не использованного,
// не отозванного, не просроченного) refresh-токена. Иначе — ErrNotFound.
func (q *Queries) UserIDByActiveRefreshHash(ctx context.Context, tokenHash []byte) (int64, error) {
	var uid int64
	err := q.db.QueryRowContext(ctx, `
		SELECT user_id FROM refresh_tokens
		WHERE token_hash = $1 AND used_at IS NULL AND revoked_at IS NULL
		  AND expires_at > clock_timestamp() AND family_expires_at > clock_timestamp()`, tokenHash).Scan(&uid)
	return uid, mapErr(err)
}

// DeleteExpiredSessions удаляет семейства, чей предел жизни истёк больше retention назад.
// Пока семейство могло бы быть живо, все его токены (включая использованные) остаются —
// по ним обнаруживается повторное использование. Возвращает число удалённых токенов.
func (q *Queries) DeleteExpiredSessions(ctx context.Context, retention time.Duration) (int64, error) {
	res, err := q.db.ExecContext(ctx, `
		DELETE FROM refresh_tokens
		WHERE family_expires_at < clock_timestamp() - make_interval(secs => $1::float8)`, secs(retention))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
