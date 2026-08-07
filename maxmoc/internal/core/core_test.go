package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"maxmock/internal/config"
	"maxmock/internal/events"
	"maxmock/internal/specs"
	"maxmock/internal/store"
	"maxmock/internal/webhook"
	"maxmock/internal/wire"
)

// stand — фейковый стенд КЦ: принимает webhook-и и проверяет каждое тело
// против контракта, чтобы ошибка формы всплывала прямо в тесте домена.
type stand struct {
	t      *testing.T
	specs  *specs.Specs
	srv    *httptest.Server
	mu     sync.Mutex
	events []map[string]any
}

func newStand(t *testing.T, sp *specs.Specs) *stand {
	s := &stand{t: t, specs: sp}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := sp.ValidateWebhookBody(context.Background(), body); err != nil {
			t.Errorf("стенд получил событие, не соответствующее контракту: %v\nтело: %s", err, body)
		}
		var ev map[string]any
		_ = json.Unmarshal(body, &ev)
		s.mu.Lock()
		s.events = append(s.events, ev)
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stand) received() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, len(s.events))
	copy(out, s.events)
	return out
}

func (s *stand) ofType(updateType string) []map[string]any {
	var out []map[string]any
	for _, ev := range s.received() {
		if ev["update_type"] == updateType {
			out = append(out, ev)
		}
	}
	return out
}

type fixture struct {
	core   *Core
	store  *store.Store
	disp   *webhook.Dispatcher
	bus    *events.Bus
	stand  *stand
	bot    *store.Bot
	client *store.Client
	chatID int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sp, err := specs.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Webhook = config.Webhook{TimeoutSec: 2, Retries: 0, BackoffSec: []int{0}}
	bus := events.New()
	disp, err := webhook.New(st, sp, bus, cfg.Webhook)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(disp.Close)

	c := New(st, disp, bus, cfg)
	bot, err := st.CreateBot("Бот поддержки", "support_bot", "демо")
	if err != nil {
		t.Fatal(err)
	}
	cl, d, err := c.CreateClient(bot.ID, store.ClientInput{FirstName: "Иван", Username: "ivan"})
	if err != nil {
		t.Fatal(err)
	}
	s := newStand(t, sp)
	if _, err := st.AddSubscription(bot.ID, s.srv.URL, nil, "секретное-слово"); err != nil {
		t.Fatal(err)
	}
	return &fixture{core: c, store: st, disp: disp, bus: bus, stand: s, bot: bot, client: cl, chatID: d.ChatID}
}

func keyboardBody(text string, payloads ...string) wire.NewMessageBody {
	row := make([]wire.Button, 0, len(payloads))
	for _, p := range payloads {
		row = append(row, wire.Button{Type: "callback", Text: p, Payload: p})
	}
	payload, _ := json.Marshal(wire.InlineKeyboardPayload{Buttons: [][]wire.Button{row}})
	return wire.NewMessageBody{
		Text:        wire.Ptr(text),
		Attachments: []wire.AttachmentRequest{{Type: wire.AttachmentInlineKeyboard, Payload: payload}},
	}
}

func TestSendMessageToUnknownRecipient(t *testing.T) {
	f := newFixture(t)
	unknown := int64(999999)
	if _, err := f.core.SendMessage(f.bot, &unknown, nil, wire.NewMessageBody{Text: wire.Ptr("привет")}); !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("ожидался ErrChatNotFound, получено %v", err)
	}
	if _, err := f.core.SendMessage(f.bot, nil, nil, wire.NewMessageBody{Text: wire.Ptr("привет")}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("без user_id и chat_id ожидался ErrBadRequest, получено %v", err)
	}
}

func TestSendMessageBuildsMessage(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, wire.NewMessageBody{Text: wire.Ptr("здравствуйте")})
	if err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if msg.Body.Mid == "" || msg.Body.Seq == 0 {
		t.Errorf("сообщению не назначены mid/seq: %+v", msg.Body)
	}
	if msg.Sender == nil || !msg.Sender.IsBot {
		t.Errorf("отправитель не бот: %+v", msg.Sender)
	}
	if msg.Recipient.UserID == nil || *msg.Recipient.UserID != f.client.UserID {
		t.Errorf("получатель не клиент: %+v", msg.Recipient)
	}
	if *msg.Recipient.ChatID != f.chatID || msg.Recipient.ChatType != wire.ChatTypeDialog {
		t.Errorf("получатель диалога: %+v", msg.Recipient)
	}
}

// Живой Max не присылает боту webhook о сообщениях, которые бот отправил сам:
// иначе любой отвечающий бот зациклился бы на собственном эхо. Веб-чат при
// этом обязан увидеть сообщение — он играет роль клиента.
func TestBotDoesNotReceiveWebhookForOwnMessage(t *testing.T) {
	f := newFixture(t)
	ch, unsubscribe := f.bus.Subscribe(f.bot.ID)
	defer unsubscribe()

	if _, err := f.core.SendMessage(f.bot, nil, &f.chatID, wire.NewMessageBody{Text: wire.Ptr("здравствуйте")}); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if got := f.stand.ofType(wire.UpdateMessageCreated); len(got) != 0 {
		t.Errorf("боту доставлено %d событий message_created о его же сообщении, want 0", len(got))
	}

	select {
	case ev := <-ch:
		if ev.ChatID != f.chatID {
			t.Errorf("веб-чат получил событие по чату %d, want %d", ev.ChatID, f.chatID)
		}
	default:
		t.Error("веб-чат не получил сообщение бота")
	}
}

// А сообщение клиента боту доставляется — ради этого мок и существует.
func TestClientMessageReachesBotWebhook(t *testing.T) {
	f := newFixture(t)

	if _, err := f.core.ClientSendMessage(f.chatID, "привет", nil); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if got := f.stand.ofType(wire.UpdateMessageCreated); len(got) != 1 {
		t.Fatalf("боту доставлено %d событий message_created, want 1", len(got))
	}
}

func TestSendMessageByChatID(t *testing.T) {
	f := newFixture(t)
	if _, err := f.core.SendMessage(f.bot, nil, &f.chatID, wire.NewMessageBody{Text: wire.Ptr("по chat_id")}); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	msgs, err := f.core.DialogMessages(f.chatID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Body.Text == nil || *msgs[0].Body.Text != "по chat_id" {
		t.Errorf("сообщение по chat_id не попало в диалог: %+v", msgs)
	}
}

func TestBotCannotTouchForeignDialog(t *testing.T) {
	f := newFixture(t)
	other, err := f.store.CreateBot("Другой", "other_bot", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.SendMessage(other, nil, &f.chatID, wire.NewMessageBody{Text: wire.Ptr("чужой")}); !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("чужой диалог должен быть невидим: %v", err)
	}
}

func TestButtonPressProducesCallback(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, keyboardBody("Подтвердите", "yes", "no"))
	if err != nil {
		t.Fatal(err)
	}

	// Нажать можно только существующую кнопку.
	if err := f.core.ClientPressButton(f.chatID, msg.Body.Mid, "выдуманная"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("несуществующая кнопка принята: %v", err)
	}
	if err := f.core.ClientPressButton(f.chatID, msg.Body.Mid, "yes"); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	callbacks := f.stand.ofType(wire.UpdateMessageCallback)
	if len(callbacks) != 1 {
		t.Fatalf("доставлено %d событий message_callback", len(callbacks))
	}
	cb, _ := callbacks[0]["callback"].(map[string]any)
	if cb["payload"] != "yes" {
		t.Errorf("полезная нагрузка кнопки: %v", cb["payload"])
	}
	if cb["callback_id"] == "" || cb["callback_id"] == nil {
		t.Error("callback_id не заполнен")
	}
	if callbacks[0]["message"] == nil {
		t.Error("исходное сообщение не приложено к событию")
	}
}

func TestAnswerCallbackEditsMessage(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, keyboardBody("Подтвердите", "yes"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.core.ClientPressButton(f.chatID, msg.Body.Mid, "yes"); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	callbackID := f.stand.ofType(wire.UpdateMessageCallback)[0]["callback"].(map[string]any)["callback_id"].(string)

	// Ответ с телом заменяет текст и убирает клавиатуру.
	answer := wire.CallbackAnswer{Message: &wire.NewMessageBody{Text: wire.Ptr("Принято")}}
	if err := json.Unmarshal([]byte(`{"text":"Принято","attachments":[],"link":null}`), answer.Message); err != nil {
		t.Fatal(err)
	}
	if err := f.core.AnswerCallback(f.bot, callbackID, answer); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	updated, err := f.core.GetMessageByID(f.bot, msg.Body.Mid)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body.Text == nil || *updated.Body.Text != "Принято" {
		t.Errorf("текст не изменён: %v", updated.Body.Text)
	}
	if len(updated.Body.Attachments) != 0 {
		t.Errorf("attachments:[] должен был убрать клавиатуру: %+v", updated.Body.Attachments)
	}
	// На стенд правка не уходит: её сделал сам бот. Подробности и замер — в
	// TestBotOwnEditDoesNotReachStand.
	if n := len(f.stand.ofType(wire.UpdateMessageEdited)); n != 0 {
		t.Errorf("боту доставлено %d событий о его же правке, want 0", n)
	}

	if err := f.core.AnswerCallback(f.bot, "cb.несуществующий", wire.CallbackAnswer{}); !errors.Is(err, ErrCallbackNotFound) {
		t.Errorf("неизвестный callback_id: %v", err)
	}
}

func TestEditKeepsAttachmentsWhenNull(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, keyboardBody("Выберите", "a"))
	if err != nil {
		t.Fatal(err)
	}

	var edit wire.NewMessageBody
	if err := json.Unmarshal([]byte(`{"text":"Выберите вариант","attachments":null,"link":null}`), &edit); err != nil {
		t.Fatal(err)
	}
	if err := f.core.EditMessage(f.bot, msg.Body.Mid, edit); err != nil {
		t.Fatal(err)
	}

	updated, _ := f.core.GetMessageByID(f.bot, msg.Body.Mid)
	if *updated.Body.Text != "Выберите вариант" {
		t.Errorf("текст: %v", *updated.Body.Text)
	}
	if len(updated.Body.Attachments) != 1 {
		t.Errorf("attachments:null не должен трогать вложения: %+v", updated.Body.Attachments)
	}
}

func TestBotCannotEditOrDeleteClientMessage(t *testing.T) {
	f := newFixture(t)
	if err := f.core.ClientStart(f.chatID, ""); err != nil {
		t.Fatal(err)
	}
	msg, err := f.core.ClientSendMessage(f.chatID, "здравствуйте", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.core.DeleteMessage(f.bot, msg.Body.Mid); !errors.Is(err, ErrForbidden) {
		t.Errorf("бот удалил сообщение клиента: %v", err)
	}
	if err := f.core.EditMessage(f.bot, msg.Body.Mid, wire.NewMessageBody{Text: wire.Ptr("подмена")}); !errors.Is(err, ErrForbidden) {
		t.Errorf("бот отредактировал сообщение клиента: %v", err)
	}
}

func TestDeleteMessage(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, wire.NewMessageBody{Text: wire.Ptr("удалить")})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.core.DeleteMessage(f.bot, msg.Body.Mid); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if len(f.stand.ofType(wire.UpdateMessageRemoved)) != 1 {
		t.Error("message_removed не доставлен")
	}
	if _, err := f.core.GetMessageByID(f.bot, msg.Body.Mid); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("удалённое сообщение остаётся доступным: %v", err)
	}
	if err := f.core.DeleteMessage(f.bot, msg.Body.Mid); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
}

func TestClientStartOnlyOnce(t *testing.T) {
	f := newFixture(t)
	if err := f.core.ClientStart(f.chatID, "deeplink"); err != nil {
		t.Fatal(err)
	}
	if err := f.core.ClientStart(f.chatID, ""); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	started := f.stand.ofType(wire.UpdateBotStarted)
	if len(started) != 1 {
		t.Fatalf("bot_started доставлен %d раз, ожидался один", len(started))
	}
	if started[0]["payload"] != "deeplink" {
		t.Errorf("payload из диплинка потерян: %v", started[0]["payload"])
	}
	if int64(started[0]["chat_id"].(float64)) != f.chatID {
		t.Errorf("chat_id: %v", started[0]["chat_id"])
	}
}

func TestClientStopAndRestart(t *testing.T) {
	f := newFixture(t)
	if err := f.core.ClientStart(f.chatID, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.core.ClientStop(f.chatID); err != nil {
		t.Fatal(err)
	}
	// Повторная остановка молчит: выключенного бота нельзя выключить дважды.
	if err := f.core.ClientStop(f.chatID); err != nil {
		t.Fatal(err)
	}
	// А вот старт после остановки обязан сработать: контракт описывает
	// bot_started как событие, которое приходит, когда пользователь
	// «начнёт или возобновит общение с ботом».
	if err := f.core.ClientStart(f.chatID, ""); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	stopped := f.stand.ofType(wire.UpdateBotStopped)
	if len(stopped) != 1 {
		t.Fatalf("bot_stopped доставлен %d раз, ожидался один", len(stopped))
	}
	if int64(stopped[0]["chat_id"].(float64)) != f.chatID {
		t.Errorf("chat_id в bot_stopped: %v", stopped[0]["chat_id"])
	}
	user, ok := stopped[0]["user"].(map[string]any)
	if !ok {
		t.Fatalf("в bot_stopped нет пользователя: %v", stopped[0])
	}
	if int64(user["user_id"].(float64)) != f.client.UserID {
		t.Errorf("user_id в bot_stopped: %v", user["user_id"])
	}
	if len(f.stand.ofType(wire.UpdateBotStarted)) != 2 {
		t.Errorf("bot_started после возобновления: %d, ожидалось 2",
			len(f.stand.ofType(wire.UpdateBotStarted)))
	}
}

func TestClientEditAndDelete(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.ClientSendMessage(f.chatID, "первая версия", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.core.ClientEditMessage(f.chatID, msg.Body.Mid, "вторая версия"); err != nil {
		t.Fatal(err)
	}
	if err := f.core.ClientDeleteMessage(f.chatID, msg.Body.Mid); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if len(f.stand.ofType(wire.UpdateMessageEdited)) != 1 {
		t.Error("message_edited от клиента не доставлен")
	}
	if len(f.stand.ofType(wire.UpdateMessageRemoved)) != 1 {
		t.Error("message_removed от клиента не доставлен")
	}
	if err := f.core.ClientEditMessage(f.chatID, msg.Body.Mid, "поздно"); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("правка удалённого сообщения: %v", err)
	}
}

func TestClientEmptyMessageRejected(t *testing.T) {
	f := newFixture(t)
	if _, err := f.core.ClientSendMessage(f.chatID, "", nil); !errors.Is(err, ErrBadRequest) {
		t.Errorf("пустое сообщение принято: %v", err)
	}
}

func TestUploadAndAttach(t *testing.T) {
	f := newFixture(t)

	endpoint, err := f.core.CreateUpload(f.bot, wire.AttachmentFile)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Token == "" || endpoint.URL == "" {
		t.Fatalf("upload-эндпоинт неполон: %+v", endpoint)
	}
	if err := f.store.CompleteAttachment(endpoint.Token, "отчёт.pdf", "application/pdf", "/tmp/x", 2048); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(wire.UploadedInfo{Token: endpoint.Token})
	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, wire.NewMessageBody{
		Text:        wire.Ptr("документ"),
		Attachments: []wire.AttachmentRequest{{Type: wire.AttachmentFile, Payload: payload}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if len(msg.Body.Attachments) != 1 {
		t.Fatalf("вложение потеряно: %+v", msg.Body.Attachments)
	}
	att := msg.Body.Attachments[0]
	if att.Type != wire.AttachmentFile || att.Filename != "отчёт.pdf" || att.Size == nil || *att.Size != 2048 {
		t.Errorf("метаданные файла: %+v", att)
	}
	var media wire.MediaPayload
	if err := json.Unmarshal(att.Payload, &media); err != nil {
		t.Fatal(err)
	}
	if media.Token != endpoint.Token || media.URL == "" {
		t.Errorf("ссылка на файл: %+v", media)
	}

	// Неизвестный токен вложения — осмысленная доменная ошибка.
	bad, _ := json.Marshal(wire.UploadedInfo{Token: "att.нет"})
	if _, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, wire.NewMessageBody{
		Attachments: []wire.AttachmentRequest{{Type: wire.AttachmentFile, Payload: bad}},
	}); !errors.Is(err, ErrAttachmentNotFound) {
		t.Errorf("неизвестный токен: %v", err)
	}
}

func TestFailedSendLeavesNoGap(t *testing.T) {
	f := newFixture(t)
	bad, _ := json.Marshal(wire.UploadedInfo{Token: "att.нет"})
	if _, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, wire.NewMessageBody{
		Attachments: []wire.AttachmentRequest{{Type: wire.AttachmentFile, Payload: bad}},
	}); err == nil {
		t.Fatal("отправка с битым вложением должна падать")
	}
	if _, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, wire.NewMessageBody{Text: wire.Ptr("первое")}); err != nil {
		t.Fatal(err)
	}
	all, err := f.store.ListMessages(store.MessageFilter{ChatID: &f.chatID})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("неудачная отправка оставила запись в чате: %d сообщений вместо 1", len(all))
	}
}

func TestGetMessages(t *testing.T) {
	f := newFixture(t)
	for _, text := range []string{"раз", "два", "три"} {
		if _, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, wire.NewMessageBody{Text: wire.Ptr(text)}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := f.core.GetMessages(f.bot, store.MessageFilter{ChatID: &f.chatID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Messages) != 3 {
		t.Fatalf("получено %d сообщений", len(list.Messages))
	}
	if *list.Messages[0].Body.Text != "три" {
		t.Errorf("порядок не «новые первыми»: %v", *list.Messages[0].Body.Text)
	}

	unknown := int64(123456789)
	if _, err := f.core.GetMessages(f.bot, store.MessageFilter{ChatID: &unknown}); !errors.Is(err, ErrChatNotFound) {
		t.Errorf("неизвестный чат: %v", err)
	}
}

func TestSubscriptions(t *testing.T) {
	f := newFixture(t)
	if err := f.core.Subscribe(f.bot, wire.SubscriptionRequestBody{
		URL: "http://stand.local/hook", UpdateTypes: []string{wire.UpdateMessageCreated}, Secret: "abcdef",
	}); err != nil {
		t.Fatal(err)
	}
	list, err := f.core.Subscriptions(f.bot)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Subscriptions) != 2 { // одна создана фикстурой
		t.Fatalf("подписок: %d", len(list.Subscriptions))
	}
	if err := f.core.Unsubscribe(f.bot, "http://stand.local/hook"); err != nil {
		t.Fatal(err)
	}
	// Повторная отписка идемпотентна.
	if err := f.core.Unsubscribe(f.bot, "http://stand.local/hook"); err != nil {
		t.Errorf("повторная отписка должна проходить молча: %v", err)
	}
}

// TestEditMyCommandsNotifiesUI — меню команд в веб-чате рисуется из того, что
// бот опубликовал. Без события шины полоса обновлялась бы только перезагрузкой
// страницы, и «команды не появились» выглядело бы как дефект мока.
func TestEditMyCommandsNotifiesUI(t *testing.T) {
	f := newFixture(t)
	ch, cancel := f.bus.Subscribe(f.bot.ID)
	defer cancel()

	// Патч собирается разбором тела, а не литералом: признак «поле commands
	// пришло» у BotCommandsPatch неэкспортируемый и выставляется только в
	// UnmarshalJSON. Литерал дал бы CommandsSet() == false, EditMyCommands
	// вышел бы по ветке «ничего не менять», и тест падал бы не по делу.
	var patch wire.BotCommandsPatch
	if err := json.Unmarshal([]byte(
		`{"commands":[{"name":"start","description":"Начать разговор"}]}`), &patch); err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.EditMyCommands(f.bot, patch); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-ch:
		if ev.Kind != events.KindBot {
			t.Fatalf("вид события %q, ожидался %q", ev.Kind, events.KindBot)
		}
	case <-time.After(time.Second):
		t.Fatal("событие об изменении команд не опубликовано")
	}
}
