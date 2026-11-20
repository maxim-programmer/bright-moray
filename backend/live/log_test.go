package live

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"auction/logging"
)

// Остальные тесты хаба делают сотни ставок; логгер по умолчанию заглушаем, чтобы они
// не засоряли вывод. Тесты, которым нужен лог, передают свой через Options.Logger.
func TestMain(m *testing.M) {
	logging.SetDefault(zap.NewNop())
	os.Exit(m.Run())
}

func findLog(logs *observer.ObservedLogs, msg string) (observer.LoggedEntry, bool) {
	for _, e := range logs.All() {
		if e.Message == msg {
			return e, true
		}
	}
	return observer.LoggedEntry{}, false
}

// Ставка по WebSocket попадает в лог с request_id рукопожатия, user_id и lot_id —
// так её можно связать с access-записью GET /api/ws.
func TestBidLogCarriesHandshakeRequestID(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	st := newFakeStore()
	st.addLot(1, 100, time.Hour, 500, 50)
	h := newHub(t, st, Options{Logger: zap.New(core)})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// как Middleware: у запроса рукопожатия уже есть request_id
		ctx, _ := logging.WithRequest(r.Context(), zap.NewNop(), "handshake-0001")
		h.ServeWS(w, r.WithContext(ctx), 1)
	}))
	t.Cleanup(srv.Close)

	c := dial(t, srv, 1)
	c.want("hello")
	c.subscribe(1)
	c.sendJSON(msg{"type": "bid", "lotId": 1, "amount": 500, "reqId": "r1"})
	c.want("bid") // подписчик сначала получает событие, затем подтверждение
	c.want("bid_ok")

	e, ok := findLog(logs, "ставка принята")
	if !ok {
		t.Fatalf("нет записи о ставке: %v", logs.All())
	}
	if e.Level != zapcore.InfoLevel {
		t.Errorf("уровень %v", e.Level)
	}
	m := e.ContextMap()
	want := map[string]any{"component": "live", "request_id": "handshake-0001", "user_id": int64(1), "lot_id": int64(1)}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, ожидалось %v (%v)", k, m[k], v, m)
		}
	}
	if conn, ok := findLog(logs, "WebSocket подключён"); !ok || conn.Level != zapcore.DebugLevel ||
		conn.ContextMap()["request_id"] != "handshake-0001" {
		t.Errorf("подключение: %v %v", ok, conn)
	}
}

// Без запроса рукопожатия (например, прямой вызов ServeWS) соединение всё равно
// получает собственный request_id.
func TestClientGetsOwnRequestIDWithoutHandshakeID(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	h := newHub(t, newFakeStore(), Options{Logger: zap.New(core)})
	srv := newServer(t, h)

	c := dial(t, srv, 5)
	c.want("hello")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := findLog(logs, "WebSocket подключён"); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	e, ok := findLog(logs, "WebSocket подключён")
	if !ok {
		t.Fatal("нет записи о подключении")
	}
	m := e.ContextMap()
	if id, _ := m["request_id"].(string); !logging.ValidRequestID(id) || m["user_id"] != int64(5) {
		t.Errorf("поля соединения: %v", m)
	}
}

// Записи таймера (вне запроса) — с component=live и lot_id, но без request_id.
func TestFinishLogHasNoRequestScope(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	st := newFakeStore()
	st.addLot(1, 100, 30*time.Millisecond, 500, 50)
	h := newHub(t, st, Options{FinishSlack: 20 * time.Millisecond, Logger: zap.New(core)})
	srv := newServer(t, h)
	h.LotCreated(1, time.Now().Add(30*time.Millisecond))

	c := dial(t, srv, 0)
	c.want("hello")
	c.subscribe(1)
	c.want("lot_finished")

	e, ok := findLog(logs, "аукцион завершён")
	if !ok {
		t.Fatalf("нет записи о завершении: %v", logs.All())
	}
	m := e.ContextMap()
	if m["component"] != "live" || m["lot_id"] != int64(1) {
		t.Errorf("поля: %v", m)
	}
	if _, has := m["request_id"]; has {
		t.Errorf("у записи таймера не должно быть request_id: %v", m)
	}
}
