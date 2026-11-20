// Package logging — настройка zap и доступ к логгеру текущего запроса.
//
// Схема вывода (zapcore.NewTee): одна запись уходит в две ветки с одним и тем же
// набором полей, различается только кодировщик:
//
//	logger ──► Tee ──┬─► Console-кодировщик ─► stdout        (для человека)
//	                 └─► JSON-кодировщик ────► файл + ротация (для машин, позже — Loki)
//
// Общие поля (ts, level, caller, msg, service, component) и ключи у обеих веток
// одинаковые: база EncoderConfig общая, а service и component добавляются на уровне
// логгера, а не кодировщика.
//
// Запросный контекст (request_id, user_id) живёт в context.Context — см. context.go.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

// Параметры ротации файла. Пока константы: нужна ли настройка, станет ясно на практике.
const (
	rotateMaxSizeMB  = 100 // размер файла, при котором он ротируется
	rotateMaxBackups = 5   // сколько старых файлов хранить
	rotateMaxAgeDays = 14  // и не дольше стольких дней
)

// Config — параметры логирования.
type Config struct {
	Level   string // debug, info, warn или error; пусто — info
	File    string // путь к JSON-файлу; пусто — только stdout
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
	return 0, fmt.Errorf("ожидается debug, info, warn или error, получено %q", s)
}

// New собирает логгер: Console в stdout и, если задан cfg.File, JSON в файл с ротацией.
// Вторым значением возвращается функция, которую нужно вызвать при остановке
// (сбрасывает буферы и закрывает файл).
//
// Недоступный файл — ошибка старта: пустой лог в production хуже упавшего запуска.
func New(cfg Config) (*zap.Logger, func(), error) {
	lvl, err := ParseLevel(cfg.Level)
	if err != nil {
		return nil, nil, fmt.Errorf("уровень лога: %w", err)
	}

	var (
		file      zapcore.WriteSyncer
		closeFile = func() error { return nil }
	)
	if cfg.File != "" {
		// lumberjack открывает файл лениво, при первой записи, и ошибку мы бы заметили
		// слишком поздно. Поэтому проверяем доступность файла сразу.
		if err := checkWritable(cfg.File); err != nil {
			return nil, nil, fmt.Errorf("файл лога: %w", err)
		}
		lj := &lumberjack.Logger{
			Filename:   cfg.File,
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

func checkWritable(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 0644: файл создаётся процессом внутри контейнера (uid 10001), а читать его должен
	// и пользователь хоста, когда каталог лога смонтирован с диска. Ротированные файлы
	// lumberjack создаёт с тем же режимом.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
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