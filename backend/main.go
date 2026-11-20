package main

import (
	"auction/config"
	"auction/db"
	"auction/handler"
	"auction/logging"
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
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Логгер строится из конфигурации, поэтому ошибки самого конфига — стандартным log.
		log.Fatalf("Конфигурация: %v", err)
	}

	logger, closeLog, err := logging.New(logging.Config{Level: cfg.LogLevel, File: cfg.LogFile})
	if err != nil {
		log.Fatalf("Логирование: %v", err)
	}
	defer closeLog()
	logging.SetDefault(logger) // его используют Middleware и всё, что не получило логгер явно
	lg := logger.With(zap.String("component", logging.ComponentMain))
	lg.Info("логирование настроено", zap.String("log_level", cfg.LogLevel), zap.String("log_file", cfg.LogFile))

	sqlDB, err := openDB(cfg.DSN())
	if err != nil {
		lg.Fatal("база данных", zap.Error(err))
	}
	defer sqlDB.Close()

	q := db.New(sqlDB)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := q.Migrate(ctx); err != nil {
		cancel()
		lg.Fatal("миграции", zap.Error(err))
	}
	cancel()

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		lg.Fatal("папка загрузок", zap.Error(err))
	}

	h := handler.New(q, cfg, handler.WithLogger(logger))
	liveCtx, liveCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := h.StartLive(liveCtx); err != nil {
		liveCancel()
		lg.Fatal("реалтайм", zap.Error(err))
	}
	liveCancel()

	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	defer stopCleanup()
	h.StartSessionCleanup(cleanupCtx)

	mux := http.NewServeMux()
	handler.SetupRoutes(mux, h)

	// собственные сообщения net/http (например, «superfluous WriteHeader») — в тот же лог
	httpErrLog, err := zap.NewStdLogAt(logger.With(zap.String("component", logging.ComponentServer)), zapcore.WarnLevel)
	if err != nil {
		lg.Fatal("лог HTTP-сервера", zap.Error(err))
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.Middleware(h.Guard(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          httpErrLog,
	}

	go func() {
		lg.Info("HTTP-сервер запущен", zap.String("port", cfg.Port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			lg.Fatal("HTTP-сервер", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	lg.Info("получен сигнал остановки", zap.String("signal", sig.String()))

	stopCleanup()
	// WebSocket-соединения «угнаны» у net/http и Shutdown их не видит — закрываем сами
	h.StopLive()

	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		lg.Error("остановка сервера", zap.Error(err))
	}
	lg.Info("сервер остановлен")
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
