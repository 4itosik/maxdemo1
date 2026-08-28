package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"maxbotdemo/internal/bot"
	"maxbotdemo/internal/maxapi"
)

// fakeAPI — заглушка Max API, которая ведёт список подписок так же, как
// настоящий сервер: подписка опознаётся по URL, повторный POST на тот же адрес
// обновляет набор событий, а на другой — заводит вторую запись.
type fakeAPI struct {
	calls    []string
	subs     []maxapi.Subscription
	failPath string
}

// upsert повторяет дедупликацию Max: ключ подписки — её URL.
func (f *fakeAPI) upsert(sub maxapi.Subscription) {
	for i, s := range f.subs {
		if s.URL == sub.URL {
			f.subs[i] = sub
			return
		}
	}
	f.subs = append(f.subs, sub)
}

func (f *fakeAPI) handler(w http.ResponseWriter, r *http.Request) {
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")

	if r.URL.Path == f.failPath {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"code":"verify.token","message":"неверный токен"}`)
		return
	}

	switch {
	case r.URL.Path == "/me" && r.Method == http.MethodGet:
		_, _ = io.WriteString(w, `{"user_id":1,"first_name":"Демобот","username":"demo_bot","is_bot":true}`)

	case r.URL.Path == "/subscriptions" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(maxapi.GetSubscriptionsResult{Subscriptions: f.subs})

	case r.URL.Path == "/subscriptions" && r.Method == http.MethodPost:
		var body maxapi.SubscriptionRequestBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.upsert(maxapi.Subscription{URL: body.URL, UpdateTypes: body.UpdateTypes})
		_, _ = io.WriteString(w, `{"success":true}`)

	case r.URL.Path == "/subscriptions" && r.Method == http.MethodDelete:
		target := r.URL.Query().Get("url")
		kept := f.subs[:0]
		for _, s := range f.subs {
			if s.URL != target {
				kept = append(kept, s)
			}
		}
		f.subs = kept
		_, _ = io.WriteString(w, `{"success":true}`)

	default:
		_, _ = io.WriteString(w, `{"success":true}`)
	}
}

func newFakeAPI(t *testing.T, existing []maxapi.Subscription, failPath string) (*maxapi.Client, *fakeAPI) {
	t.Helper()

	api := &fakeAPI{subs: existing, failPath: failPath}
	srv := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(srv.Close)

	return maxapi.New("test-token", maxapi.WithBaseURL(srv.URL)), api
}

func testConfig() config {
	return config{
		Token:       "test-token",
		WebhookURL:  "https://example.test" + webhookPathSample,
		WebhookPath: webhookPathSample,
		Secret:      "top-secret_1",
	}
}

// subURLs возвращает адреса подписок в порядке их хранения.
func subURLs(subs []maxapi.Subscription) []string {
	urls := make([]string, 0, len(subs))
	for _, s := range subs {
		urls = append(urls, s.URL)
	}
	return urls
}

func TestRegisterChecksTokenPublishesCommandsAndSubscribes(t *testing.T) {
	client, api := newFakeAPI(t, nil, "")

	info, err := register(context.Background(), client, testConfig())
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	want := "GET /me, PATCH /me/commands, GET /subscriptions, POST /subscriptions"
	if strings.Join(api.calls, ", ") != want {
		t.Errorf("вызовы = %v,\nwant %s", api.calls, want)
	}
	if got := subURLs(api.subs); len(got) != 1 || got[0] != testConfig().WebhookURL {
		t.Errorf("подписки = %v, want только наш адрес", got)
	}
	if info.Username != "demo_bot" {
		t.Errorf("Username = %q, want %q", info.Username, "demo_bot")
	}
}

func TestRegisterRemovesStaleSubscriptions(t *testing.T) {
	existing := []maxapi.Subscription{
		{URL: "https://мёртвый-туннель.test/webhook"},
		{URL: "https://ещё-один-старый.test/webhook"},
	}
	client, api := newFakeAPI(t, existing, "")

	if _, err := register(context.Background(), client, testConfig()); err != nil {
		t.Fatalf("register: %v", err)
	}

	got := subURLs(api.subs)
	if len(got) != 1 || got[0] != testConfig().WebhookURL {
		t.Errorf("подписки = %v, want только %s", got, testConfig().WebhookURL)
	}
}

// reversed возвращает копию списка в обратном порядке: порядок update_types в
// ответе API контрактом не оговорён, и сверка не должна на него полагаться.
func reversed(in []string) []string {
	out := make([]string, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		out = append(out, in[i])
	}
	return out
}

func TestRegisterDoesNotResubscribeWhenUpdateTypesMatch(t *testing.T) {
	existing := []maxapi.Subscription{{
		URL:         testConfig().WebhookURL,
		UpdateTypes: reversed(bot.UpdateTypes()),
	}}
	client, api := newFakeAPI(t, existing, "")

	if _, err := register(context.Background(), client, testConfig()); err != nil {
		t.Fatalf("register: %v", err)
	}

	if strings.Contains(strings.Join(api.calls, ", "), "POST /subscriptions") {
		t.Errorf("вызовы = %v, want без повторной подписки: набор совпадает", api.calls)
	}
	if got := subURLs(api.subs); len(got) != 1 {
		t.Errorf("подписки = %v, want одну", got)
	}
}

// Подписка с нашим адресом, но старым набором событий: без обновления новые
// события не пришли бы ни разу, причём молча.
func TestRegisterResubscribesWhenUpdateTypesDiffer(t *testing.T) {
	existing := []maxapi.Subscription{{
		URL:         testConfig().WebhookURL,
		UpdateTypes: []string{maxapi.UpdateMessageCreated},
	}}
	client, api := newFakeAPI(t, existing, "")

	if _, err := register(context.Background(), client, testConfig()); err != nil {
		t.Fatalf("register: %v", err)
	}

	calls := strings.Join(api.calls, ", ")
	if !strings.Contains(calls, "POST /subscriptions") {
		t.Errorf("вызовы = %v, want POST с новым набором событий", api.calls)
	}
	// DELETE оставил бы окно, в котором события падают в никуда: POST на тот
	// же URL обновляет подписку сам.
	if strings.Contains(calls, "DELETE /subscriptions") {
		t.Errorf("вызовы = %v, want без DELETE своего же адреса", api.calls)
	}
	if len(api.subs) != 1 {
		t.Fatalf("подписки = %v, want одну", subURLs(api.subs))
	}
	if !sameSet(api.subs[0].UpdateTypes, bot.UpdateTypes()) {
		t.Errorf("update_types = %v, want %v", api.subs[0].UpdateTypes, bot.UpdateTypes())
	}
}

func TestSameSet(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"одинаковые", []string{"a", "b"}, []string{"a", "b"}, true},
		{"другой порядок", []string{"b", "a"}, []string{"a", "b"}, true},
		{"разная длина", []string{"a"}, []string{"a", "b"}, false},
		{"разный состав", []string{"a", "c"}, []string{"a", "b"}, false},
		{"дубликат вместо элемента", []string{"a", "a"}, []string{"a", "b"}, false},
		{"обе пустые", nil, nil, true},
		{"одна пустая", nil, []string{"a"}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sameSet(c.a, c.b); got != c.want {
				t.Errorf("sameSet(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestRegisterStopsOnBadToken(t *testing.T) {
	client, api := newFakeAPI(t, nil, "/me")

	_, err := register(context.Background(), client, testConfig())
	if err == nil {
		t.Fatal("register вернул nil, want ошибку")
	}
	if len(api.calls) != 1 {
		t.Errorf("вызовы = %v, want остановку после GET /me", api.calls)
	}
}

func TestRegisterStopsWhenSubscriptionRejected(t *testing.T) {
	client, _ := newFakeAPI(t, nil, "/subscriptions")

	_, err := register(context.Background(), client, testConfig())
	if err == nil {
		t.Fatal("register вернул nil, want ошибку")
	}
	if !strings.Contains(err.Error(), "подпис") {
		t.Errorf("ошибка = %q, want упоминание подписки", err)
	}
}
