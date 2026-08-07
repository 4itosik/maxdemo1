package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"maxbotdemo/internal/maxapi"
)

const validUpdateJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000000,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.1", "seq": 1, "text": "привет"}
  }
}`

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newRecordingServer возвращает сервер и канал, в который попадают
// обработанные события.
func newRecordingServer(secret string) (*Server, chan maxapi.Update) {
	received := make(chan maxapi.Update, 1)
	handler := func(_ context.Context, u maxapi.Update) { received <- u }
	return New(secret, handler, discardLogger()), received
}

// newLoggingServer возвращает сервер, буфер с его логом и канал событий.
// Записи о полученном событии делаются в ServeHTTP синхронно, до запуска
// обработчика, поэтому читать буфер безопасно сразу после post.
func newLoggingServer(secret string) (*Server, *bytes.Buffer, chan maxapi.Update) {
	var buf bytes.Buffer
	received := make(chan maxapi.Update, 1)
	handler := func(_ context.Context, u maxapi.Update) { received <- u }
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	return New(secret, handler, log), &buf, received
}

// findRecord ищет в логе запись с заданным msg. Одна строка — одна запись.
func findRecord(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("строка лога не разбирается как JSON: %v (%s)", err, line)
		}
		if rec["msg"] == msg {
			return rec
		}
	}
	t.Fatalf("в логе нет записи %q, лог: %s", msg, buf.String())
	return nil
}

// post отправляет запрос серверу и возвращает записанный ответ.
func post(srv *Server, body, secretHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	if secretHeader != "" {
		req.Header.Set(SecretHeader, secretHeader)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// awaitUpdate ждёт события от обработчика.
func awaitUpdate(t *testing.T, ch chan maxapi.Update) maxapi.Update {
	t.Helper()
	select {
	case u := <-ch:
		return u
	case <-time.After(2 * time.Second):
		t.Fatal("обработчик не был вызван")
		return maxapi.Update{}
	}
}

// assertNoUpdate проверяет, что обработчик не вызывался.
func assertNoUpdate(t *testing.T, srv *Server, ch chan maxapi.Update) {
	t.Helper()
	srv.Wait()
	select {
	case u := <-ch:
		t.Fatalf("обработчик вызван для %+v, want ни одного вызова", u)
	default:
	}
}

func TestValidRequestIsAcceptedAndHandled(t *testing.T) {
	srv, received := newRecordingServer("topsecret")

	rec := post(srv, validUpdateJSON, "topsecret")

	if rec.Code != http.StatusOK {
		t.Errorf("статус = %d, want %d", rec.Code, http.StatusOK)
	}
	u := awaitUpdate(t, received)
	if u.UpdateType != maxapi.UpdateMessageCreated {
		t.Errorf("update_type = %q, want %q", u.UpdateType, maxapi.UpdateMessageCreated)
	}
	if u.Text() != "привет" {
		t.Errorf("текст = %q, want %q", u.Text(), "привет")
	}
}

func TestRequestWithoutConfiguredSecretNeedsNoHeader(t *testing.T) {
	srv, received := newRecordingServer("")

	rec := post(srv, validUpdateJSON, "")

	if rec.Code != http.StatusOK {
		t.Errorf("статус = %d, want %d", rec.Code, http.StatusOK)
	}
	awaitUpdate(t, received)
}

func TestMissingSecretHeaderIsRejected(t *testing.T) {
	srv, received := newRecordingServer("topsecret")

	rec := post(srv, validUpdateJSON, "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("статус = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertNoUpdate(t, srv, received)
}

func TestWrongSecretHeaderIsRejected(t *testing.T) {
	srv, received := newRecordingServer("topsecret")

	rec := post(srv, validUpdateJSON, "неправильный")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("статус = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertNoUpdate(t, srv, received)
}

func TestNonPostIsRejected(t *testing.T) {
	srv, received := newRecordingServer("")

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("статус = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	assertNoUpdate(t, srv, received)
}

func TestMalformedJSONIsRejected(t *testing.T) {
	srv, received := newRecordingServer("")

	rec := post(srv, `{"update_type":`, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("статус = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertNoUpdate(t, srv, received)
}

func TestReceivedEventIsLoggedWithPayload(t *testing.T) {
	srv, logged, received := newLoggingServer("")

	post(srv, validUpdateJSON, "")
	awaitUpdate(t, received)

	rec := findRecord(t, logged, "получено событие")
	if rec["update_type"] != "message_created" {
		t.Errorf("update_type = %#v, want %q", rec["update_type"], "message_created")
	}

	payload, ok := rec["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want объект", rec["payload"])
	}
	message, ok := payload["message"].(map[string]any)
	if !ok {
		t.Fatalf(`payload["message"] = %#v, want объект`, payload["message"])
	}
	body, ok := message["body"].(map[string]any)
	if !ok {
		t.Fatalf(`message["body"] = %#v, want объект`, message["body"])
	}
	if body["text"] != "привет" {
		t.Errorf(`body["text"] = %#v, want "привет"`, body["text"])
	}
}

// Неразбираемое тело тоже должно попадать в лог — строкой, чтобы сама запись
// осталась валидным JSON.
func TestMalformedBodyIsLoggedAsString(t *testing.T) {
	srv, logged, received := newLoggingServer("")

	rec := post(srv, `{"update_type":`, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("статус = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertNoUpdate(t, srv, received)

	entry := findRecord(t, logged, "не удалось разобрать событие")
	if entry["payload"] != `{"update_type":` {
		t.Errorf("payload = %#v, want строку с исходным телом", entry["payload"])
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	srv, _, received := newLoggingServer("")

	body := `{"update_type":"message_created","padding":"` +
		strings.Repeat("a", maxBodyBytes) + `"}`
	rec := post(srv, body, "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("статус = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertNoUpdate(t, srv, received)
}

func TestWaitBlocksUntilHandlerFinishes(t *testing.T) {
	finished := make(chan struct{})
	started := make(chan struct{})
	srv := New("", func(_ context.Context, _ maxapi.Update) {
		close(started)
		time.Sleep(50 * time.Millisecond)
		close(finished)
	}, discardLogger())

	post(srv, validUpdateJSON, "")
	<-started
	srv.Wait()

	select {
	case <-finished:
	default:
		t.Error("Wait() вернулся до завершения обработчика")
	}
}
