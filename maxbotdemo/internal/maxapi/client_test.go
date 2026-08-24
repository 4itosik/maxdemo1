package maxapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// capturedRequest хранит то, что заглушка Max API получила от клиента.
type capturedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Auth   string
	Body   []byte
}

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

func assertJSONBody(t *testing.T, body []byte, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("тело запроса не JSON: %v (%s)", err, body)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("тело запроса = %#v, want %#v", got, want)
	}
}

func TestSendMessageToChat(t *testing.T) {
	client, got := newStub(t, `{"message":{"body":{"mid":"mid.7","text":"привет"}}}`)

	msg, err := client.SendMessage(context.Background(),
		Target{ChatID: 777, UserID: 42},
		NewMessageBody{Text: "привет"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if got.Method != http.MethodPost || got.Path != "/messages" {
		t.Errorf("запрос = %s %s, want POST /messages", got.Method, got.Path)
	}
	if got.Auth != "test-token" {
		t.Errorf("Authorization = %q, want %q", got.Auth, "test-token")
	}
	if q := got.Query.Get("chat_id"); q != "777" {
		t.Errorf("chat_id = %q, want %q", q, "777")
	}
	if got.Query.Has("user_id") {
		t.Error("в query не должно быть user_id, когда задан chat_id")
	}
	assertJSONBody(t, got.Body, map[string]any{
		"text": "привет", "attachments": nil, "link": nil,
	})

	if msg.Body.MID != "mid.7" {
		t.Errorf("MID = %q, want %q", msg.Body.MID, "mid.7")
	}
}

// Контракт объявляет text, attachments и link обязательными полями
// NewMessageBody. Живой Max принимает тело и без них, но эмулятор max-mock
// проверяет запросы по контракту — поэтому пустые поля уходят как null.
func TestSendMessageSendsContractRequiredFields(t *testing.T) {
	client, got := newStub(t, `{"message":{}}`)

	_, err := client.SendMessage(context.Background(),
		Target{ChatID: 7}, NewMessageBody{Text: "привет"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	assertJSONBody(t, got.Body, map[string]any{
		"text":        "привет",
		"attachments": nil,
		"link":        nil,
	})
}

func TestSendMessageToUserWhenChatUnknown(t *testing.T) {
	client, got := newStub(t, `{"message":{}}`)

	_, err := client.SendMessage(context.Background(),
		Target{UserID: 42},
		NewMessageBody{Text: "привет"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if q := got.Query.Get("user_id"); q != "42" {
		t.Errorf("user_id = %q, want %q", q, "42")
	}
	if got.Query.Has("chat_id") {
		t.Error("в query не должно быть chat_id, когда он неизвестен")
	}
}

func TestSendMessageWithoutTargetDoesNotCallAPI(t *testing.T) {
	client, got := newStub(t, `{"message":{}}`)

	_, err := client.SendMessage(context.Background(), Target{}, NewMessageBody{Text: "привет"})
	if err == nil {
		t.Fatal("SendMessage без адресата вернул nil, want ошибку")
	}
	if got.Method != "" {
		t.Errorf("запрос к API не должен уходить, получен %s %s", got.Method, got.Path)
	}
}

func TestSendMessageEncodesKeyboardAndFormat(t *testing.T) {
	client, got := newStub(t, `{"message":{}}`)

	_, err := client.SendMessage(context.Background(), Target{ChatID: 777}, NewMessageBody{
		Text:   "выберите",
		Format: FormatMarkdown,
		Attachments: []AttachmentRequest{{
			Type: AttachmentInlineKeyboard,
			Payload: &KeyboardPayload{Buttons: [][]Button{{
				{Type: ButtonCallback, Text: "Да", Payload: "yes"},
			}}},
		}},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	assertJSONBody(t, got.Body, map[string]any{
		"text":   "выберите",
		"format": "markdown",
		"link":   nil,
		"attachments": []any{map[string]any{
			"type": "inline_keyboard",
			"payload": map[string]any{
				"buttons": []any{[]any{map[string]any{
					"type": "callback", "text": "Да", "payload": "yes",
				}}},
			},
		}},
	})
}

func TestAnswerOnCallback(t *testing.T) {
	client, got := newStub(t, `{"success":true}`)

	err := client.AnswerOnCallback(context.Background(), "cb-1", CallbackAnswer{
		Message: &NewMessageBody{Text: "принято"},
	})
	if err != nil {
		t.Fatalf("AnswerOnCallback: %v", err)
	}

	if got.Method != http.MethodPost || got.Path != "/answers" {
		t.Errorf("запрос = %s %s, want POST /answers", got.Method, got.Path)
	}
	if q := got.Query.Get("callback_id"); q != "cb-1" {
		t.Errorf("callback_id = %q, want %q", q, "cb-1")
	}
	assertJSONBody(t, got.Body, map[string]any{
		"message": map[string]any{"text": "принято", "attachments": nil, "link": nil},
	})
}

func TestAnswerOnCallbackWithoutIDDoesNotCallAPI(t *testing.T) {
	client, got := newStub(t, `{"success":true}`)

	err := client.AnswerOnCallback(context.Background(), "", CallbackAnswer{})
	if err == nil {
		t.Fatal("AnswerOnCallback с пустым id вернул nil, want ошибку")
	}
	if got.Method != "" {
		t.Errorf("запрос к API не должен уходить, получен %s %s", got.Method, got.Path)
	}
}

func TestGetMyInfo(t *testing.T) {
	client, got := newStub(t, `{"user_id":1,"first_name":"Демобот","username":"demo_bot","is_bot":true}`)

	info, err := client.GetMyInfo(context.Background())
	if err != nil {
		t.Fatalf("GetMyInfo: %v", err)
	}

	if got.Method != http.MethodGet || got.Path != "/me" {
		t.Errorf("запрос = %s %s, want GET /me", got.Method, got.Path)
	}
	if info.Username != "demo_bot" || !info.IsBot {
		t.Errorf("BotInfo = %+v, want username=demo_bot is_bot=true", info)
	}
}

func TestSetCommands(t *testing.T) {
	client, got := newStub(t, `{"commands":[{"name":"help"}]}`)

	err := client.SetCommands(context.Background(), []BotCommand{
		{Name: "help", Description: "Справка"},
	})
	if err != nil {
		t.Fatalf("SetCommands: %v", err)
	}

	if got.Method != http.MethodPatch || got.Path != "/me/commands" {
		t.Errorf("запрос = %s %s, want PATCH /me/commands", got.Method, got.Path)
	}
	assertJSONBody(t, got.Body, map[string]any{
		"commands": []any{map[string]any{"name": "help", "description": "Справка"}},
	})
}

func TestSubscribe(t *testing.T) {
	client, got := newStub(t, `{"success":true}`)

	err := client.Subscribe(context.Background(),
		"https://example.test/webhook", "topsecret",
		[]string{UpdateMessageCreated, UpdateBotStarted})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if got.Method != http.MethodPost || got.Path != "/subscriptions" {
		t.Errorf("запрос = %s %s, want POST /subscriptions", got.Method, got.Path)
	}
	assertJSONBody(t, got.Body, map[string]any{
		"url":          "https://example.test/webhook",
		"secret":       "topsecret",
		"update_types": []any{"message_created", "bot_started"},
	})
}

// Подписка на петлевой http-адрес уходит на сервер, а не отвергается до
// запроса: по нему живёт max-mock.
func TestSubscribeAllowsLoopbackHTTP(t *testing.T) {
	client, got := newStub(t, `{"success":true}`)

	err := client.Subscribe(context.Background(),
		"http://localhost:8081/webhook", "", []string{UpdateMessageCreated})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if got.Path != "/subscriptions" {
		t.Errorf("путь запроса = %q, want /subscriptions", got.Path)
	}
}

func TestSubscribeRejectsExternalHTTP(t *testing.T) {
	client, got := newStub(t, `{"success":true}`)

	err := client.Subscribe(context.Background(),
		"http://example.test/webhook", "", []string{UpdateMessageCreated})
	if err == nil {
		t.Fatal("Subscribe вернул nil, want ошибку")
	}
	if got.Method != "" {
		t.Errorf("на сервер ушёл запрос %s %s, want ни одного", got.Method, got.Path)
	}
}

func TestGetSubscriptions(t *testing.T) {
	client, got := newStub(t, `{"subscriptions":[
		{"url":"https://old.test/webhook","time":1,"update_types":["message_created"]},
		{"url":"https://new.test/webhook","time":2,"update_types":["bot_started"]}
	]}`)

	subs, err := client.GetSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("GetSubscriptions: %v", err)
	}

	if got.Method != http.MethodGet || got.Path != "/subscriptions" {
		t.Errorf("запрос = %s %s, want GET /subscriptions", got.Method, got.Path)
	}
	if len(subs) != 2 {
		t.Fatalf("подписок = %d, want 2", len(subs))
	}
	if subs[0].URL != "https://old.test/webhook" || subs[1].URL != "https://new.test/webhook" {
		t.Errorf("подписки = %+v", subs)
	}
}

func TestUnsubscribe(t *testing.T) {
	client, got := newStub(t, `{"success":true}`)

	err := client.Unsubscribe(context.Background(), "https://example.test/webhook")
	if err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	if got.Method != http.MethodDelete || got.Path != "/subscriptions" {
		t.Errorf("запрос = %s %s, want DELETE /subscriptions", got.Method, got.Path)
	}
	if q := got.Query.Get("url"); q != "https://example.test/webhook" {
		t.Errorf("url = %q, want %q", q, "https://example.test/webhook")
	}
}

func TestAPIErrorCarriesStatusAndCode(t *testing.T) {
	client, _ := newStubWithStatus(t, http.StatusBadRequest,
		`{"code":"attachment.not.ready","message":"Вложение ещё не готово"}`)

	_, err := client.SendMessage(context.Background(), Target{ChatID: 777}, NewMessageBody{Text: "x"})
	if err == nil {
		t.Fatal("SendMessage вернул nil, want ошибку")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("ошибка %v (%T), want *APIError", err, err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", apiErr.Status, http.StatusBadRequest)
	}
	if apiErr.Code != "attachment.not.ready" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "attachment.not.ready")
	}
	if apiErr.Message != "Вложение ещё не готово" {
		t.Errorf("Message = %q, want %q", apiErr.Message, "Вложение ещё не готово")
	}
}

func TestAPIErrorOnNonJSONBody(t *testing.T) {
	client, _ := newStubWithStatus(t, http.StatusUnauthorized, `Unauthorized`)

	_, err := client.GetMyInfo(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("ошибка %v (%T), want *APIError", err, err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", apiErr.Status, http.StatusUnauthorized)
	}
	if apiErr.Message == "" {
		t.Error("Message пустое, want текст тела ответа")
	}
}

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
