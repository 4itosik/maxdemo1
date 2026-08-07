# Полученное и отправленное сообщение в логе — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Видеть в логе содержимое переписки — тело события, которое прислал MAX, и тело запроса, который бот отправил в ответ, — при том что весь лог становится JSON.

**Architecture:** Новый пакет `internal/jsonlog` даёт тип `Raw`, который кладёт сырое тело в строку лога объектом, а не экранированной строкой: `slog.JSONHandler` кодирует значение `slog.Any` через `json.Marshal`, поэтому достаточно метода `MarshalJSON`, возвращающего тело как есть. Снимаются тела на границе HTTP: входящее — в `internal/webhook`, где тело теперь читается в память целиком; исходящее и ответ API — в `Client.do` пакета `internal/maxapi`, куда добавляется опциональный логгер. `internal/bot` не меняется вовсе.

**Tech Stack:** Go 1.24, только стандартная библиотека (`log/slog`, `encoding/json`, `net/http`). Модуль `maxbotdemo`. Тесты — `testing` и `net/http/httptest`, без сети.

Спека: [`docs/superpowers/specs/2026-08-06-message-logging-design.md`](../specs/2026-08-06-message-logging-design.md).

## Global Constraints

- **Никаких внешних зависимостей.** Только стандартная библиотека Go — правило проекта, `go.mod` без `require`.
- **Комментарии и сообщения лога — на русском**, как весь существующий код.
- **Тесты не ходят в сеть.** Клиент проверяется на `httptest`, webhook — на `httptest.NewRecorder`.
- **Логгер в тестах — `slog.NewJSONHandler` в `bytes.Buffer`**, записи разбираются обратно `json.Unmarshal`. Проверять подстроками то, ради чего затевался машиночитаемый формат, нельзя.
- **Комментарий объясняет «почему», а не «что»** — так написан весь остальной код проекта.
- **После каждой задачи зелёные** `go test ./...` и `go test -race ./...`.
- **Секреты в лог не попадают.** Токен уходит заголовком `Authorization` и не логируется; поле `secret` в теле маскируется (задача 4).

## Состав файлов

| Файл | Что делает | Задачи |
|---|---|---|
| `internal/jsonlog/jsonlog.go` (новый) | тип `Raw` — сырой JSON внутри строки лога | 1 |
| `internal/jsonlog/jsonlog_test.go` (новый) | три случая: валидный JSON, не-JSON, пусто | 1 |
| `internal/webhook/server.go` | чтение тела в память под лимитом, `payload` в записях о событии | 2 |
| `internal/webhook/server_test.go` | помощники разбора лога, три новых теста | 2 |
| `internal/maxapi/client.go` | опция `WithLogger`, записи «запрос к API» и «ответ API», маскирование | 3, 4 |
| `internal/maxapi/client_test.go` | помощники разбора лога, тесты записей и маскирования | 3, 4 |
| `cmd/bot/main.go` | `slog.NewJSONHandler`, передача логгера клиенту | 5 |
| `README.md` | раздел «Логи», строка в «Известные ограничения» | 5 |

---

### Task 1: Пакет `internal/jsonlog` — сырой JSON внутри строки лога

`slog.JSONHandler` кодирует значение атрибута через `json.Marshal`. Обычная строка при этом экранируется — в логе получилось бы `"payload":"{\"text\":\"привет\"}"`, и разобрать такую запись одним проходом нельзя. Тип с методом `MarshalJSON`, возвращающим тело как есть, вкладывается объектом.

Пакет отдельный, потому что тип нужен двум пакетам сразу (`webhook` и `maxapi`), а к предмету ни одного из них не относится.

**Files:**
- Create: `internal/jsonlog/jsonlog.go`
- Test: `internal/jsonlog/jsonlog_test.go`

**Interfaces:**
- Consumes: ничего
- Produces: `type jsonlog.Raw []byte` с методом `MarshalJSON() ([]byte, error)`. Используется как `slog.Any`-значение: `log.Info("…", "payload", jsonlog.Raw(body))`

- [x] **Step 1: Написать падающие тесты**

Создать `internal/jsonlog/jsonlog_test.go`:

```go
package jsonlog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

// logPayload пишет значение в JSON-лог и возвращает разобранную запись.
// Проверять MarshalJSON напрямую мало: смысл типа в том, как он выглядит
// внутри строки лога, а её собирает slog.
func logPayload(t *testing.T, v Raw) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("запись", "payload", v)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("строка лога не разбирается как JSON: %v (%s)", err, buf.String())
	}
	return rec
}

func TestValidJSONIsEmbeddedAsObject(t *testing.T) {
	rec := logPayload(t, Raw(`{"text":"привет","n":1}`))

	payload, ok := rec["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want объект", rec["payload"])
	}
	if payload["text"] != "привет" {
		t.Errorf(`payload["text"] = %#v, want "привет"`, payload["text"])
	}
	if payload["n"] != float64(1) {
		t.Errorf(`payload["n"] = %#v, want 1`, payload["n"])
	}
}

// Вложенные объекты не должны схлопываться в строку: именно так приезжает
// тело события с сообщением и вложениями.
func TestNestedObjectSurvives(t *testing.T) {
	rec := logPayload(t, Raw(`{"message":{"body":{"text":"привет"}}}`))

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

func TestNonJSONBecomesString(t *testing.T) {
	rec := logPayload(t, Raw(`{"update_type":`))

	if rec["payload"] != `{"update_type":` {
		t.Errorf("payload = %#v, want строку с исходным текстом", rec["payload"])
	}
}

func TestEmptyBecomesNull(t *testing.T) {
	rec := logPayload(t, Raw(nil))

	v, ok := rec["payload"]
	if !ok {
		t.Fatal("в записи нет поля payload, want null")
	}
	if v != nil {
		t.Errorf("payload = %#v, want null", v)
	}
}
```

- [x] **Step 2: Убедиться, что тесты не собираются**

Run: `go test ./internal/jsonlog/`
Expected: FAIL — `undefined: Raw` (файла с типом ещё нет).

- [x] **Step 3: Написать реализацию**

Создать `internal/jsonlog/jsonlog.go`:

```go
// Package jsonlog кладёт сырой JSON в лог так, чтобы он остался JSON'ом, а не
// строкой с экранированными кавычками.
package jsonlog

import "encoding/json"

// Raw — сырое тело запроса или ответа, пригодное как значение атрибута slog.
//
// slog.JSONHandler кодирует такое значение через json.Marshal, поэтому тело
// возвращается как есть и вкладывается в строку лога объектом. Побочно
// json.Marshal сжимает отступы и переводы строк — читать лог это не мешает.
type Raw []byte

// MarshalJSON всегда возвращает валидный JSON: строка лога не должна ломаться
// из-за того, что в неё попало.
func (r Raw) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		// Тела нет вовсе — например, у GET /me.
		return []byte("null"), nil
	}
	if !json.Valid(r) {
		// Не JSON: обрезанное тело или мусор от чужого запроса. Кладём
		// текстом, чтобы причина не потерялась.
		return json.Marshal(string(r))
	}
	return r, nil
}
```

- [x] **Step 4: Убедиться, что тесты проходят**

Run: `go test ./internal/jsonlog/ -v`
Expected: PASS — все четыре теста.

- [x] **Step 5: Коммит**

```bash
git add internal/jsonlog/
git commit -m "Класть сырой JSON в лог объектом, а не строкой"
```

---

### Task 2: Тело полученного события в логе

Сейчас тело разбирается декодером прямо из `r.Body` (`internal/webhook/server.go:64`) и после разбора недоступно. Тело читается в память, чтобы попасть в лог в точности как его прислал MAX — со всеми полями, которых `maxapi.Update` не знает.

Лимит на размер — прямое следствие буферизации: пока тело читалось декодером из потока, класть его целиком в память никто не обещал.

**Files:**
- Modify: `internal/webhook/server.go:1-14` (импорты), `internal/webhook/server.go:19-23` (константы), `internal/webhook/server.go:63-71` (чтение, разбор, запись в лог)
- Test: `internal/webhook/server_test.go`

**Interfaces:**
- Consumes: `jsonlog.Raw` из задачи 1
- Produces: константа `maxBodyBytes = 1 << 20`; записи лога `получено событие` (поля `update_type`, `payload`), `не удалось разобрать событие` (поля `error`, `payload`), `не удалось прочитать тело запроса` (поле `error`)

- [x] **Step 1: Написать падающие тесты**

Добавить в `internal/webhook/server_test.go`. В импорты дописать `bytes` и `encoding/json` — сейчас файл импортирует `context`, `io`, `log/slog`, `net/http`, `net/http/httptest`, `strings`, `testing`, `time` и `maxbotdemo/internal/maxapi`.

Помощники — рядом с существующим `newRecordingServer`:

```go
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
```

Сами тесты:

```go
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
```

- [x] **Step 2: Убедиться, что тесты падают**

Run: `go test ./internal/webhook/ -run 'Payload|MalformedBody|Oversized' -v`
Expected: FAIL — `undefined: maxBodyBytes`, а после её появления — `в логе нет записи "получено событие"` (в записи нет `payload`).

- [x] **Step 3: Написать реализацию**

В `internal/webhook/server.go` дописать импорты `io` и `maxbotdemo/internal/jsonlog`:

```go
import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"maxbotdemo/internal/jsonlog"
	"maxbotdemo/internal/maxapi"
)
```

Рядом с `handlerTimeout` добавить константу:

```go
// maxBodyBytes ограничивает размер тела события. Лимит нужен потому, что тело
// читается в память целиком: иначе один большой запрос стоил бы боту памяти.
const maxBodyBytes = 1 << 20
```

Заменить блок разбора (`internal/webhook/server.go:63-71`):

```go
	// Тело читается целиком, а не разбирается из потока: в лог оно должно
	// попасть в точности таким, каким его прислал MAX.
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.log.Warn("не удалось прочитать тело запроса", "error", err)
		http.Error(w, "тело запроса не прочитано", http.StatusBadRequest)
		return
	}

	var u maxapi.Update
	if err := json.Unmarshal(raw, &u); err != nil {
		s.log.Warn("не удалось разобрать событие", "error", err, "payload", jsonlog.Raw(raw))
		http.Error(w, "тело запроса не является объектом Update", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	s.log.Info("получено событие", "update_type", u.UpdateType, "payload", jsonlog.Raw(raw))
```

- [x] **Step 4: Убедиться, что тесты проходят**

Run: `go test ./internal/webhook/ -v`
Expected: PASS — три новых теста и все существующие, включая `TestMalformedJSONIsRejected`.

- [x] **Step 5: Проверить весь пакет на гонки**

Run: `go test -race ./...`
Expected: PASS без предупреждений детектора гонок.

- [x] **Step 6: Коммит**

```bash
git add internal/webhook/
git commit -m "Писать в лог тело полученного события"
```

---

### Task 3: Тело запроса и ответа API в логе

`Client.do` маршалит тело в локальную переменную `encoded` (`internal/maxapi/client.go:89`), а ответ читает в `raw` (`internal/maxapi/client.go:111`) — наружу не выходит ни то, ни другое. Клиенту нужен логгер.

По умолчанию логгер молчит (`slog.DiscardHandler`), а не `nil`: так `do` не обрастает проверками, а тесты, создающие клиента без опции, продолжают работать.

**Files:**
- Modify: `internal/maxapi/client.go:8-18` (импорты), `internal/maxapi/client.go:23-54` (поле `log`, опция, значение по умолчанию), `internal/maxapi/client.go:81-118` (записи в лог внутри `do`)
- Test: `internal/maxapi/client_test.go:24-50` (помощники), новые тесты в конце файла

**Interfaces:**
- Consumes: `jsonlog.Raw` из задачи 1
- Produces: `func maxapi.WithLogger(log *slog.Logger) Option`; записи лога `запрос к API` (поля `method`, `path`, `query`, `payload`) и `ответ API` (поля `method`, `path`, `status`, `payload`)

- [x] **Step 1: Написать падающие тесты**

В `internal/maxapi/client_test.go` дописать импорты `bytes` и `log/slog` — сейчас файл импортирует `context`, `encoding/json`, `errors`, `io`, `net/http`, `net/http/httptest`, `net/url`, `reflect`, `testing`.

Заменить помощники `newStub` и `newStubWithStatus` (`internal/maxapi/client_test.go:24-50`) так, чтобы у заглушки появился буфер лога, а существующие вызовы не изменились:

```go
// newStub поднимает заглушку Max API, отвечающую заданным JSON со статусом 200.
func newStub(t *testing.T, responseJSON string) (*Client, *capturedRequest) {
	t.Helper()
	client, got, _ := newLoggingStub(t, http.StatusOK, responseJSON)
	return client, got
}

func newStubWithStatus(t *testing.T, status int, responseJSON string) (*Client, *capturedRequest) {
	t.Helper()
	client, got, _ := newLoggingStub(t, status, responseJSON)
	return client, got
}

// newLoggingStub поднимает заглушку и возвращает клиента, пишущего лог в буфер.
func newLoggingStub(t *testing.T, status int, responseJSON string) (*Client, *capturedRequest, *bytes.Buffer) {
	t.Helper()

	var got capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = capturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Auth:   r.Header.Get("Authorization"),
			Body:   body,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, responseJSON)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	client := New("test-token",
		WithBaseURL(srv.URL),
		WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
	return client, &got, &buf
}

// findRecord ищет в логе запись с заданным msg. Одна строка — одна запись.
// Тот же помощник есть в internal/webhook: пакеты разные, а тащить ради
// пятнадцати строк общий testutil незачем.
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
```

`findRecord` использует `strings` — этот импорт тоже нужно дописать.

Новые тесты в конец файла:

```go
func TestRequestAndResponseAreLogged(t *testing.T) {
	client, _, logged := newLoggingStub(t, http.StatusOK,
		`{"message":{"body":{"mid":"mid.7","text":"привет"}}}`)

	_, err := client.SendMessage(context.Background(),
		Target{ChatID: 777}, NewMessageBody{Text: "привет"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	req := findRecord(t, logged, "запрос к API")
	if req["method"] != http.MethodPost || req["path"] != "/messages" {
		t.Errorf("запись запроса = %#v, want POST /messages", req)
	}
	if req["query"] != "chat_id=777" {
		t.Errorf(`query = %#v, want "chat_id=777"`, req["query"])
	}
	reqPayload, ok := req["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload запроса = %#v, want объект", req["payload"])
	}
	if reqPayload["text"] != "привет" {
		t.Errorf(`payload["text"] = %#v, want "привет"`, reqPayload["text"])
	}

	resp := findRecord(t, logged, "ответ API")
	if resp["status"] != float64(http.StatusOK) {
		t.Errorf("status = %#v, want 200", resp["status"])
	}
	respPayload, ok := resp["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload ответа = %#v, want объект", resp["payload"])
	}
	message, ok := respPayload["message"].(map[string]any)
	if !ok {
		t.Fatalf(`payload["message"] = %#v, want объект`, respPayload["message"])
	}
	body, ok := message["body"].(map[string]any)
	if !ok {
		t.Fatalf(`message["body"] = %#v, want объект`, message["body"])
	}
	if body["mid"] != "mid.7" {
		t.Errorf(`body["mid"] = %#v, want "mid.7"`, body["mid"])
	}
}

// У GET /me тела запроса нет — в логе должен быть null, а не пустая строка.
func TestRequestWithoutBodyLogsNullPayload(t *testing.T) {
	client, _, logged := newLoggingStub(t, http.StatusOK, `{"user_id":1,"is_bot":true}`)

	if _, err := client.GetMyInfo(context.Background()); err != nil {
		t.Fatalf("GetMyInfo: %v", err)
	}

	req := findRecord(t, logged, "запрос к API")
	v, ok := req["payload"]
	if !ok {
		t.Fatal("в записи нет поля payload, want null")
	}
	if v != nil {
		t.Errorf("payload = %#v, want null", v)
	}
}

// Отказ API логируется целиком, до превращения тела в *APIError: причина
// видна вся, а не только в свёрнутом виде "max api: 400 …".
func TestFailedResponseIsLogged(t *testing.T) {
	client, _, logged := newLoggingStub(t, http.StatusBadRequest,
		`{"code":"attachment.not.ready","message":"Вложение ещё не готово"}`)

	_, err := client.SendMessage(context.Background(),
		Target{ChatID: 7}, NewMessageBody{Text: "x"})
	if err == nil {
		t.Fatal("SendMessage вернул nil, want ошибку")
	}

	resp := findRecord(t, logged, "ответ API")
	if resp["status"] != float64(http.StatusBadRequest) {
		t.Errorf("status = %#v, want 400", resp["status"])
	}
	payload, ok := resp["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want объект", resp["payload"])
	}
	if payload["code"] != "attachment.not.ready" {
		t.Errorf(`payload["code"] = %#v, want "attachment.not.ready"`, payload["code"])
	}
}

// Клиент без WithLogger не должен падать: логгер по умолчанию молчит, а не nil.
func TestClientWithoutLoggerDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"user_id":1,"is_bot":true}`)
	}))
	t.Cleanup(srv.Close)

	client := New("test-token", WithBaseURL(srv.URL))
	if _, err := client.GetMyInfo(context.Background()); err != nil {
		t.Fatalf("GetMyInfo: %v", err)
	}
}
```

- [x] **Step 2: Убедиться, что тесты не собираются**

Run: `go test ./internal/maxapi/`
Expected: FAIL — `undefined: WithLogger`.

- [x] **Step 3: Написать реализацию**

В `internal/maxapi/client.go` дописать импорты `log/slog` и `maxbotdemo/internal/jsonlog`:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"maxbotdemo/internal/jsonlog"
)
```

Добавить поле в `Client` (`internal/maxapi/client.go:23-28`):

```go
// Client выполняет запросы к Max Bot API.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
	log     *slog.Logger
}
```

Опцию — рядом с `WithHTTPClient`:

```go
// WithLogger включает запись запросов и ответов API в лог.
func WithLogger(log *slog.Logger) Option {
	return func(c *Client) { c.log = log }
}
```

Значение по умолчанию — в `New` (`internal/maxapi/client.go:44-54`):

```go
// New создаёт клиента с указанным токеном бота.
func New(token string, opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		token:   token,
		hc:      &http.Client{Timeout: 30 * time.Second},
		// Молчащий логгер, а не nil: так do не обрастает проверками.
		log: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}
```

В `do` вынести `encoded` из блока `if in != nil`, чтобы тело дожило до записи в лог, и добавить две записи:

```go
	var encoded []byte
	var body io.Reader
	if in != nil {
		var err error
		encoded, err = json.Marshal(in)
		if err != nil {
			return fmt.Errorf("кодирование тела запроса: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("создание запроса: %w", err)
	}
	req.Header.Set("Authorization", c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Заголовки не логируются: в Authorization лежит токен.
	c.log.Info("запрос к API",
		"method", method, "path", path, "query", q.Encode(), "payload", jsonlog.Raw(encoded))

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("чтение ответа %s %s: %w", method, path, err)
	}

	// До проверки статуса: тело отказа в логе полезнее свёрнутого *APIError.
	c.log.Info("ответ API",
		"method", method, "path", path, "status", resp.StatusCode, "payload", jsonlog.Raw(raw))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(resp.StatusCode, raw)
	}
```

Остаток `do` не меняется.

- [x] **Step 4: Убедиться, что тесты проходят**

Run: `go test ./internal/maxapi/ -v`
Expected: PASS — четыре новых теста и все существующие.

- [x] **Step 5: Коммит**

```bash
git add internal/maxapi/
git commit -m "Писать в лог запросы к Max API и ответы на них"
```

---

### Task 4: Маскирование секрета подписки

Тело `POST /subscriptions` содержит поле `secret` — по смыслу пароль, и после задачи 3 оно уходит в лог как есть. Маскирование применяется и к запросу, и к ответу: чужой контракт менять не нам, и если `secret` однажды появится в ответе `GET /subscriptions`, защита уже на месте.

**Files:**
- Modify: `internal/maxapi/client.go` (функция `redact` рядом с `newAPIError`, вызовы в двух записях лога внутри `do`)
- Test: `internal/maxapi/client_test.go`

**Interfaces:**
- Consumes: записи лога из задачи 3
- Produces: `func redact(raw []byte) []byte` — неэкспортируемая, вызывается только из `do`

- [x] **Step 1: Написать падающие тесты**

Добавить в конец `internal/maxapi/client_test.go`:

```go
func TestSubscriptionSecretIsRedactedInLog(t *testing.T) {
	client, _, logged := newLoggingStub(t, http.StatusOK, `{"success":true}`)

	err := client.Subscribe(context.Background(),
		"https://example.test/webhook", "topsecret", []string{UpdateMessageCreated})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	req := findRecord(t, logged, "запрос к API")
	payload, ok := req["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want объект", req["payload"])
	}
	if payload["secret"] != "***" {
		t.Errorf(`payload["secret"] = %#v, want "***"`, payload["secret"])
	}
	// Соседние поля маскирование трогать не должно.
	if payload["url"] != "https://example.test/webhook" {
		t.Errorf(`payload["url"] = %#v, want адрес webhook`, payload["url"])
	}
	if strings.Contains(logged.String(), "topsecret") {
		t.Errorf("секрет попал в лог: %s", logged.String())
	}
}

// Тело без secret не должно меняться: маскирование не пересобирает то, чего
// не трогает.
func TestBodyWithoutSecretIsLoggedUnchanged(t *testing.T) {
	client, _, logged := newLoggingStub(t, http.StatusOK, `{"message":{}}`)

	_, err := client.SendMessage(context.Background(),
		Target{ChatID: 7}, NewMessageBody{Text: "привет"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	req := findRecord(t, logged, "запрос к API")
	payload, ok := req["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want объект", req["payload"])
	}
	if payload["text"] != "привет" {
		t.Errorf(`payload["text"] = %#v, want "привет"`, payload["text"])
	}
	if _, ok := payload["secret"]; ok {
		t.Error("в payload появилось поле secret, которого не было в теле")
	}
}
```

- [x] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/maxapi/ -run 'Redacted|Unchanged' -v`
Expected: FAIL — `TestSubscriptionSecretIsRedactedInLog`: `payload["secret"] = "topsecret", want "***"`. `TestBodyWithoutSecretIsLoggedUnchanged` при этом проходит — он закрепляет, что маскирование не портит обычные тела.

- [x] **Step 3: Написать реализацию**

Добавить в `internal/maxapi/client.go` рядом с `newAPIError`:

```go
// redactedFields — поля тела, которым в логе не место. secret уходит в
// POST /subscriptions и по смыслу равен паролю.
var redactedFields = []string{"secret"}

// redact подменяет значения чувствительных полей на "***". Тело, которое не
// разбирается как JSON-объект, возвращается как есть: секретов в нём быть не
// может, а потерять его хуже, чем напечатать.
func redact(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw
	}

	found := false
	for _, name := range redactedFields {
		if _, ok := fields[name]; ok {
			fields[name] = json.RawMessage(`"***"`)
			found = true
		}
	}
	if !found {
		return raw
	}

	out, err := json.Marshal(fields)
	if err != nil {
		// Собрать обратно только что разобранное нечему помешать, но если
		// вдруг — лучше потерять запись, чем напечатать секрет.
		return []byte(`{"redacted":true}`)
	}
	return out
}
```

Обернуть оба тела в записях лога внутри `do`:

```go
	c.log.Info("запрос к API",
		"method", method, "path", path, "query", q.Encode(), "payload", jsonlog.Raw(redact(encoded)))
```

```go
	c.log.Info("ответ API",
		"method", method, "path", path, "status", resp.StatusCode, "payload", jsonlog.Raw(redact(raw)))
```

- [x] **Step 4: Убедиться, что тесты проходят**

Run: `go test ./internal/maxapi/ -v`
Expected: PASS — оба новых теста и все существующие.

- [x] **Step 5: Проверить весь проект**

Run: `go test ./... && go test -race ./... && go vet ./...`
Expected: PASS без замечаний.

- [x] **Step 6: Коммит**

```bash
git add internal/maxapi/
git commit -m "Маскировать секрет подписки в логе"
```

---

### Task 5: Включить JSON-лог в боте и описать его

Пока логгер клиента молчит по умолчанию, а `main.go` создаёт `TextHandler` — вся конструкция из задач 1–4 в работающем боте не видна. Эта задача её включает и документирует.

**Files:**
- Modify: `cmd/bot/main.go:29` (обработчик лога), `cmd/bot/main.go:46` (вызов), `cmd/bot/main.go:87-95` (`newAPIClient`)
- Modify: `README.md` (новый раздел после «Запуск против max-mock», строка в «Известные ограничения»)

**Interfaces:**
- Consumes: `maxapi.WithLogger` из задачи 3
- Produces: `func newAPIClient(cfg config, log *slog.Logger) (*maxapi.Client, error)` — сигнатура меняется, добавляется параметр

- [x] **Step 1: Переключить обработчик лога на JSON**

В `cmd/bot/main.go:29`:

```go
func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil {
		log.Error("бот остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}
```

- [x] **Step 2: Передать логгер клиенту API**

В `cmd/bot/main.go:46` — вызов:

```go
	api, err := newAPIClient(cfg, log)
```

В `cmd/bot/main.go:87-95` — сама функция:

```go
// newAPIClient создаёт клиента Max API. Если задан MAX_API_CA_FILE, клиент
// дополнительно доверяет корневому сертификату из этого файла: *.max.ru
// подписан цепочкой Минцифры, которой нет в системном наборе macOS.
func newAPIClient(cfg config, log *slog.Logger) (*maxapi.Client, error) {
	if cfg.CAFile == "" {
		return maxapi.New(cfg.Token,
			maxapi.WithBaseURL(cfg.BaseURL), maxapi.WithLogger(log)), nil
	}
	return maxapi.NewWithRootCA(cfg.Token, cfg.CAFile,
		maxapi.WithBaseURL(cfg.BaseURL), maxapi.WithLogger(log))
}
```

- [x] **Step 3: Убедиться, что всё собирается и тесты зелёные**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS. `newAPIClient` вызывается только из `run` (`cmd/bot/main.go:46`) — других мест нет, тесты `cmd/bot` его не трогают.

- [x] **Step 4: Проверить на живом моке**

Мок и бот — в двух терминалах:

```bash
cd ../maxmoc && ./max-mock          # мок слушает :8080
```

```bash
set -a && . ./.env && set +a
go run ./cmd/bot 2>&1 | tee /tmp/bot.log
```

Отправить боту сообщение через control-API мока — тем же путём, которым ходит веб-чат:

Обе ручки отдают голый массив, бот выбирается по токену из `.env`:

```bash
BOT=$(curl -s localhost:8080/mock/api/bots |
      jq -r --arg t "$MAX_BOT_TOKEN" '.[] | select(.token == $t) | .id')
CHAT=$(curl -s localhost:8080/mock/api/bots/$BOT/clients | jq -r '.[0].chat_id')
curl -s -X POST localhost:8080/mock/api/dialogs/$CHAT/actions \
     -H 'Content-Type: application/json' \
     -d '{"action":"send","text":"привет"}'
```

Проверить лог:

```bash
jq -r 'select(.msg == "получено событие") | .payload.message.body.text' /tmp/bot.log
jq  'select(.msg == "запрос к API") | {path, payload}' /tmp/bot.log
grep -c "$WEBHOOK_SECRET" /tmp/bot.log      # ожидается 0
```

Ожидается: каждая строка `/tmp/bot.log` разбирается `jq`; в первой команде — `привет`; во второй — `{"path":"/messages","payload":{"text":"Вы написали: привет"}}`; `grep -c` печатает `0`, а в записи `запрос к API` для `/subscriptions` поле `secret` равно `"***"`.

Если бот в моке ещё не заведён — зарегистрировать его в админке `http://localhost:8080/mock`, положить выданный токен в `.env` и повторить.

- [x] **Step 5: Описать логи в README**

Вставить раздел после «Запуск против max-mock» (перед `## Переменные окружения`, `README.md:153`):

````markdown
## Логи

Лог идёт в `stderr` в формате JSON — по объекту на строку. Содержимое переписки
попадает в поле `payload` вложенным объектом, а не строкой с экранированными
кавычками, поэтому запись разбирается одним проходом `jq`.

На одно событие приходится до четырёх записей:

| Запись | Откуда | Что в `payload` |
|---|---|---|
| `получено событие` | `internal/webhook` | тело webhook-запроса ровно таким, каким его прислал MAX — со всеми полями, которых `maxapi.Update` не знает |
| `запрос к API` | `internal/maxapi` | тело `POST /messages` или `POST /answers`; у запросов без тела — `null` |
| `ответ API` | `internal/maxapi` | тело ответа Max, включая тело отказа при `4xx` |
| `ответ отправлен` | `internal/bot` | ничего: только адресат и исход |

Прочитать, что писали боту:

```sh
go run ./cmd/bot 2>&1 | jq -r 'select(.msg == "получено событие") | .payload.message.body.text'
```

Посмотреть, что ушло в Max:

```sh
go run ./cmd/bot 2>&1 | jq 'select(.msg == "запрос к API") | {path, payload}'
```

Токен в лог не попадает: он уходит заголовком `Authorization`, а заголовки не
логируются вовсе. Секрет подписки в теле `POST /subscriptions` заменяется на
`"***"`.

Размер тела события ограничен 1 МиБ (`webhook.maxBodyBytes`) — тело читается в
память целиком, чтобы попасть в лог неизменным.
````

- [x] **Step 6: Дописать ограничение**

Добавить в список «Известные ограничения» (`README.md:189`) последним пунктом:

```markdown
- **В логе персональные данные.** Телефон и координаты из вложений пишутся в
  `payload` как есть — ради этого лог и заведён. Для продакшена он в таком виде
  не годится: там нужны маскирование, уровни и ротация.
```

- [x] **Step 7: Финальная проверка**

Run: `go build ./... && go vet ./... && go test ./... && go test -race ./...`
Expected: PASS без замечаний.

- [x] **Step 8: Коммит**

```bash
git add cmd/bot/main.go README.md
git commit -m "Включить JSON-лог в боте и описать его"
```
