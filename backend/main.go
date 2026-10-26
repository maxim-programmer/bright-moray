package main

import (
	"bright-moray/config"
	"bright-moray/db"
	"bright-moray/handler"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := config.Load()

	sqlDB, err := newPool(cfg.DSN())
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("Unable to reach database: %v", err)
	}
	defer sqlDB.Close()

	q := db.New(sqlDB)
	h := handler.New(q)

	mux := http.NewServeMux()
	handler.SetupRoutes(mux, h)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	go func() {
		fmt.Println("Запуск HTTP сервера на порту", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)

	fmt.Println("Сервер остановлен")
}

func newPool(dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to open connection: %w", err)
	}

	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return sqlDB, nil
}
