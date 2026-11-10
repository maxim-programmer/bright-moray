// Package ws — минимальная серверная реализация WebSocket (RFC 6455) на стандартной
// библиотеке: рукопожатие, текстовые сообщения, фрагментация, ping/pong, close.
// Расширения (сжатие) и подпротоколы не поддерживаются — клиентам они не нужны.
package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Коды операций кадров.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// Коды закрытия (RFC 6455, раздел 7.4.1).
const (
	CloseNormal         = 1000
	CloseGoingAway      = 1001
	CloseProtocolError  = 1002
	CloseUnsupported    = 1003
	CloseInvalidPayload = 1007
	ClosePolicy         = 1008
	CloseTooBig         = 1009
	CloseInternal       = 1011
)

const handshakeGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// ErrClosed возвращается при попытке записи в уже закрытое соединение.
var ErrClosed = errors.New("ws: connection closed")

// CloseError — соединение закрыто по протоколу (получен или отправлен кадр Close).
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	return fmt.Sprintf("ws: close %d %s", e.Code, e.Reason)
}

// Upgrader превращает HTTP-запрос в WebSocket-соединение.
type Upgrader struct {
	// CheckOrigin решает, можно ли принять запрос с таким Origin. Nil — разрешить всё.
	CheckOrigin func(r *http.Request) bool
	// MaxMessageSize — максимальный размер входящего сообщения в байтах (по умолчанию 4096).
	MaxMessageSize int64
	// ReadTimeout — сколько можно ждать очередной кадр от клиента (по умолчанию 60 с).
	// Клиент, который молчит дольше (не отвечает даже на ping), считается отвалившимся.
	ReadTimeout time.Duration
	// WriteTimeout — предел на запись одного кадра (по умолчанию 10 с).
	WriteTimeout time.Duration
}

// Upgrade проверяет рукопожатие и забирает соединение у net/http. При ошибке ответ
// клиенту уже отправлен, вызывающему остаётся просто выйти.
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	fail := func(status int, msg string) (*Conn, error) {
		if status == http.StatusUpgradeRequired {
			w.Header().Set("Sec-WebSocket-Version", "13")
		}
		http.Error(w, msg, status)
		return nil, errors.New("ws: handshake: " + msg)
	}

	switch {
	case r.Method != http.MethodGet:
		w.Header().Set("Allow", http.MethodGet)
		return fail(http.StatusMethodNotAllowed, "method not allowed")
	case !r.ProtoAtLeast(1, 1):
		return fail(http.StatusBadRequest, "HTTP/1.1 required")
	case !headerHasToken(r.Header, "Connection", "upgrade"):
		return fail(http.StatusBadRequest, "missing Connection: Upgrade")
	case !headerHasToken(r.Header, "Upgrade", "websocket"):
		return fail(http.StatusBadRequest, "missing Upgrade: websocket")
	case r.Header.Get("Sec-WebSocket-Version") != "13":
		return fail(http.StatusUpgradeRequired, "unsupported WebSocket version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if raw, err := base64.StdEncoding.DecodeString(key); err != nil || len(raw) != 16 {
		return fail(http.StatusBadRequest, "bad Sec-WebSocket-Key")
	}
	if u.CheckOrigin != nil && !u.CheckOrigin(r) {
		return fail(http.StatusForbidden, "origin not allowed")
	}

	nc, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return fail(http.StatusInternalServerError, "websocket not supported")
	}

	c := &Conn{
		nc:           nc,
		br:           brw.Reader,
		maxMsg:       u.MaxMessageSize,
		readTimeout:  u.ReadTimeout,
		writeTimeout: u.WriteTimeout,
	}
	if c.maxMsg <= 0 {
		c.maxMsg = 4096
	}
	if c.readTimeout <= 0 {
		c.readTimeout = 60 * time.Second
	}
	if c.writeTimeout <= 0 {
		c.writeTimeout = 10 * time.Second
	}

	sum := sha1.Sum([]byte(key + handshakeGUID))
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"
	_ = nc.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	if _, err := io.WriteString(nc, resp); err != nil {
		nc.Close()
		return nil, err
	}
	_ = nc.SetWriteDeadline(time.Time{})
	return c, nil
}

// headerHasToken: заголовок может повторяться и содержать список через запятую.
func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// Conn — установленное WebSocket-соединение. Читать (ReadMessage) должна одна горутина;
// писать можно из нескольких — записи сериализуются.
type Conn struct {
	nc           net.Conn
	br           *bufio.Reader
	maxMsg       int64
	readTimeout  time.Duration
	writeTimeout time.Duration

	wmu      sync.Mutex
	wclosed  bool // после отправки Close или ошибки записи писать больше нельзя
	closeOne sync.Once
}

// ReadMessage блокируется до прихода целого текстового сообщения (фрагменты склеиваются).
// Ping обрабатывается автоматически (отвечаем pong), Pong игнорируется. Получив Close,
// отвечает тем же кодом и возвращает *CloseError. При нарушении протокола отправляет
// Close с подходящим кодом и возвращает ошибку. После любой ошибки соединение нужно
// закрыть методом Close.
func (c *Conn) ReadMessage() ([]byte, error) {
	var msg []byte
	inFragment := false
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch op {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return nil, err
			}
		case opPong:
			// keep-alive от клиента: достаточно того, что кадр пришёл и сбросил таймаут
		case opClose:
			code, reason, ok := parseClose(payload)
			if !ok {
				return nil, c.protocolError(CloseProtocolError, "bad close frame")
			}
			_ = c.WriteClose(code, "")
			return nil, &CloseError{Code: code, Reason: reason}
		case opText, opBinary:
			if inFragment {
				return nil, c.protocolError(CloseProtocolError, "new message inside fragmented message")
			}
			if op == opBinary {
				return nil, c.protocolError(CloseUnsupported, "binary frames are not supported")
			}
			if fin {
				return c.checkText(payload)
			}
			inFragment = true
			msg = append(msg[:0], payload...)
		case opContinuation:
			if !inFragment {
				return nil, c.protocolError(CloseProtocolError, "unexpected continuation frame")
			}
			if int64(len(msg))+int64(len(payload)) > c.maxMsg {
				return nil, c.protocolError(CloseTooBig, "message too big")
			}
			msg = append(msg, payload...)
			if fin {
				return c.checkText(msg)
			}
		default:
			return nil, c.protocolError(CloseProtocolError, "unknown opcode")
		}
	}
}

func (c *Conn) checkText(p []byte) ([]byte, error) {
	if !utf8.Valid(p) {
		return nil, c.protocolError(CloseInvalidPayload, "invalid UTF-8")
	}
	return p, nil
}

// protocolError отправляет клиенту Close с кодом и возвращает соответствующую ошибку.
func (c *Conn) protocolError(code int, reason string) error {
	_ = c.WriteClose(code, reason)
	return &CloseError{Code: code, Reason: reason}
}

func (c *Conn) readFrame() (fin bool, op byte, payload []byte, err error) {
	// Таймаут действует на один кадр целиком: молчащий или «капающий» по байту клиент
	// не сможет держать соединение бесконечно.
	_ = c.nc.SetReadDeadline(time.Now().Add(c.readTimeout))

	var h [2]byte
	if _, err = io.ReadFull(c.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 { // RSV1-3 без согласованных расширений должны быть нулями
		err = c.protocolError(CloseProtocolError, "reserved bits set")
		return
	}
	op = h[0] & 0x0f
	if h[1]&0x80 == 0 { // кадры от клиента обязаны быть замаскированы
		err = c.protocolError(CloseProtocolError, "client frame is not masked")
		return
	}
	n := int64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		n = int64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		u := binary.BigEndian.Uint64(b[:])
		if u>>63 != 0 {
			err = c.protocolError(CloseProtocolError, "bad frame length")
			return
		}
		n = int64(u)
	}
	if op >= opClose && (!fin || n > 125) { // управляющие кадры: без фрагментации и ≤125 байт
		err = c.protocolError(CloseProtocolError, "bad control frame")
		return
	}
	if n > c.maxMsg { // проверяем до выделения памяти
		err = c.protocolError(CloseTooBig, "message too big")
		return
	}

	var key [4]byte
	if _, err = io.ReadFull(c.br, key[:]); err != nil {
		return
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	for i := range payload {
		payload[i] ^= key[i&3]
	}
	return
}

// parseClose разбирает тело кадра Close. Пустое тело — код 1005 («без кода»),
// в ответе мы используем 1000.
func parseClose(p []byte) (code int, reason string, ok bool) {
	switch {
	case len(p) == 0:
		return CloseNormal, "", true
	case len(p) == 1:
		return 0, "", false
	}
	code = int(binary.BigEndian.Uint16(p))
	if !validCloseCode(code) || !utf8.Valid(p[2:]) {
		return 0, "", false
	}
	return code, string(p[2:]), true
}

func validCloseCode(c int) bool {
	switch {
	case c >= 1000 && c <= 1003, c >= 1007 && c <= 1011, c >= 3000 && c <= 4999:
		return true
	}
	return false
}

/* ---------- запись ---------- */

// WriteText отправляет одно текстовое сообщение.
func (c *Conn) WriteText(p []byte) error { return c.writeFrame(opText, p) }

// WritePing отправляет ping (клиент-браузер отвечает pong автоматически).
func (c *Conn) WritePing() error { return c.writeFrame(opPing, nil) }

// WriteClose отправляет кадр Close. После него писать в соединение нельзя;
// закрыть сокет нужно отдельно (Close) — лучше после ответного Close клиента.
func (c *Conn) WriteClose(code int, reason string) error {
	if len(reason) > 123 {
		reason = strings.ToValidUTF8(reason[:123], "")
	}
	body := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(body, uint16(code))
	copy(body[2:], reason)
	return c.writeFrame(opClose, body) // writeFrame сам запрещает дальнейшую запись
}

func (c *Conn) writeFrame(op byte, p []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.wclosed {
		return ErrClosed
	}

	var hdr [10]byte
	hdr[0] = 0x80 | op // FIN, без фрагментации; сервер не маскирует
	n := len(p)
	hl := 2
	switch {
	case n < 126:
		hdr[1] = byte(n)
	case n <= 0xffff:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
		hl = 4
	default:
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
		hl = 10
	}

	if op == opClose {
		c.wclosed = true // после Close данные слать нельзя; флаг ставим под тем же замком
	}
	_ = c.nc.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	bufs := net.Buffers{hdr[:hl], p}
	if _, err := bufs.WriteTo(c.nc); err != nil {
		c.wclosed = true // кадр мог уйти частично — поток испорчен
		return err
	}
	return nil
}

// Close закрывает сокет. Можно вызывать повторно и из любой горутины;
// заблокированные Read/Write завершатся с ошибкой.
func (c *Conn) Close() error {
	var err error
	c.closeOne.Do(func() { err = c.nc.Close() })
	return err
}

// RemoteAddr — адрес клиента (для логов).
func (c *Conn) RemoteAddr() net.Addr { return c.nc.RemoteAddr() }
