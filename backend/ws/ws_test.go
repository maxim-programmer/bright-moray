package ws

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

/* ---------- тестовый клиент ---------- */

type testClient struct {
	t  *testing.T
	nc net.Conn
	br *bufio.Reader
}

const testKey = "dGhlIHNhbXBsZSBub25jZQ==" // пример из RFC 6455: accept = s3pPLMBiTxaQ9kYGzzhZRbK+xOo=

// rawHandshake отправляет запрос рукопожатия и возвращает ответ целиком.
func rawHandshake(t *testing.T, srv *httptest.Server, headers map[string]string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	nc, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	b.WriteString("GET /ws HTTP/1.1\r\nHost: " + strings.TrimPrefix(srv.URL, "http://") + "\r\n")
	for k, v := range headers {
		b.WriteString(k + ": " + v + "\r\n")
	}
	b.WriteString("\r\n")
	nc.Write(b.Bytes())
	br := bufio.NewReader(nc)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	return nc, br, resp
}

func dial(t *testing.T, srv *httptest.Server) *testClient {
	t.Helper()
	nc, br, resp := rawHandshake(t, srv, map[string]string{
		"Connection": "Upgrade", "Upgrade": "websocket",
		"Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": testKey,
	})
	if resp.StatusCode != 101 {
		t.Fatalf("ожидали 101, получили %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("неверный Sec-WebSocket-Accept: %q", got)
	}
	c := &testClient{t: t, nc: nc, br: br}
	t.Cleanup(func() { nc.Close() })
	return c
}

// frame собирает кадр клиента. masked=false нужен для проверки нарушений.
func frame(fin bool, rsv byte, op byte, payload []byte, masked bool) []byte {
	var b bytes.Buffer
	b0 := op | rsv
	if fin {
		b0 |= 0x80
	}
	b.WriteByte(b0)
	var m byte
	if masked {
		m = 0x80
	}
	n := len(payload)
	switch {
	case n < 126:
		b.WriteByte(m | byte(n))
	case n <= 0xffff:
		b.WriteByte(m | 126)
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(n))
		b.Write(l[:])
	default:
		b.WriteByte(m | 127)
		var l [8]byte
		binary.BigEndian.PutUint64(l[:], uint64(n))
		b.Write(l[:])
	}
	if masked {
		key := [4]byte{0x37, 0xfa, 0x21, 0x3d}
		b.Write(key[:])
		for i, c := range payload {
			b.WriteByte(c ^ key[i&3])
		}
	} else {
		b.Write(payload)
	}
	return b.Bytes()
}

func (c *testClient) send(fin bool, op byte, payload []byte) {
	c.t.Helper()
	if _, err := c.nc.Write(frame(fin, 0, op, payload, true)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *testClient) sendRaw(b []byte) {
	c.t.Helper()
	if _, err := c.nc.Write(b); err != nil {
		c.t.Fatal(err)
	}
}

// read читает один кадр сервера.
func (c *testClient) read() (op byte, payload []byte) {
	c.t.Helper()
	c.nc.SetReadDeadline(time.Now().Add(3 * time.Second))
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		c.t.Fatalf("чтение кадра: %v", err)
	}
	if h[1]&0x80 != 0 {
		c.t.Fatal("сервер не должен маскировать кадры")
	}
	op = h[0] & 0x0f
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var l [2]byte
		io.ReadFull(c.br, l[:])
		n = int(binary.BigEndian.Uint16(l[:]))
	case 127:
		var l [8]byte
		io.ReadFull(c.br, l[:])
		n = int(binary.BigEndian.Uint64(l[:]))
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		c.t.Fatalf("чтение тела: %v", err)
	}
	return
}

// expectClose ждёт кадр Close с нужным кодом.
func (c *testClient) expectClose(code int) {
	c.t.Helper()
	op, p := c.read()
	if op != opClose || len(p) < 2 {
		c.t.Fatalf("ожидали Close, получили op=%d payload=%v", op, p)
	}
	if got := int(binary.BigEndian.Uint16(p)); got != code {
		c.t.Fatalf("код закрытия: ожидали %d, получили %d", code, got)
	}
}

/* ---------- тестовый сервер: эхо ---------- */

func echoServer(t *testing.T, u *Upgrader) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := u.Upgrade(w, r)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteText(msg); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

/* ---------- тесты ---------- */

func TestHandshakeAcceptAndEcho(t *testing.T) {
	c := dial(t, echoServer(t, &Upgrader{}))
	c.send(true, opText, []byte("привет"))
	op, p := c.read()
	if op != opText || string(p) != "привет" {
		t.Fatalf("эхо: op=%d %q", op, p)
	}
}

func TestEchoLengths(t *testing.T) {
	// 125 — однобайтная длина, 126 и 65535 — двухбайтная, 65536 — восьмибайтная
	c := dial(t, echoServer(t, &Upgrader{MaxMessageSize: 1 << 20}))
	for _, n := range []int{0, 1, 125, 126, 127, 65535, 65536, 100000} {
		msg := bytes.Repeat([]byte("a"), n)
		c.send(true, opText, msg)
		op, p := c.read()
		if op != opText || !bytes.Equal(p, msg) {
			t.Fatalf("длина %d: op=%d len=%d", n, op, len(p))
		}
	}
}

func TestFragmentedMessageWithInterleavedPing(t *testing.T) {
	c := dial(t, echoServer(t, &Upgrader{}))
	c.send(false, opText, []byte("Hel"))
	c.send(true, opPing, []byte("p")) // управляющий кадр между фрагментами разрешён
	c.send(false, opContinuation, []byte("lo, "))
	c.send(true, opContinuation, []byte("мир"))
	op, p := c.read()
	if op != opPong || string(p) != "p" {
		t.Fatalf("ожидали pong, получили op=%d %q", op, p)
	}
	op, p = c.read()
	if op != opText || string(p) != "Hello, мир" {
		t.Fatalf("склейка фрагментов: op=%d %q", op, p)
	}
}

func TestMultibyteRuneSplitAcrossFragments(t *testing.T) {
	// «й» = 2 байта; режем посередине — валидация UTF-8 только по целому сообщению
	c := dial(t, echoServer(t, &Upgrader{}))
	b := []byte("й")
	c.send(false, opText, b[:1])
	c.send(true, opContinuation, b[1:])
	op, p := c.read()
	if op != opText || string(p) != "й" {
		t.Fatalf("op=%d %q", op, p)
	}
}

func TestPongIsIgnored(t *testing.T) {
	c := dial(t, echoServer(t, &Upgrader{}))
	c.send(true, opPong, nil)
	c.send(true, opText, []byte("x"))
	if op, p := c.read(); op != opText || string(p) != "x" {
		t.Fatalf("op=%d %q", op, p)
	}
}

func TestClientCloseIsEchoed(t *testing.T) {
	c := dial(t, echoServer(t, &Upgrader{}))
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, CloseNormal)
	c.send(true, opClose, body)
	c.expectClose(CloseNormal)
	// после этого сервер закрывает сокет
	c.nc.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.br.ReadByte(); err == nil {
		t.Fatal("ожидали закрытие соединения после Close")
	}
}

func TestEmptyCloseFrame(t *testing.T) {
	c := dial(t, echoServer(t, &Upgrader{}))
	c.send(true, opClose, nil)
	c.expectClose(CloseNormal)
}

func TestProtocolViolations(t *testing.T) {
	cases := []struct {
		name string
		send func(c *testClient)
		want int
	}{
		{"кадр без маски", func(c *testClient) { c.sendRaw(frame(true, 0, opText, []byte("x"), false)) }, CloseProtocolError},
		{"RSV1", func(c *testClient) { c.sendRaw(frame(true, 0x40, opText, []byte("x"), true)) }, CloseProtocolError},
		{"неизвестный опкод", func(c *testClient) { c.sendRaw(frame(true, 0, 0x3, nil, true)) }, CloseProtocolError},
		{"фрагментированный ping", func(c *testClient) { c.sendRaw(frame(false, 0, opPing, nil, true)) }, CloseProtocolError},
		{"ping длиннее 125", func(c *testClient) { c.sendRaw(frame(true, 0, opPing, make([]byte, 126), true)) }, CloseProtocolError},
		{"continuation без начала", func(c *testClient) { c.sendRaw(frame(true, 0, opContinuation, []byte("x"), true)) }, CloseProtocolError},
		{"новое сообщение посреди фрагментов", func(c *testClient) {
			c.sendRaw(frame(false, 0, opText, []byte("a"), true))
			c.sendRaw(frame(true, 0, opText, []byte("b"), true))
		}, CloseProtocolError},
		{"бинарный кадр", func(c *testClient) { c.sendRaw(frame(true, 0, opBinary, []byte{1}, true)) }, CloseUnsupported},
		{"невалидный UTF-8", func(c *testClient) { c.sendRaw(frame(true, 0, opText, []byte{0xff, 0xfe}, true)) }, CloseInvalidPayload},
		{"слишком длинное сообщение", func(c *testClient) { c.sendRaw(frame(true, 0, opText, make([]byte, 5000), true)) }, CloseTooBig},
		{"слишком длинное в сумме фрагментов", func(c *testClient) {
			c.sendRaw(frame(false, 0, opText, make([]byte, 3000), true))
			c.sendRaw(frame(true, 0, opContinuation, make([]byte, 3000), true))
		}, CloseTooBig},
		{"Close с телом в 1 байт", func(c *testClient) { c.sendRaw(frame(true, 0, opClose, []byte{1}, true)) }, CloseProtocolError},
		{"Close с недопустимым кодом", func(c *testClient) {
			b := make([]byte, 2)
			binary.BigEndian.PutUint16(b, 1005)
			c.sendRaw(frame(true, 0, opClose, b, true))
		}, CloseProtocolError},
		{"длина 64 бита со старшим битом", func(c *testClient) {
			c.sendRaw([]byte{0x81, 0x80 | 127, 0x80, 0, 0, 0, 0, 0, 0, 0})
		}, CloseProtocolError},
	}
	srv := echoServer(t, &Upgrader{MaxMessageSize: 4096})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := dial(t, srv)
			tc.send(c)
			c.expectClose(tc.want)
		})
	}
}

func TestHugeDeclaredLengthIsRejectedBeforeAllocation(t *testing.T) {
	c := dial(t, echoServer(t, &Upgrader{MaxMessageSize: 4096}))
	// заявлено 2^40 байт, тела нет — сервер обязан ответить 1009, а не пытаться выделить память
	c.sendRaw([]byte{0x81, 0x80 | 127, 0, 0, 1, 0, 0, 0, 0, 0})
	c.expectClose(CloseTooBig)
}

func TestHandshakeRejections(t *testing.T) {
	srv := echoServer(t, &Upgrader{CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") != "http://evil.example"
	}})
	good := func() map[string]string {
		return map[string]string{
			"Connection": "Upgrade", "Upgrade": "websocket",
			"Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": testKey,
		}
	}
	cases := []struct {
		name   string
		mutate func(h map[string]string)
		status int
	}{
		{"нет Upgrade", func(h map[string]string) { delete(h, "Upgrade") }, 400},
		{"нет Connection", func(h map[string]string) { delete(h, "Connection") }, 400},
		{"версия 8", func(h map[string]string) { h["Sec-WebSocket-Version"] = "8" }, 426},
		{"нет ключа", func(h map[string]string) { delete(h, "Sec-WebSocket-Key") }, 400},
		{"ключ не base64", func(h map[string]string) { h["Sec-WebSocket-Key"] = "!!!" }, 400},
		{"ключ не 16 байт", func(h map[string]string) { h["Sec-WebSocket-Key"] = base64.StdEncoding.EncodeToString([]byte("short")) }, 400},
		{"чужой Origin", func(h map[string]string) { h["Origin"] = "http://evil.example" }, 403},
		{"Connection списком", func(h map[string]string) { h["Connection"] = "keep-alive, Upgrade" }, 101},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := good()
			tc.mutate(h)
			nc, _, resp := rawHandshake(t, srv, h)
			defer nc.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("ожидали %d, получили %d", tc.status, resp.StatusCode)
			}
			if tc.status == 426 && resp.Header.Get("Sec-WebSocket-Version") != "13" {
				t.Fatal("в ответе 426 должен быть Sec-WebSocket-Version: 13")
			}
		})
	}
}

func TestNonGETRejected(t *testing.T) {
	srv := echoServer(t, &Upgrader{})
	resp, err := http.Post(srv.URL, "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("ожидали 405, получили %d", resp.StatusCode)
	}
}

func TestReadTimeout(t *testing.T) {
	// молчащий клиент отваливается по ReadTimeout
	srv := echoServer(t, &Upgrader{ReadTimeout: 200 * time.Millisecond})
	c := dial(t, srv)
	c.nc.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.br.ReadByte(); err == nil {
		t.Fatal("ожидали, что сервер оборвёт соединение молчащего клиента")
	}
}

func TestWriteAfterCloseFrameIsRejected(t *testing.T) {
	ch := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := &Upgrader{}
		c, err := u.Upgrade(w, r)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteClose(CloseGoingAway, "bye")
		ch <- c.WriteText([]byte("late"))
	}))
	defer srv.Close()
	c := dial(t, srv)
	c.expectClose(CloseGoingAway)
	if err := <-ch; err != ErrClosed {
		t.Fatalf("запись после Close: ожидали ErrClosed, получили %v", err)
	}
}

func TestSurvivesHTTPServerTimeouts(t *testing.T) {
	// После Hijack таймауты http.Server (Read/WriteTimeout) не должны убивать соединение —
	// в проде они равны 30 с, здесь 300 мс.
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&Upgrader{}).Upgrade(w, r)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			m, err := c.ReadMessage()
			if err != nil {
				return
			}
			c.WriteText(m)
		}
	}))
	srv.Config.ReadTimeout = 300 * time.Millisecond
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()

	c := dial(t, srv)
	time.Sleep(900 * time.Millisecond)
	c.send(true, opText, []byte("жив"))
	if op, p := c.read(); op != opText || string(p) != "жив" {
		t.Fatalf("op=%d %q", op, p)
	}
}

func TestFramesSentImmediatelyAfterHandshakeAreNotLost(t *testing.T) {
	// клиент шлёт кадр сразу вместе с рукопожатием — он окажется в буфере bufio сервера
	srv := echoServer(t, &Upgrader{})
	nc, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	req := "GET /ws HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + testKey + "\r\n\r\n"
	nc.Write(append([]byte(req), frame(true, 0, opText, []byte("ранний"), true)...))
	c := &testClient{t: t, nc: nc, br: bufio.NewReader(nc)}
	resp, err := http.ReadResponse(c.br, nil)
	if err != nil || resp.StatusCode != 101 {
		t.Fatalf("рукопожатие: %v %v", resp, err)
	}
	if op, p := c.read(); op != opText || string(p) != "ранний" {
		t.Fatalf("op=%d %q", op, p)
	}
}
