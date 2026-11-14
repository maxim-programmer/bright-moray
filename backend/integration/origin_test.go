package integration

import (
	"net/http"
	"strings"
	"testing"

	"auction/config"
)

const trusted = "https://app.example.com"

func withTrusted(c *config.Config) {
	c.RefreshReuseGap = 0
	c.AllowedOrigins = []string{trusted}
}

func TestCSRFOriginCheck(t *testing.T) {
	e := newEnv(t, withTrusted)
	d := e.newDevice()
	user := d.register()

	cases := []struct {
		name string
		opts []reqOpt
		want int
	}{
		{"тот же origin", nil, 200},
		{"чужой Origin", []reqOpt{withHeader("Origin", "https://evil.example")}, 403},
		{"Origin: null", []reqOpt{withHeader("Origin", "null")}, 403},
		{"Origin мусор", []reqOpt{withHeader("Origin", "::::")}, 403},
		{"Sec-Fetch-Site: cross-site", []reqOpt{withHeader("Sec-Fetch-Site", "cross-site")}, 403},
		{"Sec-Fetch-Site: same-site (другой поддомен)", []reqOpt{withHeader("Sec-Fetch-Site", "same-site"), withHeader("Origin", "https://api.example.org")}, 403},
		{"Sec-Fetch-Site: same-origin", []reqOpt{withHeader("Sec-Fetch-Site", "same-origin")}, 200},
		{"cross-site, но Origin подделан под наш", []reqOpt{withHeader("Sec-Fetch-Site", "cross-site"), withHeader("Origin", e.srv.URL)}, 403},
		{"без Origin (не-браузер)", []reqOpt{withoutHeader("Origin")}, 200},
		{"доверенный Origin", []reqOpt{withHeader("Origin", trusted), withHeader("Sec-Fetch-Site", "same-site")}, 200},
		{"доверенный Origin в другом регистре", []reqOpt{withHeader("Origin", "HTTPS://APP.EXAMPLE.COM")}, 200},
		{"похожий на доверенный", []reqOpt{withHeader("Origin", trusted+".evil.com")}, 403},
		{"доверенный хост с другой схемой", []reqOpt{withHeader("Origin", "http://app.example.com")}, 403},
	}
	for _, c := range cases {
		resp, raw := d.do("POST", "/api/auth/login", map[string]string{"username": user.Username, "password": password}, c.opts...)
		if resp.StatusCode != c.want {
			t.Errorf("%s: статус %d, ожидали %d (%s)", c.name, resp.StatusCode, c.want, raw)
		}
		if c.want == 403 && errCode(raw) != "bad_origin" {
			t.Errorf("%s: код ошибки %q", c.name, errCode(raw))
		}
	}
}

func TestCSRFBlocksEveryUnsafeMethodAndRoute(t *testing.T) {
	e := newEnv(t, withTrusted)
	d := e.newDevice()
	d.register()
	evil := withHeader("Origin", "https://evil.example")

	for _, r := range [][2]string{
		{"POST", "/api/auth/refresh"}, {"POST", "/api/auth/logout"}, {"POST", "/api/auth/logout-all"},
		{"PUT", "/api/me/password"}, {"PUT", "/api/me/profile"}, {"POST", "/api/lots"},
		{"DELETE", "/api/lots/1"}, {"POST", "/api/lots/1/bids"}, {"POST", "/api/uploads"},
	} {
		resp, _ := d.do(r[0], r[1], map[string]string{}, evil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s с чужого Origin: %d", r[0], r[1], resp.StatusCode)
		}
	}
	// отказ не должен ни отзывать, ни расходовать токены
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NOT NULL OR used_at IS NOT NULL`); n != 0 {
		t.Fatalf("заблокированные запросы изменили состояние сессий (%d)", n)
	}
	if resp, _ := d.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
		t.Fatalf("после блокировок refresh должен работать: %d", resp.StatusCode)
	}
}

func TestSafeMethodsNotBlockedByOrigin(t *testing.T) {
	e := newEnv(t, withTrusted)
	d := e.newDevice()
	d.register()
	resp, _ := d.do("GET", "/api/auth/me", nil, withHeader("Origin", "https://evil.example"))
	if resp.StatusCode != 200 {
		t.Fatalf("GET: %d", resp.StatusCode)
	}
	// но чужой сайт не получает права читать ответ
	if h := resp.Header.Get("Access-Control-Allow-Origin"); h != "" {
		t.Fatalf("CORS для чужого Origin: %q", h)
	}
	if resp.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("Allow-Credentials для чужого Origin")
	}
}

func TestCORSCredentialsForTrustedOrigin(t *testing.T) {
	e := newEnv(t, withTrusted)
	d := e.newDevice()
	user := d.register()

	resp, _ := d.do("POST", "/api/auth/login", map[string]string{"username": user.Username, "password": password}, withHeader("Origin", trusted))
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != trusted {
		t.Fatalf("Allow-Origin = %q", got)
	}
	if resp.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("нет Allow-Credentials")
	}
	if !strings.Contains(resp.Header.Get("Vary"), "Origin") {
		t.Fatalf("нет Vary: Origin (%q)", resp.Header.Get("Vary"))
	}

	// Предзапрос
	resp, _ = d.do("OPTIONS", "/api/auth/refresh", nil,
		withHeader("Origin", trusted), withHeader("Access-Control-Request-Method", "POST"),
		withHeader("Access-Control-Request-Headers", "content-type"))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight: %d", resp.StatusCode)
	}
	h := resp.Header
	if h.Get("Access-Control-Allow-Origin") != trusted || h.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("preflight без credentials: %v", h)
	}
	if h.Get("Access-Control-Allow-Origin") == "*" || !strings.Contains(h.Get("Access-Control-Allow-Methods"), "POST") ||
		!strings.Contains(strings.ToLower(h.Get("Access-Control-Allow-Headers")), "content-type") {
		t.Fatalf("preflight: %v", h)
	}

	// Предзапрос с чужого Origin отклоняется и ничего не разрешает
	resp, _ = d.do("OPTIONS", "/api/auth/refresh", nil,
		withHeader("Origin", "https://evil.example"), withHeader("Access-Control-Request-Method", "POST"))
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("preflight чужого Origin: %d %v", resp.StatusCode, resp.Header)
	}
}

func TestNoCORSWithoutConfiguredOrigins(t *testing.T) {
	e := newEnv(t, strict)
	d := e.newDevice()
	resp, _ := d.do("GET", "/api/dorms", nil, withHeader("Origin", "https://app.example.com"))
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS включён без настройки")
	}
}
