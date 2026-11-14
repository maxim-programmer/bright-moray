package config

import (
	"testing"
	"time"
)

func TestNormalizeOrigin(t *testing.T) {
	good := map[string]string{
		"https://App.Example.com":      "https://app.example.com",
		"https://app.example.com/":     "https://app.example.com",
		"http://localhost:5173":        "http://localhost:5173",
		"  https://example.com:8443  ": "https://example.com:8443",
	}
	for in, want := range good {
		got, err := NormalizeOrigin(in)
		if err != nil || got != want {
			t.Errorf("%q → %q, %v (ожидали %q)", in, got, err, want)
		}
	}
	for _, in := range []string{
		"*", "null", "", "example.com", "ftp://example.com", "https://", "https://example.com/path",
		"https://example.com?x=1", "https://example.com#f", "https://user@example.com", "javascript:alert(1)",
	} {
		if got, err := NormalizeOrigin(in); err == nil {
			t.Errorf("%q принят как %q", in, got)
		}
	}
}

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"JWT_SECRET", "JWT_ISSUER", "JWT_AUDIENCE", "ALLOWED_ORIGINS", "WS_ALLOWED_ORIGINS", "REFRESH_REUSE_GAP_SEC", "COOKIE_SECURE"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": "0123456789abcdef0123"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccessTokenTTL != 15*time.Minute || cfg.RefreshIdleTTL != 7*24*time.Hour || cfg.SessionMaxTTL != 30*24*time.Hour {
		t.Errorf("сроки: %v %v %v", cfg.AccessTokenTTL, cfg.RefreshIdleTTL, cfg.SessionMaxTTL)
	}
	if cfg.JWTIssuer == "" || cfg.JWTAudience == "" || cfg.RefreshReuseGap != DefaultRefreshReuse {
		t.Errorf("значения по умолчанию: %+v", cfg)
	}
	if len(cfg.AllowedOrigins) != 0 {
		t.Errorf("AllowedOrigins по умолчанию должен быть пуст: %v", cfg.AllowedOrigins)
	}
}

func TestLoadAllowedOrigins(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": "0123456789abcdef0123", "ALLOWED_ORIGINS": "https://A.example.com/, http://localhost:5173 ,"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://a.example.com" || cfg.AllowedOrigins[1] != "http://localhost:5173" {
		t.Fatalf("%v", cfg.AllowedOrigins)
	}

	for _, bad := range []string{"*", "https://a.example.com,*", "example.com", "https://a.example.com/x"} {
		setEnv(t, map[string]string{"JWT_SECRET": "0123456789abcdef0123", "ALLOWED_ORIGINS": bad})
		if _, err := Load(); err == nil {
			t.Errorf("ALLOWED_ORIGINS=%q принят", bad)
		}
	}
}

func TestLoadReuseGap(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": "0123456789abcdef0123", "REFRESH_REUSE_GAP_SEC": "0"})
	cfg, err := Load()
	if err != nil || cfg.RefreshReuseGap != 0 {
		t.Fatalf("0 должен отключать окно: %v %v", cfg.RefreshReuseGap, err)
	}
	for _, bad := range []string{"-1", "abc", "61", "3600"} {
		setEnv(t, map[string]string{"JWT_SECRET": "0123456789abcdef0123", "REFRESH_REUSE_GAP_SEC": bad})
		if _, err := Load(); err == nil {
			t.Errorf("REFRESH_REUSE_GAP_SEC=%q принят", bad)
		}
	}
}

func TestLoadRequiresSecret(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": "short"})
	if _, err := Load(); err == nil {
		t.Fatal("короткий секрет принят")
	}
}

func TestApplyDefaultsKeepsExplicitValues(t *testing.T) {
	c := Config{AccessTokenTTL: time.Minute, JWTIssuer: "x"}
	c.ApplyDefaults()
	if c.AccessTokenTTL != time.Minute || c.JWTIssuer != "x" || c.JWTAudience != DefaultJWTAudience {
		t.Fatalf("%+v", c)
	}
}
