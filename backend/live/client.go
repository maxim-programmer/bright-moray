package live

import (
	"auction/db"
	"auction/model"
	"auction/ws"
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"
)

const (
	rateWindow  = 10 * time.Second
	rateMax     = 100 // сообщений за окно (не меньше лимита подписок); больше — отвечаем rate_limited
	rateAbusive = 4   // во сколько раз превысили — разрываем соединение
	maxReqIDLen = 64
)

type client struct {
	h    *Hub
	conn *ws.Conn
	uid  int64

	send   chan []byte
	ctx    context.Context
	cancel context.CancelFunc

	subs map[int64]struct{} // под h.mu; nil после отключения

	// ограничение частоты — только из цикла чтения
	winStart time.Time
	winCount int
}

func newClient(h *Hub, conn *ws.Conn, uid int64) *client {
	ctx, cancel := context.WithCancel(context.Background())
	return &client{
		h: h, conn: conn, uid: uid,
		send: make(chan []byte, h.opts.SendBuffer),
		ctx:  ctx, cancel: cancel,
		subs: map[int64]struct{}{},
	}
}

// enqueue кладёт готовое сообщение в очередь на отправку, не блокируясь.
// false — очередь переполнена (клиент не успевает читать).
func (c *client) enqueue(data []byte) bool {
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

// reply отправляет сообщение именно этому клиенту. Переполнение очереди — это
// медленный клиент: отключаем, он переподключится и получит свежий снимок.
func (c *client) reply(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("live: сериализация ответа: %v", err)
		return
	}
	if !c.enqueue(data) {
		c.conn.Close()
	}
}

func (c *client) replyError(code, msg, reqID string, lotID int64) {
	c.reply(errorMsg{Type: "error", Code: code, Message: msg, ReqID: reqID, LotID: lotID})
}

// failure переводит ошибку слоя данных в ответ клиенту (коды те же, что у REST).
func (c *client) failure(err error, reqID string, lotID int64) {
	var ae *model.APIError
	switch {
	case errors.As(err, &ae):
		c.replyError(ae.Code, ae.Message, reqID, lotID)
	case errors.Is(err, db.ErrNotFound):
		c.replyError("not_found", "Лот не найден", reqID, lotID)
	case errors.Is(err, context.Canceled):
		// клиент ушёл — отвечать некому
	default:
		log.Printf("live: внутренняя ошибка: %v", err)
		c.replyError("internal", "Внутренняя ошибка сервера", reqID, lotID)
	}
}

// closeWith вежливо закрывает соединение: шлёт Close и ждёт ответный Close клиента;
// если он не придёт — через closeGrace закрывает сокет принудительно.
func (c *client) closeWith(code int, reason string) {
	if err := c.conn.WriteClose(code, reason); err != nil {
		c.conn.Close()
		return
	}
	time.AfterFunc(closeGrace, func() { c.conn.Close() })
}

func (c *client) sendHello() {
	as := c.h.opts.AntiSnipe
	hello := helloMsg{
		Type: "hello", Now: nowMs(),
		AntiSnipe: antiSnipeInfo{Enabled: as.Enabled()},
	}
	if as.Enabled() {
		hello.AntiSnipe.WindowMs = as.Window.Milliseconds()
		hello.AntiSnipe.ExtendMs = as.Extend.Milliseconds()
	}
	if c.uid > 0 {
		id := c.uid
		hello.UserID = &id
	}
	c.reply(hello)
}

/* ---------- циклы ---------- */

// writeLoop — единственная горутина, пишущая события в сокет; заодно шлёт ping.
func (c *client) writeLoop() {
	ping := time.NewTicker(c.h.opts.PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case data := <-c.send:
			if err := c.conn.WriteText(data); err != nil {
				if !errors.Is(err, ws.ErrClosed) { // ErrClosed — мы уже отправили Close, ждём ответ
					c.conn.Close()
				}
				return
			}
		case <-ping.C:
			if err := c.conn.WritePing(); err != nil {
				if !errors.Is(err, ws.ErrClosed) {
					c.conn.Close()
				}
				return
			}
		}
	}
}

func (c *client) readLoop() {
	for {
		data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		c.handle(data)
	}
}

// allow ограничивает частоту сообщений одного соединения.
func (c *client) allow() (ok, abusive bool) {
	now := time.Now()
	if now.Sub(c.winStart) > rateWindow {
		c.winStart, c.winCount = now, 0
	}
	c.winCount++
	return c.winCount <= rateMax, c.winCount > rateMax*rateAbusive
}

func (c *client) handle(data []byte) {
	if ok, abusive := c.allow(); !ok {
		if abusive {
			c.closeWith(ws.ClosePolicy, "too many messages")
			return
		}
		c.replyError("rate_limited", "Слишком много сообщений, подождите", "", 0)
		return
	}

	var m clientMsg
	if err := json.Unmarshal(data, &m); err != nil {
		c.replyError("bad_json", "Некорректное сообщение", "", 0)
		return
	}
	if len(m.ReqID) > maxReqIDLen {
		c.replyError("bad_request", "reqId не длиннее 64 символов", "", m.LotID)
		return
	}

	switch m.Type {
	case "ping":
		c.reply(pongMsg{Type: "pong", Now: nowMs()})
	case "subscribe":
		c.subscribe(m)
	case "unsubscribe":
		if m.LotID <= 0 {
			c.replyError("bad_request", "Укажите lotId", m.ReqID, 0)
			return
		}
		c.h.removeSub(c, m.LotID)
		c.reply(unsubscribedMsg{Type: "unsubscribed", LotID: m.LotID, ReqID: m.ReqID})
	case "bid":
		c.bid(m)
	default:
		c.replyError("unknown_type", "Неизвестный тип сообщения", m.ReqID, 0)
	}
}

// subscribe: снимок состояния читается и подписка оформляется под мьютексом лота,
// поэтому ни одно событие по этому лоту не может потеряться или прийти раньше снимка.
func (c *client) subscribe(m clientMsg) {
	if m.LotID <= 0 {
		c.replyError("bad_request", "Укажите lotId", m.ReqID, 0)
		return
	}
	mu := c.h.lotLock(m.LotID)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(c.ctx, dbTimeout)
	defer cancel()
	live, err := c.h.store.GetLotLive(ctx, m.LotID)
	if err != nil {
		c.failure(err, m.ReqID, m.LotID)
		return
	}
	if !c.h.addSub(c, m.LotID) {
		c.replyError("too_many_subscriptions", "Слишком много подписок", m.ReqID, m.LotID)
		return
	}
	c.reply(snapshotMsg{Type: "snapshot", LotID: m.LotID, Lot: live, Now: nowMs(), ReqID: m.ReqID})
}

func (c *client) bid(m clientMsg) {
	switch {
	case c.uid == 0:
		c.replyError("unauthorized", "Войдите в аккаунт", m.ReqID, m.LotID)
		return
	case m.LotID <= 0 || m.Amount <= 0:
		c.replyError("validation", "Укажите lotId и сумму ставки", m.ReqID, m.LotID)
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 2*dbTimeout)
	defer cancel()
	placed, err := c.h.PlaceBid(ctx, m.LotID, c.uid, m.Amount)
	if err != nil {
		c.failure(err, m.ReqID, m.LotID)
		return
	}
	c.reply(bidOKMsg{
		Type: "bid_ok", ReqID: m.ReqID, LotID: m.LotID,
		Bid: placed.Bid, Lot: placed.Lot, Extended: placed.Extended, Now: nowMs(),
	})
}
