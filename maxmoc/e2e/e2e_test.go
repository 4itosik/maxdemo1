package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// TestFullIntegrationScenario проходит путь, ради которого мок и написан:
// регистрация бота → подписка стенда → диалог с кнопками → ответ на нажатие
// → правка → удаление → вложение. Каждый шаг проверяется с обеих сторон:
// ответ мока — по контракту Bot API, событие на стенде — по контракту
// webhook-а.
func TestFullIntegrationScenario(t *testing.T) {
	m := newMock(t)
	s := newStand(t, m.specs)

	// --- Шаг 1. КЦ регистрирует бота в моке и получает access_token. ---
	bot := decode[store.Bot](t, m.mustCall(t, "POST", "/mock/api/bots",
		`{"name":"Бот поддержки","username":"support_bot","description":"демо КЦ"}`, ""))
	if bot.Token == "" {
		t.Fatal("access_token не выдан")
	}
	token := bot.Token

	// --- Шаг 2. Стенд подписывается на события. ---
	sub := `{"url":"` + s.srv.URL + `","secret":"` + secret + `"}`
	res := decode[wire.SimpleQueryResult](t, m.mustCall(t, "POST", "/subscriptions", sub, token))
	if !res.Success {
		t.Fatalf("подписка не создана: %+v", res)
	}

	subs := decode[wire.GetSubscriptionsResult](t, m.mustCall(t, "GET", "/subscriptions", "", token))
	if len(subs.Subscriptions) != 1 || subs.Subscriptions[0].URL != s.srv.URL {
		t.Fatalf("подписки: %+v", subs.Subscriptions)
	}

	// --- Шаг 3. GET /me отдаёт того бота, чей токен предъявлен. ---
	info := decode[wire.BotInfo](t, m.mustCall(t, "GET", "/me", "", token))
	if info.UserID != bot.UserID || !info.IsBot {
		t.Fatalf("GET /me: %+v", info)
	}

	// Контракт объявляет ещё две схемы — Bearer и access_token в query, — но
	// живой Max отвергает обе (замер 2026-08-06). Мок повторяет прод, и
	// сквозной тест это фиксирует: иначе расхождение всплывёт у интегратора.
	if status, raw := m.call(t, "GET", "/me?access_token="+token, "", ""); status != http.StatusUnauthorized {
		t.Fatalf("токен в query должен отвергаться: %d %s", status, raw)
	}

	// --- Шаг 3а. КЦ публикует список команд бота. ---
	cmds := decode[wire.BotCommandsInfo](t, m.mustCall(t, "PATCH", "/me/commands",
		`{"commands":[{"name":"start","description":"начать диалог"},{"name":"operator"}]}`, token))
	if len(cmds.Commands) != 2 {
		t.Fatalf("PATCH /me/commands вернул %d команд", len(cmds.Commands))
	}
	withCommands := decode[wire.BotInfo](t, m.mustCall(t, "GET", "/me", "", token))
	if len(withCommands.Commands) != 2 || withCommands.Commands[0].Name != "start" {
		t.Fatalf("GET /me не отдаёт опубликованные команды: %+v", withCommands.Commands)
	}

	// --- Шаг 4. Тестировщик создаёт клиента и нажимает «Начать». ---
	created := decode[struct {
		Client store.Client `json:"client"`
		Dialog store.Dialog `json:"dialog"`
	}](t, m.mustCall(t, "POST", "/mock/api/bots/"+itoa(bot.ID)+"/clients", `{"first_name":"Иван"}`, ""))
	chatID, userID := created.Dialog.ChatID, created.Client.UserID

	m.mustCall(t, "POST", dialogActions(chatID), `{"action":"start","payload":"из-диплинка"}`, "")
	started := s.await(t, wire.UpdateBotStarted)
	if int64(started["chat_id"].(float64)) != chatID {
		t.Errorf("bot_started пришёл с чужим chat_id: %v", started["chat_id"])
	}
	if started["payload"] != "из-диплинка" {
		t.Errorf("payload диплинка потерян: %v", started["payload"])
	}

	// --- Шаг 5. Клиент пишет боту. ---
	m.mustCall(t, "POST", dialogActions(chatID), `{"action":"send","text":"здравствуйте, нужна помощь"}`, "")
	incoming := s.await(t, wire.UpdateMessageCreated)
	incomingMsg := incoming["message"].(map[string]any)
	if incomingMsg["body"].(map[string]any)["text"] != "здравствуйте, нужна помощь" {
		t.Errorf("текст входящего: %v", incomingMsg["body"])
	}
	if recipient := incomingMsg["recipient"].(map[string]any); recipient["chat_type"] != wire.ChatTypeDialog {
		t.Errorf("тип чата входящего: %v", recipient["chat_type"])
	}

	// --- Шаг 6. КЦ отвечает сообщением с инлайн-клавиатурой. ---
	keyboard := `{"text":"Выберите тему обращения","link":null,"attachments":[{"type":"inline_keyboard",` +
		`"payload":{"buttons":[[{"type":"callback","text":"Оплата","payload":"topic:payment"},` +
		`{"type":"callback","text":"Доставка","payload":"topic:delivery"}]]}}]}`
	sent := decode[wire.SendMessageResult](t, m.mustCall(t, "POST",
		"/messages?user_id="+itoa(userID), keyboard, token))
	mid := sent.Message.Body.Mid
	if mid == "" || len(sent.Message.Body.Attachments) != 1 {
		t.Fatalf("сообщение с клавиатурой: %+v", sent.Message.Body)
	}

	// --- Шаг 7. Клиент нажимает кнопку. ---
	m.mustCall(t, "POST", dialogActions(chatID), `{"action":"press","mid":"`+mid+`","payload":"topic:payment"}`, "")
	callbackEvent := s.await(t, wire.UpdateMessageCallback)
	callback := callbackEvent["callback"].(map[string]any)
	if callback["payload"] != "topic:payment" {
		t.Fatalf("payload нажатия: %v", callback["payload"])
	}
	if callbackEvent["message"] == nil {
		t.Fatal("к событию нажатия не приложено исходное сообщение")
	}
	callbackID := callback["callback_id"].(string)

	// --- Шаг 8. КЦ отвечает на нажатие, заменяя сообщение. ---
	answer := `{"message":{"text":"Тема «Оплата» принята в работу","attachments":[],"link":null}}`
	m.mustCall(t, "POST", "/answers?callback_id="+callbackID, answer, token)

	// Событие о правке на стенд не приходит — её сделал сам бот, и живой Max
	// такого не присылает. Результат проверяется чтением сообщения.
	afterAnswer := decode[wire.Message](t, m.mustCall(t, "GET", "/messages/"+mid, "", token))
	if afterAnswer.Body.Text == nil || *afterAnswer.Body.Text != "Тема «Оплата» принята в работу" {
		t.Errorf("текст после ответа на нажатие: %+v", afterAnswer.Body)
	}
	if len(afterAnswer.Body.Attachments) != 0 {
		t.Errorf("attachments:[] должен был убрать клавиатуру: %+v", afterAnswer.Body.Attachments)
	}

	// Повторный ответ на то же нажатие: контракт для /answers не объявляет
	// 404, поэтому мок отвечает 400 с кодом mock.*, а не выдумывает статус.
	m.mustCall(t, "POST", "/answers?callback_id="+callbackID, `{}`, token)
	status, raw := m.call(t, "POST", "/answers?callback_id=cb.несуществующий", `{}`, token)
	if status != http.StatusBadRequest {
		t.Errorf("неизвестный callback_id: статус %d (%s)", status, raw)
	}
	if code := decode[wire.Error](t, raw).Code; code != "mock.callback.not_found" {
		t.Errorf("код ошибки: %s", code)
	}

	// --- Шаг 9. КЦ правит сообщение напрямую. ---
	m.mustCall(t, "PUT", "/messages?message_id="+mid,
		`{"text":"Заявка №123 зарегистрирована","attachments":null,"link":null}`, token)
	byID := decode[wire.Message](t, m.mustCall(t, "GET", "/messages/"+mid, "", token))
	if byID.Body.Text == nil || *byID.Body.Text != "Заявка №123 зарегистрирована" {
		t.Errorf("GET /messages/{mid}: %+v", byID.Body)
	}

	// --- Шаг 10. Список сообщений диалога. ---
	list := decode[wire.MessageList](t, m.mustCall(t, "GET", "/messages?chat_id="+itoa(chatID), "", token))
	if len(list.Messages) != 2 {
		t.Errorf("в диалоге %d сообщений, ожидалось 2", len(list.Messages))
	}

	// --- Шаг 11. Двухфазная загрузка и отправка вложения. ---
	endpoint := decode[wire.UploadEndpoint](t, m.mustCall(t, "POST", "/uploads?type=image", "", token))
	uploadToken := uploadFile(t, endpoint.URL, "скриншот.png", "image/png", []byte("\x89PNG данные"))

	withPhoto := `{"text":"Скриншот прикреплён","link":null,"attachments":[{"type":"image",` +
		`"payload":{"token":"` + uploadToken + `"}}]}`
	photoMsg := decode[wire.SendMessageResult](t, m.mustCall(t, "POST",
		"/messages?chat_id="+itoa(chatID), withPhoto, token))
	if len(photoMsg.Message.Body.Attachments) != 1 {
		t.Fatalf("вложение не попало в сообщение: %+v", photoMsg.Message.Body)
	}
	media := decode[wire.MediaPayload](t, photoMsg.Message.Body.Attachments[0].Payload)
	if media.Token == "" || media.URL == "" || media.PhotoID == 0 {
		t.Errorf("полезная нагрузка изображения неполна: %+v", media)
	}
	if body := fetch(t, media.URL); string(body) != "\x89PNG данные" {
		t.Errorf("файл по ссылке из сообщения: %q", body)
	}

	// --- Шаг 12. Удаление сообщения. ---
	m.mustCall(t, "DELETE", "/messages?message_id="+mid, "", token)
	removed := s.await(t, wire.UpdateMessageRemoved)
	if removed["message_id"] != mid {
		t.Errorf("удалено не то сообщение: %v", removed["message_id"])
	}
	status, _ = m.call(t, "GET", "/messages/"+mid, "", token)
	if status != http.StatusNotFound {
		t.Errorf("чтение удалённого сообщения: %d", status)
	}

	// --- Шаг 13. Отписка. ---
	m.mustCall(t, "DELETE", "/subscriptions?url="+s.srv.URL, "", token)
	subs = decode[wire.GetSubscriptionsResult](t, m.mustCall(t, "GET", "/subscriptions", "", token))
	if len(subs.Subscriptions) != 0 {
		t.Errorf("после отписки осталось: %+v", subs.Subscriptions)
	}

	// После отписки события не доставляются.
	before := s.count(wire.UpdateMessageCreated)
	m.mustCall(t, "POST", dialogActions(chatID), `{"action":"send","text":"уже не дойдёт"}`, "")
	m.disp.Wait()
	if n := s.count(wire.UpdateMessageCreated); n != before {
		t.Errorf("после отписки доставлено ещё %d message_created", n-before)
	}

	// --- Шаг 14. Журналы содержат обе стороны обмена. ---
	logEntries := decode[[]store.RequestLogEntry](t,
		m.mustCall(t, "GET", "/mock/api/bots/"+itoa(bot.ID)+"/log?limit=100", "", ""))
	if len(logEntries) == 0 {
		t.Error("журнал запросов пуст")
	}
	deliveries := decode[[]store.DeliveryEntry](t,
		m.mustCall(t, "GET", "/mock/api/bots/"+itoa(bot.ID)+"/deliveries?limit=100", "", ""))
	if len(deliveries) == 0 {
		t.Error("журнал доставок пуст")
	}
	// Записи журнала бывают двух разных родов, и смешивать их в одну проверку
	// нельзя: Attempt >= 1 — была попытка достучаться до стенда, и здесь по
	// сценарию она обязана была получиться; Attempt == 0 с пустым URL —
	// попытки не было вовсе, ровно запись о «уже не дойдёт» из шага 13, и это
	// не сбой, а честная фиксация того, что стенд не подписан.
	var skipped *store.DeliveryEntry
	for i := range deliveries {
		d := deliveries[i]
		if d.Attempt == 0 {
			if skipped != nil {
				t.Errorf("записей о недоставленном событии больше одной: %+v и %+v", *skipped, d)
				continue
			}
			skipped = &d
			continue
		}
		if d.Status != http.StatusOK {
			t.Errorf("неуспешная попытка доставки в журнале: %+v", d)
		}
	}
	if skipped == nil {
		t.Fatal("запись о событии, недоставленном после отписки, не найдена")
	}
	if skipped.URL != "" || skipped.UpdateType != wire.UpdateMessageCreated {
		t.Errorf("недоставленная запись — не то событие: %+v", *skipped)
	}
	if !strings.Contains(skipped.Error, "нет подписки на этот тип события") {
		t.Errorf("причина недоставки не записана: %q", skipped.Error)
	}
}

// TestSubscriptionFilterIsolatesStands проверяет, что несколько стендов
// получают только те события, на которые подписались.
func TestSubscriptionFilterIsolatesStands(t *testing.T) {
	m := newMock(t)
	all := newStand(t, m.specs)
	onlyCreated := newStand(t, m.specs)

	bot := decode[store.Bot](t, m.mustCall(t, "POST", "/mock/api/bots", `{"name":"Бот","username":"bot"}`, ""))
	m.mustCall(t, "POST", "/subscriptions", `{"url":"`+all.srv.URL+`","secret":"`+secret+`"}`, bot.Token)
	m.mustCall(t, "POST", "/subscriptions",
		`{"url":"`+onlyCreated.srv.URL+`","secret":"`+secret+`","update_types":["message_created"]}`, bot.Token)

	created := decode[struct {
		Dialog store.Dialog `json:"dialog"`
	}](t, m.mustCall(t, "POST", "/mock/api/bots/"+itoa(bot.ID)+"/clients", `{"first_name":"Иван"}`, ""))

	m.mustCall(t, "POST", dialogActions(created.Dialog.ChatID), `{"action":"start"}`, "")
	m.mustCall(t, "POST", dialogActions(created.Dialog.ChatID), `{"action":"send","text":"привет"}`, "")
	m.disp.Wait()

	all.await(t, wire.UpdateBotStarted)
	all.await(t, wire.UpdateMessageCreated)
	onlyCreated.await(t, wire.UpdateMessageCreated)
	onlyCreated.refute(t, wire.UpdateBotStarted)
}

// TestUnavailableStandDoesNotBreakMock: мок обязан пережить неотвечающий
// стенд — зафиксировать неудачу в журнале и продолжить обслуживать API.
func TestUnavailableStandDoesNotBreakMock(t *testing.T) {
	m := newMock(t)
	bot := decode[store.Bot](t, m.mustCall(t, "POST", "/mock/api/bots", `{"name":"Бот","username":"bot"}`, ""))
	m.mustCall(t, "POST", "/subscriptions",
		`{"url":"http://127.0.0.1:1/недоступно","secret":"`+secret+`"}`, bot.Token)

	created := decode[struct {
		Client store.Client `json:"client"`
		Dialog store.Dialog `json:"dialog"`
	}](t, m.mustCall(t, "POST", "/mock/api/bots/"+itoa(bot.ID)+"/clients", `{"first_name":"Иван"}`, ""))

	// Доставку порождает сообщение клиента: собственные сообщения бота
	// webhook-ом не рассылаются.
	m.mustCall(t, "POST", dialogActions(created.Dialog.ChatID),
		`{"action":"send","text":"уйдёт в никуда"}`, "")
	m.disp.Wait()

	deliveries := decode[[]store.DeliveryEntry](t,
		m.mustCall(t, "GET", "/mock/api/bots/"+itoa(bot.ID)+"/deliveries", "", ""))
	if len(deliveries) != 2 { // одна попытка плюс один повтор
		t.Fatalf("попыток доставки: %d, ожидалось 2", len(deliveries))
	}
	for _, d := range deliveries {
		if d.Status != 0 || d.Error == "" {
			t.Errorf("неудачная доставка должна быть помечена ошибкой: %+v", d)
		}
	}

	// API продолжает работать.
	m.mustCall(t, "GET", "/me", "", bot.Token)
}

// TestDialogFeedTellsTheWholeStory проходит путь, на котором разбор
// интеграции раньше упирался в тупик: старт, команда из меню, нажатие кнопки,
// остановка. Лента диалога обязана содержать все звенья — включая события,
// которые никуда не ушли, потому что стенд на их тип не подписан.
func TestDialogFeedTellsTheWholeStory(t *testing.T) {
	f := newE2EFixture(t)
	// Стенд подписан на три типа — ровно как демобот, чей разбор и завёл эту
	// задачу. bot_stopped в этот список не входит: демобот не знает о нём,
	// он появился позже. message_edited и подавно не входит — правка клиента
	// его породит, и это тот самый пропавший след из спеки.
	f.subscribe(t, []string{wire.UpdateMessageCreated, wire.UpdateMessageCallback, wire.UpdateBotStarted})

	chatID := f.chatID
	f.action(t, chatID, `{"action":"start"}`)
	f.stand.await(t, wire.UpdateBotStarted)

	f.botSend(t, chatID, `{"text":"Здравствуйте","attachments":[{"type":"inline_keyboard","payload":{"buttons":[[{"type":"callback","text":"Привет","payload":"hello"}]]}}],"link":null}`)

	f.action(t, chatID, `{"action":"send","text":"/start"}`)
	created := f.stand.await(t, wire.UpdateMessageCreated)

	// Клиент правит собственное сообщение: правку живой Max боту присылает —
	// в отличие от правки, которую бот сделал сам. Стенд на этот тип не
	// подписан, поэтому событие осядет в ленте недоставленным.
	clientMid := created["message"].(map[string]any)["body"].(map[string]any)["mid"].(string)
	f.action(t, chatID, `{"action":"edit","mid":"`+clientMid+`","text":"/start, пожалуйста"}`)

	mid := f.lastBotMessageMid(t, chatID)
	f.action(t, chatID, `{"action":"press","mid":"`+mid+`","payload":"hello"}`)
	cb := f.stand.await(t, wire.UpdateMessageCallback)
	callbackID := cb["callback"].(map[string]any)["callback_id"].(string)

	f.botAnswer(t, callbackID, `{"message":{"text":"Вы нажали: Привет","attachments":null,"link":null}}`)
	f.action(t, chatID, `{"action":"stop"}`)
	// message_edited и bot_stopped на стенд не приходят — ждать здесь нечего;
	// дожидаемся, пока диспетчер разберёт обе очереди и запишет недоставку.
	f.m.disp.Wait()

	feed := f.dialogEvents(t, chatID)

	has := func(pred func(store.DialogEvent) bool, what string) {
		t.Helper()
		for _, e := range feed {
			if pred(e) {
				return
			}
		}
		t.Errorf("в ленте нет звена: %s", what)
	}
	// Обмен «max → бот» целиком: событие ушло и стенд ответил — обе половины
	// в одной записи, ровно то, что показывает лента.
	has(func(e store.DialogEvent) bool {
		return e.Kind == "delivery" && e.Delivery.UpdateType == wire.UpdateBotStarted &&
			e.Delivery.Status == http.StatusOK && e.Delivery.ResponseBody == standAck
	}, "доставка bot_started с ответом стенда")
	// Обмен «бот → max»: команда из меню доехала до стенда, и это видно по
	// доставке message_created с телом события.
	has(func(e store.DialogEvent) bool {
		return e.Kind == "delivery" && e.Delivery.UpdateType == wire.UpdateMessageCreated &&
			strings.Contains(e.Delivery.Body, "/start")
	}, "доставка message_created с командой /start")
	has(func(e store.DialogEvent) bool {
		return e.Kind == "request" && e.Request.Path == "/answers" &&
			e.Request.ChatID != nil && *e.Request.ChatID == chatID
	}, "вызов /answers с проставленным диалогом")
	has(func(e store.DialogEvent) bool {
		return e.Kind == "delivery" && e.Delivery.UpdateType == wire.UpdateMessageEdited &&
			e.Delivery.Status == 0 && strings.Contains(e.Delivery.Error, "нет подписки")
	}, "недоставленное message_edited с причиной")
	// bot_stopped здесь не может быть доставкой-успехом: подписки на него нет
	// (см. subscribe выше). Проверка на «недоставку», а не на факт доставки —
	// это и есть честный сценарий: тест обязан отличать «стенд не подписан» от
	// воображаемого 200, иначе он сам повторит ту же путаницу, из-за которой
	// эта задача появилась.
	has(func(e store.DialogEvent) bool {
		return e.Kind == "delivery" && e.Delivery.UpdateType == wire.UpdateBotStopped &&
			e.Delivery.Status == 0 && strings.Contains(e.Delivery.Error, "нет подписки")
	}, "недоставленное bot_stopped с причиной")
}

// e2eFixture — мок, стенд, бот и клиент с диалогом, поднятые одним вызовом:
// первые шаги TestFullIntegrationScenario (регистрация бота, создание
// клиента) сценарию ленты не интересны, а повторять их незачем.
type e2eFixture struct {
	m      *mock
	stand  *stand
	bot    store.Bot
	token  string
	chatID int64
}

func newE2EFixture(t *testing.T) *e2eFixture {
	t.Helper()
	m := newMock(t)
	s := newStand(t, m.specs)
	bot := decode[store.Bot](t, m.mustCall(t, "POST", "/mock/api/bots",
		`{"name":"Бот поддержки","username":"support_bot"}`, ""))

	created := decode[struct {
		Dialog store.Dialog `json:"dialog"`
	}](t, m.mustCall(t, "POST", "/mock/api/bots/"+itoa(bot.ID)+"/clients", `{"first_name":"Иван"}`, ""))

	return &e2eFixture{m: m, stand: s, bot: bot, token: bot.Token, chatID: created.Dialog.ChatID}
}

// subscribe подписывает стенд фикстуры на перечисленные типы событий.
func (f *e2eFixture) subscribe(t *testing.T, updateTypes []string) {
	t.Helper()
	body, err := json.Marshal(wire.SubscriptionRequestBody{
		URL: f.stand.srv.URL, Secret: secret, UpdateTypes: updateTypes,
	})
	if err != nil {
		t.Fatal(err)
	}
	res := decode[wire.SimpleQueryResult](t, f.m.mustCall(t, "POST", "/subscriptions", string(body), f.token))
	if !res.Success {
		t.Fatalf("подписка не создана: %+v", res)
	}
}

// action выполняет действие тестировщика через control-API — то же самое,
// что нажатие кнопки в веб-чате.
func (f *e2eFixture) action(t *testing.T, chatID int64, body string) []byte {
	t.Helper()
	return f.m.mustCall(t, "POST", dialogActions(chatID), body, "")
}

// botSend отправляет сообщение от имени бота, как это делает КЦ через Bot
// API.
func (f *e2eFixture) botSend(t *testing.T, chatID int64, body string) wire.Message {
	t.Helper()
	return decode[wire.SendMessageResult](t,
		f.m.mustCall(t, "POST", "/messages?chat_id="+itoa(chatID), body, f.token)).Message
}

// botAnswer отвечает на нажатие кнопки от имени бота.
func (f *e2eFixture) botAnswer(t *testing.T, callbackID, body string) {
	t.Helper()
	f.m.mustCall(t, "POST", "/answers?callback_id="+callbackID, body, f.token)
}

// lastBotMessageMid возвращает mid последнего сообщения бота в диалоге — то
// есть с клавиатурой. В диалоге вперемешку лежат и сообщения клиента (в т.ч.
// команда из меню), поэтому фильтр по отправителю обязателен: без него можно
// схватить mid не той стороны.
func (f *e2eFixture) lastBotMessageMid(t *testing.T, chatID int64) string {
	t.Helper()
	list := decode[wire.MessageList](t,
		f.m.mustCall(t, "GET", "/messages?chat_id="+itoa(chatID), "", f.token))
	for _, msg := range list.Messages {
		if msg.Sender != nil && msg.Sender.IsBot {
			return msg.Body.Mid
		}
	}
	t.Fatal("в диалоге нет сообщений от бота")
	return ""
}

// dialogEvents читает ленту событий диалога.
func (f *e2eFixture) dialogEvents(t *testing.T, chatID int64) []store.DialogEvent {
	t.Helper()
	return decode[[]store.DialogEvent](t,
		f.m.mustCall(t, "GET", "/mock/api/dialogs/"+itoa(chatID)+"/events?limit=200", "", ""))
}

func dialogActions(chatID int64) string {
	return "/mock/api/dialogs/" + itoa(chatID) + "/actions"
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// uploadFile заливает файл на выданный моком адрес и возвращает токен
// вложения — так же, как это делает КЦ.
func uploadFile(t *testing.T, url, filename, contentType string, content []byte) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("data", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()

	resp, err := http.Post(url, mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("заливка файла: %d %s", resp.StatusCode, raw)
	}

	// Для изображений ответ — словарь photos, как того требует
	// PhotoAttachmentRequestPayload.
	var photos struct {
		Photos map[string]wire.PhotoToken `json:"photos"`
	}
	if err := json.Unmarshal(raw, &photos); err == nil && len(photos.Photos) > 0 {
		for _, p := range photos.Photos {
			return p.Token
		}
	}
	var uploaded wire.UploadedInfo
	if err := json.Unmarshal(raw, &uploaded); err != nil || uploaded.Token == "" {
		t.Fatalf("ответ заливки не содержит токена: %s", raw)
	}
	return uploaded.Token
}

func fetch(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("скачивание %s: %d", url, resp.StatusCode)
	}
	return body
}
