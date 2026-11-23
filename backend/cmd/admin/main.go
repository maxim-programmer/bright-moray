// Команда admin — управление администраторами из терминала.
//
//	admin create          создать администратора или повысить существующего пользователя
//	admin delete [--yes]  откатить: удалить созданного администратора или снять роль
//
// Обычно запускается через Makefile: make admin-create, make admin-delete.
package main

import (
	"auction/admincli"
	"auction/config"
	"auction/db"
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	cmd, yes := args[0], false
	for _, a := range args[1:] {
		if a == "--yes" || a == "-y" {
			yes = true
			continue
		}
		return usage()
	}
	if cmd != "create" && cmd != "delete" {
		return usage()
	}

	q, closeDB, err := connect(config.LoadDatabase().DSN())
	if err != nil {
		return err
	}
	defer closeDB()

	// Схема обновляется той же функцией, что и при запуске сервера (с блокировкой), поэтому
	// команда работает, даже если сервер с новой версией ещё не запускали.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := q.Migrate(ctx); err != nil {
		return fmt.Errorf("миграции: %w", err)
	}

	c := admincli.NewConsole()
	if cmd == "create" {
		return admincli.Create(ctx, q, c)
	}
	return admincli.Delete(ctx, q, c, yes)
}

func usage() error {
	return fmt.Errorf("использование: admin create | admin delete [--yes]")
}

func connect(dsn string) (*db.Queries, func(), error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("подключение к БД: %w", err)
	}
	var last error
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		last = sqlDB.PingContext(ctx)
		cancel()
		if last == nil {
			return db.New(sqlDB), func() { sqlDB.Close() }, nil
		}
		time.Sleep(time.Second)
	}
	sqlDB.Close()
	return nil, nil, fmt.Errorf("база данных недоступна: %w", last)
}
