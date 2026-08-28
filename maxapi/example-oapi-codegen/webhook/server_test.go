package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	maxapi "maxapi-oapi-codegen"
)

// Те же тела, что в ../../example-quicktype/webhook/server_test.go: сравнивать два
// варианта кодогенерации имеет смысл только на одинаковых входах.
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

const liveMessageCallback = `{
  "update_type": "message_callback",
  "timestamp": 1786081600397,
  "callback": {"callback_id": "cb.1", "payload": "yes", "timestamp": 1786081600397,
               "user": {"user_id": 174756854, "first_name": "Артём", "is_bot": false,
                        "last_activity_time": 1786081600000, "name": "Артём"}},
  "message": {
    "recipient": {"chat_id": 442209522, "chat_type": "dialog", "user_id": 271648516},
    "timestamp": 1786081600397,
    "body": {"mid": "mid.2", "seq": 117052520019991876, "text": "вопрос"}
  }
}`

func post(t *testing.T, s *Server, secret, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if secret != "" {
		req.Header.Set(SecretHeader, secret)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Result()
}

func TestMessageCreatedДоезжаетДоОбработчикаЗначением(t *testing.T) {
	var got maxapi.MessageCreatedUpdate
	called := 0
	srv := New("s3cret", Handler{
		MessageCreated: func(_ context.Context, u maxapi.MessageCreatedUpdate) error {
			got, called = u, called+1
			return nil
		},
	}, nil)

	if resp := post(t, srv, "s3cret", liveMessageCreated); resp.StatusCode != http.StatusOK {
		t.Fatalf("статус = %d, ожидался 200", resp.StatusCode)
	}
	if called != 1 {
		t.Fatalf("обработчик вызван %d раз, ожидался 1", called)
	}
	if got.Message.Body.Mid != "mid.000000001a5b94f2019fdaa593db1d43" {
		t.Errorf("mid = %q", got.Message.Body.Mid)
	}
	if got.Message.Body.Seq != 117052520019991875 {
		t.Errorf("seq = %d", got.Message.Body.Seq)
	}
	if got.Message.Recipient.ChatId == nil || *got.Message.Recipient.ChatId != 442209522 {
		t.Errorf("chat_id = %v", got.Message.Recipient.ChatId)
	}
}

func TestMessageCallbackПопадаетВСвойОбработчик(t *testing.T) {
	var got maxapi.MessageCallbackUpdate
	srv := New("s3cret", Handler{
		MessageCreated: func(context.Context, maxapi.MessageCreatedUpdate) error {
			t.Error("message_callback ушёл в обработчик message_created")
			return nil
		},
		MessageCallback: func(_ context.Context, u maxapi.MessageCallbackUpdate) error {
			got = u
			return nil
		},
	}, nil)

	if resp := post(t, srv, "s3cret", liveMessageCallback); resp.StatusCode != http.StatusOK {
		t.Fatalf("статус = %d", resp.StatusCode)
	}
	if got.Callback.CallbackId != "cb.1" {
		t.Errorf("callback_id = %q", got.Callback.CallbackId)
	}
	if got.Callback.Payload == nil || *got.Callback.Payload != "yes" {
		t.Errorf("payload = %v", got.Callback.Payload)
	}
}

func TestПрочиеСобытияУходятВOther(t *testing.T) {
	var gotType string
	var gotValue any
	srv := New("", Handler{
		Other: func(_ context.Context, updateType string, value any) error {
			gotType, gotValue = updateType, value
			return nil
		},
	}, nil)

	body := `{"update_type":"bot_started","timestamp":1786079712219,"chat_id":442209522,
	          "user":{"user_id":174756854,"first_name":"Артём","is_bot":false,
	                  "last_activity_time":1786079712000,"name":"Артём"}}`
	if resp := post(t, srv, "", body); resp.StatusCode != http.StatusOK {
		t.Fatalf("статус = %d", resp.StatusCode)
	}
	if gotType != "bot_started" {
		t.Errorf("update_type = %q", gotType)
	}
	// Вариант приезжает уже разобранным в конкретный тип — это и есть
	// разница с вариантом quicktype, где в Other приходит плоский Update.
	started, ok := gotValue.(maxapi.BotStartedUpdate)
	if !ok {
		t.Fatalf("тип %T, ожидался maxapi.BotStartedUpdate", gotValue)
	}
	if started.ChatId != 442209522 {
		t.Errorf("chat_id = %d", started.ChatId)
	}
}

func TestНеверныйСекретОтклоняется(t *testing.T) {
	srv := New("s3cret", Handler{
		MessageCreated: func(context.Context, maxapi.MessageCreatedUpdate) error {
			t.Error("обработчик вызван при неверном секрете")
			return nil
		},
	}, nil)

	for _, tc := range []struct{ name, secret string }{
		{"чужой секрет", "wrong"},
		{"без заголовка", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if resp := post(t, srv, tc.secret, liveMessageCreated); resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("статус = %d, ожидался 401", resp.StatusCode)
			}
		})
	}
}

func TestТолькоPOST(t *testing.T) {
	srv := New("", Handler{}, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("статус = %d, ожидался 405", rec.Code)
	}
}

func TestБитоеТелоПодтверждаетсяИНеПовторяется(t *testing.T) {
	srv := New("", Handler{
		MessageCreated: func(context.Context, maxapi.MessageCreatedUpdate) error {
			t.Error("обработчик вызван на битом теле")
			return nil
		},
	}, nil)
	if resp := post(t, srv, "", `{"update_type": оно не json}`); resp.StatusCode != http.StatusOK {
		t.Errorf("статус = %d, ожидался 200", resp.StatusCode)
	}
}

func TestНеизвестныйТипСобытияНеРоняетСервер(t *testing.T) {
	// ValueByDiscriminator вернёт ошибку на update_type вне контракта —
	// это тоже «битое тело», повторять доставку незачем.
	srv := New("", Handler{
		Other: func(context.Context, string, any) error {
			t.Error("Other вызван на неизвестном типе")
			return nil
		},
	}, nil)
	body := `{"update_type":"невиданное_событие","timestamp":1}`
	if resp := post(t, srv, "", body); resp.StatusCode != http.StatusOK {
		t.Errorf("статус = %d, ожидался 200", resp.StatusCode)
	}
}

func TestОшибкаОбработчикаДаётПятисотку(t *testing.T) {
	srv := New("", Handler{
		MessageCreated: func(context.Context, maxapi.MessageCreatedUpdate) error {
			return errors.New("база недоступна")
		},
	}, nil)
	if resp := post(t, srv, "", liveMessageCreated); resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("статус = %d, ожидался 500", resp.StatusCode)
	}
}

func TestСобытиеБезОбработчикаНеРоняетСервер(t *testing.T) {
	srv := New("", Handler{}, nil)
	if resp := post(t, srv, "", liveMessageCreated); resp.StatusCode != http.StatusOK {
		t.Errorf("статус = %d, ожидался 200", resp.StatusCode)
	}
}
