package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"auction/migrations"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")      // нарушение уникальности
	ErrBadRef   = errors.New("bad reference") // нарушение внешнего ключа
)

type Queries struct {
	db *sql.DB
}

func New(db *sql.DB) *Queries { return &Queries{db: db} }

// mapErr переводит коды ошибок Postgres в доменные ошибки.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var s interface{ SQLState() string }
	if errors.As(err, &s) {
		switch s.SQLState() {
		case "23505":
			return ErrConflict
		case "23503":
			return ErrBadRef
		}
	}
	return err
}

func ms(t time.Time) int64 { return t.UnixMilli() }

// Migrate применяет ещё не применённые *.sql из папки migrations по порядку имён.
func (q *Queries) Migrate(ctx context.Context) error {
	conn, err := q.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(727274)`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(727274)`)

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var done bool
		if err := conn.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("миграция %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queries) Ping(ctx context.Context) error { return q.db.PingContext(ctx) }
