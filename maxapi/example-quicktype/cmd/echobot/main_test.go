package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	maxapi "maxapi-quicktype"
	"example-quicktype/maxclient"
	"example-quicktype/webhook"
)

const liveMessageCreated = `{
  "update_type": "message_created",
  "timestamp": 1786079712219,
  "user_locale": "ru",
  "message": {
    "recipient": {"chat_id": 442209522, "chat_type": "dialog", "user_id": 271648516},
    "timestamp": 1786079712219,
    "body": {"mid": "mid.000000001a5b94f2019fdaa593db1d43", "seq": 117052520019991875, "text": "привет"},
    "sender": {"user_id": 174756854, "first_name": "Артём", "is_bot": false,
               "last_activity_time": 1786079712000, "name": "Артём"}
  }
}`

// TestСквознойПроходСобытиеОтвет собирает обе половины примера вместе:
// webhook-сервер принимает событие, обработчик отвечает через HTTP-клиент.
// Роль Max Bot API играет httptest.Server — в сеть тест не ходит.
func TestСквознойПроходСобытиеОтвет(t *testing.T) {
	type sent struct {
		query string
		body  maxapi.NewMessageBody
	}
	outbound := make(chan sent, 1)

	maxAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("неожиданный путь %s", r.URL.Path)
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("чтение тела: %v", err)
		}
		var body maxapi.NewMessageBody
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Errorf("тело не разобралось как NewMessageBody: %v", err)
		}
		outbound <- sent{query: r.URL.RawQuery, body: body}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(maxapi.SendMessageResult{
			Message: maxapi.Message{Body: maxapi.MessageBody{Mid: "mid.reply", Seq: 117052520019991876}},
		})
	}))
	defer maxAPI.Close()

	client := maxclient.New("test-token", maxclient.WithBaseURL(maxAPI.URL))
	log := slog.New(slog.DiscardHandler)
	server := webhook.New("s3cret", webhook.Handler{MessageCreated: echoMessage(client, log)}, log)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(liveMessageCreated))
	req.Header.Set(webhook.SecretHeader, "s3cret")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("webhook ответил %d, ожидался 200", rec.Code)
	}

	select {
	case got := <-outbound:
		if got.query != "chat_id=442209522" {
			t.Errorf("адресат = %q, ожидался chat_id из события", got.query)
		}
		if got.body.Text == nil || *got.body.Text != "Вы написали: привет" {
			t.Errorf("текст ответа = %v", got.body.Text)
		}
	default:
		t.Fatal("клиент не отправил ответ в Max Bot API")
	}
}

// TestСообщениеБезТекстаНеВызываетОтвет — вложение без подписи не должно
// порождать пустой ответ.
func TestСообщениеБезТекстаНеВызываетОтвет(t *testing.T) {
	maxAPI := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("клиент сходил в API, хотя текста в сообщении не было")
	}))
	defer maxAPI.Close()

	client := maxclient.New("test-token", maxclient.WithBaseURL(maxAPI.URL))
	log := slog.New(slog.DiscardHandler)
	server := webhook.New("", webhook.Handler{MessageCreated: echoMessage(client, log)}, log)

	body := `{"update_type":"message_created","timestamp":1,"message":{
		"recipient":{"chat_id":1,"chat_type":"dialog","user_id":2},
		"timestamp":1,"body":{"mid":"mid.1","seq":1,"text":null}}}`
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Errorf("статус = %d, ожидался 200", rec.Code)
	}
}
