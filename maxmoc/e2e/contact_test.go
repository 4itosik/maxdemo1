package e2e

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// createdClient — ответ служебного эндпоинта создания клиента.
type createdClient struct {
	Client store.Client `json:"client"`
	Dialog store.Dialog `json:"dialog"`
}

// dialogAction выполняет действие клиента в диалоге — то же, что делает
// веб-чат тестировщика.
func (m *mock) dialogAction(t *testing.T, chatID int64, body string) []byte {
	t.Helper()
	return m.mustCall(t, "POST", "/mock/api/dialogs/"+strconv.FormatInt(chatID, 10)+"/actions", body, "")
}

// awaitAttachment дожидается message_created с вложением нужного типа.
//
// Отдельно от stand.await, потому что тому достаточно типа события, а здесь
// событий одного типа несколько и различаются они вложением.
func (s *stand) awaitAttachment(t *testing.T, attType string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		for _, ev := range s.received {
			if ev["update_type"] != wire.UpdateMessageCreated {
				continue
			}
			message, _ := ev["message"].(map[string]any)
			body, _ := message["body"].(map[string]any)
			attachments, _ := body["attachments"].([]any)
			for _, raw := range attachments {
				att, _ := raw.(map[string]any)
				if att["type"] == attType {
					s.mu.Unlock()
					return ev
				}
			}
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("событие с вложением %s не пришло за 5 секунд; получены: %v", attType, s.types())
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestBotAsksContactClientShares — сценарий, который у КЦ первый в очереди:
// бот просит телефон кнопкой, абонент делится, КЦ связывает пришедший
// контакт со своей записью по user_id.
//
// Обе стороны обмена проверяются по контракту, как и в основном сценарии.
func TestBotAsksContactClientShares(t *testing.T) {
	m := newMock(t)
	s := newStand(t, m.specs)

	bot := decode[store.Bot](t, m.mustCall(t, "POST", "/mock/api/bots",
		`{"name":"Бот поддержки","username":"support_bot"}`, ""))
	token := bot.Token
	m.mustCall(t, "POST", "/subscriptions", `{"url":"`+s.srv.URL+`","secret":"`+secret+`"}`, token)

	// --- Клиент с карточкой: КЦ задаёт user_id своего тестового абонента. ---
	const knownUserID = 1234567890123
	bots := "/mock/api/bots/" + strconv.FormatInt(bot.ID, 10) + "/clients"
	created := decode[createdClient](t, m.mustCall(t, "POST", bots,
		`{"user_id":`+strconv.Itoa(knownUserID)+`,"first_name":"Пётр","last_name":"Сидоров",`+
			`"phone":"+79001234567","latitude":55.751244,"longitude":37.618423}`, ""))
	if created.Client.UserID != knownUserID {
		t.Fatalf("заданный user_id не применён: %d", created.Client.UserID)
	}
	chat := created.Dialog.ChatID

	// Занятый user_id обязан отвергаться: подмена порвала бы связку с записью
	// абонента молча.
	if status, _ := m.call(t, "POST", bots,
		`{"user_id":`+strconv.Itoa(knownUserID)+`,"first_name":"Двойник"}`, ""); status != http.StatusConflict {
		t.Errorf("повторный user_id: статус %d, ожидался 409", status)
	}

	// --- Диалог начат: фамилия обязана доехать уже здесь. ---
	m.dialogAction(t, chat, `{"action":"start"}`)
	started := s.await(t, wire.UpdateBotStarted)
	if user, _ := started["user"].(map[string]any); user["last_name"] != "Сидоров" {
		t.Errorf("фамилия не доехала в bot_started: %v", user)
	}

	// --- Бот просит контакт кнопкой request_contact. ---
	// `link` передаётся явным null: контракт объявляет его одновременно
	// required и nullable.
	m.mustCall(t, "POST", "/messages?chat_id="+strconv.FormatInt(chat, 10),
		`{"text":"Поделитесь номером","link":null,"attachments":[{"type":"inline_keyboard","payload":{"buttons":[[`+
			`{"type":"request_contact","text":"Отправить телефон"}]]}}]}`, token)

	// --- Клиент делится: ровно то, что делает нажатие кнопки в веб-чате. ---
	m.dialogAction(t, chat, `{"action":"send_contact"}`)

	ev := s.awaitAttachment(t, "contact")
	message, _ := ev["message"].(map[string]any)
	sender, _ := message["sender"].(map[string]any)
	if int64(sender["user_id"].(float64)) != knownUserID {
		t.Errorf("sender.user_id = %v, ожидался %d", sender["user_id"], knownUserID)
	}

	body, _ := message["body"].(map[string]any)
	attachments, _ := body["attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("контакт обязан быть единственным вложением, их %d", len(attachments))
	}
	payload, _ := attachments[0].(map[string]any)["payload"].(map[string]any)

	// Номер уезжает нормализованным, как на проде: только цифры, с семёрки.
	if vcf, _ := payload["vcf_info"].(string); !strings.Contains(vcf, "79001234567") {
		t.Errorf("телефона нет в vcf_info: %q", vcf)
	}
	maxInfo, _ := payload["max_info"].(map[string]any)
	if maxInfo == nil {
		t.Fatal("max_info пуст — КЦ нечем связать контакт с абонентом")
	}
	if int64(maxInfo["user_id"].(float64)) != knownUserID {
		t.Errorf("max_info.user_id = %v, ожидался %d", maxInfo["user_id"], knownUserID)
	}
	if hash, _ := payload["hash"].(string); hash == "" {
		t.Error("hash пуст: это означало бы «номер не привязан к аккаунту Max»")
	}
	// Форма запроса не должна протечь в событие.
	for _, leaked := range []string{"vcf_phone", "contact_id", "name"} {
		if _, ok := payload[leaked]; ok {
			raw, _ := json.Marshal(payload)
			t.Errorf("в событии поле формы запроса %q: %s", leaked, raw)
		}
	}

	// --- Геопозиция: координаты берутся из карточки. ---
	m.dialogAction(t, chat, `{"action":"send_location"}`)
	ev = s.awaitAttachment(t, "location")
	message, _ = ev["message"].(map[string]any)
	body, _ = message["body"].(map[string]any)
	attachments, _ = body["attachments"].([]any)
	att, _ := attachments[0].(map[string]any)
	if att["latitude"] != 55.751244 || att["longitude"] != 37.618423 {
		t.Errorf("координаты не из карточки: %v, %v", att["latitude"], att["longitude"])
	}
}

// Кнопка message шлёт обычное сообщение с её текстом: поля, в котором до
// бота доехал бы токен кнопки, контракт не предусматривает.
func TestMessageButtonSendsPlainText(t *testing.T) {
	m := newMock(t)
	s := newStand(t, m.specs)

	bot := decode[store.Bot](t, m.mustCall(t, "POST", "/mock/api/bots",
		`{"name":"Бот","username":"bot"}`, ""))
	m.mustCall(t, "POST", "/subscriptions", `{"url":"`+s.srv.URL+`","secret":"`+secret+`"}`, bot.Token)

	created := decode[createdClient](t, m.mustCall(t, "POST",
		"/mock/api/bots/"+strconv.FormatInt(bot.ID, 10)+"/clients", `{"first_name":"Иван"}`, ""))

	m.dialogAction(t, created.Dialog.ChatID, `{"action":"send","text":"Оформить заявку"}`)

	ev := s.await(t, wire.UpdateMessageCreated)
	message, _ := ev["message"].(map[string]any)
	body, _ := message["body"].(map[string]any)
	if body["text"] != "Оформить заявку" {
		t.Errorf("текст кнопки не доехал: %v", body["text"])
	}
}
