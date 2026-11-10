package live

import (
	"auction/db"
	"auction/model"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

/* ---------- фейковое хранилище ---------- */

type fakeLot struct {
	author     int64
	status     string
	endsAt     time.Time
	startPrice int
	step       int
	bids       []model.Bid
}

type fakeStore struct {
	mu      sync.Mutex
	lots    map[int64]*fakeLot
	nextBid int64
	jitter  bool // после фиксации ставки «задумывается» — провоцирует перестановку событий
}

func newFakeStore() *fakeStore { return &fakeStore{lots: map[int64]*fakeLot{}} }

func (s *fakeStore) addLot(id, author int64, left time.Duration, start, step int) *fakeLot {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := &fakeLot{author: author, status: "active", endsAt: time.Now().Add(left), startPrice: start, step: step}
	s.lots[id] = l
	return l
}

func (s *fakeStore) setEnds(id int64, t time.Time) {
	s.mu.Lock()
	s.lots[id].endsAt = t
	s.mu.Unlock()
}

func (l *fakeLot) live(id int64) model.LotLive {
	st := l.status
	if st == "active" && !l.endsAt.After(time.Now()) {
		st = "finished"
	}
	price, min := l.startPrice, l.startPrice
	var leader *int64
	if n := len(l.bids); n > 0 {
		top := l.bids[n-1]
		price, min = top.Amount, top.Amount+l.step
		id := top.UserID
		leader = &id
	}
	return model.LotLive{
		ID: id, Status: st, CurrentPrice: price, MinBid: min,
		BidsCount: len(l.bids), LeaderID: leader, EndsAt: l.endsAt.UnixMilli(),
	}
}

func (s *fakeStore) PlaceBid(ctx context.Context, lotID, userID int64, amount int, as db.AntiSnipe) (db.PlacedBid, error) {
	s.mu.Lock()
	l, ok := s.lots[lotID]
	if !ok {
		s.mu.Unlock()
		return db.PlacedBid{}, db.ErrNotFound
	}
	now := time.Now()
	switch {
	case l.author == userID:
		s.mu.Unlock()
		return db.PlacedBid{}, model.Errf(403, "own_lot", "Нельзя делать ставку на свой лот")
	case l.status != "active" || !l.endsAt.After(now):
		s.mu.Unlock()
		return db.PlacedBid{}, model.Errf(409, "lot_closed", "Аукцион уже закрыт")
	}
	min := l.startPrice
	if n := len(l.bids); n > 0 {
		min = l.bids[n-1].Amount + l.step
		if l.bids[n-1].UserID == userID {
			s.mu.Unlock()
			return db.PlacedBid{}, model.Errf(409, "already_leading", "Ваша ставка и так лидирует")
		}
	}
	if amount < min {
		s.mu.Unlock()
		return db.PlacedBid{}, model.Errf(400, "bid_too_low", "Минимальная ставка: %d", min)
	}
	s.nextBid++
	b := model.Bid{
		ID: s.nextBid, LotID: lotID, UserID: userID, Username: "user" + strconv.FormatInt(userID, 10),
		Amount: amount, CreatedAt: now.UnixMilli(),
	}
	l.bids = append(l.bids, b)
	res := db.PlacedBid{Bid: b}
	left := l.endsAt.Sub(now)
	if as.Enabled() && left <= as.Window {
		if ne := now.Add(as.Extend); ne.After(l.endsAt) {
			l.endsAt, left, res.Extended = ne, as.Extend, true
		}
	}
	res.EndsAt, res.Left = l.endsAt, left
	s.mu.Unlock()
	if s.jitter {
		time.Sleep(time.Duration(rand.Intn(3000)) * time.Microsecond)
	}
	return res, nil
}

func (s *fakeStore) CancelLot(ctx context.Context, lotID, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lots[lotID]
	if !ok {
		return db.ErrNotFound
	}
	if l.author != userID {
		return model.Errf(403, "forbidden", "Отменить лот может только его автор")
	}
	if len(l.bids) > 0 {
		return model.Errf(409, "has_bids", "Нельзя отменить лот, на который уже сделаны ставки")
	}
	l.status = "cancelled"
	return nil
}

func (s *fakeStore) GetLotLive(ctx context.Context, lotID int64) (model.LotLive, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lots[lotID]
	if !ok {
		return model.LotLive{}, db.ErrNotFound
	}
	return l.live(lotID), nil
}

func (s *fakeStore) LotRemaining(ctx context.Context, lotID int64) (string, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lots[lotID]
	if !ok {
		return "", 0, db.ErrNotFound
	}
	return l.status, time.Until(l.endsAt), nil
}

func (s *fakeStore) ActiveLotEnds(ctx context.Context) ([]db.LotEnd, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []db.LotEnd
	for id, l := range s.lots {
		if l.status == "active" && l.endsAt.After(time.Now()) {
			out = append(out, db.LotEnd{ID: id, Left: time.Until(l.endsAt)})
		}
	}
	return out, nil
}

/* ---------- тестовый WebSocket-клиент ---------- */

type wsc struct {
	t  *testing.T
	nc net.Conn
	br *bufio.Reader
}

func newServer(t *testing.T, h *Hub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, _ := strconv.ParseInt(r.URL.Query().Get("uid"), 10, 64)
		h.ServeWS(w, r, uid)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newHub(t *testing.T, st Store, opts Options) *Hub {
	t.Helper()
	h := New(st, opts)
	t.Cleanup(h.Shutdown)
	return h
}

// dialRaw выполняет рукопожатие и возвращает HTTP-статус; клиент создаётся только при 101.
func dialRaw(t *testing.T, srv *httptest.Server, uid int64, hdr map[string]string) (*wsc, int) {
	t.Helper()
	nc, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	b.WriteString("GET /ws?uid=" + strconv.FormatInt(uid, 10) + " HTTP/1.1\r\nHost: " + strings.TrimPrefix(srv.URL, "http://") +
		"\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n")
	for k, v := range hdr {
		b.WriteString(k + ": " + v + "\r\n")
	}
	b.WriteString("\r\n")
	nc.Write(b.Bytes())
	br := bufio.NewReader(nc)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		nc.Close()
		return nil, resp.StatusCode
	}
	c := &wsc{t: t, nc: nc, br: br}
	t.Cleanup(func() { nc.Close() })
	return c, 101
}

func dial(t *testing.T, srv *httptest.Server, uid int64) *wsc {
	t.Helper()
	c, status := dialRaw(t, srv, uid, nil)
	if status != 101 {
		t.Fatalf("рукопожатие: статус %d", status)
	}
	return c
}

func (c *wsc) sendJSON(v any) {
	c.t.Helper()
	payload, _ := json.Marshal(v)
	c.sendFrame(0x1, payload)
}

func (c *wsc) sendFrame(op byte, payload []byte) {
	c.t.Helper()
	var b bytes.Buffer
	b.WriteByte(0x80 | op)
	n := len(payload)
	if n < 126 {
		b.WriteByte(0x80 | byte(n))
	} else {
		b.WriteByte(0x80 | 126)
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(n))
		b.Write(l[:])
	}
	key := [4]byte{1, 2, 3, 4}
	b.Write(key[:])
	for i, ch := range payload {
		b.WriteByte(ch ^ key[i&3])
	}
	if _, err := c.nc.Write(b.Bytes()); err != nil {
		c.t.Fatal(err)
	}
}

// readFrame читает кадр сервера (op, payload) или ошибку по таймауту/закрытию.
func (c *wsc) readFrame(d time.Duration) (byte, []byte, error) {
	c.nc.SetReadDeadline(time.Now().Add(d))
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		return 0, nil, err
	}
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var l [2]byte
		if _, err := io.ReadFull(c.br, l[:]); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint16(l[:]))
	case 127:
		var l [8]byte
		if _, err := io.ReadFull(c.br, l[:]); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint64(l[:]))
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(c.br, p); err != nil {
		return 0, nil, err
	}
	return h[0] & 0x0f, p, nil
}

type msg = map[string]any

// next возвращает следующее JSON-сообщение (ping/pong пропускает).
func (c *wsc) next(d time.Duration) (msg, bool) {
	c.t.Helper()
	deadline := time.Now().Add(d)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return nil, false
		}
		op, p, err := c.readFrame(left)
		if err != nil {
			return nil, false
		}
		switch op {
		case 0x1:
			var m msg
			if err := json.Unmarshal(p, &m); err != nil {
				c.t.Fatalf("не JSON: %q", p)
			}
			return m, true
		case 0x8:
			return msg{"type": "<close>", "code": float64(binary.BigEndian.Uint16(append(p, 0, 0)[:2]))}, true
		}
	}
}

func (c *wsc) want(typ string) msg {
	c.t.Helper()
	m, ok := c.next(3 * time.Second)
	if !ok {
		c.t.Fatalf("ждали %q, но сообщения нет", typ)
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

func lotOf(m msg) msg { return m["lot"].(map[string]any) }
func num(v any) int64 { return int64(v.(float64)) }

// subscribe подписывает клиента и проверяет снимок.
func (c *wsc) subscribe(lotID int64) msg {
	c.t.Helper()
	c.sendJSON(msg{"type": "subscribe", "lotId": lotID, "reqId": "s"})
	m := c.want("snapshot")
	if num(m["lotId"]) != lotID || m["reqId"] != "s" {
		c.t.Fatalf("снимок: %v", m)
	}
	return m
}

/* ---------- тесты ---------- */

func TestHelloAndPong(t *testing.T) {
	h := newHub(t, newFakeStore(), Options{AntiSnipe: db.AntiSnipe{Window: 2 * time.Minute, Extend: 3 * time.Minute}})
	srv := newServer(t, h)

	c := dial(t, srv, 7)
	hello := c.want("hello")
	if num(hello["userId"]) != 7 {
		t.Fatalf("userId: %v", hello)
	}
	as := hello["antiSnipe"].(map[string]any)
	if as["enabled"] != true || num(as["windowMs"]) != 120000 || num(as["extendMs"]) != 180000 {
		t.Fatalf("antiSnipe: %v", as)
	}
	if d := time.Now().UnixMilli() - num(hello["now"]); d < -2000 || d > 2000 {
		t.Fatalf("now расходится с часами: %d мс", d)
	}
	c.sendJSON(msg{"type": "ping"})
	c.want("pong")

	anon := dial(t, srv, 0)
	if anon.want("hello")["userId"] != nil {
		t.Fatal("у анонима userId должен быть null")
	}
}

func TestSubscribeSnapshotAndBidBroadcast(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, time.Hour, 500, 50)
	h := newHub(t, st, Options{})
	srv := newServer(t, h)

	a, b, watcher := dial(t, srv, 1), dial(t, srv, 2), dial(t, srv, 0)
	for _, c := range []*wsc{a, b, watcher} {
		c.want("hello")
		snap := c.subscribe(1)
		l := lotOf(snap)
		if l["status"] != "active" || num(l["currentPrice"]) != 500 || num(l["minBid"]) != 500 || num(l["bidsCount"]) != 0 || l["leaderId"] != nil {
			t.Fatalf("снимок: %v", snap)
		}
	}

	// первая ставка может быть равна стартовой цене
	a.sendJSON(msg{"type": "bid", "lotId": 1, "amount": 500, "reqId": "r1"})
	for i, c := range []*wsc{a, b, watcher} {
		ev := c.want("bid")
		l := lotOf(ev)
		if num(ev["lotId"]) != 1 || num(l["currentPrice"]) != 500 || num(l["minBid"]) != 550 ||
			num(l["bidsCount"]) != 1 || num(l["leaderId"]) != 1 || ev["extended"] != false {
			t.Fatalf("событие bid: %v", ev)
		}
		if ev["bid"].(map[string]any)["username"] != "user1" {
			t.Fatalf("имя ставившего: %v", ev["bid"])
		}
		if i == 0 { // ставившему после события приходит ещё и подтверждение
			ok := c.want("bid_ok")
			if ok["reqId"] != "r1" || ok["extended"] != false || num(ok["bid"].(map[string]any)["amount"]) != 500 {
				t.Fatalf("bid_ok: %v", ok)
			}
		}
	}

	// отказ приходит только ставившему и с кодом как в REST
	b.sendJSON(msg{"type": "bid", "lotId": 1, "amount": 520, "reqId": "r2"})
	e := b.want("error")
	if e["code"] != "bid_too_low" || e["reqId"] != "r2" || num(e["lotId"]) != 1 {
		t.Fatalf("ошибка: %v", e)
	}
	a.wantNothing(150 * time.Millisecond)
	watcher.wantNothing(50 * time.Millisecond)
}

func TestBidErrors(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, time.Hour, 500, 50)
	h := newHub(t, st, Options{})
	srv := newServer(t, h)

	anon := dial(t, srv, 0)
	anon.want("hello")
	anon.sendJSON(msg{"type": "bid", "lotId": 1, "amount": 600})
	if e := anon.want("error"); e["code"] != "unauthorized" {
		t.Fatalf("%v", e)
	}

	owner := dial(t, srv, 100)
	owner.want("hello")
	owner.sendJSON(msg{"type": "bid", "lotId": 1, "amount": 600})
	if e := owner.want("error"); e["code"] != "own_lot" {
		t.Fatalf("%v", e)
	}
	owner.sendJSON(msg{"type": "bid", "lotId": 999, "amount": 600})
	if e := owner.want("error"); e["code"] != "not_found" {
		t.Fatalf("%v", e)
	}
	owner.sendJSON(msg{"type": "bid", "lotId": 1, "amount": -5})
	if e := owner.want("error"); e["code"] != "validation" {
		t.Fatalf("%v", e)
	}
	owner.sendJSON(msg{"type": "subscribe", "lotId": 999})
	if e := owner.want("error"); e["code"] != "not_found" {
		t.Fatalf("%v", e)
	}
	owner.sendJSON(msg{"type": "nope"})
	if e := owner.want("error"); e["code"] != "unknown_type" {
		t.Fatalf("%v", e)
	}
	owner.sendFrame(0x1, []byte("{не json"))
	if e := owner.want("error"); e["code"] != "bad_json" {
		t.Fatalf("%v", e)
	}
	owner.sendJSON(msg{"type": "subscribe", "lotId": 1, "reqId": strings.Repeat("x", 65)})
	if e := owner.want("error"); e["code"] != "bad_request" {
		t.Fatalf("%v", e)
	}
	owner.sendJSON(msg{"type": "subscribe"})
	if e := owner.want("error"); e["code"] != "bad_request" {
		t.Fatalf("%v", e)
	}
	// соединение после ошибок остаётся живым
	owner.sendJSON(msg{"type": "ping"})
	owner.want("pong")
}

func TestUnsubscribeStopsEvents(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, time.Hour, 100, 10)
	h := newHub(t, st, Options{})
	srv := newServer(t, h)

	w, bidder := dial(t, srv, 0), dial(t, srv, 2)
	w.want("hello")
	bidder.want("hello")
	w.subscribe(1)
	w.sendJSON(msg{"type": "unsubscribe", "lotId": 1, "reqId": "u"})
	if m := w.want("unsubscribed"); num(m["lotId"]) != 1 || m["reqId"] != "u" {
		t.Fatalf("%v", m)
	}
	bidder.sendJSON(msg{"type": "bid", "lotId": 1, "amount": 100})
	bidder.want("bid_ok") // bidder на лот не подписан — получает только подтверждение
	w.wantNothing(200 * time.Millisecond)
}

func TestSubscriptionLimit(t *testing.T) {
	st := newFakeStore()
	for i := int64(1); i <= maxSubsPerClient+1; i++ {
		st.addLot(i, 100, time.Hour, 100, 10)
	}
	h := newHub(t, st, Options{})
	c := dial(t, newServer(t, h), 0)
	c.want("hello")
	for i := int64(1); i <= maxSubsPerClient; i++ {
		c.subscribe(i)
	}
	c.sendJSON(msg{"type": "subscribe", "lotId": maxSubsPerClient + 1})
	if e := c.want("error"); e["code"] != "too_many_subscriptions" {
		t.Fatalf("%v", e)
	}
	c.subscribe(1) // повторная подписка на уже подписанный лот — не считается
}

func TestFinishEventAtDeadline(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, 300*time.Millisecond, 100, 10)
	h := newHub(t, st, Options{FinishSlack: 20 * time.Millisecond})
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := dial(t, newServer(t, h), 0)
	c.want("hello")
	c.subscribe(1)

	start := time.Now()
	ev := c.want("lot_finished")
	el := time.Since(start)
	if el < 200*time.Millisecond || el > 1500*time.Millisecond {
		t.Fatalf("lot_finished пришёл через %v, ждали ≈300 мс", el)
	}
	if lotOf(ev)["status"] != "finished" || num(ev["lotId"]) != 1 {
		t.Fatalf("%v", ev)
	}
	c.wantNothing(200 * time.Millisecond) // событие приходит ровно один раз

	// подписка на уже завершённый лот сразу даёт status=finished
	if st := lotOf(c.subscribe(1))["status"]; st != "finished" {
		t.Fatalf("статус в снимке: %v", st)
	}
}

func TestLotCreatedSchedulesFinish(t *testing.T) {
	st := newFakeStore()
	h := newHub(t, st, Options{FinishSlack: 20 * time.Millisecond})
	srv := newServer(t, h)
	c := dial(t, srv, 0)
	c.want("hello")

	st.addLot(5, 100, 250*time.Millisecond, 100, 10)
	c.subscribe(5)
	h.LotCreated(5, time.Now().Add(250*time.Millisecond))
	c.want("lot_finished")
}

func TestAntiSnipeExtendsAndTimerFollows(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, 500*time.Millisecond, 100, 10)
	as := db.AntiSnipe{Window: 400 * time.Millisecond, Extend: 700 * time.Millisecond}
	h := newHub(t, st, Options{AntiSnipe: as, FinishSlack: 20 * time.Millisecond})
	h.LotCreated(1, time.Now().Add(500*time.Millisecond))
	srv := newServer(t, h)

	w, bidder := dial(t, srv, 0), dial(t, srv, 2)
	w.want("hello")
	bidder.want("hello")
	snap := w.subscribe(1)
	endsBefore := num(lotOf(snap)["endsAt"])

	time.Sleep(250 * time.Millisecond) // осталось ≈250 мс < окна 400 мс
	bidder.sendJSON(msg{"type": "bid", "lotId": 1, "amount": 100})
	ev := w.want("bid")
	if ev["extended"] != true {
		t.Fatalf("ставка в последние секунды должна продлить аукцион: %v", ev)
	}
	endsAfter := num(lotOf(ev)["endsAt"])
	if endsAfter <= endsBefore+100 {
		t.Fatalf("endsAt не вырос: %d → %d", endsBefore, endsAfter)
	}
	if left := endsAfter - num(ev["now"]); left < 600 || left > 750 {
		t.Fatalf("после продления должно остаться ≈700 мс, осталось %d", left)
	}

	// на старом сроке (≈250 мс от ставки) финиша быть не должно, на новом — должен быть
	start := time.Now()
	fin := w.want("lot_finished")
	if el := time.Since(start); el < 500*time.Millisecond {
		t.Fatalf("lot_finished пришёл слишком рано (%v): таймер не перенесён", el)
	}
	if num(lotOf(fin)["endsAt"]) != endsAfter {
		t.Fatalf("endsAt в финише: %v, ждали %d", lotOf(fin)["endsAt"], endsAfter)
	}
}

func TestBidOutsideWindowDoesNotExtend(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, time.Hour, 100, 10)
	h := newHub(t, st, Options{AntiSnipe: db.AntiSnipe{Window: time.Minute, Extend: time.Minute}})
	c := dial(t, newServer(t, h), 2)
	c.want("hello")
	c.subscribe(1)
	c.sendJSON(msg{"type": "bid", "lotId": 1, "amount": 100})
	if ev := c.want("bid"); ev["extended"] != false {
		t.Fatalf("%v", ev)
	}
}

func TestFinishReschedulesWhenDeadlineMovedBehindOurBack(t *testing.T) {
	// срок продлён в БД, а таймер об этом не знает (например, другой экземпляр):
	// сработавший таймер обязан свериться с БД и не объявлять конец раньше времени
	st := newFakeStore()
	st.addLot(1, 100, 200*time.Millisecond, 100, 10)
	h := newHub(t, st, Options{FinishSlack: 20 * time.Millisecond})
	h.LotCreated(1, time.Now().Add(200*time.Millisecond))
	c := dial(t, newServer(t, h), 0)
	c.want("hello")
	c.subscribe(1)

	st.setEnds(1, time.Now().Add(700*time.Millisecond))
	start := time.Now()
	c.want("lot_finished")
	if el := time.Since(start); el < 600*time.Millisecond {
		t.Fatalf("конец объявлен слишком рано: %v", el)
	}
}

func TestCancelBroadcastsAndStopsTimer(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, 300*time.Millisecond, 100, 10)
	h := newHub(t, st, Options{FinishSlack: 20 * time.Millisecond})
	h.LotCreated(1, time.Now().Add(300*time.Millisecond))
	c := dial(t, newServer(t, h), 0)
	c.want("hello")
	c.subscribe(1)

	if err := h.CancelLot(context.Background(), 1, 555); err == nil {
		t.Fatal("чужой лот отменять нельзя")
	}
	if err := h.CancelLot(context.Background(), 1, 100); err != nil {
		t.Fatal(err)
	}
	ev := c.want("lot_cancelled")
	if lotOf(ev)["status"] != "cancelled" {
		t.Fatalf("%v", ev)
	}
	c.wantNothing(600 * time.Millisecond) // lot_finished для отменённого лота не приходит
	h.tmu.Lock()
	n := len(h.timers)
	h.tmu.Unlock()
	if n != 0 {
		t.Fatalf("таймер отменённого лота не снят: %d", n)
	}
}

func TestStartSchedulesActiveLots(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, 250*time.Millisecond, 100, 10)
	st.addLot(2, 100, time.Hour, 100, 10)
	h := newHub(t, st, Options{FinishSlack: 20 * time.Millisecond})
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.tmu.Lock()
	n := len(h.timers)
	h.tmu.Unlock()
	if n != 2 {
		t.Fatalf("ждали 2 таймера, есть %d", n)
	}
	c := dial(t, newServer(t, h), 0)
	c.want("hello")
	c.subscribe(1)
	c.want("lot_finished")
}

func TestOriginCheck(t *testing.T) {
	h := newHub(t, newFakeStore(), Options{AllowedOrigins: []string{"https://app.example.com"}})
	srv := newServer(t, h)
	host := strings.TrimPrefix(srv.URL, "http://")
	hostname, _, _ := net.SplitHostPort(host)

	cases := []struct {
		origin string
		status int
	}{
		{"", 101}, // не браузер
		{"http://" + host, 101},
		{"http://" + hostname + ":5173", 101}, // другой порт того же хоста (dev-прокси, nginx без порта)
		{"https://app.example.com", 101},
		{"https://app.example.com/", 101},
		{"http://evil.example.com", 403},
		{"https://app.example.com.evil.net", 403},
		{"null", 403},
		{"::garbage::", 403},
	}
	for _, tc := range cases {
		hdr := map[string]string{}
		if tc.origin != "" {
			hdr["Origin"] = tc.origin
		}
		c, status := dialRaw(t, srv, 0, hdr)
		if status != tc.status {
			t.Errorf("Origin %q: статус %d, ждали %d", tc.origin, status, tc.status)
		}
		if c != nil {
			c.nc.Close()
		}
	}
}

func TestRateLimit(t *testing.T) {
	h := newHub(t, newFakeStore(), Options{})
	c := dial(t, newServer(t, h), 0)
	c.want("hello")
	for i := 0; i < rateMax+3; i++ {
		c.sendJSON(msg{"type": "ping"})
	}
	pongs, limited := 0, 0
	for i := 0; i < rateMax+3; i++ {
		m, ok := c.next(2 * time.Second)
		if !ok {
			t.Fatal("ответ не пришёл")
		}
		switch {
		case m["type"] == "pong":
			pongs++
		case m["type"] == "error" && m["code"] == "rate_limited":
			limited++
		}
	}
	// в окно уже вошло hello? нет — hello исходящее; считаем только входящие
	if pongs != rateMax || limited != 3 {
		t.Fatalf("pong=%d rate_limited=%d, ждали %d и 3", pongs, limited, rateMax)
	}
}

func TestAbusiveClientIsDisconnected(t *testing.T) {
	h := newHub(t, newFakeStore(), Options{})
	c := dial(t, newServer(t, h), 0)
	c.want("hello")
	closed := false
	for i := 0; i < rateMax*rateAbusive+20 && !closed; i++ {
		c.sendJSON(msg{"type": "ping"})
	}
	for i := 0; i < 400; i++ {
		m, ok := c.next(2 * time.Second)
		if !ok || m["type"] == "<close>" {
			closed = true
			if ok && num(m["code"]) != 1008 {
				t.Fatalf("код закрытия %v, ждали 1008", m["code"])
			}
			break
		}
	}
	if !closed {
		t.Fatal("флудер не отключён")
	}
}

func TestSlowConsumerIsDroppedWithoutBlockingOthers(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, time.Hour, 100, 10)
	h := newHub(t, st, Options{SendBuffer: 16})
	srv := newServer(t, h)

	slow, fast := dial(t, srv, 0), dial(t, srv, 0)
	slow.want("hello")
	fast.want("hello")
	slow.subscribe(1)
	fast.subscribe(1)

	// быстрый клиент читает всё в фоне; медленный — ничего не читает
	var got int
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := fast.readFrame(5 * time.Second); err != nil {
				return
			}
			got++
		}
	}()

	big := map[string]string{"type": "x", "pad": strings.Repeat("a", 64<<10)}
	started := time.Now()
	for i := 0; i < 600; i++ {
		h.publish(1, big)
		time.Sleep(time.Millisecond) // быстрый клиент успевает читать, медленный — нет
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("publish блокируется из-за медленного клиента")
	}

	// медленного клиента сервер должен отключить
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.mu.RLock()
		n := len(h.rooms[1])
		h.mu.RUnlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("в комнате лота %d подписчиков, ждали 1 (медленный должен быть удалён)", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	fast.nc.Close()
	<-done
	if got == 0 {
		t.Fatal("быстрый клиент ничего не получил")
	}
}

func TestShutdownClosesClients(t *testing.T) {
	h := New(newFakeStore(), Options{})
	srv := newServer(t, h)
	c := dial(t, srv, 0)
	c.want("hello")

	done := make(chan struct{})
	go func() { h.Shutdown(); close(done) }()
	m := c.want("<close>")
	if num(m["code"]) != 1001 {
		t.Fatalf("код %v, ждали 1001", m["code"])
	}
	c.sendFrame(0x8, []byte{0x03, 0xe9}) // ответный Close 1001
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("Shutdown не завершился")
	}
	// новые подключения после остановки не принимаются
	if _, status := dialRaw(t, srv, 0, nil); status != 503 {
		t.Fatalf("после Shutdown ждали 503, получили %d", status)
	}
}

func TestMaxConns(t *testing.T) {
	h := newHub(t, newFakeStore(), Options{MaxConns: 2})
	srv := newServer(t, h)
	a, b := dial(t, srv, 0), dial(t, srv, 0)
	a.want("hello")
	b.want("hello")
	if _, status := dialRaw(t, srv, 0, nil); status != 503 {
		t.Fatalf("сверх лимита ждали 503, получили %d", status)
	}
	a.nc.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if c, status := dialRaw(t, srv, 0, nil); status == 101 {
			c.nc.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("слот не освободился после отключения клиента")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDisconnectCleansUpRooms(t *testing.T) {
	st := newFakeStore()
	st.addLot(1, 100, time.Hour, 100, 10)
	h := newHub(t, st, Options{})
	c := dial(t, newServer(t, h), 0)
	c.want("hello")
	c.subscribe(1)
	c.nc.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.RLock()
		rooms, clients := len(h.rooms), len(h.clients)
		h.mu.RUnlock()
		if rooms == 0 && clients == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("утечка: комнат %d, клиентов %d", rooms, clients)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Главная проверка порядка: ставки летят параллельно, а подписчики (в том числе
// подписывающиеся прямо посреди потока) должны видеть неубывающую цену и
// непрерывную последовательность ставок после снимка.
func TestEventOrderUnderConcurrentBids(t *testing.T) {
	st := newFakeStore()
	st.jitter = true
	st.addLot(1, 1000, time.Hour, 100, 1)
	h := newHub(t, st, Options{SendBuffer: 2048})
	srv := newServer(t, h)

	const bidders, perBidder = 8, 10
	early := dial(t, srv, 0)
	early.want("hello")
	early.subscribe(1)

	// подписчики, приходящие во время ставок
	var late []*wsc
	for i := 0; i < 5; i++ {
		c := dial(t, srv, 0)
		c.want("hello")
		late = append(late, c)
	}

	var wg sync.WaitGroup
	for b := 0; b < bidders; b++ {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			for i := 0; i < perBidder; i++ {
				// ставим минимум с запасом; «уже лидирует» — повторяем
				for attempt := 0; attempt < 400; attempt++ {
					live, _ := st.GetLotLive(context.Background(), 1)
					// запас +100: пока ждём очередь, цену успевают поднять
					if _, err := h.PlaceBid(context.Background(), 1, uid, live.MinBid+100); err == nil {
						break
					}
					time.Sleep(time.Millisecond) // «уже лидирует» отклоняется мгновенно — ждём, пока лидера перебьют
				}
			}
		}(int64(b + 1))
	}
	go func() {
		for i, c := range late {
			time.Sleep(time.Duration(i+1) * 4 * time.Millisecond)
			c.sendJSON(msg{"type": "subscribe", "lotId": 1})
		}
	}()
	wg.Wait()

	check := func(name string, c *wsc, wantSnapshot bool) {
		t.Helper()
		lastCount, lastPrice := int64(-1), int64(-1)
		sawSnapshot := !wantSnapshot
		deadline := time.Now().Add(1500 * time.Millisecond)
		for time.Now().Before(deadline) {
			m, ok := c.next(300 * time.Millisecond)
			if !ok {
				break
			}
			switch m["type"] {
			case "snapshot":
				sawSnapshot = true
			case "bid":
				if !sawSnapshot {
					t.Fatalf("%s: событие раньше снимка", name)
				}
			default:
				continue
			}
			l := lotOf(m)
			cnt, price := num(l["bidsCount"]), num(l["currentPrice"])
			if cnt < lastCount || price < lastPrice {
				t.Fatalf("%s: порядок нарушен: bidsCount %d→%d, цена %d→%d", name, lastCount, cnt, lastPrice, price)
			}
			if m["type"] == "bid" && lastCount >= 0 && cnt != lastCount+1 {
				t.Fatalf("%s: потеряно событие: bidsCount %d→%d", name, lastCount, cnt)
			}
			lastCount, lastPrice = cnt, price
		}
		st.mu.Lock()
		final := int64(len(st.lots[1].bids))
		st.mu.Unlock()
		if lastCount != final {
			t.Fatalf("%s: последнее состояние bidsCount=%d, в БД %d", name, lastCount, final)
		}
	}
	check("early", early, false)
	for i, c := range late {
		check("late"+strconv.Itoa(i), c, true)
	}
	st.mu.Lock()
	n := len(st.lots[1].bids)
	st.mu.Unlock()
	// последний оставшийся участник не может перебить сам себя, поэтому точное число недостижимо
	if n < bidders*perBidder*3/4 {
		t.Fatalf("принято ставок: %d из %d — слишком мало для проверки порядка", n, bidders*perBidder)
	}
}
