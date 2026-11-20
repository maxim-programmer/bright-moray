package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// syncBuf — потокобезопасный буфер с методом Sync, чтобы быть zapcore.WriteSyncer.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) Sync() error { return nil }
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// noDuplicateKeys проверяет, что в верхнем уровне JSON-объекта нет повторяющихся ключей
// (например, поле "level" рядом с системным "level"): такие строки по-разному разбираются
// разными парсерами, в том числе в Loki.
func noDuplicateKeys(t *testing.T, line string) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("не JSON-объект: %q", line)
	}
	seen := map[string]bool{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		key := k.(string)
		if seen[key] {
			t.Errorf("ключ %q повторяется: %s", key, line)
		}
		seen[key] = true
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNoDuplicateKeysInRequestRecord(t *testing.T) {
	js := &syncBuf{}
	l := zap.New(newCore(&syncBuf{}, false, js, zapcore.DebugLevel), zap.AddCaller(),
		zap.Fields(zap.String("service", "auction")))
	ctx, _ := WithRequest(context.Background(), l, "req-12345678")
	SetUser(ctx, 7)
	SetUser(ctx, 7) // повторный вызов не должен дублировать user_id
	FromContext(ctx, ComponentHandler).Info("x", zap.Int64("lot_id", 1), zap.Error(errors.New("e")))
	noDuplicateKeys(t, js.String())
}

func TestTeeSameFieldsDifferentFormat(t *testing.T) {
	con, js := &syncBuf{}, &syncBuf{}
	l := zap.New(newCore(con, false, js, zapcore.DebugLevel),
		zap.AddCaller(), zap.Fields(zap.String("service", "auction")))

	l.With(zap.String("component", "handler"), zap.String("request_id", "req-12345678")).
		Warn("отозваны сессии", zap.Int64("user_id", 42), zap.Error(errors.New("boom")))

	// JSON-ветка: все поля на месте, уровень строчными.
	noDuplicateKeys(t, js.String())
	var rec map[string]any
	if err := json.Unmarshal([]byte(js.String()), &rec); err != nil {
		t.Fatalf("файл — не JSON: %v\n%s", err, js.String())
	}
	want := map[string]any{
		"level": "warn", "msg": "отозваны сессии", "service": "auction",
		"component": "handler", "request_id": "req-12345678", "user_id": float64(42), "error": "boom",
	}
	for k, v := range want {
		if rec[k] != v {
			t.Errorf("JSON: %s = %v, ожидалось %v", k, rec[k], v)
		}
	}
	for _, k := range []string{"ts", "caller"} {
		if s, _ := rec[k].(string); s == "" {
			t.Errorf("JSON: нет поля %s", k)
		}
	}
	if !strings.HasSuffix(rec["ts"].(string), "Z") {
		t.Errorf("JSON: ts должен быть в UTC, получено %v", rec["ts"])
	}
	if !strings.Contains(rec["caller"].(string), "logging_test.go:") {
		t.Errorf("JSON: caller = %v", rec["caller"])
	}

	// Console-ветка: те же поля, но другой формат. Строка: ts \t LEVEL \t caller \t msg \t {поля}.
	out := con.String()
	if json.Valid([]byte(out)) {
		t.Errorf("console не должен быть JSON: %s", out)
	}
	parts := strings.Split(strings.TrimRight(out, "\n"), "\t")
	if len(parts) != 5 {
		t.Fatalf("console: ожидалось 5 колонок, получено %d: %q", len(parts), out)
	}
	if parts[1] != "WARN" || parts[3] != "отозваны сессии" || !strings.Contains(parts[2], "logging_test.go:") {
		t.Errorf("console: level/caller/msg = %q / %q / %q", parts[1], parts[2], parts[3])
	}
	var conFields map[string]any
	if err := json.Unmarshal([]byte(parts[4]), &conFields); err != nil {
		t.Fatalf("console: хвост не JSON: %v (%q)", err, parts[4])
	}
	// Главная проверка: контекстные поля в обеих ветках идентичны.
	for _, k := range []string{"service", "component", "request_id", "user_id", "error"} {
		if conFields[k] != rec[k] {
			t.Errorf("поле %s различается: console=%v json=%v", k, conFields[k], rec[k])
		}
	}
	if len(conFields) != 5 {
		t.Errorf("console: лишние поля: %v", conFields)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("цвет выключен, а в выводе escape-коды: %q", out)
	}
}

func TestConsoleColor(t *testing.T) {
	con := &syncBuf{}
	l := zap.New(newCore(con, true, nil, zapcore.InfoLevel))
	l.Info("x")
	if !strings.Contains(con.String(), "\x1b[") {
		t.Errorf("ожидался цветной уровень: %q", con.String())
	}
}

func TestLevelFilter(t *testing.T) {
	con, js := &syncBuf{}, &syncBuf{}
	l := zap.New(newCore(con, false, js, zapcore.WarnLevel))
	l.Info("тихо")
	l.Warn("громко")
	for name, out := range map[string]string{"console": con.String(), "json": js.String()} {
		if strings.Contains(out, "тихо") || !strings.Contains(out, "громко") {
			t.Errorf("%s: фильтр уровня не работает: %q", name, out)
		}
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]zapcore.Level{
		"": zapcore.InfoLevel, "info": zapcore.InfoLevel, " DEBUG ": zapcore.DebugLevel,
		"Warn": zapcore.WarnLevel, "error": zapcore.ErrorLevel,
	} {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"fatal", "trace", "warning", "1"} {
		if _, err := ParseLevel(bad); err == nil {
			t.Errorf("ParseLevel(%q): ожидалась ошибка", bad)
		}
	}
}

// runFiles возвращает имена файлов запусков в dir (по возрастанию, т. е. по времени).
func runFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if runFileRE.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestNewWritesJSONFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	l, closeFn, err := New(Config{Level: "debug", Dir: dir, Service: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hello", zap.String("component", "main"))
	closeFn()

	files := runFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("файлов запуска: %v", files)
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 { // «log file created» и hello
		t.Fatalf("строк в файле: %d\n%s", len(lines), data)
	}
	for _, line := range lines {
		noDuplicateKeys(t, line)
	}
	var created, hello map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &created); err != nil {
		t.Fatalf("в файле не JSON: %v\n%s", err, lines[0])
	}
	if err := json.Unmarshal([]byte(lines[1]), &hello); err != nil {
		t.Fatalf("в файле не JSON: %v\n%s", err, lines[1])
	}
	if created["msg"] != "log file created" || created["log_file"] != filepath.Join(dir, files[0]) {
		t.Errorf("запись о файле: %v", created)
	}
	if hello["msg"] != "hello" || hello["service"] != "svc" || hello["level"] != "info" {
		t.Errorf("запись: %v", hello)
	}
}

// Каждый запуск пишет в свой файл, а не дописывает в общий.
func TestNewCreatesFilePerRun(t *testing.T) {
	dir := t.TempDir()
	for _, msg := range []string{"first run", "second run"} {
		l, closeFn, err := New(Config{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		l.Info(msg)
		closeFn()
	}

	files := runFiles(t, dir)
	if len(files) != 2 || files[0] == files[1] {
		t.Fatalf("ожидалось два разных файла, получено %v", files)
	}
	for i, msg := range []string{"first run", "second run"} {
		data, err := os.ReadFile(filepath.Join(dir, files[i]))
		if err != nil {
			t.Fatal(err)
		}
		other := []string{"second run", "first run"}[i]
		if !strings.Contains(string(data), msg) || strings.Contains(string(data), other) {
			t.Errorf("файл %s: %s", files[i], data)
		}
	}
}

func TestRunFileNameAndMode(t *testing.T) {
	dir := t.TempDir()
	before := time.Now().UTC().Add(-time.Second)
	path, err := createRunFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	if filepath.Dir(path) != dir || !runFileRE.MatchString(name) {
		t.Fatalf("путь %q", path)
	}
	// имя — время старта в UTC: 2026-11-19T12-00-00.123456.log
	ts, err := time.Parse(runFileLayout, strings.TrimSuffix(name, ".log"))
	if err != nil {
		t.Fatalf("имя %q: %v", name, err)
	}
	if ts.Before(before) || ts.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("время в имени %v далеко от текущего", ts)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o444 != 0o444&^umaskBits() { // читается пользователем хоста
		t.Errorf("режим файла %v", info.Mode())
	}
}

// umaskBits возвращает текущую маску процесса, не меняя её.
func umaskBits() os.FileMode {
	old := syscall.Umask(0)
	syscall.Umask(old)
	return os.FileMode(old)
}

func TestPruneOldRuns(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	touch := func(name string, age time.Duration) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	const day = 24 * time.Hour
	touch("2026-11-15T10-00-00.000001.log", 4*day)
	touch("2026-11-16T10-00-00.000001.log", 3*day)
	touch("2026-11-17T10-00-00.000001.log", 2*day)
	touch("2026-11-18T10-00-00.000001.log", 1*day)
	touch("2026-11-18T10-00-00.000001-2026-11-18T15-00-00.000.log.gz", 1*day) // ротированная копия запуска
	touch("2026-10-01T10-00-00.000001.log", 49*day)                           // старше 14 суток
	touch("notes.txt", 100*day)                                               // чужие файлы не трогаем
	touch("app.log", 100*day)

	pruneOldRuns(dir, 2, 14*day, now)

	left := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left[e.Name()] = true
	}
	for _, name := range []string{
		"2026-11-17T10-00-00.000001.log",
		"2026-11-18T10-00-00.000001.log",
		"2026-11-18T10-00-00.000001-2026-11-18T15-00-00.000.log.gz", // уходит и остаётся вместе со своим запуском
		"notes.txt", "app.log",
	} {
		if !left[name] {
			t.Errorf("%s удалён, а не должен", name)
		}
	}
	for _, name := range []string{
		"2026-11-15T10-00-00.000001.log", "2026-11-16T10-00-00.000001.log", // сверх keep=2
		"2026-10-01T10-00-00.000001.log", // старше maxAge
	} {
		if left[name] {
			t.Errorf("%s должен быть удалён", name)
		}
	}

	// по возрасту удаляются и недавние по номеру запуска, если файл старый
	touch("2026-11-19T01-00-00.000001.log", 15*day)
	pruneOldRuns(dir, 10, 14*day, now)
	if _, err := os.Stat(filepath.Join(dir, "2026-11-19T01-00-00.000001.log")); err == nil {
		t.Error("файл старше maxAge не удалён")
	}
}

func TestPruneMissingDirIsNoop(t *testing.T) {
	pruneOldRuns(filepath.Join(t.TempDir(), "nope"), 1, time.Hour, time.Now()) // не должно паниковать
}

// New чистит старые запуски: после keepRuns+5 запусков остаётся ровно keepRuns файлов
// (включая файл последнего запуска).
func TestNewPrunesOldRuns(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < keepRuns+5; i++ {
		l, closeFn, err := New(Config{Dir: dir, Level: "error"})
		if err != nil {
			t.Fatal(err)
		}
		_ = l
		closeFn()
	}
	if n := len(runFiles(t, dir)); n != keepRuns {
		t.Errorf("файлов после серии запусков: %d, ожидалось %d", n, keepRuns)
	}
}

func TestNewWithoutFile(t *testing.T) {
	l, closeFn, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	closeFn()
	if l == nil {
		t.Fatal("логгер nil")
	}
}

func TestNewFailsFastOnBadDirOrLevel(t *testing.T) {
	// родитель — обычный файл, каталог создать нельзя
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New(Config{Dir: filepath.Join(blocker, "logs")}); err == nil {
		t.Error("недоступный каталог: ожидалась ошибка при старте")
	}
	if _, _, err := New(Config{Level: "loud"}); err == nil {
		t.Error("неизвестный уровень: ожидалась ошибка")
	}
}

func TestDefaultFallsBackToStdLog(t *testing.T) {
	SetDefault(nil)
	buf := &syncBuf{}
	old := log.Writer()
	log.SetOutput(buf)
	defer log.SetOutput(old)

	Default().Info("запасной", zap.String("component", "main"))
	var rec map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &rec); err != nil {
		t.Fatalf("запасной логгер пишет не JSON: %v\n%q", err, buf.String())
	}
	if rec["msg"] != "запасной" || rec["service"] != DefaultService {
		t.Errorf("запись: %v", rec)
	}
}

func TestSetDefault(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	SetDefault(zap.New(core))
	defer SetDefault(nil)

	FromContext(context.Background(), ComponentMain).Info("вне запроса")
	if logs.Len() != 1 {
		t.Fatalf("записей: %d", logs.Len())
	}
	m := logs.All()[0].ContextMap()
	if m["component"] != "main" {
		t.Errorf("component: %v", m)
	}
	if _, ok := m["request_id"]; ok {
		t.Errorf("вне запроса не должно быть request_id: %v", m)
	}
}

func TestRequestContext(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	ctx, req := WithRequest(context.Background(), zap.New(core), "req-12345678")

	FromContext(ctx, ComponentHandler).Info("до авторизации")
	SetUser(ctx, 42)
	FromContext(ctx, ComponentAuth).Info("после авторизации")

	all := logs.All()
	if len(all) != 2 {
		t.Fatalf("записей: %d", len(all))
	}
	before, after := all[0].ContextMap(), all[1].ContextMap()
	if before["request_id"] != "req-12345678" || before["component"] != "handler" {
		t.Errorf("до: %v", before)
	}
	if _, ok := before["user_id"]; ok {
		t.Errorf("до авторизации user_id быть не должно: %v", before)
	}
	if after["request_id"] != "req-12345678" || after["user_id"] != int64(42) || after["component"] != "auth" {
		t.Errorf("после: %v", after)
	}
	if req.UserID() != 42 || req.ID() != "req-12345678" || RequestID(ctx) != "req-12345678" {
		t.Errorf("аксессоры: uid=%d id=%q", req.UserID(), req.ID())
	}
	if RequestFrom(ctx) != req {
		t.Error("RequestFrom вернул другое состояние")
	}
}

func TestSetUserIgnoresInvalidAndMissingRequest(t *testing.T) {
	SetUser(context.Background(), 5) // не должно паниковать
	SetError(context.Background(), errors.New("x"))
	if RequestID(context.Background()) != "" || RequestFrom(context.Background()) != nil {
		t.Error("вне запроса состояния быть не должно")
	}

	ctx, req := WithRequest(context.Background(), zap.NewNop(), "req-12345678")
	SetUser(ctx, 0)
	SetUser(ctx, -3)
	if req.UserID() != 0 {
		t.Errorf("uid = %d", req.UserID())
	}
}

func TestSetErrorFirstWins(t *testing.T) {
	ctx, req := WithRequest(context.Background(), zap.NewNop(), "req-12345678")
	first, second := errors.New("first"), errors.New("second")
	SetError(ctx, nil)
	if req.Err() != nil {
		t.Fatal("nil не должен записываться")
	}
	SetError(ctx, first)
	SetError(ctx, second)
	if req.Err() != first {
		t.Errorf("err = %v, ожидалась первая", req.Err())
	}
}

func TestRequestStateIsRaceFree(t *testing.T) {
	ctx, req := WithRequest(context.Background(), zap.NewNop(), "req-12345678")
	var wg sync.WaitGroup
	for i := 1; i <= 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			SetUser(ctx, int64(i))
			SetError(ctx, errors.New("e"))
			FromContext(ctx, ComponentLive).Debug("x")
			_ = req.UserID()
			_ = req.Err()
		}(i)
	}
	wg.Wait()
}

func TestRequestIDs(t *testing.T) {
	a, b := NewRequestID(), NewRequestID()
	if a == b || len(a) != 32 || !ValidRequestID(a) {
		t.Errorf("NewRequestID: %q, %q", a, b)
	}
	for _, ok := range []string{"abcdefgh", "A1b2C3d4-e5_f6", strings.Repeat("x", 64)} {
		if !ValidRequestID(ok) {
			t.Errorf("ValidRequestID(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "short", strings.Repeat("x", 65), "has space 123", "new\nline1234", "кириллица1", `a"b"c"d"e"f`} {
		if ValidRequestID(bad) {
			t.Errorf("ValidRequestID(%q) = true", bad)
		}
	}
}
