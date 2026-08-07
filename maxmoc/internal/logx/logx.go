// Package logx настраивает консольный лог сервиса и готовит к печати то, что
// в строку лога не помещается как есть: тела запросов, ответы стендов, токены.
//
// Формат, уровень и предел на тело выбираются один раз при старте, дальше весь
// сервис пишет через slog.Default(). Отдельный объект-логгер поэтому никто не
// носит: место вызова и так известно по полям строки.
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// DefaultBodyLimit — сколько байт тела попадает в строку, пока Setup не сказал
// иначе. Хватает на сообщение с клавиатурой и не заливает терминал ответом на
// GET /messages.
const DefaultBodyLimit = 4096

// bodyLimit хранится глобально, а не в объекте-логгере: тела печатают четыре
// разных пакета, и протаскивать до каждого ещё один параметр значило бы
// менять сигнатуры ради значения, которое после старта не меняется.
var bodyLimit atomic.Int64

func init() { bodyLimit.Store(DefaultBodyLimit) }

// Setup ставит формат, уровень и предел тела для всего сервиса.
//
// Непонятное значение — ошибка, а не молчаливый откат к умолчанию:
// «log.level: инфо» иначе означало бы выключенный лог ровно тогда, когда его
// включали.
func Setup(w io.Writer, format, level string, limit int) error {
	lv, err := ParseLevel(level)
	if err != nil {
		return err
	}
	if limit < 0 {
		return fmt.Errorf("log.body_limit=%d: ожидалось неотрицательное число", limit)
	}
	opts := &slog.HandlerOptions{Level: lv}
	var h slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		h = slog.NewTextHandler(w, opts)
	case "json":
		h = slog.NewJSONHandler(w, opts)
	default:
		return fmt.Errorf("log.format=%q: ожидалось text или json", format)
	}
	bodyLimit.Store(int64(limit))
	slog.SetDefault(slog.New(h))
	return nil
}

// Discard выключает лог целиком. Нужен тестам: без него каждый прогон
// httptest печатает в терминал строку на запрос.
func Discard() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// ParseLevel переводит значение из конфига в уровень slog.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("log.level=%q: ожидалось debug, info, warn или error", s)
}

// Limit — действующий предел на тело в строке лога. 0 — тела не печатаются.
func Limit() int { return int(bodyLimit.Load()) }

// Body кладёт тело в атрибут. Пустое тело даёт пустой атрибут, который slog
// отбрасывает: строка не обрастает `req=""` на каждом GET.
func Body(key string, b []byte) slog.Attr { return BodyOf(key, b, len(b)) }

// BodyOf кладёт начало тела, зная его полный размер отдельно.
//
// Нужен тем, кто читает из потока только то, что попадёт в лог: там len(b) —
// размер прочитанного куска, и пометка «всего столько-то», посчитанная по
// нему, соврала бы ровно на интересном случае. total < 0 — размер неизвестен
// (chunked-запрос без Content-Length).
func BodyOf(key string, b []byte, total int) slog.Attr {
	if len(b) == 0 || Limit() == 0 {
		return slog.Attr{}
	}
	return slog.String(key, truncate(string(b), total))
}

// Truncate обрезает строку до предела, не разрывая руну, и сообщает исходный
// размер: «обрезано» без него неотличимо от «столько и было».
func Truncate(s string) string { return truncate(s, len(s)) }

func truncate(s string, total int) string {
	limit := Limit()
	if limit == 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if total < 0 {
		return s[:cut] + "…(обрезано)"
	}
	return fmt.Sprintf("%s…(обрезано, всего %d Б)", s[:cut], total)
}

// Query кладёт строку запроса в атрибут, замаскировав токен.
func Query(raw string) slog.Attr {
	if raw == "" {
		return slog.Attr{}
	}
	return slog.String("query", MaskQuery(raw))
}

// MaskQuery прячет значение access_token. Контракт Max этот параметр больше не
// принимает, но клиенты его пробуют — и тогда ключ бота лёг бы в лог открытым
// на каждом запросе.
func MaskQuery(raw string) string {
	if !strings.Contains(raw, "access_token") {
		return raw
	}
	parts := strings.Split(raw, "&")
	for i, p := range parts {
		if k, _, ok := strings.Cut(p, "="); ok && k == "access_token" {
			parts[i] = "access_token=***"
		}
	}
	return strings.Join(parts, "&")
}

// Err добавляет причину, если она есть.
func Err(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	return slog.String("error", err.Error())
}

// Int64 добавляет числовое поле, опуская нулевое: chat=0 и bot=0 означают
// «неизвестно», и в строке от них только шум.
func Int64(key string, v int64) slog.Attr {
	if v == 0 {
		return slog.Attr{}
	}
	return slog.Int64(key, v)
}

// LevelForStatus поднимает уровень строки по статусу ответа, чтобы отказ был
// виден и при level=warn. Статус 0 — ответа не было вовсе (сетевой сбой при
// доставке вебхука), это тоже ошибка.
func LevelForStatus(status int) slog.Level {
	switch {
	case status == 0 || status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
