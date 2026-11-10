package handler

// Сквозные тесты: настоящий PostgreSQL, настоящие middleware, REST и WebSocket.
// Запускаются только при заданной TEST_DATABASE_URL, например:
//
//	TEST_DATABASE_URL=postgres://auction_user:auction_pass@localhost:5432/auction?sslmode=disable go test ./handler
//
// Тесты создают собственных пользователей и лоты (с уникальными именами) и ничего не удаляют.

import (
	"auction/config"
	"auction/db"
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type env struct {
	t   *testing.T
	srv *httptest.Server
	h   *Handler
	sql *sql.DB
	id  int64 // уникальный суффикс запуска
	n   int
}

func setup(t *testing.T, window, extend time.Duration) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — сквозные тесты пропущены")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	q := db.New(sqlDB)
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		JWTSecret:       "test-secret-test-secret",
		AntiSnipeWindow: window,
		AntiSnipeExtend: extend,
	}
	h := New(q, cfg)
	if err := h.StartLive(context.Background()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	SetupRoutes(mux, h)
	srv := httptest.NewServer(Middleware(mux)) // именно с Middleware: он оборачивает ResponseWriter
	t.Cleanup(func() {
		h.StopLive()
		srv.Close()
	})
	return &env{t: t, srv: srv, h: h, sql: sqlDB, id: time.Now().UnixNano() % 1_000_000_000}
}

/* ---------- REST-помощники ---------- */

func (e *env) do(method, path, cookie string, body any) (int, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if c := resp.Header.Get("Set-Cookie"); c != "" {
		if out == nil {
			out = map[string]any{}
		}
		out["_cookie"] = strings.SplitN(c, ";", 2)[0]
	}
	return resp.StatusCode, out
}

type user struct {
	id     int64
	cookie string
}

func (e *env) register(label string) user {
	e.t.Helper()
	e.n++
	name := fmt.Sprintf("%s%d_%d", label, e.id, e.n)
	status, out := e.do("POST", "/api/auth/register", "", map[string]any{
		"name": name, "username": name, "telegram": "tg_" + name, "dormId": 1,
		"password": "secret123", "confirm": "secret123",
	})
	if status != 201 {
		e.t.Fatalf("регистрация: %d %v", status, out)
	}
	return user{id: int64(out["id"].(float64)), cookie: out["_cookie"].(string)}
}

// lot создаёт лот на минуту с хвостиком (минимально допустимая длительность).
func (e *env) lot(seller user, start, step int) int64 {
	e.t.Helper()
	status, out := e.do("POST", "/api/lots", seller.cookie, map[string]any{
		"title": "Лот", "description": "Описание", "image": "/uploads/" + strings.Repeat("a", 32) + ".png",
		"category": "furniture", "subcategory": "tables", "startPrice": start, "step": step, "hours": 0.02,
	})
	if status != 201 {
		e.t.Fatalf("создание лота: %d %v", status, out)
	}
	return int64(out["id"].(float64))
}

// endsIn переносит конец аукциона напрямую в БД и пересоздаёт таймер (как при старте сервера).
func (e *env) endsIn(lotID int64, d time.Duration) {
	e.t.Helper()
	if _, err := e.sql.Exec(`UPDATE lots SET ends_at = clock_timestamp() + $2 * interval '1 millisecond' WHERE id = $1`,
		lotID, d.Milliseconds()); err != nil {
		e.t.Fatal(err)
	}
	e.h.hub.LotCreated(lotID, time.Now().Add(d))
}

func (e *env) bid(u user, lotID int64, amount int) (int, map[string]any) {
	e.t.Helper()
	return e.do("POST", fmt.Sprintf("/api/lots/%d/bids", lotID), u.cookie, map[string]any{"amount": amount})
}

func (e *env) getLot(lotID int64) map[string]any {
	e.t.Helper()
	status, out := e.do("GET", fmt.Sprintf("/api/lots/%d", lotID), "", nil)
	if status != 200 {
		e.t.Fatalf("GET lot: %d", status)
	}
	return out
}

/* ---------- WebSocket-клиент ---------- */

type wsc struct {
	t  *testing.T
	nc net.Conn
	br *bufio.Reader
}

func (e *env) ws(cookie string, hdr map[string]string) (*wsc, int) {
	e.t.Helper()
	nc, err := net.Dial("tcp", strings.TrimPrefix(e.srv.URL, "http://"))
	if err != nil {
		e.t.Fatal(err)
	}
	var b bytes.Buffer
	b.WriteString("GET /api/ws HTTP/1.1\r\nHost: " + strings.TrimPrefix(e.srv.URL, "http://") +
		"\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n")
	if cookie != "" {
		b.WriteString("Cookie: " + cookie + "\r\n")
	}
	for k, v := range hdr {
		b.WriteString(k + ": " + v + "\r\n")
	}
	b.WriteString("\r\n")
	nc.Write(b.Bytes())
	br := bufio.NewReader(nc)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		nc.Close()
		return nil, resp.StatusCode
	}
	c := &wsc{t: e.t, nc: nc, br: br}
	e.t.Cleanup(func() { nc.Close() })
	return c, 101
}

func (e *env) dial(cookie string) *wsc {
	e.t.Helper()
	c, status := e.ws(cookie, nil)
	if status != 101 {
		e.t.Fatalf("WebSocket: статус %d", status)
	}
	return c
}

func (c *wsc) send(v any) {
	c.t.Helper()
	p, _ := json.Marshal(v)
	var b bytes.Buffer
	b.WriteByte(0x81)
	if len(p) < 126 {
		b.WriteByte(0x80 | byte(len(p)))
	} else {
		b.WriteByte(0x80 | 126)
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(p)))
		b.Write(l[:])
	}
	key := [4]byte{9, 8, 7, 6}
	b.Write(key[:])
	for i, ch := range p {
		b.WriteByte(ch ^ key[i&3])
	}
	if _, err := c.nc.Write(b.Bytes()); err != nil {
		c.t.Fatal(err)
	}
}

type msg = map[string]any

func (c *wsc) next(d time.Duration) (msg, bool) {
	deadline := time.Now().Add(d)
	for {
		c.nc.SetReadDeadline(deadline)
		var h [2]byte
		if _, err := io.ReadFull(c.br, h[:]); err != nil {
			return nil, false
		}
		n := int(h[1] & 0x7f)
		if n == 126 {
			var l [2]byte
			io.ReadFull(c.br, l[:])
			n = int(binary.BigEndian.Uint16(l[:]))
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(c.br, p); err != nil {
			return nil, false
		}
		if h[0]&0x0f == 0x1 {
			var m msg
			if err := json.Unmarshal(p, &m); err != nil {
				c.t.Fatalf("не JSON: %q", p)
			}
			return m, true
		}
	}
}

func (c *wsc) want(typ string) msg {
	c.t.Helper()
	m, ok := c.next(5 * time.Second)
	if !ok {
		c.t.Fatalf("ждали %q, сообщения нет", typ)
	}
	if m["type"] != typ {
		c.t.Fatalf("ждали %q, получили %v", typ, m)
	}
	return m
}

func (c *wsc) wantNothing(d time.Duration) {
	c.t.Helper()
	if m, ok := c.next(d); ok {
		c.t.Fatalf("не ждали сообщений, получили %v", m)
	}
}

func (c *wsc) subscribe(lotID int64) msg {
	c.t.Helper()
	c.send(msg{"type": "subscribe", "lotId": lotID})
	return c.want("snapshot")
}

func lotOf(m msg) msg      { return m["lot"].(map[string]any) }
func num(v any) int64      { return int64(v.(float64)) }
func ms(t time.Time) int64 { return t.UnixMilli() }

/* ---------- тесты ---------- */

func TestE2E_BidBroadcastAndAntiSnipe(t *testing.T) {
	e := setup(t, 90*time.Second, 150*time.Second)
	seller, bidder := e.register("seller"), e.register("bidder")
	lotID := e.lot(seller, 100, 10) // длится ≈72 с < окна 90 с

	watcher := e.dial("") // аноним: можно смотреть
	hello := watcher.want("hello")
	if hello["userId"] != nil {
		t.Fatalf("hello: %v", hello)
	}
	snap := watcher.subscribe(lotID)
	if l := lotOf(snap); l["status"] != "active" || num(l["minBid"]) != 100 || num(l["bidsCount"]) != 0 {
		t.Fatalf("снимок: %v", snap)
	}
	endsBefore := num(lotOf(snap)["endsAt"])

	// первая ставка равна стартовой цене; осталось меньше окна — срабатывает Anti-snipe
	status, res := e.bid(bidder, lotID, 100)
	if status != 201 || res["extended"] != true {
		t.Fatalf("ставка: %d %v", status, res)
	}
	ev := watcher.want("bid")
	l := lotOf(ev)
	if ev["extended"] != true || num(l["currentPrice"]) != 100 || num(l["minBid"]) != 110 ||
		num(l["bidsCount"]) != 1 || num(l["leaderId"]) != bidder.id {
		t.Fatalf("событие: %v", ev)
	}
	endsAfter := num(l["endsAt"])
	if want := ms(time.Now().Add(150 * time.Second)); endsAfter < want-5000 || endsAfter > want+1000 {
		t.Fatalf("endsAt после продления: %d, ждали ≈%d", endsAfter, want)
	}
	if endsAfter <= endsBefore {
		t.Fatalf("endsAt не вырос: %d → %d", endsBefore, endsAfter)
	}
	// REST отдаёт то же время окончания
	if got := num(e.getLot(lotID)["endsAt"]); got != endsAfter {
		t.Fatalf("REST endsAt %d ≠ событие %d", got, endsAfter)
	}
	// после продления до конца ≈150 с, окно 90 с — следующая ставка продлевать уже не должна
	other := e.register("other")
	status, res = e.bid(other, lotID, 110)
	if status != 201 || res["extended"] != false {
		t.Fatalf("вторая ставка: %d %v", status, res)
	}
	if ev := watcher.want("bid"); ev["extended"] != false || num(lotOf(ev)["endsAt"]) != endsAfter {
		t.Fatalf("вторая ставка не должна двигать срок: %v", ev)
	}
}

func TestE2E_BidViaWebSocket(t *testing.T) {
	e := setup(t, 0, 0) // Anti-snipe выключен
	seller, bidder := e.register("seller"), e.register("bidder")
	lotID := e.lot(seller, 500, 50)

	c := e.dial(bidder.cookie)
	hello := c.want("hello")
	if num(hello["userId"]) != bidder.id || hello["antiSnipe"].(map[string]any)["enabled"] != false {
		t.Fatalf("hello: %v", hello)
	}
	c.subscribe(lotID)

	c.send(msg{"type": "bid", "lotId": lotID, "amount": 500, "reqId": "a"}) // = стартовой цене
	ev := c.want("bid")
	if num(lotOf(ev)["currentPrice"]) != 500 || num(lotOf(ev)["minBid"]) != 550 || ev["extended"] != false {
		t.Fatalf("событие: %v", ev)
	}
	if ok := c.want("bid_ok"); ok["reqId"] != "a" || num(ok["bid"].(msg)["userId"]) != bidder.id {
		t.Fatalf("bid_ok: %v", ok)
	}
	if got := e.getLot(lotID); num(got["bidsCount"]) != 1 || num(got["currentPrice"]) != 500 {
		t.Fatalf("REST после ставки по WS: %v", got)
	}

	// правила те же, что у REST
	c.send(msg{"type": "bid", "lotId": lotID, "amount": 600, "reqId": "b"})
	if er := c.want("error"); er["code"] != "already_leading" || er["reqId"] != "b" {
		t.Fatalf("%v", er)
	}
	s := e.dial(seller.cookie)
	s.want("hello")
	s.send(msg{"type": "bid", "lotId": lotID, "amount": 600})
	if er := s.want("error"); er["code"] != "own_lot" {
		t.Fatalf("%v", er)
	}
}

func TestE2E_FinishTimer(t *testing.T) {
	e := setup(t, 0, 0)
	seller := e.register("seller")
	lotID := e.lot(seller, 100, 10)
	e.endsIn(lotID, 1500*time.Millisecond)

	w := e.dial("")
	w.want("hello")
	w.subscribe(lotID)
	start := time.Now()
	ev := w.want("lot_finished")
	if el := time.Since(start); el < 900*time.Millisecond || el > 3*time.Second {
		t.Fatalf("lot_finished через %v, ждали ≈1,5 с", el)
	}
	if lotOf(ev)["status"] != "finished" {
		t.Fatalf("%v", ev)
	}
	if got := e.getLot(lotID)["status"]; got != "finished" {
		t.Fatalf("REST: статус %v", got)
	}
	// после конца ставка отклоняется по часам БД
	bidder := e.register("bidder")
	if status, res := e.bid(bidder, lotID, 100); status != 409 || res["code"] != "lot_closed" {
		t.Fatalf("ставка после конца: %d %v", status, res)
	}
}

func TestE2E_AntiSnipeDelaysFinish(t *testing.T) {
	e := setup(t, time.Second, 1500*time.Millisecond)
	seller, bidder := e.register("seller"), e.register("bidder")
	lotID := e.lot(seller, 100, 10)
	e.endsIn(lotID, 1500*time.Millisecond)

	w := e.dial("")
	w.want("hello")
	w.subscribe(lotID)

	time.Sleep(900 * time.Millisecond) // осталось ≈600 мс < окна 1 с
	bidAt := time.Now()
	status, res := e.bid(bidder, lotID, 100)
	if status != 201 || res["extended"] != true {
		t.Fatalf("ставка: %d %v", status, res)
	}
	if ev := w.want("bid"); ev["extended"] != true {
		t.Fatalf("%v", ev)
	}

	// на прежнем сроке (≈600 мс после ставки) аукцион обязан ещё идти
	time.Sleep(1000*time.Millisecond - time.Since(bidAt))
	if got := e.getLot(lotID)["status"]; got != "active" {
		t.Fatalf("через 1 с после ставки статус %v, ждали active (срок продлён до ≈1,5 с)", got)
	}
	ev := w.want("lot_finished")
	if el := time.Since(bidAt); el < 1300*time.Millisecond || el > 4*time.Second {
		t.Fatalf("lot_finished через %v после ставки, ждали ≈1,5 с", el)
	}
	if lotOf(ev)["status"] != "finished" || num(lotOf(ev)["bidsCount"]) != 1 {
		t.Fatalf("%v", ev)
	}
}

func TestE2E_ConcurrentEqualBidsExactlyOneWins(t *testing.T) {
	e := setup(t, 0, 0)
	seller := e.register("seller")
	lotID := e.lot(seller, 100, 10)
	w := e.dial("")
	w.want("hello")
	w.subscribe(lotID)

	const n = 6
	users := make([]user, n)
	for i := range users {
		users[i] = e.register("u")
	}
	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := range users {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i], _ = e.bid(users[i], lotID, 100)
		}(i)
	}
	wg.Wait()

	won, low := 0, 0
	for _, s := range statuses {
		switch s {
		case 201:
			won++
		case 400:
			low++
		}
	}
	if won != 1 || low != n-1 {
		t.Fatalf("статусы %v: ждали одну принятую и %d bid_too_low", statuses, n-1)
	}
	w.want("bid")
	w.wantNothing(300 * time.Millisecond) // событие ровно одно
	var cnt int
	e.sql.QueryRow(`SELECT count(*) FROM bids WHERE lot_id = $1`, lotID).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("в БД %d ставок, ждали 1", cnt)
	}
}

func TestE2E_CancelNotifiesAndNoFinish(t *testing.T) {
	e := setup(t, 0, 0)
	seller := e.register("seller")
	lotID := e.lot(seller, 100, 10)
	e.endsIn(lotID, 1200*time.Millisecond)
	w := e.dial("")
	w.want("hello")
	w.subscribe(lotID)

	if status, res := e.do("DELETE", fmt.Sprintf("/api/lots/%d", lotID), seller.cookie, nil); status != 200 && status != 204 {
		t.Fatalf("отмена: %d %v", status, res)
	}
	if ev := w.want("lot_cancelled"); lotOf(ev)["status"] != "cancelled" {
		t.Fatalf("%v", ev)
	}
	w.wantNothing(2 * time.Second) // lot_finished для отменённого лота не приходит
}

func TestE2E_ClosedLotRejectsBidAndHandshakeOrigin(t *testing.T) {
	e := setup(t, time.Minute, time.Minute)
	seller, bidder := e.register("seller"), e.register("bidder")
	lotID := e.lot(seller, 100, 10)
	if _, err := e.sql.Exec(`UPDATE lots SET ends_at = created_at + interval '1 millisecond' WHERE id = $1`, lotID); err != nil {
		t.Fatal(err)
	}
	if status, res := e.bid(bidder, lotID, 100); status != 409 || res["code"] != "lot_closed" {
		t.Fatalf("%d %v", status, res)
	}

	if _, status := e.ws("", map[string]string{"Origin": "http://evil.example"}); status != 403 {
		t.Fatalf("чужой Origin: статус %d, ждали 403", status)
	}
	host := strings.TrimPrefix(e.srv.URL, "http://")
	if c, status := e.ws("", map[string]string{"Origin": "http://" + host}); status != 101 {
		t.Fatalf("свой Origin: статус %d", status)
	} else {
		c.nc.Close()
	}
}
