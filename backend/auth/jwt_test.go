package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

func newTestJWT(t *testing.T, now func() time.Time) *JWT {
	t.Helper()
	j, err := NewJWT(Options{Secret: testSecret, Issuer: "auction", Audience: "auction-web", TTL: 15 * time.Minute, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// forge собирает токен с произвольными заголовком и payload, подписанный секретом key.
func forge(header, payload any, key []byte) string {
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(payload)
	in := b64.EncodeToString(h) + "." + b64.EncodeToString(p)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(in))
	return in + "." + b64.EncodeToString(m.Sum(nil))
}

func goodPayload(now time.Time) map[string]any {
	return map[string]any{
		"iss": "auction", "sub": "42", "aud": "auction-web", "jti": "abc",
		"iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
	}
}

func TestIssueVerify(t *testing.T) {
	j := newTestJWT(t, nil)
	tok, c, err := j.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	got, err := j.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != 42 || got.ID == "" || got.ID != c.ID {
		t.Fatalf("claims: %+v / %+v", got, c)
	}
	if d := got.ExpiresAt.Sub(got.IssuedAt); d != 15*time.Minute {
		t.Fatalf("время жизни = %v, ожидали 15m", d)
	}
	if _, _, err := j.Issue(0); err == nil {
		t.Fatal("userID<=0 должен отклоняться")
	}
}

func TestClaimsPresent(t *testing.T) {
	j := newTestJWT(t, nil)
	tok, _, _ := j.Issue(7)
	raw, _ := b64.DecodeString(strings.Split(tok, ".")[1])
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"sub", "exp", "iat", "jti", "iss", "aud"} {
		if _, ok := m[k]; !ok {
			t.Errorf("в токене нет claim %q", k)
		}
	}
}

func TestUniqueJTI(t *testing.T) {
	j := newTestJWT(t, nil)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		_, c, _ := j.Issue(1)
		if seen[c.ID] {
			t.Fatal("jti повторился")
		}
		seen[c.ID] = true
	}
}

func TestExpiry(t *testing.T) {
	now := time.Now()
	clock := now
	j := newTestJWT(t, func() time.Time { return clock })
	tok, _, _ := j.Issue(1)

	clock = now.Add(15*time.Minute - time.Second)
	if _, err := j.Verify(tok); err != nil {
		t.Fatalf("за секунду до конца токен должен быть годен: %v", err)
	}
	clock = now.Add(15 * time.Minute)
	if _, err := j.Verify(tok); err != ErrExpiredToken {
		t.Fatalf("в момент exp токен должен быть просрочен, получили %v", err)
	}
	clock = now.Add(time.Hour)
	if _, err := j.Verify(tok); err == nil {
		t.Fatal("просроченный токен принят")
	}
}

func TestRejectWrongSecretAndTamper(t *testing.T) {
	j := newTestJWT(t, nil)
	tok, _, _ := j.Issue(1)

	other, _ := NewJWT(Options{Secret: []byte("another-secret-another-secret!!!"), Issuer: "auction", Audience: "auction-web", TTL: time.Minute})
	if _, err := other.Verify(tok); err == nil {
		t.Fatal("токен с чужим секретом принят")
	}

	parts := strings.Split(tok, ".")
	// подмена payload (sub=1 -> sub=2) без пересчёта подписи
	raw, _ := b64.DecodeString(parts[1])
	forged := strings.Replace(string(raw), `"sub":"1"`, `"sub":"2"`, 1)
	if forged == string(raw) {
		t.Fatal("тест некорректен: sub не заменён")
	}
	if _, err := j.Verify(parts[0] + "." + b64.EncodeToString([]byte(forged)) + "." + parts[2]); err == nil {
		t.Fatal("подменённый payload принят")
	}
	if _, err := j.Verify(tok + "x"); err == nil {
		t.Fatal("испорченная подпись принята")
	}
	if _, err := j.Verify(parts[0] + "." + parts[1] + "."); err == nil {
		t.Fatal("пустая подпись принята")
	}
}

func TestRejectAlgConfusion(t *testing.T) {
	j := newTestJWT(t, nil)
	now := time.Now()
	p := goodPayload(now)

	cases := map[string]string{
		"alg=none, без подписи": func() string {
			h, _ := json.Marshal(map[string]string{"alg": "none", "typ": "at+jwt"})
			pb, _ := json.Marshal(p)
			return b64.EncodeToString(h) + "." + b64.EncodeToString(pb) + "."
		}(),
		"alg=none, с подписью":  forge(map[string]string{"alg": "none", "typ": "at+jwt"}, p, testSecret),
		"alg=HS512":             forge(map[string]string{"alg": "HS512", "typ": "at+jwt"}, p, testSecret),
		"alg=RS256":             forge(map[string]string{"alg": "RS256", "typ": "at+jwt"}, p, testSecret),
		"alg в нижнем регистре": forge(map[string]string{"alg": "hs256", "typ": "at+jwt"}, p, testSecret),
		"alg отсутствует":       forge(map[string]string{"typ": "at+jwt"}, p, testSecret),
		"чужой typ":             forge(map[string]string{"alg": "HS256", "typ": "JWT"}, p, testSecret),
		"typ отсутствует":       forge(map[string]string{"alg": "HS256"}, p, testSecret),
	}
	for name, tok := range cases {
		if _, err := j.Verify(tok); err == nil {
			t.Errorf("%s: токен принят", name)
		}
	}
	// контроль: корректно собранный токен проходит — значит, отказы выше вызваны именно заголовком
	if _, err := j.Verify(forge(map[string]string{"alg": "HS256", "typ": "at+jwt"}, p, testSecret)); err != nil {
		t.Fatalf("контрольный токен отклонён: %v", err)
	}
}

func TestRejectBadClaims(t *testing.T) {
	j := newTestJWT(t, nil)
	now := time.Now()
	hdr := map[string]string{"alg": "HS256", "typ": "at+jwt"}

	mutate := func(f func(m map[string]any)) string {
		p := goodPayload(now)
		f(p)
		return forge(hdr, p, testSecret)
	}
	cases := map[string]string{
		"чужой iss":         mutate(func(m map[string]any) { m["iss"] = "evil" }),
		"чужой aud":         mutate(func(m map[string]any) { m["aud"] = "other" }),
		"нет aud":           mutate(func(m map[string]any) { delete(m, "aud") }),
		"нет jti":           mutate(func(m map[string]any) { delete(m, "jti") }),
		"пустой jti":        mutate(func(m map[string]any) { m["jti"] = "" }),
		"нет sub":           mutate(func(m map[string]any) { delete(m, "sub") }),
		"sub не число":      mutate(func(m map[string]any) { m["sub"] = "admin" }),
		"sub=0":             mutate(func(m map[string]any) { m["sub"] = "0" }),
		"sub отрицательный": mutate(func(m map[string]any) { m["sub"] = "-5" }),
		"sub с плюсом":      mutate(func(m map[string]any) { m["sub"] = "+42" }),
		"sub с нулями":      mutate(func(m map[string]any) { m["sub"] = "042" }),
		"sub числом":        mutate(func(m map[string]any) { m["sub"] = 42 }),
		"exp в прошлом":     mutate(func(m map[string]any) { m["exp"] = now.Add(-time.Minute).Unix() }),
		"нет exp":           mutate(func(m map[string]any) { delete(m, "exp") }),
		"exp дробный":       mutate(func(m map[string]any) { m["exp"] = float64(now.Unix()) + 0.5 }),
		"exp <= iat":        mutate(func(m map[string]any) { m["exp"] = m["iat"] }),
		"iat из будущего":   mutate(func(m map[string]any) { m["iat"] = now.Add(time.Hour).Unix(); m["exp"] = now.Add(2 * time.Hour).Unix() }),
	}
	for name, tok := range cases {
		if _, err := j.Verify(tok); err == nil {
			t.Errorf("%s: токен принят", name)
		}
	}
}

func TestRejectGarbage(t *testing.T) {
	j := newTestJWT(t, nil)
	for _, s := range []string{"", "garbage", "a.b", "a.b.c", "...", "a.b.c.d", strings.Repeat("a", maxJWTLen+1), "eyJ.eyJ.eyJ"} {
		if _, err := j.Verify(s); err == nil {
			t.Errorf("%q принят", s)
		}
	}
}

func TestRefreshTokenIsNotAccessToken(t *testing.T) {
	j := newTestJWT(t, nil)
	tok, _, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Verify(tok); err == nil {
		t.Fatal("refresh-токен принят как access")
	}
}

func TestNewJWTValidation(t *testing.T) {
	ok := Options{Secret: testSecret, Issuer: "i", Audience: "a", TTL: time.Minute}
	if _, err := NewJWT(ok); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(o *Options){
		"пустой секрет": func(o *Options) { o.Secret = nil },
		"нет issuer":    func(o *Options) { o.Issuer = "" },
		"нет audience":  func(o *Options) { o.Audience = "" },
		"нулевой TTL":   func(o *Options) { o.TTL = 0 },
	} {
		o := ok
		mod(&o)
		if _, err := NewJWT(o); err == nil {
			t.Errorf("%s: ошибка не возвращена", name)
		}
	}
}
