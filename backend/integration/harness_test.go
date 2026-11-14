// Package integration — сквозные тесты авторизации на настоящем PostgreSQL.
//
// Запуск (нужна доступная БД, например контейнер из docker-compose.dev.yml):
//
//	TEST_DATABASE_URL='postgres://auction_user:ПАРОЛЬ@127.0.0.1:5432/auction?sslmode=disable' \
//	    go test ./integration/ -count=1 -race
//
// Каждый тест работает в собственной временной схеме (CREATE SCHEMA … / DROP SCHEMA … CASCADE),
// рабочие данные в базе не затрагиваются. Без TEST_DATABASE_URL тесты пропускаются.
package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"auction/auth"
	"auction/config"
	"auction/db"
	"auction/handler"
)

const (
	testSecret = "integration-test-secret-0123456789"
	password   = "secret123"
)

type env struct {
	t     *testing.T
	srv   *httptest.Server
	url   *url.URL
	q     *db.Queries
	sql   *sql.DB
	cfg   config.Config
	users int
}

// newEnv поднимает изолированную схему, применяет миграции и запускает HTTP-сервер
// с теми же обёртками, что и в main.go.
func newEnv(t *testing.T, mod func(*config.Config)) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционные тесты пропущены")
	}

	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		t.Fatal(err)
	}
	schema := "it_" + hex.EncodeToString(rnd[:])

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatalf("создание схемы: %v", err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	qs := u.Query()
	qs.Set("search_path", schema)
	u.RawQuery = qs.Encode()
	sqlDB, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(20)

	t.Cleanup(func() {
		sqlDB.Close()
		admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		admin.Close()
	})

	q := db.New(sqlDB)
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatalf("миграции: %v", err)
	}

	cfg := config.Config{JWTSecret: testSecret, UploadDir: t.TempDir()}
	cfg.ApplyDefaults()
	if mod != nil {
		mod(&cfg)
	}

	h := handler.New(q, cfg)
	mux := http.NewServeMux()
	handler.SetupRoutes(mux, h)
	srv := httptest.NewServer(handler.Middleware(h.Guard(mux)))
	t.Cleanup(srv.Close)
	su, _ := url.Parse(srv.URL)

	return &env{t: t, srv: srv, url: su, q: q, sql: sqlDB, cfg: cfg}
}

// device — «браузер»: собственное хранилище cookie по правилам RFC 6265.
type device struct {
	e      *env
	jar    *cookiejar.Jar
	client *http.Client
}

func (e *env) newDevice() *device {
	jar, err := cookiejar.New(nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return &device{e: e, jar: jar, client: &http.Client{Jar: jar, Timeout: 20 * time.Second}}
}

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withoutHeader(k string) reqOpt { return func(r *http.Request) { r.Header.Del(k) } }

// do отправляет запрос от имени устройства. По умолчанию — как страница нашего же сайта.
func (d *device) do(method, path string, body any, opts ...reqOpt) (*http.Response, []byte) {
	d.e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, d.e.srv.URL+path, rd)
	if err != nil {
		d.e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", d.e.srv.URL)
	for _, o := range opts {
		o(req)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		d.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// cookie возвращает значение cookie, которое устройство отправило бы на данный путь.
func (d *device) cookie(path, name string) string {
	u := *d.e.url
	u.Path = path
	for _, c := range d.jar.Cookies(&u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func (d *device) setCookie(path, name, value, cpath string) {
	u := *d.e.url
	u.Path = path
	d.jar.SetCookies(&u, []*http.Cookie{{Name: name, Value: value, Path: cpath}})
}

func (d *device) access() string  { return d.cookie("/api/me/stats", "session") }
func (d *device) refresh() string { return d.cookie("/api/auth/refresh", "refresh_token") }

type userJSON struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// register регистрирует нового пользователя (и тем самым открывает сессию на устройстве).
func (d *device) register() userJSON {
	d.e.t.Helper()
	d.e.users++
	n := d.e.users
	resp, raw := d.do("POST", "/api/auth/register", map[string]any{
		"name": "Тест", "username": "user" + itoa(n), "telegram": "tg_user_" + itoa(n),
		"dormId": 1, "password": password, "confirm": password,
	})
	if resp.StatusCode != http.StatusCreated {
		d.e.t.Fatalf("регистрация: %d %s", resp.StatusCode, raw)
	}
	var u userJSON
	if err := json.Unmarshal(raw, &u); err != nil {
		d.e.t.Fatal(err)
	}
	return u
}

func (d *device) login(username, pass string) (*http.Response, []byte) {
	return d.do("POST", "/api/auth/login", map[string]string{"username": username, "password": pass})
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func hashHex(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

func errCode(raw []byte) string {
	var m struct{ Code string }
	json.Unmarshal(raw, &m)
	return m.Code
}

// findCookie ищет Set-Cookie с заданным именем в ответе.
func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.sql.QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// lockedBuffer — потокобезопасный приёмник лога.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// jwtWith выпускает access-токен с произвольными параметрами для негативных проверок.
func jwtWith(t *testing.T, o auth.Options, uid int64) string {
	t.Helper()
	if o.Secret == nil {
		o.Secret = []byte(testSecret)
	}
	if o.Issuer == "" {
		o.Issuer = config.DefaultJWTIssuer
	}
	if o.Audience == "" {
		o.Audience = config.DefaultJWTAudience
	}
	if o.TTL == 0 {
		o.TTL = 15 * time.Minute
	}
	j, err := auth.NewJWT(o)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := j.Issue(uid)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func logWriter() io.Writer { return log.Writer() }
func setLog(w io.Writer)   { log.SetOutput(w) }

func contextBackground() context.Context { return context.Background() }

func hashHex2(h []byte) string { return hex.EncodeToString(h) }
