package maxclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	maxapi "maxapi-quicktype"
)

// capture — то, что сервер увидел в последнем запросе.
type capture struct {
	method string
	path   string
	query  string
	auth   string
	body   string
}

// newServer поднимает поддельный Max Bot API, отдающий заданный ответ, и
// возвращает клиента к нему плюс указатель на запись о запросе.
func newServer(t *testing.T, status int, response any) (*Client, *capture) {
	t.Helper()
	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("чтение тела запроса: %v", err)
		}
		*got = capture{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			auth:   r.Header.Get("Authorization"),
			body:   string(payload),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("запись ответа: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return New("test-token", WithBaseURL(srv.URL)), got
}

func TestGetSubscriptionsРазбираетОтвет(t *testing.T) {
	client, got := newServer(t, http.StatusOK, maxapi.GetSubscriptionsResult{
		Subscriptions: []maxapi.Subscription{
			{URL: "https://bot.example-quicktype.ru/hook", Time: 1786079712219, UpdateTypes: []string{"message_created"}},
		},
	})

	result, err := client.GetSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("GetSubscriptions: %v", err)
	}

	if got.method != http.MethodGet || got.path != "/subscriptions" {
		t.Errorf("запрос = %s %s, ожидался GET /subscriptions", got.method, got.path)
	}
	if got.auth != "test-token" {
		t.Errorf("Authorization = %q, ожидался токен", got.auth)
	}
	if got.query != "" {
		t.Errorf("query = %q, ожидалась пустая: токен не должен уходить в URL", got.query)
	}
	if len(result.Subscriptions) != 1 || result.Subscriptions[0].URL != "https://bot.example-quicktype.ru/hook" {
		t.Errorf("подписки = %+v", result.Subscriptions)
	}
}

func TestSubscribeОтправляетТелоИзКонтракта(t *testing.T) {
	client, got := newServer(t, http.StatusOK, maxapi.SimpleQueryResult{Success: true})

	secret := "s3cret-value"
	result, err := client.Subscribe(context.Background(), maxapi.SubscriptionRequestBody{
		URL:         "https://bot.example-quicktype.ru/hook",
		UpdateTypes: []string{"message_created", "message_callback"},
		Secret:      &secret,
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if !result.Success {
		t.Error("success = false, ожидался true")
	}
	if got.method != http.MethodPost || got.path != "/subscriptions" {
		t.Errorf("запрос = %s %s, ожидался POST /subscriptions", got.method, got.path)
	}

	var sent maxapi.SubscriptionRequestBody
	if err := json.Unmarshal([]byte(got.body), &sent); err != nil {
		t.Fatalf("тело запроса не разобралось как SubscriptionRequestBody: %v (%s)", err, got.body)
	}
	if sent.URL != "https://bot.example-quicktype.ru/hook" {
		t.Errorf("url = %q", sent.URL)
	}
	if sent.Secret == nil || *sent.Secret != secret {
		t.Errorf("secret = %v", sent.Secret)
	}
	if len(sent.UpdateTypes) != 2 {
		t.Errorf("update_types = %v", sent.UpdateTypes)
	}
}

func TestUnsubscribeКладётURLвQuery(t *testing.T) {
	client, got := newServer(t, http.StatusOK, maxapi.SimpleQueryResult{Success: true})

	if _, err := client.Unsubscribe(context.Background(), "https://old.example-quicktype.ru/hook"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if got.method != http.MethodDelete {
		t.Errorf("метод = %s, ожидался DELETE", got.method)
	}
	if got.query != "url=https%3A%2F%2Fold.example-quicktype.ru%2Fhook" {
		t.Errorf("query = %q", got.query)
	}
}

func TestSendMessageАдресуетПоUserIDиChatID(t *testing.T) {
	for _, tc := range []struct {
		name      string
		target    Target
		wantQuery string
	}{
		{"пользователю", ToUser(174756854), "user_id=174756854"},
		{"в чат", ToChat(442209522), "chat_id=442209522"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, got := newServer(t, http.StatusOK, maxapi.SendMessageResult{
				Message: maxapi.Message{Body: maxapi.MessageBody{Mid: "mid.1", Seq: 117052520019991875}},
			})

			result, err := client.SendMessage(context.Background(), tc.target, TextMessage("привет"))
			if err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			if got.method != http.MethodPost || got.path != "/messages" {
				t.Errorf("запрос = %s %s, ожидался POST /messages", got.method, got.path)
			}
			if got.query != tc.wantQuery {
				t.Errorf("query = %q, ожидался %q", got.query, tc.wantQuery)
			}

			var sent maxapi.NewMessageBody
			if err := json.Unmarshal([]byte(got.body), &sent); err != nil {
				t.Fatalf("тело не разобралось как NewMessageBody: %v (%s)", err, got.body)
			}
			if sent.Text == nil || *sent.Text != "привет" {
				t.Errorf("text = %v", sent.Text)
			}
			if result.Message.Body.Seq != 117052520019991875 {
				t.Errorf("seq в ответе = %d", result.Message.Body.Seq)
			}
		})
	}
}

func TestSendMessageБезАдресатаНеХодитВСеть(t *testing.T) {
	client, got := newServer(t, http.StatusOK, maxapi.SendMessageResult{})

	if _, err := client.SendMessage(context.Background(), Target{}, TextMessage("привет")); err == nil {
		t.Fatal("ожидалась ошибка при пустом адресате")
	}
	if got.method != "" {
		t.Errorf("запрос ушёл на сервер (%s %s), а не должен был", got.method, got.path)
	}
}

func TestОшибкаAPIРазбираетсяВErrorИзКонтракта(t *testing.T) {
	detail := "invalid.token"
	client, _ := newServer(t, http.StatusUnauthorized, maxapi.Error{
		Code:    "verify.token",
		Message: "Неверный токен доступа",
		Error:   &detail,
	})

	_, err := client.GetSubscriptions(context.Background())
	if err == nil {
		t.Fatal("ожидалась ошибка на 401")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("ошибка типа %T, ожидался *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("статус = %d", apiErr.StatusCode)
	}
	if apiErr.Body.Code != "verify.token" || apiErr.Body.Message != "Неверный токен доступа" {
		t.Errorf("тело ошибки = %+v", apiErr.Body)
	}
}

func TestEnsureSubscriptionСнимаетЧужиеИСтавитНужную(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(maxapi.GetSubscriptionsResult{
				Subscriptions: []maxapi.Subscription{
					{URL: "https://old.example-quicktype.ru/hook"},
					{URL: "https://bot.example-quicktype.ru/hook"}, // нужный — снимать не надо
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(maxapi.SimpleQueryResult{Success: true})
	}))
	defer srv.Close()

	client := New("test-token", WithBaseURL(srv.URL))
	removed, err := client.EnsureSubscription(context.Background(), maxapi.SubscriptionRequestBody{
		URL: "https://bot.example-quicktype.ru/hook",
	})
	if err != nil {
		t.Fatalf("EnsureSubscription: %v", err)
	}

	if len(removed) != 1 || removed[0] != "https://old.example-quicktype.ru/hook" {
		t.Errorf("снято = %v, ожидался только чужой URL", removed)
	}
	want := []string{
		"GET /subscriptions?",
		"DELETE /subscriptions?url=https%3A%2F%2Fold.example-quicktype.ru%2Fhook",
		"POST /subscriptions?",
	}
	if len(calls) != len(want) {
		t.Fatalf("вызовы = %v, ожидалось %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("вызов %d = %q, ожидался %q", i, calls[i], want[i])
		}
	}
}
