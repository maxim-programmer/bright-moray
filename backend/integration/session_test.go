package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"auction/config"
	"auction/db"
)

// refreshOnce выполняет POST /api/auth/refresh с заданным refresh-токеном, как отдельный клиент.
func (e *env) refreshWith(token string) (*http.Response, []byte) {
	d := e.newDevice()
	d.setCookie("/api/auth/refresh", "refresh_token", token, "/api/auth")
	return d.do("POST", "/api/auth/refresh", nil)
}

func strict(c *config.Config) { c.RefreshReuseGap = 0 }

func TestRefreshRotates(t *testing.T) {
	e := newEnv(t, strict)
	d := e.newDevice()
	d.register()
	oldRefresh, oldAccess := d.refresh(), d.access()

	resp, raw := d.do("POST", "/api/auth/refresh", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh: %d %s", resp.StatusCode, raw)
	}
	newRefresh, newAccess := d.refresh(), d.access()
	if newRefresh == "" || newRefresh == oldRefresh {
		t.Fatal("refresh-токен не ротирован")
	}
	if newAccess == "" || newAccess == oldAccess {
		t.Fatal("access-токен не обновлён")
	}
	if c := findCookie(resp, "refresh_token"); c == nil || c.Path != "/api/auth" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("атрибуты новой refresh-cookie: %+v", c)
	}
	if resp, _ := d.do("GET", "/api/auth/me", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("me с новым access: %d", resp.StatusCode)
	}

	// БД: старый токен использован и указывает на новый, одно семейство
	if n := e.count(`SELECT count(*) FROM refresh_tokens o JOIN refresh_tokens n ON o.replaced_by = n.id
		WHERE o.used_at IS NOT NULL AND o.family_id = n.family_id AND encode(o.token_hash,'hex') = $1 AND encode(n.token_hash,'hex') = $2`,
		hashHex(oldRefresh), hashHex(newRefresh)); n != 1 {
		t.Fatalf("цепочка old→new не записана (n=%d)", n)
	}
	if n := e.count(`SELECT count(DISTINCT family_id) FROM refresh_tokens`); n != 1 {
		t.Fatalf("семейство должно сохраняться при ротации, семейств: %d", n)
	}
	// несколько ротаций подряд
	for i := 0; i < 3; i++ {
		if resp, raw := d.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
			t.Fatalf("ротация #%d: %d %s", i, resp.StatusCode, raw)
		}
	}
}

func TestRefreshWithoutOrWithGarbageToken(t *testing.T) {
	e := newEnv(t, strict)
	d := e.newDevice()
	resp, raw := d.do("POST", "/api/auth/refresh", nil)
	if resp.StatusCode != 401 || errCode(raw) != "invalid_refresh" {
		t.Fatalf("без cookie: %d %s", resp.StatusCode, raw)
	}
	for _, tok := range []string{"garbage", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		resp, raw := e.refreshWith(tok)
		if resp.StatusCode != 401 || errCode(raw) != "invalid_refresh" {
			t.Fatalf("токен %q: %d %s", tok, resp.StatusCode, raw)
		}
	}
}

func TestRefreshRejectsAccessToken(t *testing.T) {
	e := newEnv(t, strict)
	d := e.newDevice()
	d.register()
	resp, _ := e.refreshWith(d.access())
	if resp.StatusCode != 401 {
		t.Fatalf("access-токен принят как refresh: %d", resp.StatusCode)
	}
}

func TestReuseDetectionRevokesFamily(t *testing.T) {
	e := newEnv(t, strict)
	victim := e.newDevice()
	user := victim.register()
	stolen := victim.refresh() // злоумышленник скопировал токен

	if resp, _ := victim.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
		t.Fatal("легитимное обновление")
	}
	current := victim.refresh()

	// Злоумышленник предъявляет старый токен
	resp, raw := e.refreshWith(stolen)
	if resp.StatusCode != 401 || errCode(raw) != "refresh_reused" {
		t.Fatalf("повтор не обнаружен: %d %s", resp.StatusCode, raw)
	}
	for _, name := range []string{"session", "refresh_token"} {
		if c := findCookie(resp, name); c == nil || c.MaxAge >= 0 {
			t.Errorf("cookie %s не стёрта", name)
		}
	}
	// Всё семейство отозвано — в том числе актуальный токен жертвы
	if resp, raw := e.refreshWith(current); resp.StatusCode != 401 {
		t.Fatalf("актуальный токен после отзыва семейства жив: %d %s", resp.StatusCode, raw)
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL`); n != 0 {
		t.Fatalf("остались неотозванные токены: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_reason = 'reuse_detected'`); n == 0 {
		t.Fatal("причина отзыва не записана")
	}
	// Другие сессии этого же пользователя не затронуты
	other := e.newDevice()
	if resp, _ := other.login(user.Username, password); resp.StatusCode != 200 {
		t.Fatal("login")
	}
	if resp, _ := e.refreshWith(stolen); resp.StatusCode != 401 {
		t.Fatal("украденный токен снова работает")
	}
	if resp, _ := other.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
		t.Fatalf("независимая сессия пострадала: %d", resp.StatusCode)
	}
}

func TestReuseDetectionSurvivesLongChain(t *testing.T) {
	e := newEnv(t, strict)
	d := e.newDevice()
	d.register()
	first := d.refresh()
	for i := 0; i < 5; i++ {
		if resp, _ := d.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
			t.Fatal("refresh")
		}
	}
	// самый первый токен цепочки по-прежнему опознаётся как использованный
	resp, raw := e.refreshWith(first)
	if resp.StatusCode != 401 || errCode(raw) != "refresh_reused" {
		t.Fatalf("%d %s", resp.StatusCode, raw)
	}
}

func TestReuseGapDoesNotRevoke(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RefreshReuseGap = time.Hour })
	d := e.newDevice()
	d.register()
	old := d.refresh()
	if resp, _ := d.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
		t.Fatal("refresh")
	}
	current := d.refresh()

	// «вторая вкладка» с ещё не обновлённым cookie
	resp, raw := e.refreshWith(old)
	if resp.StatusCode != http.StatusConflict || errCode(raw) != "refresh_conflict" {
		t.Fatalf("ожидали 409 refresh_conflict: %d %s", resp.StatusCode, raw)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatalf("при гонке cookie трогать нельзя: %v", resp.Cookies())
	}
	// сессия жива, новый токен работает
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NOT NULL`); n != 0 {
		t.Fatal("гонка не должна отзывать токены")
	}
	if resp, raw := d.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
		t.Fatalf("текущий токен после гонки: %d %s (%s)", resp.StatusCode, raw, current)
	}
}

// Параллельные обновления одного токена: ротация обязана быть атомарной —
// выжить может не более одного «потомка».
func TestConcurrentRefreshStrict(t *testing.T) {
	e := newEnv(t, strict)
	d := e.newDevice()
	d.register()
	token := d.refresh()

	const n = 12
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resp, _ := e.refreshWith(token)
			codes[i] = resp.StatusCode
		}(i)
	}
	close(start)
	wg.Wait()

	ok := 0
	for _, c := range codes {
		if c == 200 {
			ok++
		} else if c != 401 {
			t.Errorf("неожиданный статус %d", c)
		}
	}
	if ok != 1 {
		t.Fatalf("успешных обновлений %d, ожидали ровно 1 (%v)", ok, codes)
	}
	if got := e.count(`SELECT count(*) FROM refresh_tokens`); got != 2 {
		t.Fatalf("в семействе %d токенов, ожидали 2 (оригинал + один потомок)", got)
	}
	if got := e.count(`SELECT count(*) FROM refresh_tokens WHERE replaced_by IS NOT NULL`); got != 1 {
		t.Fatalf("у оригинала должен быть один заместитель, всего замен: %d", got)
	}
}

func TestConcurrentRefreshWithGap(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RefreshReuseGap = time.Hour })
	d := e.newDevice()
	d.register()
	token := d.refresh()

	const n = 12
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resp, _ := e.refreshWith(token)
			codes[i] = resp.StatusCode
		}(i)
	}
	close(start)
	wg.Wait()

	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case 200:
			ok++
		case 409:
			conflict++
		default:
			t.Errorf("неожиданный статус %d", c)
		}
	}
	if ok != 1 || conflict != n-1 {
		t.Fatalf("ok=%d conflict=%d (%v)", ok, conflict, codes)
	}
	if got := e.count(`SELECT count(*) FROM refresh_tokens`); got != 2 {
		t.Fatalf("токенов в семействе %d, ожидали 2", got)
	}
	if got := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NOT NULL`); got != 0 {
		t.Fatal("семейство отозвано")
	}
}

func TestLogoutEndsCurrentSessionOnly(t *testing.T) {
	e := newEnv(t, strict)
	a := e.newDevice()
	user := a.register()
	b := e.newDevice()
	if resp, _ := b.login(user.Username, password); resp.StatusCode != 200 {
		t.Fatal("login b")
	}
	refreshA, accessA := a.refresh(), a.access()

	resp, raw := a.do("POST", "/api/auth/logout", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("logout: %d %s", resp.StatusCode, raw)
	}
	for _, name := range []string{"session", "refresh_token"} {
		if c := findCookie(resp, name); c == nil || c.MaxAge >= 0 || c.Value != "" {
			t.Errorf("cookie %s не удалена: %+v", name, c)
		}
	}
	if a.access() != "" || a.refresh() != "" {
		t.Error("браузер сохранил cookie после выхода")
	}
	if resp, _ := e.refreshWith(refreshA); resp.StatusCode != 401 {
		t.Fatalf("refresh после выхода: %d", resp.StatusCode)
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_reason = 'logout'`); n != 1 {
		t.Fatalf("помечено logout: %d", n)
	}
	// другое устройство не пострадало
	if resp, _ := b.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
		t.Fatalf("сессия b: %d", resp.StatusCode)
	}
	// access-токен — без состояния, он доживает до exp (это сознательный компромисс)
	if resp, _ := e.newDevice().do("GET", "/api/auth/me", nil, withHeader("Authorization", "Bearer "+accessA)); resp.StatusCode != 200 {
		t.Fatalf("access-токен должен действовать до истечения срока: %d", resp.StatusCode)
	}
}

func TestLogoutIsIdempotent(t *testing.T) {
	e := newEnv(t, strict)
	d := e.newDevice()
	for i := 0; i < 2; i++ {
		resp, _ := d.do("POST", "/api/auth/logout", nil)
		if resp.StatusCode != 200 {
			t.Fatalf("logout без сессии: %d", resp.StatusCode)
		}
	}
	r2 := e.newDevice()
	r2.setCookie("/api/auth/logout", "refresh_token", "garbage", "/api/auth")
	if resp, _ := r2.do("POST", "/api/auth/logout", nil); resp.StatusCode != 200 {
		t.Fatalf("logout с мусорным токеном: %d", resp.StatusCode)
	}
}

func TestLogoutAll(t *testing.T) {
	e := newEnv(t, strict)
	a := e.newDevice()
	user := a.register()
	b, c := e.newDevice(), e.newDevice()
	for _, d := range []*device{b, c} {
		if resp, _ := d.login(user.Username, password); resp.StatusCode != 200 {
			t.Fatal("login")
		}
	}
	stranger := e.newDevice()
	stranger.register()

	refreshB := b.refresh()
	resp, raw := a.do("POST", "/api/auth/logout-all", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("logout-all: %d %s", resp.StatusCode, raw)
	}
	if a.access() != "" || a.refresh() != "" {
		t.Error("cookie инициатора не стёрты")
	}
	for name, tok := range map[string]string{"b": refreshB, "c": c.refresh()} {
		if resp, _ := e.refreshWith(tok); resp.StatusCode != 401 {
			t.Errorf("сессия %s пережила logout-all: %d", name, resp.StatusCode)
		}
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL`, user.ID); n != 0 {
		t.Fatalf("активных токенов пользователя: %d", n)
	}
	// чужие сессии не затронуты
	if resp, _ := stranger.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 200 {
		t.Fatalf("сессия другого пользователя пострадала: %d", resp.StatusCode)
	}
}

func TestLogoutAllWithExpiredAccessUsesRefresh(t *testing.T) {
	e := newEnv(t, strict)
	a := e.newDevice()
	user := a.register()
	b := e.newDevice()
	b.login(user.Username, password)

	// у устройства истёк access, остался только refresh
	only := e.newDevice()
	only.setCookie("/api/auth/logout-all", "refresh_token", a.refresh(), "/api/auth")
	if resp, raw := only.do("POST", "/api/auth/logout-all", nil); resp.StatusCode != 200 {
		t.Fatalf("logout-all по refresh: %d %s", resp.StatusCode, raw)
	}
	if resp, _ := b.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 401 {
		t.Fatal("вторая сессия жива")
	}
}

func TestLogoutAllRequiresCredentials(t *testing.T) {
	e := newEnv(t, strict)
	if resp, _ := e.newDevice().do("POST", "/api/auth/logout-all", nil); resp.StatusCode != 401 {
		t.Fatalf("без токенов: %d", resp.StatusCode)
	}
	a := e.newDevice()
	a.register()
	a.do("POST", "/api/auth/logout", nil)
	stale := e.newDevice()
	stale.setCookie("/api/auth/logout-all", "refresh_token", "garbage", "/api/auth")
	if resp, _ := stale.do("POST", "/api/auth/logout-all", nil); resp.StatusCode != 401 {
		t.Fatalf("с мусорным refresh: %d", resp.StatusCode)
	}
}

func TestPasswordChangeRevokesAllSessions(t *testing.T) {
	e := newEnv(t, strict)
	a := e.newDevice()
	user := a.register()
	b := e.newDevice()
	b.login(user.Username, password)
	oldRefreshA, refreshB := a.refresh(), b.refresh()

	resp, raw := a.do("PUT", "/api/me/password", map[string]string{
		"current": password, "newPassword": "new-password-1", "confirm": "new-password-1",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("смена пароля: %d %s", resp.StatusCode, raw)
	}
	for name, tok := range map[string]string{"A (старый токен)": oldRefreshA, "B": refreshB} {
		if r, _ := e.refreshWith(tok); r.StatusCode != 401 {
			t.Errorf("сессия %s пережила смену пароля: %d", name, r.StatusCode)
		}
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_reason = 'password_change'`); n < 2 {
		t.Fatalf("отозвано по смене пароля: %d", n)
	}
	// Устройство, сменившее пароль, получило новую сессию и осталось в аккаунте
	if a.refresh() == "" || a.refresh() == oldRefreshA {
		t.Fatal("новая сессия не выдана")
	}
	if r, _ := a.do("GET", "/api/auth/me", nil); r.StatusCode != 200 {
		t.Fatalf("me после смены пароля: %d", r.StatusCode)
	}
	if r, _ := a.do("POST", "/api/auth/refresh", nil); r.StatusCode != 200 {
		t.Fatalf("refresh новой сессии: %d", r.StatusCode)
	}
	if r, _ := e.newDevice().login(user.Username, password); r.StatusCode != 401 {
		t.Fatal("старый пароль всё ещё действует")
	}
	if r, _ := e.newDevice().login(user.Username, "new-password-1"); r.StatusCode != 200 {
		t.Fatal("новый пароль не работает")
	}
}

func TestWrongCurrentPasswordKeepsSessions(t *testing.T) {
	e := newEnv(t, strict)
	a := e.newDevice()
	a.register()
	r, _ := a.do("PUT", "/api/me/password", map[string]string{"current": "nope-nope", "newPassword": "new-password-1", "confirm": "new-password-1"})
	if r.StatusCode != 400 {
		t.Fatalf("%d", r.StatusCode)
	}
	if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NOT NULL`); n != 0 {
		t.Fatal("неудачная смена пароля отозвала сессии")
	}
}

func TestSessionLifetimes(t *testing.T) {
	e := newEnv(t, strict)

	t.Run("бездействие 7 суток", func(t *testing.T) {
		d := e.newDevice()
		d.register()
		e.sql.Exec(`UPDATE refresh_tokens SET created_at = created_at - interval '8 days', expires_at = clock_timestamp() - interval '1 second',
			family_expires_at = family_expires_at WHERE encode(token_hash,'hex') = $1`, hashHex(d.refresh()))
		resp, raw := d.do("POST", "/api/auth/refresh", nil)
		if resp.StatusCode != 401 || errCode(raw) != "invalid_refresh" {
			t.Fatalf("просроченный по бездействию токен принят: %d %s", resp.StatusCode, raw)
		}
	})

	t.Run("предел сессии 30 суток", func(t *testing.T) {
		d := e.newDevice()
		d.register()
		h := hashHex(d.refresh())
		if _, err := e.sql.Exec(`UPDATE refresh_tokens SET created_at = created_at - interval '31 days',
			family_expires_at = clock_timestamp() - interval '1 second', expires_at = clock_timestamp() - interval '1 second'
			WHERE encode(token_hash,'hex') = $1`, h); err != nil {
			t.Fatal(err)
		}
		if resp, _ := d.do("POST", "/api/auth/refresh", nil); resp.StatusCode != 401 {
			t.Fatalf("токен после предела сессии принят: %d", resp.StatusCode)
		}
	})

	t.Run("новый токен не переживает семейство", func(t *testing.T) {
		d := e.newDevice()
		d.register()
		h := hashHex(d.refresh())
		// до предела семейства остаётся сутки, хотя бездействие позволило бы 7
		if _, err := e.sql.Exec(`UPDATE refresh_tokens SET family_expires_at = clock_timestamp() + interval '1 day',
			expires_at = LEAST(expires_at, clock_timestamp() + interval '1 day') WHERE encode(token_hash,'hex') = $1`, h); err != nil {
			t.Fatal(err)
		}
		resp, raw := d.do("POST", "/api/auth/refresh", nil)
		if resp.StatusCode != 200 {
			t.Fatalf("refresh: %d %s", resp.StatusCode, raw)
		}
		c := findCookie(resp, "refresh_token")
		if c == nil || c.MaxAge > 24*3600 || c.MaxAge < 24*3600-30 {
			t.Fatalf("MaxAge нового refresh = %+v, ожидали ≤ суток", c)
		}
		if n := e.count(`SELECT count(*) FROM refresh_tokens WHERE expires_at > family_expires_at`); n != 0 {
			t.Fatal("expires_at вышел за предел семейства")
		}
	})

	t.Run("ротация продлевает бездействие до 7 суток", func(t *testing.T) {
		d := e.newDevice()
		d.register()
		// токен остался жить сутки, но не просрочен
		e.sql.Exec(`UPDATE refresh_tokens SET expires_at = clock_timestamp() + interval '1 day' WHERE encode(token_hash,'hex') = $1`, hashHex(d.refresh()))
		resp, _ := d.do("POST", "/api/auth/refresh", nil)
		if c := findCookie(resp, "refresh_token"); c == nil || c.MaxAge < 7*24*3600-30 {
			t.Fatalf("новый refresh должен снова жить 7 суток: %+v", c)
		}
	})
}

func TestCleanupKeepsHistoryOfLiveFamilies(t *testing.T) {
	e := newEnv(t, strict)
	live := e.newDevice()
	live.register()
	live.do("POST", "/api/auth/refresh", nil) // в живом семействе есть использованный токен

	dead := e.newDevice()
	dead.register()
	dead.do("POST", "/api/auth/refresh", nil)
	deadFamily := ""
	e.sql.QueryRow(`SELECT family_id::text FROM refresh_tokens WHERE encode(token_hash,'hex') = $1`, hashHex(dead.refresh())).Scan(&deadFamily)

	// Семейство, истёкшее давно / недавно
	recent := e.newDevice()
	recent.register()
	var recentFamily string
	e.sql.QueryRow(`SELECT family_id::text FROM refresh_tokens WHERE encode(token_hash,'hex') = $1`, hashHex(recent.refresh())).Scan(&recentFamily)

	if _, err := e.sql.Exec(`UPDATE refresh_tokens SET family_expires_at = clock_timestamp() - interval '8 days',
		expires_at = clock_timestamp() - interval '9 days', created_at = clock_timestamp() - interval '10 days'
		WHERE family_id = $1`, deadFamily); err != nil {
		t.Fatal(err)
	}
	if _, err := e.sql.Exec(`UPDATE refresh_tokens SET family_expires_at = clock_timestamp() - interval '1 day',
		expires_at = clock_timestamp() - interval '2 days', created_at = clock_timestamp() - interval '3 days'
		WHERE family_id = $1`, recentFamily); err != nil {
		t.Fatal(err)
	}
	before := e.count(`SELECT count(*) FROM refresh_tokens`)
	n, err := e.q.DeleteExpiredSessions(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 { // оба токена давно умершего семейства
		t.Fatalf("удалено %d, ожидали 2", n)
	}
	if after := e.count(`SELECT count(*) FROM refresh_tokens`); after != before-2 {
		t.Fatalf("осталось %d из %d", after, before)
	}
	if e.count(`SELECT count(*) FROM refresh_tokens WHERE family_id = $1`, recentFamily) != 1 {
		t.Fatal("недавно истёкшая сессия удалена раньше срока хранения")
	}
	if e.count(`SELECT count(*) FROM refresh_tokens WHERE used_at IS NOT NULL AND revoked_at IS NULL AND family_expires_at > clock_timestamp()`) != 1 {
		t.Fatal("использованный токен живого семейства удалён — пропадёт обнаружение повторного использования")
	}
}

func TestDBLevelErrors(t *testing.T) {
	e := newEnv(t, strict)
	st, err := e.q.CreateSession(context.Background(), 0, make([]byte, 32), time.Hour, time.Hour)
	if err == nil {
		t.Fatalf("сессия несуществующего пользователя создана: %+v", st)
	}
	if err != db.ErrBadRef {
		t.Fatalf("ожидали ErrBadRef, получили %v", err)
	}
	if _, err := e.q.RotateRefreshToken(context.Background(), make([]byte, 32), make([]byte, 32), time.Hour, 0); err != db.ErrRefreshInvalid {
		t.Fatalf("неизвестный токен: %v", err)
	}
}
