package live

import "auction/model"

// Сообщения клиент → сервер (JSON, текстовые кадры):
//
//	{"type":"subscribe","lotId":12,"reqId":"1"}      подписка на лот, в ответ придёт snapshot
//	{"type":"unsubscribe","lotId":12}
//	{"type":"bid","lotId":12,"amount":1500,"reqId":"2"}   ставка (нужна сессия), в ответ bid_ok или error
//	{"type":"ping"}                                   в ответ pong с серверным временем
type clientMsg struct {
	Type   string `json:"type"`
	LotID  int64  `json:"lotId"`
	Amount int    `json:"amount"`
	ReqID  string `json:"reqId"`
}

// Сообщения сервер → клиент. Поле now — серверное время в мс Unix: по нему клиент
// поправляет свои часы и корректно отсчитывает таймер до endsAt.

type antiSnipeInfo struct {
	Enabled  bool  `json:"enabled"`
	WindowMs int64 `json:"windowMs"`
	ExtendMs int64 `json:"extendMs"`
}

// hello приходит сразу после подключения.
type helloMsg struct {
	Type      string        `json:"type"` // "hello"
	Now       int64         `json:"now"`
	UserID    *int64        `json:"userId"` // null — аноним
	AntiSnipe antiSnipeInfo `json:"antiSnipe"`
}

// snapshot — актуальное состояние лота в ответ на subscribe. После него идут события.
type snapshotMsg struct {
	Type  string        `json:"type"` // "snapshot"
	LotID int64         `json:"lotId"`
	Lot   model.LotLive `json:"lot"`
	Now   int64         `json:"now"`
	ReqID string        `json:"reqId,omitempty"`
}

// bid — новая ставка на лот (рассылается всем подписчикам, включая ставившего).
// extended = true: ставка сработала как Anti-snipe, lot.endsAt уже новый.
type bidEvent struct {
	Type     string        `json:"type"` // "bid"
	LotID    int64         `json:"lotId"`
	Bid      model.Bid     `json:"bid"`
	Lot      model.LotLive `json:"lot"`
	Extended bool          `json:"extended"`
	Now      int64         `json:"now"`
}

// lot_finished / lot_cancelled — аукцион завершился по таймеру / отменён автором.
// Контакты победителя и продавца сюда не попадают (они зависят от зрителя):
// получив lot_finished, клиент перезапрашивает GET /api/lots/{id}.
type lotEvent struct {
	Type  string        `json:"type"`
	LotID int64         `json:"lotId"`
	Lot   model.LotLive `json:"lot"`
	Now   int64         `json:"now"`
}

// bid_ok — ответ на ставку, сделанную через WebSocket.
type bidOKMsg struct {
	Type     string         `json:"type"` // "bid_ok"
	ReqID    string         `json:"reqId,omitempty"`
	LotID    int64          `json:"lotId"`
	Bid      model.Bid      `json:"bid"`
	Lot      *model.LotLive `json:"lot,omitempty"`
	Extended bool           `json:"extended"`
	Now      int64          `json:"now"`
}

type unsubscribedMsg struct {
	Type  string `json:"type"` // "unsubscribed"
	LotID int64  `json:"lotId"`
	ReqID string `json:"reqId,omitempty"`
}

type pongMsg struct {
	Type string `json:"type"` // "pong"
	Now  int64  `json:"now"`
}

// errorMsg — отказ. code совпадает с кодами REST (bid_too_low, lot_closed, …),
// плюс служебные: bad_json, bad_request, unauthorized, unknown_type, not_found,
// too_many_subscriptions, rate_limited, internal.
type errorMsg struct {
	Type    string `json:"type"` // "error"
	Code    string `json:"code"`
	Message string `json:"message"`
	ReqID   string `json:"reqId,omitempty"`
	LotID   int64  `json:"lotId,omitempty"`
}
