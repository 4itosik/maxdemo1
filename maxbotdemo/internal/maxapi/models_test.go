package maxapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// JSON ниже повторяет форму событий из документации Max Bot API:
// у message_created адресат лежит в message.recipient, у bot_started —
// в полях верхнего уровня.

const messageCreatedJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000000,
  "message": {
    "sender": {"user_id": 42, "first_name": "Иван", "username": "ivan", "is_bot": false},
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "timestamp": 1754400000000,
    "body": {"mid": "mid.1", "seq": 1, "text": "привет"}
  },
  "user_locale": "ru-RU"
}`

const messageCallbackJSON = `{
  "update_type": "message_callback",
  "timestamp": 1754400000001,
  "callback": {
    "timestamp": 1754400000001,
    "callback_id": "cb-1",
    "payload": "hello",
    "user": {"user_id": 42, "first_name": "Иван", "username": "ivan", "is_bot": false}
  },
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "timestamp": 1754400000000,
    "body": {"mid": "mid.1", "seq": 1, "text": "нажми кнопку"}
  }
}`

const botStartedJSON = `{
  "update_type": "bot_started",
  "timestamp": 1754400000002,
  "chat_id": 777,
  "user": {"user_id": 42, "first_name": "Иван", "username": "ivan", "is_bot": false},
  "payload": "ref-123",
  "user_locale": "ru-RU"
}`

func decodeUpdate(t *testing.T, raw string) Update {
	t.Helper()
	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return u
}

func TestUpdateTargetFromMessageRecipient(t *testing.T) {
	u := decodeUpdate(t, messageCreatedJSON)

	if u.UpdateType != UpdateMessageCreated {
		t.Errorf("UpdateType = %q, want %q", u.UpdateType, UpdateMessageCreated)
	}
	got := u.Target()
	want := Target{ChatID: 777, UserID: 42}
	if got != want {
		t.Errorf("Target() = %+v, want %+v", got, want)
	}
}

func TestUpdateTargetFromTopLevelFields(t *testing.T) {
	u := decodeUpdate(t, botStartedJSON)

	got := u.Target()
	want := Target{ChatID: 777, UserID: 42}
	if got != want {
		t.Errorf("Target() = %+v, want %+v", got, want)
	}
}

func TestUpdateTargetOfCallbackUsesOriginalMessage(t *testing.T) {
	u := decodeUpdate(t, messageCallbackJSON)

	got := u.Target()
	want := Target{ChatID: 777, UserID: 42}
	if got != want {
		t.Errorf("Target() = %+v, want %+v", got, want)
	}
	if u.Callback == nil || u.Callback.CallbackID != "cb-1" || u.Callback.Payload != "hello" {
		t.Errorf("Callback = %+v, want callback_id=cb-1 payload=hello", u.Callback)
	}
}

func TestUpdateSender(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"из message.sender", messageCreatedJSON},
		{"из update.user", botStartedJSON},
		{"из callback.user", messageCallbackJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := decodeUpdate(t, tt.raw)
			s := u.Sender()
			if s == nil {
				t.Fatal("Sender() = nil, want пользователя")
			}
			if s.UserID != 42 || s.FirstName != "Иван" {
				t.Errorf("Sender() = %+v, want user_id=42 first_name=Иван", s)
			}
		})
	}
}

func TestUpdateTextOfMessage(t *testing.T) {
	u := decodeUpdate(t, messageCreatedJSON)

	if got := u.Text(); got != "привет" {
		t.Errorf("Text() = %q, want %q", got, "привет")
	}
}

func TestUpdateTextWithoutMessageIsEmpty(t *testing.T) {
	u := decodeUpdate(t, botStartedJSON)

	if got := u.Text(); got != "" {
		t.Errorf("Text() = %q, want пустую строку", got)
	}
}

func TestUpdateCommand(t *testing.T) {
	tests := []struct {
		text     string
		wantName string
		wantArgs string
		wantOK   bool
	}{
		{"/help", "help", "", true},
		{"/help меня", "help", "меня", true},
		{"/help   меня  срочно ", "help", "меня  срочно", true},
		{"/help:id-773", "help", "", true},
		{"/help:id-773 меня", "help", "меня", true},
		{"/START", "start", "", true},
		{"привет", "", "", false},
		{"не /команда", "", "", false},
		{"", "", "", false},
		{"/", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			u := Update{
				UpdateType: UpdateMessageCreated,
				Message:    &Message{Body: MessageBody{Text: tt.text}},
			}
			name, args, ok := u.Command()
			if ok != tt.wantOK || name != tt.wantName || args != tt.wantArgs {
				t.Errorf("Command() = (%q, %q, %v), want (%q, %q, %v)",
					name, args, ok, tt.wantName, tt.wantArgs, tt.wantOK)
			}
		})
	}
}

const contactMessageJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000003,
  "message": {
    "sender": {"user_id": 42, "first_name": "Иван", "is_bot": false},
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {
      "mid": "mid.2", "seq": 2, "text": null,
      "attachments": [{
        "type": "contact",
        "payload": {
          "vcf_info": "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Иван Петров\r\nTEL;TYPE=CELL:+79991234567\r\nEND:VCARD\r\n",
          "hash": "9f2c",
          "max_info": {"user_id": 42, "first_name": "Иван", "last_name": "Петров", "is_bot": false}
        }
      }]
    }
  }
}`

const locationMessageJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000004,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.3", "seq": 3, "text": null,
      "attachments": [{"type": "location", "latitude": 55.751244, "longitude": 37.618423}]}
  }
}`

// Нулевые координаты — валидная точка в Гвинейском заливе. Именно ради этого
// случая Latitude и Longitude объявлены указателями.
const locationZeroJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000005,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.4", "seq": 4, "text": null,
      "attachments": [{"type": "location", "latitude": 0, "longitude": 0}]}
  }
}`

const locationWithoutCoordsJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000006,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.5", "seq": 5, "text": null,
      "attachments": [{"type": "location"}]}
  }
}`

const keyboardMessageJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000007,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.6", "seq": 6, "text": "нажми",
      "attachments": [{"type": "inline_keyboard",
        "payload": {"buttons": [[{"type": "callback", "text": "Привет", "payload": "hello"}]]}}]}
  }
}`

func TestUpdateContact(t *testing.T) {
	u := decodeUpdate(t, contactMessageJSON)

	c := u.Contact()
	if c == nil {
		t.Fatal("Contact() = nil, want вложение контакта")
	}
	if !strings.Contains(c.VcfInfo, "+79991234567") {
		t.Errorf("VcfInfo = %q, want карточку с номером", c.VcfInfo)
	}
	if c.Hash != "9f2c" {
		t.Errorf("Hash = %q, want %q", c.Hash, "9f2c")
	}
	if c.MaxInfo == nil || c.MaxInfo.UserID != 42 {
		t.Errorf("MaxInfo = %+v, want user_id=42", c.MaxInfo)
	}
}

func TestUpdateContactIsNilWithoutContactAttachment(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"обычный текст без вложений", messageCreatedJSON},
		{"клавиатура", keyboardMessageJSON},
		{"геопозиция", locationMessageJSON},
		{"событие без сообщения", botStartedJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeUpdate(t, tt.raw).Contact(); got != nil {
				t.Errorf("Contact() = %+v, want nil", got)
			}
		})
	}
}

func TestUpdateLocation(t *testing.T) {
	l := decodeUpdate(t, locationMessageJSON).Location()

	if l == nil {
		t.Fatal("Location() = nil, want координаты")
	}
	if l.Latitude != 55.751244 || l.Longitude != 37.618423 {
		t.Errorf("Location() = %+v, want 55.751244/37.618423", l)
	}
}

func TestUpdateLocationAcceptsZeroCoordinates(t *testing.T) {
	l := decodeUpdate(t, locationZeroJSON).Location()

	if l == nil {
		t.Fatal("Location() = nil, want точку 0,0 — она валидна")
	}
	if l.Latitude != 0 || l.Longitude != 0 {
		t.Errorf("Location() = %+v, want 0/0", l)
	}
}

func TestUpdateLocationWithoutCoordinatesIsNil(t *testing.T) {
	if got := decodeUpdate(t, locationWithoutCoordsJSON).Location(); got != nil {
		t.Errorf("Location() = %+v, want nil: координат во вложении нет", got)
	}
}

func TestZeroUpdateIsSafe(t *testing.T) {
	var u Update

	if got := u.Text(); got != "" {
		t.Errorf("Text() = %q, want пустую строку", got)
	}
	if got := u.Sender(); got != nil {
		t.Errorf("Sender() = %+v, want nil", got)
	}
	if got := (u.Target()); got != (Target{}) {
		t.Errorf("Target() = %+v, want нулевой Target", got)
	}
	if _, _, ok := u.Command(); ok {
		t.Error("Command() вернул ok=true для пустого Update")
	}
	if got := u.Contact(); got != nil {
		t.Errorf("Contact() = %+v, want nil", got)
	}
	if got := u.Location(); got != nil {
		t.Errorf("Location() = %+v, want nil", got)
	}
}
