package maxclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"stash.sigma.sbrf.ru/scpl/oapi/maxapi"
)

type capture struct {
	method string
	path   string
	query  string
	auth   string
	body   string
}

func newServer(t *testing.T, status int, response any) (*Client, *capture) {
	t.Helper()
	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("чтение тела: %v", err)
		}
		*got = capture{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), string(payload)}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("запись ответа: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	return New("test-token", WithBaseURL(srv.URL)), got
}

func TestТокенУходитВЗаголовкеАНеВQuery(t *testing.T) {
	client, got := newServer(t, http.StatusOK, maxapi.GetSubscriptionsResult{})

	if _, err := client.GetSubscriptions(context.Background()); err != nil {
		t.Fatalf("GetSubscriptions: %v", err)
	}
	if got.auth != "test-token" {
		t.Errorf("Authorization = %q", got.auth)
	}
	if got.query != "" {
		t.Errorf("query = %q, ожидалась пустая", got.query)
	}
	if got.method != http.MethodGet || got.path != "/subscriptions" {
		t.Errorf("запрос = %s %s", got.method, got.path)
	}
}

func TestSendMessageАдресуетВЧатИШлётТелоИзКонтракта(t *testing.T) {
	client, got := newServer(t, http.StatusOK, maxapi.SendMessageResult{
		Message: maxapi.Message{Body: maxapi.MessageBody{Mid: "mid.1", Seq: 117052520019991875}},
	})

	result, err := client.SendMessage(context.Background(), ToChat(442209522), TextMessage("привет"))
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got.method != http.MethodPost || got.path != "/messages" {
		t.Errorf("запрос = %s %s", got.method, got.path)
	}
	if got.query != "chat_id=442209522" {
		t.Errorf("query = %q", got.query)
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
}

func TestОшибкаAPIРазбираетсяВErrorИзКонтракта(t *testing.T) {
	detail := "invalid.token"
	client, _ := newServer(t, http.StatusUnauthorized, maxapi.Error{
		Code: "verify.token", Message: "Неверный токен доступа", Error: &detail,
	})

	_, err := client.SendMessage(context.Background(), ToChat(1), TextMessage("привет"))
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
					{Url: "https://old.example-quicktype.ru/hook"},
					{Url: "https://bot.example-quicktype.ru/hook"},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(maxapi.SimpleQueryResult{Success: true})
	}))
	defer srv.Close()

	client := New("test-token", WithBaseURL(srv.URL))
	removed, err := client.EnsureSubscription(context.Background(), maxapi.SubscriptionRequestBody{
		Url: "https://bot.example-quicktype.ru/hook",
	})
	if err != nil {
		t.Fatalf("EnsureSubscription: %v", err)
	}
	if len(removed) != 1 || removed[0] != "https://old.example-quicktype.ru/hook" {
		t.Errorf("снято = %v", removed)
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
