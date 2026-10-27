package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"auction/config"
	"auction/db"
	"auction/handler"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Конфигурация: %v", err)
	}

	sqlDB, err := openDB(cfg.DSN())
	if err != nil {
		log.Fatalf("База данных: %v", err)
	}
	defer sqlDB.Close()

	q := db.New(sqlDB)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := q.Migrate(ctx); err != nil {
		cancel()
		log.Fatalf("Миграции: %v", err)
	}
	cancel()

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		log.Fatalf("Папка загрузок: %v", err)
	}

	mux := http.NewServeMux()
	handler.SetupRoutes(mux, handler.New(q, cfg))

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.Middleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Println("HTTP-сервер запущен на порту", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP-сервер: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Остановка сервера: %v", err)
	}
	log.Println("Сервер остановлен")
}

// openDB открывает пул и ждёт, пока база начнёт отвечать (на старте контейнеров она может отставать).
func openDB(dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("открытие соединения: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(time.Hour)

	var lastErr error
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		lastErr = sqlDB.PingContext(ctx)
		cancel()
		if lastErr == nil {
			return sqlDB, nil
		}
		time.Sleep(time.Second)
	}
	sqlDB.Close()
	return nil, fmt.Errorf("база недоступна: %w", lastErr)
}
