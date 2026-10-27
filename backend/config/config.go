package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	DBHost       string
	DBPort       string
	DBName       string
	DBUser       string
	DBPassword   string
	Port         string
	JWTSecret    string // секрет подписи сессионных токенов
	UploadDir    string // куда сохраняются загруженные картинки
	CookieSecure bool   // true, если сайт отдаётся по HTTPS
}

func Load() (Config, error) {
	cfg := Config{
		DBHost:       env("DB_HOST", "localhost"),
		DBPort:       env("DB_PORT", "5432"),
		DBName:       env("DB_NAME", "auction"),
		DBUser:       env("DB_USER", "auction_user"),
		DBPassword:   env("DB_PASSWORD", "auction_pass"),
		Port:         env("PORT", "8080"),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		UploadDir:    env("UPLOAD_DIR", "./uploads"),
		CookieSecure: strings.EqualFold(os.Getenv("COOKIE_SECURE"), "true"),
	}
	if len(cfg.JWTSecret) < 16 {
		return cfg, errors.New("JWT_SECRET должен быть задан и содержать не менее 16 символов")
	}
	return cfg, nil
}

func (c Config) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
