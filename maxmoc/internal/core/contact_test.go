package core

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// fullClient заводит клиента с заполненной карточкой и возвращает его вместе
// с chat_id открытого диалога.
func fullClient(t *testing.T, f *fixture) (*store.Client, int64) {
	t.Helper()
	lat, lon := 55.751244, 37.618423
	cl, d, err := f.core.CreateClient(f.bot.ID, store.ClientInput{
		FirstName: "Пётр", LastName: "Сидоров", Username: "petr",
		Phone: "+79001234567", Latitude: &lat, Longitude: &lon,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cl, d.ChatID
}

// contactPayload достаёт полезную нагрузку единственного вложения-контакта.
func contactPayload(t *testing.T, msg *wire.Message) wire.ContactPayload {
	t.Helper()
	if len(msg.Body.Attachments) != 1 {
		t.Fatalf("вложений в сообщении: %d, ожидалось 1", len(msg.Body.Attachments))
	}
	att := msg.Body.Attachments[0]
	if att.Type != wire.AttachmentContact {
		t.Fatalf("тип вложения: %q", att.Type)
	}
	var p wire.ContactPayload
	if err := json.Unmarshal(att.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// Главный сценарий КЦ: бот попросил контакт, клиент поделился — в одном
// вложении должны приехать и телефон, и user_id, иначе связывать не с чем.
func TestClientSendContactCarriesPhoneAndUserID(t *testing.T) {
	f := newFixture(t)
	cl, chatID := fullClient(t, f)

	msg, err := f.core.ClientSendContact(chatID)
	if err != nil {
		t.Fatal(err)
	}
	p := contactPayload(t, msg)

	if p.VcfInfo == nil {
		t.Fatal("vcf_info пуст")
	}
	// Телефон уезжает нормализованным — как на проде, см. normalizePhone.
	if !strings.Contains(*p.VcfInfo, "79001234567") {
		t.Errorf("в VCARD нет телефона: %q", *p.VcfInfo)
	}
	if !strings.Contains(*p.VcfInfo, "Пётр Сидоров") {
		t.Errorf("в VCARD нет имени с фамилией: %q", *p.VcfInfo)
	}
	if !strings.HasPrefix(*p.VcfInfo, "BEGIN:VCARD") || !strings.Contains(*p.VcfInfo, "END:VCARD") {
		t.Errorf("VCARD не обрамлён: %q", *p.VcfInfo)
	}
	if p.MaxInfo == nil {
		t.Fatal("max_info пуст — КЦ нечем связать контакт с пользователем")
	}
	if p.MaxInfo.UserID != cl.UserID {
		t.Errorf("max_info.user_id = %d, у клиента %d", p.MaxInfo.UserID, cl.UserID)
	}
	if p.MaxInfo.LastName == nil || *p.MaxInfo.LastName != "Сидоров" {
		t.Errorf("фамилия не доехала в max_info: %+v", p.MaxInfo)
	}
	if p.Hash == nil || *p.Hash == "" {
		t.Error("hash пуст: это означало бы «номер не привязан к аккаунту Max»")
	}

	f.disp.Wait()
	if got := f.stand.ofType(wire.UpdateMessageCreated); len(got) != 1 {
		t.Fatalf("на стенд пришло %d событий message_created", len(got))
	}
}

// Хеш обязан быть повторяемым: тесты КЦ сравнивают события между прогонами.
func TestContactHashIsStable(t *testing.T) {
	f := newFixture(t)
	_, chatID := fullClient(t, f)

	first, err := f.core.ClientSendContact(chatID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.core.ClientSendContact(chatID)
	if err != nil {
		t.Fatal(err)
	}
	a, b := contactPayload(t, first), contactPayload(t, second)
	if a.Hash == nil || b.Hash == nil || *a.Hash != *b.Hash {
		t.Errorf("hash не повторяется: %v и %v", a.Hash, b.Hash)
	}
}

func TestClientSendContactWithoutPhoneIsRejected(t *testing.T) {
	f := newFixture(t)
	// Клиент из фикстуры заведён без телефона.
	_, err := f.core.ClientSendContact(f.chatID)
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("ожидался ErrBadRequest, получено: %v", err)
	}
	if !strings.Contains(err.Error(), "телефон") {
		t.Errorf("сообщение не объясняет причину: %v", err)
	}
}

func TestClientSendLocationFromCard(t *testing.T) {
	f := newFixture(t)
	_, chatID := fullClient(t, f)

	msg, err := f.core.ClientSendLocation(chatID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Body.Attachments) != 1 {
		t.Fatalf("вложений: %d", len(msg.Body.Attachments))
	}
	att := msg.Body.Attachments[0]
	if att.Type != wire.AttachmentLocation {
		t.Fatalf("тип вложения: %q", att.Type)
	}
	if att.Latitude == nil || *att.Latitude != 55.751244 || att.Longitude == nil || *att.Longitude != 37.618423 {
		t.Errorf("координаты не из карточки: %+v", att)
	}
}

func TestClientSendLocationWithExplicitCoords(t *testing.T) {
	f := newFixture(t)
	_, chatID := fullClient(t, f)

	lat, lon := 59.9386, 30.3141
	msg, err := f.core.ClientSendLocation(chatID, &lat, &lon)
	if err != nil {
		t.Fatal(err)
	}
	att := msg.Body.Attachments[0]
	if att.Latitude == nil || *att.Latitude != lat || att.Longitude == nil || *att.Longitude != lon {
		t.Errorf("явные координаты не применились: %+v", att)
	}
}

func TestClientSendLocationWithoutCoordsIsRejected(t *testing.T) {
	f := newFixture(t)
	// У клиента из фикстуры координат нет.
	if _, err := f.core.ClientSendLocation(f.chatID, nil, nil); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("ожидался ErrBadRequest, получено: %v", err)
	}
}

// Контакт от бота приходит в форме запроса, а в сообщении контракт ждёт
// другую форму. Без перевода стенд получит вложение, которого живой Max не
// пришлёт, — а валидация этого не поймает.
func TestBotContactIsConvertedToMessageForm(t *testing.T) {
	f := newFixture(t)
	cl, chatID := fullClient(t, f)

	payload, err := json.Marshal(wire.ContactRequestPayload{
		Name:      wire.Ptr("Пётр Сидоров"),
		ContactID: wire.Ptr(cl.UserID),
		VcfPhone:  wire.Ptr("+79001234567"),
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := f.core.SendMessage(f.bot, nil, &chatID, wire.NewMessageBody{
		Attachments: []wire.AttachmentRequest{{Type: wire.AttachmentContact, Payload: payload}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Верхний уровень payload должен содержать только поля формы сообщения.
	// Проверяем ключи, а не подстроки: `name` законно встречается внутри
	// max_info как поле схемы User.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(msg.Body.Attachments[0].Payload, &top); err != nil {
		t.Fatal(err)
	}
	for key := range top {
		switch key {
		case "vcf_info", "hash", "max_info":
		default:
			t.Errorf("в сообщении осталось поле формы запроса %q: %s", key, msg.Body.Attachments[0].Payload)
		}
	}
	p := contactPayload(t, msg)
	if p.VcfInfo == nil || !strings.Contains(*p.VcfInfo, "79001234567") {
		t.Errorf("телефон не переехал в VCARD: %v", p.VcfInfo)
	}
	if p.MaxInfo == nil || p.MaxInfo.UserID != cl.UserID {
		t.Errorf("contact_id известного клиента не развёрнут в max_info: %+v", p.MaxInfo)
	}
	// Webhook здесь не проверяется намеренно: собственные сообщения бота на
	// стенд не уходят.
}

// Неизвестный contact_id — не ошибка: контракт допускает контакт человека,
// не зарегистрированного в Max. Просто max_info остаётся пустым.
func TestBotContactWithUnknownContactID(t *testing.T) {
	f := newFixture(t)

	payload, err := json.Marshal(wire.ContactRequestPayload{
		Name:      wire.Ptr("Кто-то извне"),
		ContactID: wire.Ptr(int64(999999999)),
		VcfPhone:  wire.Ptr("+79990000000"),
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := f.core.SendMessage(f.bot, nil, &f.chatID, wire.NewMessageBody{
		Attachments: []wire.AttachmentRequest{{Type: wire.AttachmentContact, Payload: payload}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p := contactPayload(t, msg); p.MaxInfo != nil {
		t.Errorf("незнакомый контакт не должен получать max_info: %+v", p.MaxInfo)
	}
}

// Требование контракта: карточка контакта — единственное вложение сообщения.
func TestContactMustBeOnlyAttachment(t *testing.T) {
	f := newFixture(t)

	contact, err := json.Marshal(wire.ContactRequestPayload{Name: wire.Ptr("Пётр"), VcfPhone: wire.Ptr("+7900")})
	if err != nil {
		t.Fatal(err)
	}
	keyboard, err := json.Marshal(wire.InlineKeyboardPayload{
		Buttons: [][]wire.Button{{{Type: "callback", Text: "да", Payload: "yes"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.core.SendMessage(f.bot, nil, &f.chatID, wire.NewMessageBody{
		Attachments: []wire.AttachmentRequest{
			{Type: wire.AttachmentContact, Payload: contact},
			{Type: wire.AttachmentInlineKeyboard, Payload: keyboard},
		},
	})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("ожидался ErrBadRequest, получено: %v", err)
	}
}

// Фамилия обязана доезжать до КЦ во всех событиях, а не только в контакте.
func TestClientUserCarriesLastName(t *testing.T) {
	f := newFixture(t)
	_, chatID := fullClient(t, f)

	if err := f.core.ClientStart(chatID, ""); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	events := f.stand.ofType(wire.UpdateBotStarted)
	if len(events) != 1 {
		t.Fatalf("событий bot_started: %d", len(events))
	}
	user, _ := events[0]["user"].(map[string]any)
	if user["last_name"] != "Сидоров" {
		t.Errorf("last_name в bot_started: %v", user["last_name"])
	}
	if user["first_name"] != "Пётр" {
		t.Errorf("first_name в bot_started: %v", user["first_name"])
	}
}
