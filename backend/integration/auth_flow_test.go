package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"auction/auth"
	"auction/config"
)

func TestRegisterIssuesTokensAndCookies(t *testing.T) {
	e := newEnv(t, nil)
	d := e.newDevice()
	resp, raw := d.do("POST", "/api/auth/register", map[string]any{
		"name": "Тест", "username": "alice", "telegram": "alice_tg", "dormId": 1,
		"password": password, "confirm": password,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d %s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "refresh") || strings.Contains(string(raw), "eyJ") {
		t.Fatalf("токены не должны попадать в тело ответа: %s", raw)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}

	acc, ref := findCookie(resp, "session"), findCookie(resp, "refresh_token")
	if acc == nil || ref == nil {
		t.Fatalf("ожидали обе cookie, получили %v", resp.Cookies())
	}
	for _, c := range []*http.Cookie{acc, ref} {
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("%s: HttpOnly=%v SameSite=%v", c.Name, c.HttpOnly, c.SameSite)
		}
	}
	if acc.Path != "/" || ref.Path != "/api/auth" {
		t.Errorf("Path: access=%q refresh=%q", acc.Path, ref.Path)
	}
	if acc.MaxAge != 15*60 {
		t.Errorf("access MaxAge = %d, ожидали 900", acc.MaxAge)
	}
	if want := 7 * 24 * 3600; ref.MaxAge < want-5 || ref.MaxAge > want {
		t.Errorf("refresh MaxAge = %d, ожидали ≈%d", ref.MaxAge, want)
	}

	// refresh-токен в БД — только SHA-256
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE encode(token_hash,'hex') = $1`, hashHex(ref.Value)); n != 1 {
		t.Fatalf("хеш refresh-токена в БД не найден (n=%d)", n)
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE position($1 in encode(token_hash,'escape')) > 0`, ref.Value); n != 0 {
		t.Fatal("в БД обнаружен сам токен")
	}

	// Сроки: 7 суток бездействия и 30 суток абсолютного предела
	var idle, abs float64
	if err := e.sql.QueryRow(`SELECT EXTRACT(EPOCH FROM expires_at-created_at), EXTRACT(EPOCH FROM family_expires_at-created_at) FROM refresh_tokens`).Scan(&idle, &abs); err != nil {
		t.Fatal(err)
	}
	if idle != 7*24*3600 || abs != 30*24*3600 {
		t.Errorf("сроки в БД: idle=%v abs=%v", idle, abs)
	}

	// access — JWT со всеми обязательными claims
	cl, err := mustJWT(t).Verify(acc.Value)
	if err != nil || cl.Subject <= 0 || cl.ID == "" {
		t.Fatalf("access-токен не проверяется: %+v %v", cl, err)
	}
}

func mustJWT(t *testing.T) *auth.JWT {
	t.Helper()
	j, err := auth.NewJWT(auth.Options{Secret: []byte(testSecret), Issuer: config.DefaultJWTIssuer, Audience: config.DefaultJWTAudience, TTL: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestSecureFlagInProduction(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.CookieSecure = true })
	d := e.newDevice()
	resp, _ := d.do("POST", "/api/auth/register", map[string]any{
		"name": "Тест", "username": "bob", "telegram": "bob_tg", "dormId": 1, "password": password,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	for _, name := range []string{"session", "refresh_token"} {
		if c := findCookie(resp, name); c == nil || !c.Secure {
			t.Errorf("%s: нет флага Secure", name)
		}
	}
	// и при удалении атрибуты те же — иначе браузер cookie не сотрёт
	resp, _ = d.do("POST", "/api/auth/logout", nil)
	for _, name := range []string{"session", "refresh_token"} {
		c := findCookie(resp, name)
		if c == nil || c.MaxAge >= 0 || !c.Secure || !c.HttpOnly {
			t.Errorf("%s: неверное удаление: %+v", name, c)
		}
	}
	if c := findCookie(resp, "refresh_token"); c == nil || c.Path != "/api/auth" {
		t.Errorf("refresh cookie должна удаляться с Path=/api/auth: %+v", c)
	}
}

func TestLoginCreatesSessionAndFailureDoesNot(t *testing.T) {
	e := newEnv(t, nil)
	u := e.newDevice().register()

	d := e.newDevice()
	resp, _ := d.login(u.Username, "wrong-password")
	if resp.StatusCode != http.StatusUnauthorized || findCookie(resp, "session") != nil || findCookie(resp, "refresh_token") != nil {
		t.Fatalf("неудачный вход не должен выдавать cookie: %d %v", resp.StatusCode, resp.Cookies())
	}
	before := e.count(`SELECT count(*) FROM refresh_tokens`)

	resp, raw := d.login(u.Username, password)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", resp.StatusCode, raw)
	}
	if after := e.count(`SELECT count(*) FROM refresh_tokens`); after != before+1 {
		t.Fatalf("вход должен создать ровно одну сессию: было %d, стало %d", before, after)
	}
	if resp, _ := d.do("GET", "/api/auth/me", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("me после входа: %d", resp.StatusCode)
	}
}

func TestReloginClosesPreviousSessionOfSameBrowser(t *testing.T) {
	e := newEnv(t, nil)
	d := e.newDevice()
	u := d.register()
	first := d.refresh()

	if resp, _ := d.login(u.Username, password); resp.StatusCode != http.StatusOK {
		t.Fatal("login")
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_reason = 'relogin'`); n != 1 {
		t.Fatalf("прежняя сессия должна быть закрыта (relogin), закрыто %d", n)
	}
	other := e.newDevice()
	other.setCookie("/api/auth/refresh", "refresh_token", first, "/api/auth")
	if resp, raw := other.do("POST", "/api/auth/refresh", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("старая сессия ещё жива: %d %s", resp.StatusCode, raw)
	}
}

func TestAccessTokenValidation(t *testing.T) {
	e := newEnv(t, nil)
	d := e.newDevice()
	u := d.register()
	access, refresh := d.access(), d.refresh()

	bare := func() *device { return e.newDevice() } // без cookie

	// Bearer остаётся рабочим для не-браузерных клиентов
	if resp, _ := bare().do("GET", "/api/auth/me", nil, withHeader("Authorization", "Bearer "+access)); resp.StatusCode != 200 {
		t.Fatalf("Bearer access: %d", resp.StatusCode)
	}

	past := func() time.Time { return time.Now().Add(-time.Hour) }
	noneHeader := "eyJhbGciOiJub25lIiwidHlwIjoiYXQrand0In0" // {"alg":"none","typ":"at+jwt"}
	parts := strings.Split(access, ".")

	bad := map[string]string{
		"refresh-токен вместо access": refresh,
		"просроченный access":         jwtWith(t, auth.Options{Now: past}, u.ID),
		"чужой секрет":                jwtWith(t, auth.Options{Secret: []byte("other-secret-other-secret-123456")}, u.ID),
		"чужой aud":                   jwtWith(t, auth.Options{Audience: "other"}, u.ID),
		"чужой iss":                   jwtWith(t, auth.Options{Issuer: "other"}, u.ID),
		"alg=none":                    noneHeader + "." + parts[1] + ".",
		"alg=none с подписью":         noneHeader + "." + parts[1] + "." + parts[2],
		"мусор":                       "garbage",
		"испорченная подпись":         access + "x",
	}
	for name, tok := range bad {
		if resp, _ := bare().do("GET", "/api/auth/me", nil, withHeader("Authorization", "Bearer "+tok)); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: Bearer принят (%d)", name, resp.StatusCode)
		}
		c := bare()
		c.setCookie("/", "session", tok, "/")
		if resp, _ := c.do("GET", "/api/auth/me", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: cookie принята (%d)", name, resp.StatusCode)
		}
	}
	// контроль: сам токен годен, значит отказы выше вызваны именно дефектом токена
	if resp, _ := bare().do("GET", "/api/auth/me", nil, withHeader("Authorization", "Bearer "+access)); resp.StatusCode != 200 {
		t.Fatalf("контрольный токен отклонён: %d", resp.StatusCode)
	}
}

func TestRefreshCookieNotSentToRegularAPI(t *testing.T) {
	e := newEnv(t, nil)
	d := e.newDevice()
	d.register()
	for _, p := range []string{"/api/me/stats", "/api/lots", "/api/ws", "/api/uploads", "/uploads/x.png", "/"} {
		if got := d.cookie(p, "refresh_token"); got != "" {
			t.Errorf("refresh-cookie отправляется на %s", p)
		}
	}
	for _, p := range []string{"/api/auth/refresh", "/api/auth/logout", "/api/auth/logout-all", "/api/auth/me"} {
		if got := d.cookie(p, "refresh_token"); got == "" {
			t.Errorf("refresh-cookie не отправляется на %s", p)
		}
	}
	if d.cookie("/api/ws", "session") == "" {
		t.Error("access-cookie должна уходить на /api/ws (аутентификация WebSocket)")
	}
}

func TestMeWithOnlyRefreshCookieIsUnauthorized(t *testing.T) {
	e := newEnv(t, nil)
	d := e.newDevice()
	d.register()
	only := e.newDevice()
	only.setCookie("/api/auth/me", "refresh_token", d.refresh(), "/api/auth")
	if resp, _ := only.do("GET", "/api/auth/me", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh-токен дал доступ к /me: %d", resp.StatusCode)
	}
	if resp, _ := only.do("GET", "/api/me/stats", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh-токен дал доступ к обычному API: %d", resp.StatusCode)
	}
}

func TestProtectedRoutesNeedAccessToken(t *testing.T) {
	e := newEnv(t, nil)
	d := e.newDevice()
	for _, r := range [][2]string{{"GET", "/api/me/stats"}, {"GET", "/api/me/lots"}, {"PUT", "/api/me/password"}, {"POST", "/api/lots"}, {"POST", "/api/uploads"}} {
		if resp, _ := d.do(r[0], r[1], map[string]string{}); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s без токена: %d", r[0], r[1], resp.StatusCode)
		}
	}
}

func TestLogsDoNotContainSecrets(t *testing.T) {
	buf := &lockedBuffer{}
	e := newEnv(t, nil)
	// лог ставим после newEnv: Middleware пишет в стандартный логгер
	var restore = func() {}
	{
		old := logWriter()
		setLog(buf)
		restore = func() { setLog(old) }
	}
	defer restore()

	d := e.newDevice()
	u := d.register()
	access, refresh := d.access(), d.refresh()
	d.do("GET", "/api/auth/me", nil)
	d.do("POST", "/api/auth/refresh", nil)
	newRefresh := d.refresh()
	// повторное использование — самое «разговорчивое» событие
	other := e.newDevice()
	other.setCookie("/api/auth/refresh", "refresh_token", refresh, "/api/auth")
	other.do("POST", "/api/auth/refresh", nil)
	d.login(u.Username, "wrong")
	d.do("POST", "/api/auth/logout", nil)

	logs := buf.String()
	if logs == "" {
		t.Fatal("лог пуст — тест ничего не проверил")
	}
	for name, secret := range map[string]string{"access": access, "refresh": refresh, "refresh2": newRefresh, "jwt secret": testSecret, "password": password, "hash": hashHex(refresh)} {
		if secret != "" && strings.Contains(logs, secret) {
			t.Errorf("в логе найден %s", name)
		}
	}
}
