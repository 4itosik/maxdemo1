package maxapi

import (
	"encoding/json"
	"testing"
)

// Тот же образец, что в gen/quicktype/models_test.go: тело message_created,
// записанное с живого Max. Оба варианта кодогенерации проверяются одним и тем
// же телом — иначе сравнивать нечего.
const liveMessageCreated = `{
  "update_type": "message_created",
  "timestamp": 1786079712219,
  "user_locale": "ru",
  "message": {
    "recipient": {"chat_id": 442209522, "chat_type": "dialog", "user_id": 271648516},
    "timestamp": 1786079712219,
    "body": {
      "mid": "mid.000000001a5b94f2019fdaa593db1d43",
      "seq": 117052520019991875,
      "text": "",
      "attachments": [
        {"type": "contact", "payload": {"vcf_info": "BEGIN:VCARD\nEND:VCARD", "max_info": {
          "user_id": 174756854, "first_name": "Артём", "is_bot": false,
          "last_activity_time": 1786079712000, "name": "Артём"}}}
      ]
    },
    "sender": {
      "user_id": 174756854, "first_name": "Артём", "last_name": "",
      "is_bot": false, "last_activity_time": 1786079712000, "name": "Артём"
    }
  }
}`

func TestДискриминаторРазбираетЖивоеТело(t *testing.T) {
	var u Update
	if err := json.Unmarshal([]byte(liveMessageCreated), &u); err != nil {
		t.Fatalf("разбор Update: %v", err)
	}

	// Диспетчеризация сгенерирована — в варианте quicktype это пишется руками.
	value, err := u.ValueByDiscriminator()
	if err != nil {
		t.Fatalf("ValueByDiscriminator: %v", err)
	}
	created, ok := value.(MessageCreatedUpdate)
	if !ok {
		t.Fatalf("получен %T, ожидался MessageCreatedUpdate", value)
	}

	if created.Message.Body.Mid != "mid.000000001a5b94f2019fdaa593db1d43" {
		t.Errorf("mid = %q", created.Message.Body.Mid)
	}
	// seq вне JSON-safe диапазона 2^53-1.
	if created.Message.Body.Seq != 117052520019991875 {
		t.Errorf("seq = %d", created.Message.Body.Seq)
	}
	if created.Message.Recipient.ChatId == nil || *created.Message.Recipient.ChatId != 442209522 {
		t.Errorf("chat_id = %v", created.Message.Recipient.ChatId)
	}
	if created.UserLocale == nil || *created.UserLocale != "ru" {
		t.Errorf("user_locale = %v", created.UserLocale)
	}
}

func TestВложениеЧерезСвойДискриминатор(t *testing.T) {
	var u Update
	if err := json.Unmarshal([]byte(liveMessageCreated), &u); err != nil {
		t.Fatal(err)
	}
	value, err := u.ValueByDiscriminator()
	if err != nil {
		t.Fatal(err)
	}
	created := value.(MessageCreatedUpdate)

	attachments := created.Message.Body.Attachments
	if attachments == nil || len(*attachments) != 1 {
		t.Fatalf("вложения = %v", attachments)
	}
	// У вложений свой дискриминатор — type, тоже сгенерированный.
	kind, err := (*attachments)[0].Discriminator()
	if err != nil {
		t.Fatalf("дискриминатор вложения: %v", err)
	}
	if kind != "contact" {
		t.Errorf("тип вложения = %q", kind)
	}
	contact, err := (*attachments)[0].AsContactAttachment()
	if err != nil {
		t.Fatalf("AsContactAttachment: %v", err)
	}
	if contact.Payload.VcfInfo == nil || *contact.Payload.VcfInfo == "" {
		t.Errorf("vcf_info = %v", contact.Payload.VcfInfo)
	}
}

func TestNullableПоляСтановятсяУказателями(t *testing.T) {
	var u Update
	if err := json.Unmarshal([]byte(liveMessageCreated), &u); err != nil {
		t.Fatal(err)
	}
	value, _ := u.ValueByDiscriminator()
	created := value.(MessageCreatedUpdate)

	if created.Message.Body.Text == nil {
		t.Fatal(`text: "" разобрался в nil, ожидался указатель на ""`)
	}
	if *created.Message.Body.Text != "" {
		t.Errorf("text = %q", *created.Message.Body.Text)
	}
}

func TestRoundTripСохраняетЗначения(t *testing.T) {
	var first Update
	if err := json.Unmarshal([]byte(liveMessageCreated), &first); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}
	var second Update
	if err := json.Unmarshal(encoded, &second); err != nil {
		t.Fatalf("повторный разбор: %v", err)
	}
	v1, err := first.ValueByDiscriminator()
	if err != nil {
		t.Fatal(err)
	}
	v2, err := second.ValueByDiscriminator()
	if err != nil {
		t.Fatal(err)
	}
	if v1.(MessageCreatedUpdate).Message.Body.Seq != v2.(MessageCreatedUpdate).Message.Body.Seq {
		t.Error("round-trip изменил seq")
	}
}
