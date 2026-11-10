// Package live — реалтайм аукциона: рассылка ставок по WebSocket, серверный таймер
// завершения аукциона и Anti-snipe.
//
// Схема работы (один экземпляр бэкенда; состояние подписок хранится в памяти):
//
//	клиент ──WS──► Hub ──► Store (PostgreSQL)
//	                ▲  └──► рассылка событий подписчикам лота
//	                └── планировщик: по одному таймеру на активный лот
//
// Порядок событий. Все операции, меняющие лот (ставка, отмена, завершение, подписка
// со снимком состояния), выполняются под одним мьютексом из небольшого набора,
// выбираемого по id лота. Поэтому подписчик получает события строго в порядке
// их фиксации в БД, а снимок состояния при подписке не может «обогнать» или
// отстать от событий: либо операция полностью завершилась до снимка, либо
// начнётся после регистрации подписчика.
package live

import (
	"auction/db"
	"auction/model"
	"auction/ws"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	lockStripes       = 64
	defaultMaxConns   = 5000
	maxSubsPerClient  = 50
	sendBuffer        = 256
	maxMessageSize    = 4096
	defaultPing       = 25 * time.Second
	defaultReadWait   = 60 * time.Second
	writeWait         = 10 * time.Second
	closeGrace        = 2 * time.Second
	defaultFinishWait = 100 * time.Millisecond // запас, чтобы БД точно считала срок вышедшим
	retryAfterError   = 5 * time.Second
	dbTimeout         = 5 * time.Second
)

// Store — то, что реалтайм-слою нужно от базы данных. Реализует *db.Queries.
type Store interface {
	PlaceBid(ctx context.Context, lotID, userID int64, amount int, as db.AntiSnipe) (db.PlacedBid, error)
	CancelLot(ctx context.Context, lotID, userID int64) error
	GetLotLive(ctx context.Context, lotID int64) (model.LotLive, error)
	LotRemaining(ctx context.Context, lotID int64) (status string, left time.Duration, err error)
	ActiveLotEnds(ctx context.Context) ([]db.LotEnd, error)
}

// Options — настройки хаба. Нулевые значения заменяются разумными по умолчанию.
type Options struct {
	AntiSnipe      db.AntiSnipe
	AllowedOrigins []string // дополнительные Origin для WebSocket (помимо совпадающего с Host)
	MaxConns       int
	SendBuffer     int // размер очереди исходящих сообщений на клиента
	PingInterval   time.Duration
	ReadTimeout    time.Duration
	FinishSlack    time.Duration
}

type timerEntry struct {
	t   *time.Timer
	gen uint64
}

// Hub хранит подписчиков по лотам и таймеры завершения аукционов.
type Hub struct {
	store Store
	opts  Options
	up    *ws.Upgrader

	ctx    context.Context // для фоновой работы планировщика; отменяется в Shutdown
	cancel context.CancelFunc

	locks [lockStripes]sync.Mutex

	mu      sync.RWMutex
	clients map[*client]struct{}
	rooms   map[int64]map[*client]struct{}
	closing bool
	wg      sync.WaitGroup // живые клиенты

	tmu     sync.Mutex
	timers  map[int64]*timerEntry
	gen     uint64
	stopped bool
}

func New(store Store, opts Options) *Hub {
	if opts.MaxConns <= 0 {
		opts.MaxConns = defaultMaxConns
	}
	if opts.SendBuffer <= 0 {
		opts.SendBuffer = sendBuffer
	}
	if opts.PingInterval <= 0 {
		opts.PingInterval = defaultPing
	}
	if opts.ReadTimeout <= 0 {
		opts.ReadTimeout = defaultReadWait
	}
	if opts.FinishSlack <= 0 {
		opts.FinishSlack = defaultFinishWait
	}
	h := &Hub{
		store:   store,
		opts:    opts,
		clients: map[*client]struct{}{},
		rooms:   map[int64]map[*client]struct{}{},
		timers:  map[int64]*timerEntry{},
	}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.up = &ws.Upgrader{
		CheckOrigin:    h.checkOrigin,
		MaxMessageSize: maxMessageSize,
		ReadTimeout:    opts.ReadTimeout,
		WriteTimeout:   writeWait,
	}
	return h
}

// Start ставит таймеры завершения на все активные лоты. Вызывать один раз при старте,
// после миграций.
func (h *Hub) Start(ctx context.Context) error {
	ends, err := h.store.ActiveLotEnds(ctx)
	if err != nil {
		return err
	}
	for _, e := range ends {
		h.schedule(e.ID, e.Left)
	}
	log.Printf("live: запланировано завершение %d активных лотов", len(ends))
	return nil
}

// Shutdown закрывает все WebSocket-соединения (код 1001) и останавливает таймеры.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	h.closing = true
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	h.tmu.Lock()
	h.stopped = true
	for _, e := range h.timers {
		e.t.Stop()
	}
	h.timers = map[int64]*timerEntry{}
	h.tmu.Unlock()

	for _, c := range clients {
		c.closeWith(ws.CloseGoingAway, "server shutting down")
	}
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(closeGrace + time.Second):
		for _, c := range clients {
			c.conn.Close()
		}
	}
	h.cancel()
}

func (h *Hub) lotLock(lotID int64) *sync.Mutex {
	return &h.locks[uint64(lotID)%lockStripes]
}

/* ---------- Origin ---------- */

// checkOrigin защищает от подключения к сокету со сторонних сайтов. Запросы без Origin
// (не браузеры) пропускаем. Хосты сравниваем без порта: cookie сессии тоже не
// различает порты, а за nginx Host приходит без порта.
func (h *Hub) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	ou, err := url.Parse(origin)
	if err != nil || ou.Hostname() == "" {
		return false
	}
	if ru, err := url.Parse("//" + r.Host); err == nil && strings.EqualFold(ou.Hostname(), ru.Hostname()) {
		return true
	}
	for _, a := range h.opts.AllowedOrigins {
		if strings.EqualFold(strings.TrimRight(a, "/"), strings.TrimRight(origin, "/")) {
			return true
		}
	}
	return false
}

/* ---------- подключения ---------- */

// ServeWS принимает WebSocket-подключение и обслуживает его до закрытия.
// uid — авторизованный пользователь (0 — аноним: может подписываться, но не ставить).
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, uid int64) {
	h.mu.RLock()
	busy := h.closing || len(h.clients) >= h.opts.MaxConns
	h.mu.RUnlock()
	if busy {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	conn, err := h.up.Upgrade(w, r)
	if err != nil {
		return // ответ клиенту уже отправлен
	}
	c := newClient(h, conn, uid)
	if !h.register(c) {
		_ = conn.WriteClose(ws.CloseGoingAway, "server shutting down")
		conn.Close()
		return
	}
	defer h.unregister(c)

	go c.writeLoop()
	c.sendHello()
	c.readLoop()
}

func (h *Hub) register(c *client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.clients[c] = struct{}{}
	h.wg.Add(1)
	return true
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	for id := range c.subs {
		if room := h.rooms[id]; room != nil {
			delete(room, c)
			if len(room) == 0 {
				delete(h.rooms, id)
			}
		}
	}
	c.subs = nil
	h.mu.Unlock()

	c.cancel()
	c.conn.Close()
	h.wg.Done()
}

// addSub подписывает клиента на лот. false — достигнут лимит подписок или клиент уже ушёл.
func (h *Hub) addSub(c *client, lotID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.subs == nil {
		return false
	}
	if _, ok := c.subs[lotID]; !ok {
		if len(c.subs) >= maxSubsPerClient {
			return false
		}
		c.subs[lotID] = struct{}{}
		room := h.rooms[lotID]
		if room == nil {
			room = map[*client]struct{}{}
			h.rooms[lotID] = room
		}
		room[c] = struct{}{}
	}
	return true
}

func (h *Hub) removeSub(c *client, lotID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.subs == nil {
		return
	}
	delete(c.subs, lotID)
	if room := h.rooms[lotID]; room != nil {
		delete(room, c)
		if len(room) == 0 {
			delete(h.rooms, lotID)
		}
	}
}

// publish рассылает событие подписчикам лота. Медленных клиентов, чей буфер переполнен,
// отключаем: они переподключатся и получат свежий снимок.
func (h *Hub) publish(lotID int64, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("live: сериализация события: %v", err)
		return
	}
	var slow []*client
	h.mu.RLock()
	for c := range h.rooms[lotID] {
		if !c.enqueue(data) {
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range slow {
		log.Printf("live: отключаем медленного клиента %v", c.conn.RemoteAddr())
		c.conn.Close()
	}
}

/* ---------- операции над лотом ---------- */

// Placed — результат успешной ставки.
type Placed struct {
	Bid      model.Bid
	Lot      *model.LotLive // nil, если после ставки не удалось прочитать состояние лота
	Extended bool           // сработал Anti-snipe
}

// PlaceBid делает ставку, переносит таймер завершения и рассылает событие подписчикам.
// Это единственный путь ставки — и для REST, и для WebSocket.
func (h *Hub) PlaceBid(ctx context.Context, lotID, userID int64, amount int) (Placed, error) {
	mu := h.lotLock(lotID)
	mu.Lock()
	defer mu.Unlock()

	res, err := h.store.PlaceBid(ctx, lotID, userID, amount, h.opts.AntiSnipe)
	if err != nil {
		return Placed{}, err
	}
	// Ставка уже зафиксирована: всё, что ниже, не должно срываться из-за того,
	// что клиент закрыл соединение, поэтому контекст отвязываем от отмены.
	h.schedule(lotID, res.Left)
	out := Placed{Bid: res.Bid, Extended: res.Extended}

	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbTimeout)
	defer cancel()
	live, err := h.store.GetLotLive(bg, lotID)
	if err != nil {
		log.Printf("live: состояние лота %d после ставки: %v", lotID, err)
		return out, nil
	}
	out.Lot = &live
	h.publish(lotID, bidEvent{
		Type: "bid", LotID: lotID, Bid: res.Bid, Lot: live, Extended: res.Extended, Now: nowMs(),
	})
	return out, nil
}

// CancelLot отменяет лот автором и сообщает об этом подписчикам.
func (h *Hub) CancelLot(ctx context.Context, lotID, userID int64) error {
	mu := h.lotLock(lotID)
	mu.Lock()
	defer mu.Unlock()

	if err := h.store.CancelLot(ctx, lotID, userID); err != nil {
		return err
	}
	h.stopTimer(lotID)

	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbTimeout)
	defer cancel()
	live, err := h.store.GetLotLive(bg, lotID)
	if err != nil {
		log.Printf("live: состояние лота %d после отмены: %v", lotID, err)
		return nil
	}
	h.publish(lotID, lotEvent{Type: "lot_cancelled", LotID: lotID, Lot: live, Now: nowMs()})
	return nil
}

// LotCreated ставит таймер завершения на только что созданный лот.
func (h *Hub) LotCreated(lotID int64, endsAt time.Time) {
	h.schedule(lotID, time.Until(endsAt))
}

/* ---------- планировщик завершения ---------- */

// schedule (пере)ставит таймер завершения лота через d. Старый таймер отменяется.
func (h *Hub) schedule(lotID int64, d time.Duration) {
	if d < 0 {
		d = 0
	}
	d += h.opts.FinishSlack

	h.tmu.Lock()
	defer h.tmu.Unlock()
	if h.stopped {
		return
	}
	if old := h.timers[lotID]; old != nil {
		old.t.Stop()
	}
	h.gen++
	gen := h.gen
	h.timers[lotID] = &timerEntry{
		gen: gen,
		t:   time.AfterFunc(d, func() { h.finish(lotID, gen) }),
	}
}

func (h *Hub) stopTimer(lotID int64) {
	h.tmu.Lock()
	defer h.tmu.Unlock()
	if e := h.timers[lotID]; e != nil {
		e.t.Stop()
		delete(h.timers, lotID)
	}
}

// ownsTimer: сработавший таймер ещё актуален (его не заменили более новым).
func (h *Hub) ownsTimer(lotID int64, gen uint64) bool {
	h.tmu.Lock()
	defer h.tmu.Unlock()
	e := h.timers[lotID]
	return e != nil && e.gen == gen
}

func (h *Hub) dropTimer(lotID int64, gen uint64) {
	h.tmu.Lock()
	defer h.tmu.Unlock()
	if e := h.timers[lotID]; e != nil && e.gen == gen {
		delete(h.timers, lotID)
	}
}

// finish вызывается таймером в момент окончания срока. Источник истины — БД: если
// срок за это время продлили (Anti-snipe), таймер просто переставляется.
// Работает под тем же мьютексом, что и ставки, поэтому не может «разминуться» со ставкой,
// пришедшей в последний момент.
func (h *Hub) finish(lotID int64, gen uint64) {
	mu := h.lotLock(lotID)
	mu.Lock()
	defer mu.Unlock()

	if !h.ownsTimer(lotID, gen) || h.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(h.ctx, dbTimeout)
	defer cancel()

	status, left, err := h.store.LotRemaining(ctx, lotID)
	switch {
	case errors.Is(err, db.ErrNotFound):
		h.dropTimer(lotID, gen)
		return
	case err != nil:
		log.Printf("live: проверка срока лота %d: %v", lotID, err)
		h.schedule(lotID, retryAfterError-h.opts.FinishSlack)
		return
	case status == "cancelled":
		h.dropTimer(lotID, gen)
		return
	case status == "active" && left > 0:
		h.schedule(lotID, left)
		return
	}

	live, err := h.store.GetLotLive(ctx, lotID)
	if err != nil {
		log.Printf("live: состояние лота %d при завершении: %v", lotID, err)
		h.schedule(lotID, retryAfterError-h.opts.FinishSlack)
		return
	}
	h.dropTimer(lotID, gen)
	h.publish(lotID, lotEvent{Type: "lot_finished", LotID: lotID, Lot: live, Now: nowMs()})
}

func nowMs() int64 { return time.Now().UnixMilli() }
