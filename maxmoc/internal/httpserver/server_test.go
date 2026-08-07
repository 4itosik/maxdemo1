package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"maxmock/internal/config"
	"maxmock/internal/core"
	"maxmock/internal/events"
	"maxmock/internal/specs"
	"maxmock/internal/store"
	"maxmock/internal/webhook"
	"maxmock/internal/wire"
)

type fixture struct {
	srv *httptest.Server
	// handler — тот же обработчик, что и в srv; нужен тестам, которые
	// поднимают собственный слушатель (например, под TLS).
	handler http.Handler
	core    *core.Core
	disp    *webhook.Dispatcher
	cfg     *config.Config
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "srv.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sp, err := specs.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.BlobDir = filepath.Join(dir, "blobs")
	cfg.Webhook = config.Webhook{TimeoutSec: 1, Retries: 0, BackoffSec: []int{0}}
	bus := events.New()
	disp, err := webhook.New(st, sp, bus, cfg.Webhook)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(disp.Close)
	c := core.New(st, disp, bus, cfg)

	handler := New(c, sp, bus, cfg)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg.PublicBaseURL = srv.URL
	return &fixture{srv: srv, handler: handler, core: c, disp: disp, cfg: cfg}
}

func (f *fixture) req(t *testing.T, method, path, body string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

func (f *fixture) createBot(t *testing.T) map[string]any {
	t.Helper()
	resp, body := f.req(t, "POST", "/mock/api/bots", `{"name":"Бот","username":"bot"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("создание бота: %d %s", resp.StatusCode, body)
	}
	var bot map[string]any
	if err := json.Unmarshal(body, &bot); err != nil {
		t.Fatal(err)
	}
	return bot
}

// seedBotAndClient заводит бота с клиентом и возвращает chat_id их диалога.
func (f *fixture) seedBotAndClient(t *testing.T) (*store.Bot, int64) {
	t.Helper()
	bot, err := f.core.Store().CreateBot("Бот поддержки", "support_bot", "демо")
	if err != nil {
		t.Fatal(err)
	}
	_, d, err := f.core.CreateClient(bot.ID, store.ClientInput{FirstName: "Иван"})
	if err != nil {
		t.Fatal(err)
	}
	return bot, d.ChatID
}

// postAction выполняет действие тестировщика и проверяет код ответа.
func (f *fixture) postAction(t *testing.T, chatID int64, body string, want int) []byte {
	t.Helper()
	resp, err := http.Post(
		f.srv.URL+"/mock/api/dialogs/"+strconv.FormatInt(chatID, 10)+"/actions",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("действие %s: статус %d, ожидался %d; тело %s", body, resp.StatusCode, want, out)
	}
	return out
}

func TestDialogStopAction(t *testing.T) {
	f := newFixture(t)
	bot, chatID := f.seedBotAndClient(t)
	_ = bot

	f.postAction(t, chatID, `{"action":"start"}`, http.StatusOK)
	f.postAction(t, chatID, `{"action":"stop"}`, http.StatusOK)

	d, err := f.core.Store().DialogByChatID(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Started {
		t.Error("после действия stop диалог остался начатым")
	}
}

func TestRoutesDoNotCollide(t *testing.T) {
	f := newFixture(t)

	// Служебные страницы.
	for _, path := range []string{"/mock", "/mock/", "/mock/chat/1"} {
		resp, body := f.req(t, "GET", path, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: статус %d", path, resp.StatusCode)
			continue
		}
		if !strings.Contains(string(body), "<html") {
			t.Errorf("%s: отдан не HTML", path)
		}
	}

	// Статика.
	resp, _ := f.req(t, "GET", "/mock/static/style.css", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("статика: %d", resp.StatusCode)
	}

	// Проверка живости.
	resp, body := f.req(t, "GET", "/healthz", "", nil)
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Errorf("healthz: %d %s", resp.StatusCode, body)
	}

	// Bot API в корне продолжает работать: служебные маршруты его не съели.
	resp, body = f.req(t, "GET", "/me", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /me без токена: %d %s", resp.StatusCode, body)
	}
	var e wire.Error
	if err := json.Unmarshal(body, &e); err != nil || e.Code == "" {
		t.Errorf("тело ошибки Bot API: %s", body)
	}
}

func TestControlAPIBotLifecycle(t *testing.T) {
	f := newFixture(t)
	bot := f.createBot(t)
	botID := int64(bot["id"].(float64))
	token, _ := bot["token"].(string)
	if token == "" {
		t.Fatal("токен бота не выдан")
	}

	// Клиент и диалог.
	resp, body := f.req(t, "POST", "/mock/api/bots/"+itoa(botID)+"/clients", `{"first_name":"Иван"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("создание клиента: %d %s", resp.StatusCode, body)
	}
	var created struct {
		Client store.Client `json:"client"`
		Dialog store.Dialog `json:"dialog"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	chatID := created.Dialog.ChatID

	resp, body = f.req(t, "GET", "/mock/api/bots/"+itoa(botID)+"/clients", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Иван") {
		t.Fatalf("список клиентов: %d %s", resp.StatusCode, body)
	}

	// Действия клиента.
	for _, action := range []string{`{"action":"start"}`, `{"action":"send","text":"здравствуйте"}`} {
		resp, body = f.req(t, "POST", "/mock/api/dialogs/"+itoa(chatID)+"/actions", action, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("действие %s: %d %s", action, resp.StatusCode, body)
		}
	}

	resp, body = f.req(t, "GET", "/mock/api/dialogs/"+itoa(chatID)+"/messages", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("сообщения диалога: %d %s", resp.StatusCode, body)
	}
	var msgs []core.UIMessage
	if err := json.Unmarshal(body, &msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Sender != store.SenderClient {
		t.Fatalf("переписка: %+v", msgs)
	}

	// Неизвестное действие отвергается понятной ошибкой.
	resp, body = f.req(t, "POST", "/mock/api/dialogs/"+itoa(chatID)+"/actions", `{"action":"взлететь"}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("неизвестное действие: %d %s", resp.StatusCode, body)
	}

	// Журналы доступны.
	for _, path := range []string{"/mock/api/bots/" + itoa(botID) + "/log",
		"/mock/api/bots/" + itoa(botID) + "/deliveries",
		"/mock/api/bots/" + itoa(botID) + "/subscriptions",
		"/mock/api/log"} {
		resp, body = f.req(t, "GET", path, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
	}

	// Несуществующий бот.
	resp, _ = f.req(t, "GET", "/mock/api/bots/999999", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("несуществующий бот: %d", resp.StatusCode)
	}
}

// Журнал в разрезе бота обязан показывать только его вызовы: на одном моке
// работают несколько стендов, и чужая активность в журнале сбивала бы с толку.
func TestRequestLogIsScopedToBot(t *testing.T) {
	f := newFixture(t)

	first := f.createBot(t)
	second := f.createBot(t)
	firstID := int64(first["id"].(float64))
	secondID := int64(second["id"].(float64))

	f.req(t, "GET", "/me", "", map[string]string{"Authorization": first["token"].(string)})
	f.req(t, "GET", "/subscriptions", "", map[string]string{"Authorization": second["token"].(string)})
	f.req(t, "GET", "/me", "", nil) // без токена — ни за одним ботом не числится

	byBot := func(botID int64) []store.RequestLogEntry {
		t.Helper()
		resp, body := f.req(t, "GET", "/mock/api/bots/"+itoa(botID)+"/log", "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("журнал бота %d: %d %s", botID, resp.StatusCode, body)
		}
		var entries []store.RequestLogEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			t.Fatal(err)
		}
		return entries
	}

	for _, tc := range []struct {
		botID int64
		path  string
	}{{firstID, "/me"}, {secondID, "/subscriptions"}} {
		entries := byBot(tc.botID)
		if len(entries) != 1 {
			t.Fatalf("у бота %d %d записей, ожидалась одна: %+v", tc.botID, len(entries), entries)
		}
		if entries[0].Path != tc.path {
			t.Errorf("в журнале бота %d чужой вызов: %s", tc.botID, entries[0].Path)
		}
		if entries[0].BotID == nil || *entries[0].BotID != tc.botID {
			t.Errorf("запись не привязана к боту: %+v", entries[0])
		}
	}

	// Общий журнал видит всё, включая неавторизованный вызов без бота.
	resp, body := f.req(t, "GET", "/mock/api/log", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("общий журнал: %d %s", resp.StatusCode, body)
	}
	var all []store.RequestLogEntry
	if err := json.Unmarshal(body, &all); err != nil {
		t.Fatal(err)
	}
	var withoutBot int
	for _, e := range all {
		if e.BotID == nil {
			withoutBot++
		}
	}
	if len(all) < 3 || withoutBot == 0 {
		t.Errorf("общий журнал: %d записей, из них без бота %d", len(all), withoutBot)
	}
}

// Двухфазная загрузка целиком: POST /uploads → заливка на выданный адрес →
// отправка вложения → файл доступен по ссылке из сообщения.
func TestUploadRoundTrip(t *testing.T) {
	f := newFixture(t)
	bot := f.createBot(t)
	botID := int64(bot["id"].(float64))
	token := bot["token"].(string)
	auth := map[string]string{"Authorization": token}

	resp, body := f.req(t, "POST", "/mock/api/bots/"+itoa(botID)+"/clients", `{"first_name":"Иван"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("клиент: %d %s", resp.StatusCode, body)
	}
	var created struct {
		Client store.Client `json:"client"`
	}
	_ = json.Unmarshal(body, &created)

	// Фаза 1: адрес загрузки.
	resp, body = f.req(t, "POST", "/uploads?type=file", "", auth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /uploads: %d %s", resp.StatusCode, body)
	}
	var endpoint wire.UploadEndpoint
	if err := json.Unmarshal(body, &endpoint); err != nil {
		t.Fatal(err)
	}

	// Фаза 2: заливка файла на выданный адрес.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("data", "отчёт.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("содержимое отчёта")); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()

	uploadResp, err := http.Post(endpoint.URL, mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	uploadBody, _ := io.ReadAll(uploadResp.Body)
	_ = uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("заливка: %d %s", uploadResp.StatusCode, uploadBody)
	}
	var uploaded wire.UploadedInfo
	if err := json.Unmarshal(uploadBody, &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Token != endpoint.Token {
		t.Errorf("ответ заливки содержит другой токен: %s", uploadBody)
	}

	// Фаза 3: вложение в сообщении.
	send := `{"text":"документ","attachments":[{"type":"file","payload":{"token":"` + uploaded.Token + `"}}],"link":null}`
	resp, body = f.req(t, "POST", "/messages?user_id="+itoa(created.Client.UserID), send, auth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("отправка вложения: %d %s", resp.StatusCode, body)
	}
	var sent wire.SendMessageResult
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Message.Body.Attachments) != 1 {
		t.Fatalf("вложение потеряно: %s", body)
	}
	att := sent.Message.Body.Attachments[0]
	if att.Filename != "отчёт.txt" {
		t.Errorf("имя файла: %q", att.Filename)
	}
	var media wire.MediaPayload
	if err := json.Unmarshal(att.Payload, &media); err != nil {
		t.Fatal(err)
	}

	// Фаза 4: файл доступен по ссылке из сообщения.
	fileResp, err := http.Get(media.URL)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(fileResp.Body)
	_ = fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusOK || string(content) != "содержимое отчёта" {
		t.Fatalf("отдача файла: %d %q", fileResp.StatusCode, content)
	}

	// Повторная заливка по тому же токену отвергается.
	again, err := http.Post(endpoint.URL, "text/plain", strings.NewReader("ещё раз"))
	if err != nil {
		t.Fatal(err)
	}
	_ = again.Body.Close()
	if again.StatusCode != http.StatusBadRequest {
		t.Errorf("повторная заливка: %d", again.StatusCode)
	}
}

// Ответ upload-эндпоинта для изображений должен быть в форме, которую примет
// PhotoAttachmentRequestPayload.photos.
func TestImageUploadResponseShape(t *testing.T) {
	f := newFixture(t)
	bot := f.createBot(t)
	auth := map[string]string{"Authorization": bot["token"].(string)}

	resp, body := f.req(t, "POST", "/uploads?type=image", "", auth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /uploads: %d %s", resp.StatusCode, body)
	}
	var endpoint wire.UploadEndpoint
	_ = json.Unmarshal(body, &endpoint)

	uploadResp, err := http.Post(endpoint.URL, "image/png", strings.NewReader("\x89PNG..."))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(uploadResp.Body)
	_ = uploadResp.Body.Close()

	var out struct {
		Photos map[string]wire.PhotoToken `json:"photos"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Photos) != 1 {
		t.Fatalf("ответ для image должен содержать photos: %s", raw)
	}
	for _, p := range out.Photos {
		if p.Token != endpoint.Token {
			t.Errorf("токен в photos: %s", raw)
		}
	}
}

func TestUploadUnknownToken(t *testing.T) {
	f := newFixture(t)
	resp, _ := f.req(t, "POST", "/mock/upload/att.несуществующий", "тело", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("заливка по неизвестному токену: %d", resp.StatusCode)
	}
	resp, _ = f.req(t, "GET", "/mock/files/att.несуществующий", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("отдача неизвестного файла: %d", resp.StatusCode)
	}
}

// WebSocket отдаёт события бота: без этого чат не обновлялся бы вживую.
func TestWebSocketStreamsEvents(t *testing.T) {
	f := newFixture(t)
	bot := f.createBot(t)
	botID := int64(bot["id"].(float64))

	resp, body := f.req(t, "POST", "/mock/api/bots/"+itoa(botID)+"/clients", `{"first_name":"Иван"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatal(string(body))
	}
	var created struct {
		Dialog store.Dialog `json:"dialog"`
	}
	_ = json.Unmarshal(body, &created)

	wsURL := "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/mock/ws?bot_id=" + itoa(botID)
	conn, _, err := dialWS(t, wsURL)
	if err != nil {
		t.Fatalf("подключение к /mock/ws: %v", err)
	}
	defer conn.CloseNow()

	go func() {
		time.Sleep(100 * time.Millisecond)
		f.req(t, "POST", "/mock/api/dialogs/"+itoa(created.Dialog.ChatID)+"/actions",
			`{"action":"send","text":"привет"}`, nil)
	}()

	ev, err := readWSEvent(t, conn, 3*time.Second)
	if err != nil {
		t.Fatalf("событие не получено: %v", err)
	}
	if ev["kind"] == nil {
		t.Errorf("событие без вида: %v", ev)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestUIActionsAreJournalled(t *testing.T) {
	f := newFixture(t)
	_, chatID := f.seedBotAndClient(t)

	f.postAction(t, chatID, `{"action":"start"}`, http.StatusOK)
	f.postAction(t, chatID, `{"action":"send","text":"/start"}`, http.StatusOK)
	// Неудачное действие тоже обязано попасть в журнал: «нажал, ничего не
	// произошло» — самая частая жалоба, и причина должна быть видна.
	f.postAction(t, chatID, `{"action":"press","mid":"mid.нет","payload":"hello"}`, http.StatusNotFound)

	entries, err := f.core.Store().ListUIActions(chatID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("записей о действиях: %d, ожидалось 3: %+v", len(entries), entries)
	}
	// Новые первыми.
	if entries[0].Action != "press" || entries[0].Error == "" {
		t.Errorf("неудачное действие записано без причины: %+v", entries[0])
	}
	if entries[1].Action != "send" || !strings.Contains(entries[1].Detail, "/start") {
		t.Errorf("параметры действия не сохранены: %+v", entries[1])
	}
	if entries[2].Action != "start" {
		t.Errorf("порядок записей нарушен: %+v", entries)
	}
}

// Лента диалога — обмены между моком и ботом, и только этого диалога.
// Действия тестировщика и вызовы бота вне диалога в неё не входят: первые он
// совершил сам, вторые относятся ко всему боту и живут на вкладке «Журнал».
func TestDialogEventsEndpoint(t *testing.T) {
	f := newFixture(t)
	bot, chatID := f.seedBotAndClient(t)
	f.postAction(t, chatID, `{"action":"start"}`, http.StatusOK)
	// Вызов вне диалога тем же ботом: раньше он подмешивался в ленту.
	f.req(t, "GET", "/me", "", map[string]string{"Authorization": bot.Token})
	// Доставка bot_started уходит асинхронно; без подписки она осядет в
	// журнале записью «нет подписки», но только после разбора очереди.
	f.disp.Wait()

	resp, err := http.Get(f.srv.URL + "/mock/api/dialogs/" + strconv.FormatInt(chatID, 10) + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("статус %d", resp.StatusCode)
	}
	var feed []store.DialogEvent
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		t.Fatal(err)
	}
	if len(feed) != 1 {
		t.Fatalf("в ленте %d записей, ожидалась одна доставка bot_started: %+v", len(feed), feed)
	}
	if feed[0].Kind != "delivery" || feed[0].Delivery.UpdateType != "bot_started" {
		t.Errorf("в ленте ожидалась доставка bot_started: %+v", feed[0])
	}
}

// Действие в несуществующий чат обязано остаться 404 с кодом
// mock.chat.not_found — тем же, что отдавало бы ядро. Резолвя диалог заранее
// (чтобы отдать botID в logAction без лишнего SELECT), легко по ошибке
// прокинуть в fail сырой store.ErrNotFound и тихо подменить код на
// mock.not_found — контракт control-API поменялся бы незаметно для клиентов.
func TestDialogActionUnknownChatKeepsChatNotFoundCode(t *testing.T) {
	f := newFixture(t)

	body := f.postAction(t, 999999, `{"action":"start"}`, http.StatusNotFound)
	var e wire.Error
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "mock.chat.not_found" {
		t.Errorf("код ошибки: %q, ожидался mock.chat.not_found", e.Code)
	}
}

// Тот же путь /mock/api/dialogs/{chatId}/*, тот же контракт: лента событий
// несуществующего диалога обязана отвечать mock.chat.not_found, а не
// сырым mock.not_found из store — как уже проверено для dialogAction.
func TestDialogEventsUnknownChatKeepsChatNotFoundCode(t *testing.T) {
	f := newFixture(t)

	resp, err := http.Get(f.srv.URL + "/mock/api/dialogs/999999/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("статус %d, ожидался 404; тело %s", resp.StatusCode, body)
	}
	var e wire.Error
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "mock.chat.not_found" {
		t.Errorf("код ошибки: %q, ожидался mock.chat.not_found", e.Code)
	}
}
