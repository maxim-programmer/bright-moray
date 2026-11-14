package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBHost       string
	DBPort       string
	DBName       string
	DBUser       string
	DBPassword   string
	Port         string
	JWTSecret    string // секрет подписи access-токенов (HS256)
	JWTIssuer    string // claim iss access-токена
	JWTAudience  string // claim aud access-токена
	UploadDir    string // куда сохраняются загруженные картинки
	CookieSecure bool   // true, если сайт отдаётся по HTTPS

	// Anti-snipe: ставка, сделанная меньше чем за AntiSnipeWindow до конца аукциона,
	// продлевает его так, чтобы до конца оставалось не меньше AntiSnipeExtend.
	// AntiSnipeWindow = 0 выключает защиту.
	AntiSnipeWindow time.Duration
	AntiSnipeExtend time.Duration

	// Дополнительные Origin (кроме совпадающего с Host запроса), которым разрешено
	// открывать WebSocket. Нужен, например, если фронтенд раздаётся с другого домена.
	WSAllowedOrigins []string

	// Origin фронтенда, которым разрешены кросс-доменные запросы с cookie (CORS с credentials)
	// и запросы, изменяющие состояние. Свой Host разрешён всегда. Символ «*» недопустим.
	AllowedOrigins []string

	// Сессии. Нулевые значения заменяются значениями по умолчанию (ApplyDefaults).
	AccessTokenTTL  time.Duration // время жизни access-токена
	RefreshIdleTTL  time.Duration // refresh-токен без использования живёт столько
	SessionMaxTTL   time.Duration // абсолютный предел сессии с момента входа
	RefreshReuseGap time.Duration // окно, в котором повтор только что использованного токена считается гонкой вкладок, а не кражей; 0 — окна нет
}

// Значения по умолчанию для сессий.
const (
	DefaultAccessTokenTTL = 15 * time.Minute
	DefaultRefreshIdleTTL = 7 * 24 * time.Hour
	DefaultSessionMaxTTL  = 30 * 24 * time.Hour
	DefaultRefreshReuse   = 10 * time.Second
	DefaultJWTIssuer      = "auction"
	DefaultJWTAudience    = "auction-web"

	maxReuseGap = time.Minute
)

// ApplyDefaults подставляет значения по умолчанию вместо незаданных (нулевых).
func (c *Config) ApplyDefaults() {
	if c.JWTIssuer == "" {
		c.JWTIssuer = DefaultJWTIssuer
	}
	if c.JWTAudience == "" {
		c.JWTAudience = DefaultJWTAudience
	}
	if c.AccessTokenTTL <= 0 {
		c.AccessTokenTTL = DefaultAccessTokenTTL
	}
	if c.RefreshIdleTTL <= 0 {
		c.RefreshIdleTTL = DefaultRefreshIdleTTL
	}
	if c.SessionMaxTTL <= 0 {
		c.SessionMaxTTL = DefaultSessionMaxTTL
	}
}

const maxAntiSnipe = 24 * time.Hour

func Load() (Config, error) {
	cfg := Config{
		DBHost:       env("DB_HOST", "localhost"),
		DBPort:       env("DB_PORT", "5432"),
		DBName:       env("DB_NAME", "auction"),
		DBUser:       env("DB_USER", "auction_user"),
		DBPassword:   env("DB_PASSWORD", "auction_pass"),
		Port:         env("PORT", "8080"),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		JWTIssuer:    env("JWT_ISSUER", DefaultJWTIssuer),
		JWTAudience:  env("JWT_AUDIENCE", DefaultJWTAudience),
		UploadDir:    env("UPLOAD_DIR", "./uploads"),
		CookieSecure: strings.EqualFold(os.Getenv("COOKIE_SECURE"), "true"),
	}
	if len(cfg.JWTSecret) < 16 {
		return cfg, errors.New("JWT_SECRET должен быть задан и содержать не менее 16 символов")
	}

	var err error
	if cfg.AntiSnipeWindow, err = envSeconds("ANTI_SNIPE_WINDOW_SEC", 120); err != nil {
		return cfg, err
	}
	if cfg.AntiSnipeExtend, err = envSeconds("ANTI_SNIPE_EXTEND_SEC", 120); err != nil {
		return cfg, err
	}
	if cfg.AntiSnipeWindow > 0 && cfg.AntiSnipeExtend <= 0 {
		return cfg, errors.New("ANTI_SNIPE_EXTEND_SEC должен быть больше 0, если включён ANTI_SNIPE_WINDOW_SEC")
	}

	for _, o := range strings.Split(os.Getenv("WS_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.WSAllowedOrigins = append(cfg.WSAllowedOrigins, o)
		}
	}

	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o == "" {
			continue
		}
		norm, err := NormalizeOrigin(o)
		if err != nil {
			return cfg, fmt.Errorf("ALLOWED_ORIGINS: %w", err)
		}
		cfg.AllowedOrigins = append(cfg.AllowedOrigins, norm)
	}

	cfg.ApplyDefaults()
	if cfg.RefreshReuseGap, err = envSeconds("REFRESH_REUSE_GAP_SEC", int(DefaultRefreshReuse/time.Second)); err != nil {
		return cfg, err
	}
	if cfg.RefreshReuseGap > maxReuseGap {
		return cfg, fmt.Errorf("REFRESH_REUSE_GAP_SEC: не больше %d секунд", int(maxReuseGap/time.Second))
	}
	return cfg, nil
}

// NormalizeOrigin проверяет, что строка — origin вида scheme://host[:port] (без пути),
// и приводит её к нижнему регистру. «*» и «null» недопустимы: вместе с credentials
// они отключили бы защиту.
func NormalizeOrigin(raw string) (string, error) {
	o := strings.ToLower(strings.TrimRight(strings.TrimSpace(raw), "/"))
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("%q: ожидается origin вида https://example.com[:порт] без пути", raw)
	}
	return u.Scheme + "://" + u.Host, nil
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

// envSeconds читает целое число секунд из переменной окружения (0…24 часа).
func envSeconds(key string, defSeconds int) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return time.Duration(defSeconds) * time.Second, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: ожидается целое число секунд ≥ 0, получено %q", key, raw)
	}
	if n > int(maxAntiSnipe/time.Second) { // проверяем до умножения — иначе возможно переполнение
		return 0, fmt.Errorf("%s: не больше %d секунд", key, int(maxAntiSnipe/time.Second))
	}
	return time.Duration(n) * time.Second, nil
}
