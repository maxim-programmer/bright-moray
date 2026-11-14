package integration

import (
	"testing"
)

// Ограничения целостности таблицы refresh_tokens должны держаться на уровне БД,
// а не только в коде приложения.
func TestRefreshTokensConstraints(t *testing.T) {
	e := newEnv(t, strict)
	e.newDevice().register()

	hash := func(b byte) []byte {
		h := make([]byte, 32)
		h[0] = b
		return h
	}
	const insert = `INSERT INTO refresh_tokens (user_id, family_id, token_hash, created_at, expires_at, family_expires_at)
		VALUES (1, gen_random_uuid(), $1, now(), now() + $2::interval, now() + $3::interval)`

	if _, err := e.sql.Exec(insert, hash(1), "1 day", "2 days"); err != nil {
		t.Fatalf("корректная строка отклонена: %v", err)
	}
	cases := []struct {
		name string
		exec func() error
	}{
		{"дубликат хеша", func() error { _, err := e.sql.Exec(insert, hash(1), "1 day", "2 days"); return err }},
		{"хеш не 32 байта", func() error { _, err := e.sql.Exec(insert, []byte("short"), "1 day", "2 days"); return err }},
		{"expires_at позже предела семейства", func() error { _, err := e.sql.Exec(insert, hash(2), "3 days", "2 days"); return err }},
		{"expires_at не позже created_at", func() error { _, err := e.sql.Exec(insert, hash(3), "-1 day", "2 days"); return err }},
		{"несуществующий пользователь", func() error {
			_, err := e.sql.Exec(`INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at, family_expires_at)
				VALUES (999999, gen_random_uuid(), $1, now() + interval '1 day', now() + interval '2 days')`, hash(4))
			return err
		}},
		{"replaced_by без used_at", func() error {
			_, err := e.sql.Exec(`UPDATE refresh_tokens SET replaced_by = id WHERE token_hash = $1`, hash(1))
			return err
		}},
		{"revoked_at без причины", func() error {
			_, err := e.sql.Exec(`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1`, hash(1))
			return err
		}},
		{"неизвестная причина отзыва", func() error {
			_, err := e.sql.Exec(`UPDATE refresh_tokens SET revoked_at = now(), revoked_reason = 'because' WHERE token_hash = $1`, hash(1))
			return err
		}},
	}
	for _, c := range cases {
		if c.exec() == nil {
			t.Errorf("%s: БД приняла некорректные данные", c.name)
		}
	}
}

func TestDeletingUserRemovesSessions(t *testing.T) {
	e := newEnv(t, strict)
	u := e.newDevice().register()
	if e.count(`SELECT count(*) FROM refresh_tokens WHERE user_id = $1`, u.ID) == 0 {
		t.Fatal("нет сессии")
	}
	if _, err := e.sql.Exec(`DELETE FROM users WHERE id = $1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens`); n != 0 {
		t.Fatalf("после удаления пользователя осталось сессий: %d", n)
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	e := newEnv(t, strict)
	if err := e.q.Migrate(contextBackground()); err != nil {
		t.Fatalf("повторное применение миграций: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM schema_migrations WHERE name = '004_auth_sessions.sql'`); n != 1 {
		t.Fatalf("миграция 004 записана %d раз", n)
	}
}
