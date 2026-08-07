package logx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"
)

// setup настраивает лог в буфер и возвращает его, восстанавливая прежние
// умолчания после теста.
func setup(t *testing.T, format string, limit int) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if err := Setup(&buf, format, "debug", limit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bodyLimit.Store(DefaultBodyLimit)
		Discard()
	})
	return &buf
}

func TestTruncateKeepsShortStringIntact(t *testing.T) {
	setup(t, "text", 100)
	const s = `{"text":"привет"}`
	if got := Truncate(s); got != s {
		t.Fatalf("короткая строка изменилась: %q", got)
	}
}

// Обрезка идёт по границе руны: разорванная кириллица превратила бы строку
// лога в мусор, который не читается и не грепается.
func TestTruncateCutsOnRuneBoundary(t *testing.T) {
	setup(t, "text", 9) // 'д' занимает байты 8-9 — предел приходится на её середину
	got := Truncate("абвгдежзий")
	if !utf8.ValidString(got) {
		t.Fatalf("обрезанная строка не UTF-8: %q", got)
	}
	if !strings.HasPrefix(got, "абвг") || strings.HasPrefix(got, "абвгд") {
		t.Fatalf("обрезано не по границе руны: %q", got)
	}
	if !strings.Contains(got, "обрезано") || !strings.Contains(got, "20") {
		t.Fatalf("нет пометки об обрезке с исходным размером: %q", got)
	}
}

// Тому, кто дочитал поток только до предела лога, len(b) не годится: он
// показал бы «всего 121 Б» на теле в мегабайт.
func TestBodyOfReportsGivenTotal(t *testing.T) {
	setup(t, "text", 10)
	head := []byte("0123456789A") // прочитано на байт больше предела
	got := BodyOf("req", head, 5000).Value.String()
	if !strings.Contains(got, "всего 5000 Б") {
		t.Fatalf("напечатан не переданный размер: %q", got)
	}
	// Размера нет вовсе (chunked) — про «всего» врать нечем.
	got = BodyOf("req", head, -1).Value.String()
	if !strings.Contains(got, "обрезано") || strings.Contains(got, "всего") {
		t.Fatalf("при неизвестном размере обещан итог: %q", got)
	}
}

func TestBodySkipsEmptyAndDisabled(t *testing.T) {
	buf := setup(t, "text", 100)
	slog.LogAttrs(t.Context(), slog.LevelInfo, "проба", Body("req", nil))
	if strings.Contains(buf.String(), "req") {
		t.Fatalf("пустое тело попало в строку: %s", buf)
	}

	buf.Reset()
	bodyLimit.Store(0)
	slog.LogAttrs(t.Context(), slog.LevelInfo, "проба", Body("req", []byte("важное")))
	if strings.Contains(buf.String(), "важное") {
		t.Fatalf("тело напечатано при body_limit=0: %s", buf)
	}
}

func TestMaskQueryHidesTokenOnly(t *testing.T) {
	cases := map[string]string{
		"chat_id=1&access_token=SECRET": "chat_id=1&access_token=***",
		"access_token=SECRET":           "access_token=***",
		"chat_id=1&count=5":             "chat_id=1&count=5",
		// Параметр, в имя которого токен входит подстрокой, не наш.
		"my_access_token=SECRET": "my_access_token=SECRET",
	}
	for raw, want := range cases {
		if got := MaskQuery(raw); got != want {
			t.Errorf("MaskQuery(%q) = %q, ожидалось %q", raw, got, want)
		}
	}
}

func TestQuerySkipsEmpty(t *testing.T) {
	if a := Query(""); !a.Equal(slog.Attr{}) {
		t.Fatalf("пустая строка запроса дала атрибут %v", a)
	}
}

func TestErrSkipsNil(t *testing.T) {
	if a := Err(nil); !a.Equal(slog.Attr{}) {
		t.Fatalf("nil-ошибка дала атрибут %v", a)
	}
}

func TestInt64SkipsZero(t *testing.T) {
	if a := Int64("chat", 0); !a.Equal(slog.Attr{}) {
		t.Fatalf("нулевой chat дал атрибут %v", a)
	}
	if a := Int64("chat", 7); a.Equal(slog.Attr{}) {
		t.Fatal("непустой chat пропал")
	}
}

func TestLevelForStatus(t *testing.T) {
	cases := map[int]slog.Level{
		200: slog.LevelInfo,
		404: slog.LevelWarn,
		500: slog.LevelError,
		0:   slog.LevelError, // ответа не было вовсе — сетевой сбой доставки
	}
	for status, want := range cases {
		if got := LevelForStatus(status); got != want {
			t.Errorf("LevelForStatus(%d) = %v, ожидалось %v", status, got, want)
		}
	}
}

func TestSetupJSONFormat(t *testing.T) {
	buf := setup(t, "json", 100)
	slog.Info("проба", "path", "/messages")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("строка не разобралась как JSON: %v (%s)", err, buf)
	}
	if line["msg"] != "проба" || line["path"] != "/messages" {
		t.Fatalf("поля потерялись: %v", line)
	}
}

// Непонятное значение валит старт, а не откатывается к умолчанию: иначе
// опечатка в конфиге означала бы выключенный лог ровно тогда, когда его
// включали.
func TestSetupRejectsUnknownValues(t *testing.T) {
	var buf bytes.Buffer
	t.Cleanup(Discard)
	if err := Setup(&buf, "yaml", "info", 100); err == nil {
		t.Error("неизвестный формат принят")
	}
	if err := Setup(&buf, "text", "инфо", 100); err == nil {
		t.Error("неизвестный уровень принят")
	}
	if err := Setup(&buf, "text", "info", -1); err == nil {
		t.Error("отрицательный предел тела принят")
	}
}
