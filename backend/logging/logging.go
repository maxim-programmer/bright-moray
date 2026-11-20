// Package logging — настройка zap и доступ к логгеру текущего запроса.
//
// Схема вывода (zapcore.NewTee): одна запись уходит в две ветки с одним и тем же
// набором полей, различается только кодировщик:
//
//	logger ──► Tee ──┬─► Console-кодировщик ─► stdout                 (для человека)
//	                 └─► JSON-кодировщик ────► <LOG_DIR>/<запуск>.log (для машин, позже — Loki)
//
// Каждый запуск процесса пишет в новый файл, имя которого — время старта (UTC):
// 2026-11-19T12-00-00.123456.log. Старые файлы удаляются при старте (см. pruneOldRuns).
//
// Общие поля (ts, level, caller, msg, service, component) и ключи у обеих веток
// одинаковые: база EncoderConfig общая, а service и component добавляются на уровне
// логгера, а не кодировщика.
//
// Запросный контекст (request_id, user_id) живёт в context.Context — см. context.go.
package logging

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Значения поля component: компонент приложения, написавший запись.
const (
	ComponentMain       = "main"       // запуск и остановка
	ComponentMiddleware = "middleware" // access-лог, panic
	ComponentHandler    = "handler"    // HTTP-обработчики
	ComponentAuth       = "auth"       // сессии и токены
	ComponentLive       = "live"       // WebSocket и таймеры аукционов
	ComponentServer     = "server"     // собственные сообщения net/http
)

// DefaultService — значение поля service, если не задано другое.
const DefaultService = "auction"

// Параметры хранения. Пока константы: нужна ли настройка, станет ясно на практике.
const (
	// Ротация внутри одного запуска (lumberjack).
	rotateMaxSizeMB  = 100 // размер файла, при котором он ротируется
	rotateMaxBackups = 5   // сколько ротированных файлов хранить
	rotateMaxAgeDays = 14  // и не дольше стольких дней

	// Очистка между запусками: при старте удаляются файлы старше rotateMaxAgeDays суток
	// и файлы запусков сверх keepRuns (считая текущий). Лимит по числу нужен на случай
	// цикла перезапусков контейнера, который иначе наплодил бы тысячи файлов.
	keepRuns = 30
)

// runFileLayout — формат имени файла запуска (UTC, микросекунды). Без двоеточий:
// такие имена безопасны на любой файловой системе и сортируются по времени.
const runFileLayout = "2006-01-02T15-04-05.000000"

// runFileRE узнаёт файлы, созданные этим пакетом: файл запуска
// (2026-11-19T12-00-00.123456.log) и его ротированные копии, которые lumberjack
// называет <имя>-<время>.log и .log.gz. Группа 1 — идентификатор запуска.
var runFileRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{6})(-.+)?\.log(\.gz)?$`)

// Config — параметры логирования.
type Config struct {
	Level   string // debug, info, warn или error; пусто — info
	Dir     string // каталог для файлов логов (по файлу на запуск); пусто — только stdout
	Service string // значение поля service; пусто — DefaultService
}

// ParseLevel разбирает уровень логирования (регистр не важен, пусто — info).
func ParseLevel(s string) (zapcore.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return zapcore.InfoLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	}
	return 0, fmt.Errorf("expected debug, info, warn or error, got %q", s)
}

// New собирает логгер: Console в stdout и, если задан cfg.Dir, JSON в новый файл
// <Dir>/<время запуска>.log с ротацией по размеру. Вторым значением возвращается
// функция, которую нужно вызвать при остановке (сбрасывает буферы и закрывает файл).
//
// Недоступный каталог — ошибка старта: пустой лог в production хуже упавшего запуска.
func New(cfg Config) (*zap.Logger, func(), error) {
	lvl, err := ParseLevel(cfg.Level)
	if err != nil {
		return nil, nil, fmt.Errorf("log level: %w", err)
	}

	var (
		file      zapcore.WriteSyncer
		closeFile = func() error { return nil }
		runPath   string
	)
	if cfg.Dir != "" {
		// Один слот из keepRuns занимает файл текущего запуска, который создаётся ниже.
		pruneOldRuns(cfg.Dir, keepRuns-1, rotateMaxAgeDays*24*time.Hour, time.Now())

		// Файл создаём сразу: lumberjack открывает его лениво, при первой записи, и
		// ошибку прав мы заметили бы слишком поздно.
		runPath, err = createRunFile(cfg.Dir)
		if err != nil {
			return nil, nil, fmt.Errorf("log file: %w", err)
		}
		lj := &lumberjack.Logger{
			Filename:   runPath,
			MaxSize:    rotateMaxSizeMB,
			MaxBackups: rotateMaxBackups,
			MaxAge:     rotateMaxAgeDays,
			Compress:   true,
		}
		file = zapcore.AddSync(lj)
		closeFile = lj.Close
	}

	service := cfg.Service
	if service == "" {
		service = DefaultService
	}
	core := newCore(zapcore.Lock(os.Stdout), colorOK(os.Stdout), file, lvl)
	l := zap.New(core, zap.AddCaller(), zap.Fields(zap.String("service", service)))
	if runPath != "" {
		l.Info("log file created",
			zap.String("component", ComponentMain), zap.String("log_file", runPath))
	}

	closeFn := func() {
		_ = l.Sync() // stdout может не поддерживать Sync (терминал, pipe) — это не ошибка
		_ = closeFile()
	}
	return l, closeFn, nil
}

// newCore собирает Tee из двух веток. file может быть nil — тогда остаётся только console.
func newCore(console zapcore.WriteSyncer, color bool, file zapcore.WriteSyncer, lvl zapcore.LevelEnabler) zapcore.Core {
	conCfg := baseEncoderConfig()
	conCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	conCfg.EncodeLevel = zapcore.CapitalLevelEncoder
	if color {
		conCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	cores := []zapcore.Core{zapcore.NewCore(zapcore.NewConsoleEncoder(conCfg), console, lvl)}

	if file != nil {
		cores = append(cores, zapcore.NewCore(zapcore.NewJSONEncoder(jsonEncoderConfig()), file, lvl))
	}
	return zapcore.NewTee(cores...)
}

// baseEncoderConfig — ключи, общие для обеих веток. NameKey не задан намеренно:
// компонент пишется полем component, а не именем логгера, чтобы схема не «плавала».
func baseEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}

// jsonEncoderConfig: уровень строчными буквами, время в UTC — так удобнее разбирать
// в Loki/Grafana.
func jsonEncoderConfig() zapcore.EncoderConfig {
	c := baseEncoderConfig()
	c.EncodeLevel = zapcore.LowercaseLevelEncoder
	c.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.UTC().Format("2006-01-02T15:04:05.000Z07:00"))
	}
	return c
}

// createRunFile создаёт пустой файл нового запуска в dir (каталог создаётся при
// необходимости) и возвращает его путь.
//
// 0644: файл создаётся процессом внутри контейнера (uid 10001), а читать его должен и
// пользователь хоста, когда каталог смонтирован с диска. Ротированные файлы lumberjack
// создаёт с тем же режимом.
func createRunFile(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for attempt := 0; ; attempt++ {
		path := filepath.Join(dir, time.Now().UTC().Format(runFileLayout)+".log")
		// O_EXCL: два запуска в одну микросекунду не должны писать в один файл.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			return path, f.Close()
		}
		if errors.Is(err, fs.ErrExist) && attempt < 5 {
			time.Sleep(time.Microsecond)
			continue
		}
		return "", err
	}
}

// pruneOldRuns удаляет из dir файлы прошлых запусков: старше maxAge (по времени
// изменения) и все, кроме keep последних запусков. Затрагиваются только файлы с именами
// этого пакета (runFileRE), чужие файлы в каталоге не трогаем. Очистка best-effort:
// ошибки игнорируются, потому что недоступная очистка не повод не запускать сервер.
// Вызывать до создания файла нового запуска.
func pruneOldRuns(dir string, keep int, maxAge time.Duration, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	runs := map[string][]string{} // идентификатор запуска -> его файлы
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if m := runFileRE.FindStringSubmatch(e.Name()); m != nil {
			runs[m[1]] = append(runs[m[1]], e.Name())
		}
	}
	ids := make([]string, 0, len(runs))
	for id := range runs {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids))) // имена сортируются по времени: новые первыми

	for i, id := range ids {
		for _, name := range runs[id] {
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			if i >= keep || (err == nil && now.Sub(info.ModTime()) > maxAge) {
				_ = os.Remove(path)
			}
		}
	}
}

// colorOK — цвет уровней включаем только для терминала: в `docker logs` и в pipe
// escape-последовательности превращаются в мусор.
func colorOK(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

/* ---------- логгер по умолчанию ---------- */

var (
	defaultLogger atomic.Pointer[zap.Logger]

	fallbackOnce sync.Once
	fallback     *zap.Logger
)

// Default возвращает процессный логгер по умолчанию. Его задаёт main через SetDefault;
// им пользуется код, которому логгер не передали явно (например, Middleware).
//
// Пока SetDefault не вызван, это JSON-логгер уровня info, пишущий в текущий вывод
// стандартного пакета log. Так вывод не пропадает в тестах и при встраивании, а тесты,
// перехватывающие log.SetOutput, продолжают работать.
func Default() *zap.Logger {
	if l := defaultLogger.Load(); l != nil {
		return l
	}
	fallbackOnce.Do(func() {
		fallback = zap.New(
			zapcore.NewCore(zapcore.NewJSONEncoder(jsonEncoderConfig()), &stdLogWriter{}, zapcore.InfoLevel),
			zap.AddCaller(),
			zap.Fields(zap.String("service", DefaultService)),
		)
	})
	return fallback
}

// SetDefault задаёт логгер по умолчанию; nil возвращает запасной вариант.
// Вызывать один раз при старте, до обработки запросов.
func SetDefault(l *zap.Logger) { defaultLogger.Store(l) }

// stdLogWriter пишет в тот вывод, который сейчас настроен у стандартного пакета log.
// Вывод читается при каждой записи, поэтому log.SetOutput действует и после создания.
type stdLogWriter struct{ mu sync.Mutex }

func (w *stdLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out io.Writer = log.Writer()
	return out.Write(p)
}

func (w *stdLogWriter) Sync() error { return nil }
