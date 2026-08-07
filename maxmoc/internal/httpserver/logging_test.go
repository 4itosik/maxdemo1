package httpserver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"maxmock/internal/logx"
)

// logSink — потокобезопасный приёмник лога: строку пишет горутина сервера, а
// читает тест.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// waitFor ждёт появления подстроки: строка пишется после того, как ответ ушёл
// клиенту, поэтому к моменту проверки её может ещё не быть.
func (s *logSink) waitFor(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := s.String(); strings.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("в логе нет %q; лог:\n%s", want, s.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func captureLog(t *testing.T, level string, limit int) *logSink {
	t.Helper()
	sink := &logSink{}
	if err := logx.Setup(sink, "text", level, limit); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logx.Discard)
	return sink
}

// Предел на тело — только для лога: обработчик обязан получить запрос целиком,
// иначе включение подробного лога начало бы менять поведение мока.
func TestRequestLogTruncatesBodyButHandlerSeesAll(t *testing.T) {
	f := newFixture(t)
	sink := captureLog(t, "info", 40)

	name := strings.Repeat("Я", 60) // 120 байт — заведомо больше предела
	resp, body := f.req(t, "POST", "/mock/api/bots", `{"name":"`+name+`","username":"bot"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("создание бота: %d %s", resp.StatusCode, body)
	}
	var bot map[string]any
	if err := json.Unmarshal(body, &bot); err != nil {
		t.Fatal(err)
	}
	if bot["name"] != name {
		t.Fatalf("обработчик увидел обрезанное тело: %q", bot["name"])
	}

	line := sink.waitFor(t, "path=/mock/api/bots")
	for _, want := range []string{"method=POST", "status=200", "ms=", "обрезано"} {
		if !strings.Contains(line, want) {
			t.Errorf("в строке лога нет %q:\n%s", want, line)
		}
	}
}

// Отказ должен быть виден и тогда, когда подробный лог выключен.
func TestRequestLogRaisesLevelOnFailure(t *testing.T) {
	f := newFixture(t)
	sink := captureLog(t, "info", 4096)

	resp, _ := f.req(t, "GET", "/mock/api/bots/999999", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("статус %d, ожидался 404", resp.StatusCode)
	}
	line := sink.waitFor(t, "path=/mock/api/bots/999999")
	if !strings.Contains(line, "level=WARN") {
		t.Errorf("404 записан не как предупреждение:\n%s", line)
	}
}

// Токен из query не должен осесть в консоли: контракт Max его не принимает, но
// клиенты пробуют, и тогда ключ бота печатался бы на каждом запросе.
func TestRequestLogMasksToken(t *testing.T) {
	f := newFixture(t)
	sink := captureLog(t, "info", 4096)

	f.req(t, "GET", "/mock/api/bots?access_token=SECRET", "", nil)

	line := sink.waitFor(t, "path=/mock/api/bots")
	if strings.Contains(line, "SECRET") {
		t.Errorf("токен попал в лог:\n%s", line)
	}
	if !strings.Contains(line, "access_token=***") {
		t.Errorf("нет замаскированного токена:\n%s", line)
	}
}

func TestPolicyForPath(t *testing.T) {
	cases := []struct {
		path  string
		level slog.Level
		want  bodies
	}{
		{"/mock/api/bots", slog.LevelInfo, bodies{req: true, resp: true}},
		{"/mock/api/dialogs/42/actions", slog.LevelInfo, bodies{req: true, resp: true}},
		// Тело запроса — сам файл, до 256 МБ; в ответе токен вложения.
		{"/mock/upload/att.abc", slog.LevelInfo, bodies{resp: true}},
		{"/mock/api/bots/1/files", slog.LevelInfo, bodies{resp: true}},
		// Отдача файла — наоборот.
		{"/mock/files/att.abc", slog.LevelInfo, bodies{}},
		// Фон, на который смотрят только при разборе неполадок.
		{"/mock/static/chat.js", slog.LevelDebug, bodies{}},
		{"/mock/chat/1", slog.LevelDebug, bodies{}},
		{"/mock", slog.LevelDebug, bodies{}},
		{"/healthz", slog.LevelDebug, bodies{}},
	}
	for _, c := range cases {
		level, got := policyFor(c.path)
		if level != c.level || got != c.want {
			t.Errorf("policyFor(%q) = %v %+v, ожидалось %v %+v", c.path, level, got, c.level, c.want)
		}
	}
}
