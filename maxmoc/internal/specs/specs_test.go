package specs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func load(t *testing.T) *Specs {
	t.Helper()
	s, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestLoadReportsContractVersion(t *testing.T) {
	if v := load(t).Version(); v != "0.0.32" {
		t.Fatalf("версия контракта = %q, ожидалась 0.0.32", v)
	}
}

func TestFindRoute(t *testing.T) {
	s := load(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/me"},
		{"POST", "/messages"},
		{"PUT", "/messages"},
		{"DELETE", "/messages"},
		{"GET", "/messages/mid.abc"},
		{"POST", "/answers"},
		{"GET", "/subscriptions"},
		{"POST", "/uploads"},
	} {
		r := httptest.NewRequest(tc.method, "http://localhost:8080"+tc.path, nil)
		if _, _, err := s.FindRoute(r); err != nil {
			t.Errorf("%s %s: %v", tc.method, tc.path, err)
		}
	}

	// Путей /chats в артефакте нет — они должны честно не находиться.
	r := httptest.NewRequest("GET", "http://localhost:8080/chats", nil)
	if _, _, err := s.FindRoute(r); err == nil {
		t.Error("GET /chats неожиданно найден в контракте")
	}
}

func postMessages(body string) *http.Request {
	r := httptest.NewRequest("POST", "http://localhost:8080/messages?user_id=42", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "x")
	return r
}

func TestValidateRequestBody(t *testing.T) {
	s := load(t)
	ctx := context.Background()

	ok := postMessages(`{"text":"привет","attachments":null,"link":null}`)
	route, params, err := s.FindRoute(ok)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateRequest(ctx, ok, route, params); err != nil {
		t.Fatalf("валидное тело отвергнуто: %v", err)
	}

	// text/attachments/link объявлены required (пусть и nullable) — тело без них невалидно.
	bad := postMessages(`{"notify":true}`)
	route, params, _ = s.FindRoute(bad)
	if err := s.ValidateRequest(ctx, bad, route, params); err == nil {
		t.Fatal("тело без обязательных полей прошло валидацию")
	}

	// Неверный тип поля.
	bad2 := postMessages(`{"text":123,"attachments":null,"link":null}`)
	route, params, _ = s.FindRoute(bad2)
	if err := s.ValidateRequest(ctx, bad2, route, params); err == nil {
		t.Fatal("text числом прошёл валидацию")
	}
}

func TestValidateRequestKeepsBodyReadable(t *testing.T) {
	s := load(t)
	body := `{"text":"привет","attachments":null,"link":null}`
	r := postMessages(body)
	route, params, err := s.FindRoute(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateRequest(context.Background(), r, route, params); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(body))
	n, _ := r.Body.Read(buf)
	if string(buf[:n]) != body {
		t.Fatalf("тело после валидации нечитаемо: %q", string(buf[:n]))
	}
}

func TestValidateRequestQueryParams(t *testing.T) {
	s := load(t)
	ctx := context.Background()

	// type — обязательный query-параметр с enum.
	r := httptest.NewRequest("POST", "http://localhost:8080/uploads?type=image", nil)
	route, params, err := s.FindRoute(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateRequest(ctx, r, route, params); err != nil {
		t.Fatalf("type=image отвергнут: %v", err)
	}

	bad := httptest.NewRequest("POST", "http://localhost:8080/uploads?type=photo", nil)
	route, params, _ = s.FindRoute(bad)
	if err := s.ValidateRequest(ctx, bad, route, params); err == nil {
		t.Fatal("type=photo (значение вне enum) прошло валидацию")
	}

	missing := httptest.NewRequest("POST", "http://localhost:8080/uploads", nil)
	route, params, _ = s.FindRoute(missing)
	if err := s.ValidateRequest(ctx, missing, route, params); err == nil {
		t.Fatal("отсутствие обязательного type прошло валидацию")
	}
}

func TestValidateResponse(t *testing.T) {
	s := load(t)
	ctx := context.Background()
	r := httptest.NewRequest("GET", "http://localhost:8080/me", nil)
	route, params, err := s.FindRoute(r)
	if err != nil {
		t.Fatal(err)
	}
	h := http.Header{"Content-Type": []string{"application/json"}}

	good := []byte(`{"user_id":1,"first_name":"Бот","username":"bot","is_bot":true,` +
		`"last_activity_time":1722800000000,"name":null}`)
	if err := s.ValidateResponse(ctx, r, route, params, 200, h, good); err != nil {
		t.Fatalf("корректный BotInfo отвергнут: %v", err)
	}

	// Нет обязательного is_bot.
	bad := []byte(`{"user_id":1,"first_name":"Бот","username":"bot",` +
		`"last_activity_time":1722800000000,"name":null}`)
	if err := s.ValidateResponse(ctx, r, route, params, 200, h, bad); err == nil {
		t.Fatal("BotInfo без is_bot прошёл валидацию ответа")
	}

	// 400 в контракте Max не описан — такие ответы валидацию пропускают.
	if err := s.ValidateResponse(ctx, r, route, params, 400, h, []byte(`{"что":"угодно"}`)); err != nil {
		t.Fatalf("недокументированный статус не должен валидироваться: %v", err)
	}
}

// Контракт объявляет ChatType как enum ["chat"], хотя реальный Max отдаёт
// "dialog" для диалогов. Загрузчик расширяет enum — проверяем, что оба
// значения проходят.
func TestChatTypeDialogAccepted(t *testing.T) {
	s := load(t)
	ctx := context.Background()
	body := func(chatType string) []byte {
		return []byte(`{"update_type":"message_created","timestamp":1722800000000,"message":{` +
			`"recipient":{"chat_id":7,"chat_type":"` + chatType + `","user_id":42},` +
			`"timestamp":1722800000000,"body":{"mid":"mid.1","seq":1,"text":"привет"}}}`)
	}
	for _, ct := range []string{"dialog", "chat"} {
		if err := s.ValidateWebhookBody(ctx, body(ct)); err != nil {
			t.Errorf("chat_type=%q отвергнут: %v", ct, err)
		}
	}
	if err := s.ValidateWebhookBody(ctx, body("выдумка")); err == nil {
		t.Error("произвольный chat_type прошёл валидацию — enum не должен быть отключён")
	}
}

func TestValidateWebhookBody(t *testing.T) {
	s := load(t)
	ctx := context.Background()

	ok := []byte(`{"update_type":"message_removed","timestamp":1722800000000,` +
		`"message_id":"mid.0000000000000001","chat_id":7,"user_id":42}`)
	if err := s.ValidateWebhookBody(ctx, ok); err != nil {
		t.Fatalf("валидный message_removed отвергнут: %v", err)
	}

	started := []byte(`{"update_type":"bot_started","timestamp":1722800000000,"chat_id":7,` +
		`"user":{"user_id":42,"first_name":"Клиент","username":"client","is_bot":false,` +
		`"last_activity_time":1722800000000,"name":null},"user_locale":"ru"}`)
	if err := s.ValidateWebhookBody(ctx, started); err != nil {
		t.Fatalf("валидный bot_started отвергнут: %v", err)
	}

	for name, body := range map[string]string{
		"timestamp строкой": `{"update_type":"message_removed","timestamp":"не число","message_id":"m","chat_id":7,"user_id":42}`,
		"нет update_type":   `{"timestamp":1722800000000,"message_id":"m","chat_id":7,"user_id":42}`,
		"user не объект":    `{"update_type":"bot_started","timestamp":1722800000000,"chat_id":7,"user":5}`,
		"не JSON вообще":    `не json`,
	} {
		if err := s.ValidateWebhookBody(ctx, []byte(body)); err == nil {
			t.Errorf("%s: невалидное тело прошло валидацию", name)
		}
	}
}
