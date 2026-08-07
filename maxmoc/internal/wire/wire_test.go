package wire_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"maxmock/internal/specs"
	"maxmock/internal/wire"
)

const nowMS = int64(1722800000000)

func client() wire.User {
	return wire.User{
		UserID: 42, FirstName: "Клиент", Username: wire.Ptr("client"),
		IsBot: false, LastActivityTime: nowMS, Name: nil,
	}
}

func message(text *string) wire.Message {
	return wire.Message{
		Sender:    wire.Ptr(client()),
		Recipient: wire.Recipient{ChatID: wire.Ptr(int64(7)), ChatType: wire.ChatTypeDialog, UserID: wire.Ptr(int64(42))},
		Timestamp: nowMS,
		Body:      wire.MessageBody{Mid: "mid.000000000000000700000001", Seq: 1, Text: text},
	}
}

// Обязательные, но nullable поля обязаны присутствовать в JSON со значением
// null — иначе тело не пройдёт валидацию по контракту.
func TestRequiredNullableFieldsAreAlwaysPresent(t *testing.T) {
	b, err := json.Marshal(message(nil))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"text":null`, `"recipient"`, `"chat_id":7`, `"user_id":42`, `"name":null`} {
		if !strings.Contains(got, want) {
			t.Errorf("в JSON нет %s: %s", want, got)
		}
	}

	// username задан — null не подставляется.
	if strings.Contains(got, `"username":null`) {
		t.Errorf("username затёрт в null: %s", got)
	}
}

func TestNewMessageBodyDistinguishesNullAndEmptyAttachments(t *testing.T) {
	var withNull wire.NewMessageBody
	if err := json.Unmarshal([]byte(`{"text":"привет","attachments":null,"link":null}`), &withNull); err != nil {
		t.Fatal(err)
	}
	if withNull.AttachmentsSet() {
		t.Error("attachments:null распознан как заданный")
	}
	if withNull.Text == nil || *withNull.Text != "привет" {
		t.Errorf("text = %v", withNull.Text)
	}

	var withEmpty wire.NewMessageBody
	if err := json.Unmarshal([]byte(`{"text":null,"attachments":[],"link":null}`), &withEmpty); err != nil {
		t.Fatal(err)
	}
	if !withEmpty.AttachmentsSet() {
		t.Error("attachments:[] не распознан как заданный — правка не сможет удалить вложения")
	}
	if len(withEmpty.Attachments) != 0 {
		t.Errorf("attachments = %v", withEmpty.Attachments)
	}
	if withEmpty.Text != nil {
		t.Errorf("text:null должен давать nil, получено %v", withEmpty.Text)
	}

	var withItems wire.NewMessageBody
	raw := `{"text":null,"attachments":[{"type":"inline_keyboard","payload":{"buttons":[[{"type":"callback","text":"Да","payload":"yes"}]]}}],"link":null}`
	if err := json.Unmarshal([]byte(raw), &withItems); err != nil {
		t.Fatal(err)
	}
	if !withItems.AttachmentsSet() || len(withItems.Attachments) != 1 {
		t.Fatalf("вложения не разобраны: %+v", withItems.Attachments)
	}
	kb, err := withItems.Attachments[0].PayloadInlineKeyboard()
	if err != nil {
		t.Fatal(err)
	}
	if len(kb.Buttons) != 1 || kb.Buttons[0][0].Payload != "yes" {
		t.Fatalf("клавиатура разобрана неверно: %+v", kb)
	}
}

func TestNewMessageBodyMarshalKeepsRequiredFields(t *testing.T) {
	b, err := json.Marshal(wire.NewMessageBody{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"text":null`, `"attachments":null`, `"link":null`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("в JSON нет %s: %s", want, string(b))
		}
	}
}

// Каждое событие, которое умеет порождать мок, должно проходить валидацию
// против openapi.MaxBotWebhook.yaml — иначе диспетчер его не отправит.
func TestUpdatesValidateAgainstWebhookContract(t *testing.T) {
	s, err := specs.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	msg := message(wire.Ptr("привет"))

	updates := map[string]any{
		wire.UpdateMessageCreated: wire.MessageCreatedUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageCreated, Timestamp: nowMS},
			Message:    msg,
			UserLocale: wire.Ptr("ru"),
		},
		wire.UpdateMessageCallback: wire.MessageCallbackUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageCallback, Timestamp: nowMS},
			Callback: wire.Callback{
				Timestamp: nowMS, CallbackID: "cb.0123456789abcdef0123456789abcdef",
				Payload: "yes", User: client(),
			},
			Message: &msg,
		},
		wire.UpdateMessageCallback + "_без_сообщения": wire.MessageCallbackUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageCallback, Timestamp: nowMS},
			Callback: wire.Callback{
				Timestamp: nowMS, CallbackID: "cb.0123456789abcdef0123456789abcdef", User: client(),
			},
			Message: nil,
		},
		wire.UpdateMessageEdited: wire.MessageEditedUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageEdited, Timestamp: nowMS},
			Message:    msg,
		},
		wire.UpdateMessageRemoved: wire.MessageRemovedUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageRemoved, Timestamp: nowMS},
			MessageID:  msg.Body.Mid, ChatID: 7, UserID: 42,
		},
		wire.UpdateBotStarted: wire.BotStartedUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateBotStarted, Timestamp: nowMS},
			ChatID:     7, User: client(), UserLocale: wire.Ptr("ru"),
		},
	}

	for name, u := range updates {
		body, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := s.ValidateWebhookBody(ctx, body); err != nil {
			t.Errorf("%s не прошёл валидацию: %v\nтело: %s", name, err, body)
		}
	}
}

// Сообщение с вложениями и клавиатурой тоже должно быть контрактным.
func TestMessageWithAttachmentsValidates(t *testing.T) {
	s, err := specs.Load()
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := json.Marshal(wire.Keyboard{Buttons: [][]wire.Button{{
		{Type: "callback", Text: "Да", Payload: "yes"},
		{Type: "link", Text: "Сайт", URL: "https://example.ru"},
	}}})
	media, _ := json.Marshal(wire.MediaPayload{Token: "att.1", URL: "http://localhost:8080/mock/files/att.1"})

	msg := message(wire.Ptr("выберите"))
	msg.Body.Attachments = []wire.Attachment{
		{Type: wire.AttachmentInlineKeyboard, Payload: kb},
		{Type: wire.AttachmentFile, Payload: media, Filename: "отчёт.pdf", Size: wire.Ptr(int64(1024))},
	}
	u := wire.MessageCreatedUpdate{
		UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageCreated, Timestamp: nowMS},
		Message:    msg,
	}
	body, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateWebhookBody(context.Background(), body); err != nil {
		t.Fatalf("сообщение с вложениями не прошло валидацию: %v\nтело: %s", err, body)
	}
}
