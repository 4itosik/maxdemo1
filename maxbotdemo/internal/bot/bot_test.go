package bot

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"maxbotdemo/internal/maxapi"
)

// sentMessage — запись об одном вызове SendMessage.
type sentMessage struct {
	To   maxapi.Target
	Body maxapi.NewMessageBody
}

// sentAnswer — запись об одном вызове AnswerOnCallback.
type sentAnswer struct {
	CallbackID string
	Answer     maxapi.CallbackAnswer
}

// fakeSender подменяет клиент Max API и запоминает исходящие вызовы.
type fakeSender struct {
	messages []sentMessage
	answers  []sentAnswer
	err      error
}

func (f *fakeSender) SendMessage(_ context.Context, to maxapi.Target, body maxapi.NewMessageBody) (maxapi.Message, error) {
	f.messages = append(f.messages, sentMessage{To: to, Body: body})
	return maxapi.Message{}, f.err
}

func (f *fakeSender) AnswerOnCallback(_ context.Context, callbackID string, a maxapi.CallbackAnswer) error {
	f.answers = append(f.answers, sentAnswer{CallbackID: callbackID, Answer: a})
	return f.err
}

func newTestBot() (*Bot, *fakeSender) {
	sender := &fakeSender{}
	return New(sender, slog.New(slog.NewTextHandler(io.Discard, nil))), sender
}

// textMessage строит событие message_created с заданным текстом.
func textMessage(text string) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageCreated,
		Message: &maxapi.Message{
			Sender:    &maxapi.User{UserID: 42, FirstName: "Иван"},
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body:      maxapi.MessageBody{Text: text},
		},
	}
}

// keyboardOf достаёт кнопки из вложений сообщения.
func keyboardOf(t *testing.T, body maxapi.NewMessageBody) [][]maxapi.Button {
	t.Helper()
	for _, a := range body.Attachments {
		if a.Type == maxapi.AttachmentInlineKeyboard && a.Payload != nil {
			return a.Payload.Buttons
		}
	}
	return nil
}

func TestBotStartedGreetsByNameWithKeyboard(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{
		UpdateType: maxapi.UpdateBotStarted,
		ChatID:     777,
		User:       &maxapi.User{UserID: 42, FirstName: "Иван"},
	})

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	got := sender.messages[0]
	if got.To != (maxapi.Target{ChatID: 777, UserID: 42}) {
		t.Errorf("адресат = %+v, want chat_id=777 user_id=42", got.To)
	}
	if !strings.Contains(got.Body.Text, "Иван") {
		t.Errorf("текст = %q, want обращение по имени", got.Body.Text)
	}
	if keyboardOf(t, got.Body) == nil {
		t.Error("к приветствию не приложена клавиатура")
	}
}

func TestStartCommandGreetsWithKeyboard(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("/start"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	if keyboardOf(t, sender.messages[0].Body) == nil {
		t.Error("к ответу на /start не приложена клавиатура")
	}
}

func TestHelpCommandListsAllCommands(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("/help"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	for _, cmd := range Commands() {
		if !strings.Contains(text, cmd.Name) {
			t.Errorf("в справке нет команды %q; текст: %q", cmd.Name, text)
		}
	}
}

func TestButtonsCommandSendsKeyboard(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("/buttons"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	rows := keyboardOf(t, sender.messages[0].Body)
	if len(rows) == 0 {
		t.Fatal("клавиатура пуста")
	}

	var hasCallback, hasLink bool
	for _, row := range rows {
		for _, btn := range row {
			switch btn.Type {
			case maxapi.ButtonCallback:
				hasCallback = true
				if btn.Payload == "" {
					t.Errorf("у callback-кнопки %q пустой payload", btn.Text)
				}
			case maxapi.ButtonLink:
				hasLink = true
				if btn.URL == "" {
					t.Errorf("у link-кнопки %q пустой url", btn.Text)
				}
			}
		}
	}
	if !hasCallback {
		t.Error("в клавиатуре нет callback-кнопки")
	}
	if !hasLink {
		t.Error("в клавиатуре нет link-кнопки")
	}
}

func TestUnknownCommandFallsBackToEcho(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("/такойкомандынет"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Body.Text, "/такойкомандынет") {
		t.Errorf("текст = %q, want эхо исходного текста", sender.messages[0].Body.Text)
	}
}

func TestPlainTextIsEchoed(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("привет"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Body.Text, "привет") {
		t.Errorf("текст = %q, want эхо %q", sender.messages[0].Body.Text, "привет")
	}
}

func TestMessageWithoutTextIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage(""))

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0", len(sender.messages))
	}
}

const vcardIvan = "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Иван Петров\r\nTEL;TYPE=CELL:+79991234567\r\nEND:VCARD\r\n"

// contactMessage строит message_created с вложением contact и без текста —
// именно так выглядит ответ клиента на кнопку request_contact.
func contactMessage(payload maxapi.ContactPayload) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageCreated,
		Message: &maxapi.Message{
			Sender:    &maxapi.User{UserID: 42, FirstName: "Иван"},
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body: maxapi.MessageBody{
				Attachments: []maxapi.Attachment{{
					Type:    maxapi.AttachmentContact,
					Payload: &payload,
				}},
			},
		},
	}
}

func TestContactIsAnsweredWithPhoneNameAndUserID(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), contactMessage(maxapi.ContactPayload{
		VcfInfo: vcardIvan,
		Hash:    "9f2c",
		MaxInfo: &maxapi.User{UserID: 42, FirstName: "Иван", LastName: "Петров"},
	}))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	for _, want := range []string{
		"+79991234567",
		"Иван Петров",
		"user_id: 42",
		"Номер привязан к аккаунту Max: да",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("текст = %q, want подстроку %q", text, want)
		}
	}
}

func TestContactWithoutPhoneIsRefused(t *testing.T) {
	b, sender := newTestBot()

	// Имя и user_id на месте, номера нет — а он здесь и есть предмет.
	b.Handle(context.Background(), contactMessage(maxapi.ContactPayload{
		VcfInfo: "BEGIN:VCARD\r\nFN:Иван Петров\r\nEND:VCARD\r\n",
		MaxInfo: &maxapi.User{UserID: 42, FirstName: "Иван"},
	}))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if !strings.Contains(text, "номера телефона в нём нет") {
		t.Errorf("текст = %q, want отбойник про отсутствие номера", text)
	}
	if strings.Contains(text, "Телефон") || strings.Contains(text, "user_id") {
		t.Errorf("текст = %q, want только отбойник без отчёта", text)
	}
}

func TestContactWithoutMaxInfoOmitsUserID(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), contactMessage(maxapi.ContactPayload{VcfInfo: vcardIvan}))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if !strings.Contains(text, "+79991234567") {
		t.Errorf("текст = %q, want номер телефона", text)
	}
	if strings.Contains(text, "user_id") {
		t.Errorf("текст = %q, want без строки user_id: max_info не пришёл", text)
	}
	if !strings.Contains(text, "Номер привязан к аккаунту Max: нет") {
		t.Errorf("текст = %q, want отметку «нет»: hash пуст", text)
	}
}

// callbackUpdate строит событие нажатия кнопки с заданным payload.
func callbackUpdate(payload string) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageCallback,
		Callback: &maxapi.Callback{
			CallbackID: "cb-1",
			Payload:    payload,
			User:       &maxapi.User{UserID: 42, FirstName: "Иван"},
		},
		Message: &maxapi.Message{
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body:      maxapi.MessageBody{Text: "выберите"},
		},
	}
}

// MAX присылает в событии только payload кнопки — подписи там нет.
// Человеку показываем подпись, поэтому соответствие хранит сам бот.
func TestCallbackIsAnsweredWithButtonLabel(t *testing.T) {
	rows := demoKeyboard().Attachment().Payload.Buttons

	for _, row := range rows {
		for _, btn := range row {
			if btn.Type != maxapi.ButtonCallback {
				continue
			}
			t.Run(btn.Payload, func(t *testing.T) {
				b, sender := newTestBot()

				b.Handle(context.Background(), callbackUpdate(btn.Payload))

				if len(sender.answers) != 1 {
					t.Fatalf("ответов на callback: %d, want 1", len(sender.answers))
				}
				got := sender.answers[0]
				if got.CallbackID != "cb-1" {
					t.Errorf("callback_id = %q, want %q", got.CallbackID, "cb-1")
				}
				if got.Answer.Message == nil {
					t.Fatal("Answer.Message = nil, want изменённое сообщение")
				}
				if !strings.Contains(got.Answer.Message.Text, btn.Text) {
					t.Errorf("текст = %q, want подпись кнопки %q",
						got.Answer.Message.Text, btn.Text)
				}
				if strings.Contains(got.Answer.Message.Text, btn.Payload) {
					t.Errorf("текст = %q, want без служебного payload %q",
						got.Answer.Message.Text, btn.Payload)
				}
				if len(sender.messages) != 0 {
					t.Errorf("на callback отправлено %d обычных сообщений, want 0",
						len(sender.messages))
				}
			})
		}
	}
}

func TestCallbackWithUnknownPayloadFallsBackToPayload(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), callbackUpdate("кнопка-из-старого-сообщения"))

	if len(sender.answers) != 1 {
		t.Fatalf("ответов на callback: %d, want 1", len(sender.answers))
	}
	text := sender.answers[0].Answer.Message.Text
	if !strings.Contains(text, "кнопка-из-старого-сообщения") {
		t.Errorf("текст = %q, want сам payload, раз подпись неизвестна", text)
	}
}

func TestCallbackWithoutIDIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{
		UpdateType: maxapi.UpdateMessageCallback,
		Callback:   nil,
	})

	if len(sender.answers) != 0 {
		t.Errorf("ответов на callback: %d, want 0", len(sender.answers))
	}
}

func TestUnknownUpdateTypeIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{UpdateType: "chat_title_changed"})

	if len(sender.messages) != 0 || len(sender.answers) != 0 {
		t.Errorf("на неизвестное событие бот ответил: %d сообщений, %d ответов",
			len(sender.messages), len(sender.answers))
	}
}

func TestCommandsAreValidForAPI(t *testing.T) {
	cmds := Commands()
	if len(cmds) == 0 {
		t.Fatal("Commands() пуст")
	}
	for _, c := range cmds {
		if strings.HasPrefix(c.Name, "/") {
			t.Errorf("имя команды %q не должно начинаться со слэша", c.Name)
		}
		if c.Description == "" {
			t.Errorf("у команды %q нет описания", c.Name)
		}
	}
}

// buttonsByType собирает подписи кнопок клавиатуры по типам.
func buttonsByType(rows [][]maxapi.Button) map[string]string {
	byType := make(map[string]string)
	for _, row := range rows {
		for _, btn := range row {
			byType[btn.Type] = btn.Text
		}
	}
	return byType
}

func TestPhoneCommandAsksForContact(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("/phone"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	got := buttonsByType(keyboardOf(t, sender.messages[0].Body))
	if _, ok := got[maxapi.ButtonRequestContact]; !ok {
		t.Errorf("кнопки клавиатуры: %v, want кнопку %q",
			got, maxapi.ButtonRequestContact)
	}
}

func TestLocationCommandAsksForGeoLocation(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("/location"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	got := buttonsByType(keyboardOf(t, sender.messages[0].Body))
	if _, ok := got[maxapi.ButtonRequestGeoLocation]; !ok {
		t.Errorf("кнопки клавиатуры: %v, want кнопку %q",
			got, maxapi.ButtonRequestGeoLocation)
	}
}

func TestDemoKeyboardHasEveryButtonType(t *testing.T) {
	got := buttonsByType(demoKeyboard().Attachment().Payload.Buttons)

	for _, want := range []string{
		maxapi.ButtonCallback,
		maxapi.ButtonLink,
		maxapi.ButtonRequestContact,
		maxapi.ButtonRequestGeoLocation,
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("в демо-клавиатуре нет кнопки типа %q; есть: %v", want, got)
		}
	}
}

// Раскладка обоснована в demoKeyboard: Max рекомендует не длиннее 10 символов
// на подпись при двух кнопках в ряду, а у обеих кнопок-запросов подпись
// длиннее — поэтому каждая должна занимать ряд одна.
// TestDemoKeyboardHasEveryButtonType эту раскладку не проверяет: его хелпер
// схлопывает клавиатуру в map[type]text и о рядах ничего не знает.
func TestDemoKeyboardRequestButtonsAreEachAloneInTheirRow(t *testing.T) {
	rows := demoKeyboard().Attachment().Payload.Buttons

	for _, want := range []string{maxapi.ButtonRequestContact, maxapi.ButtonRequestGeoLocation} {
		found := false
		for _, row := range rows {
			for _, btn := range row {
				if btn.Type != want {
					continue
				}
				found = true
				if len(row) != 1 {
					t.Errorf("кнопка типа %q делит ряд с %d другими кнопками, want ряд только с ней",
						want, len(row)-1)
				}
			}
		}
		if !found {
			t.Errorf("в демо-клавиатуре нет кнопки типа %q", want)
		}
	}
}

// Подпись кнопки не должна разъезжаться между отдельной клавиатурой команды и
// демонстрационной — иначе человек увидит две разные кнопки для одного и того
// же действия.
func TestRequestButtonLabelsMatchAcrossKeyboards(t *testing.T) {
	demo := buttonsByType(demoKeyboard().Attachment().Payload.Buttons)

	b, sender := newTestBot()
	b.Handle(context.Background(), textMessage("/phone"))
	b.Handle(context.Background(), textMessage("/location"))
	if len(sender.messages) != 2 {
		t.Fatalf("отправлено сообщений: %d, want 2", len(sender.messages))
	}

	phone := buttonsByType(keyboardOf(t, sender.messages[0].Body))
	location := buttonsByType(keyboardOf(t, sender.messages[1].Body))

	if phone[maxapi.ButtonRequestContact] != demo[maxapi.ButtonRequestContact] {
		t.Errorf("подпись кнопки контакта: %q в /phone и %q в демо-клавиатуре",
			phone[maxapi.ButtonRequestContact], demo[maxapi.ButtonRequestContact])
	}
	if location[maxapi.ButtonRequestGeoLocation] != demo[maxapi.ButtonRequestGeoLocation] {
		t.Errorf("подпись кнопки геопозиции: %q в /location и %q в демо-клавиатуре",
			location[maxapi.ButtonRequestGeoLocation], demo[maxapi.ButtonRequestGeoLocation])
	}
}

// locationMessage строит message_created с вложением location и без текста.
func locationMessage(lat, lon float64) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageCreated,
		Message: &maxapi.Message{
			Sender:    &maxapi.User{UserID: 42, FirstName: "Иван"},
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body: maxapi.MessageBody{
				Attachments: []maxapi.Attachment{{
					Type:      maxapi.AttachmentLocation,
					Latitude:  &lat,
					Longitude: &lon,
				}},
			},
		},
	}
}

func TestLocationIsAnsweredWithCoordinates(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), locationMessage(55.751244, 37.618423))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	for _, want := range []string{"Широта: 55.751244", "Долгота: 37.618423"} {
		if !strings.Contains(text, want) {
			t.Errorf("текст = %q, want подстроку %q", text, want)
		}
	}
}

// %v напечатал бы мелкую координату как 1e-07 — человеку это не координата.
func TestLocationCoordinatesArePrintedWithoutExponent(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), locationMessage(0.0000001, -0.0000002))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if strings.Contains(text, "e-") || strings.Contains(text, "e+") {
		t.Errorf("текст = %q, want координаты без экспоненты", text)
	}
}

func TestLocationWithoutCoordinatesIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{
		UpdateType: maxapi.UpdateMessageCreated,
		Message: &maxapi.Message{
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body: maxapi.MessageBody{
				Attachments: []maxapi.Attachment{{Type: maxapi.AttachmentLocation}},
			},
		},
	})

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0", len(sender.messages))
	}
}

// botAddedUpdate строит событие bot_added: сообщения в нём нет, адресат лежит
// в chat_id верхнего уровня, а user — это тот, кто добавил бота.
func botAddedUpdate(user *maxapi.User) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateBotAdded,
		ChatID:     555,
		User:       user,
	}
}

func TestBotAddedGreetsChatAndNamesWhoAdded(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), botAddedUpdate(
		&maxapi.User{UserID: 42, FirstName: "Иван", LastName: "Петров"}))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	got := sender.messages[0]
	if got.To.ChatID != 555 {
		t.Errorf("chat_id адресата = %d, want 555: приветствие идёт в чат", got.To.ChatID)
	}
	if !strings.Contains(got.Body.Text, "Иван Петров") {
		t.Errorf("текст = %q, want имя добавившего", got.Body.Text)
	}
	if !strings.Contains(got.Body.Text, "/help") {
		t.Errorf("текст = %q, want справку по командам", got.Body.Text)
	}
	if keyboardOf(t, got.Body) == nil {
		t.Error("к приветствию в чат не приложена клавиатура")
	}
}

// Поле user в контракте обязательное, но бот на чужие гарантии не полагается:
// без имени приветствие должно остаться осмысленным.
func TestBotAddedWithoutUserStillGreets(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), botAddedUpdate(nil))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Body.Text, "демонстрационный бот") {
		t.Errorf("текст = %q, want приветствие без имени", sender.messages[0].Body.Text)
	}
}

func TestBotStoppedIsOnlyLogged(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{
		UpdateType: maxapi.UpdateBotStopped,
		ChatID:     777,
		User:       &maxapi.User{UserID: 42, FirstName: "Иван"},
	})

	if len(sender.messages) != 0 || len(sender.answers) != 0 {
		t.Errorf("на bot_stopped отправлено %d сообщений и %d ответов, want 0 и 0: пользователь бота остановил",
			len(sender.messages), len(sender.answers))
	}
}

// Sender() возвращает указатель, и он бывает пустым: обращение к полю без
// проверки уронило бы обработчик. DisplayName() от этого защищён, доступ к
// UserID — нет.
func TestBotStoppedWithoutUserDoesNotPanic(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{
		UpdateType: maxapi.UpdateBotStopped,
		ChatID:     777,
	})

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0", len(sender.messages))
	}
}

// Дополняет TestBotStoppedIsOnlyLogged: тот проверяет «Only» — что бот ничего
// не отправляет, — но не «Logged». Вся фича bot_stopped — одна строка лога;
// убери b.log.Info в handleBotStopped, и набор тестов остался бы зелёным без
// этой проверки.
func TestBotStoppedIsLoggedWithChatAndUser(t *testing.T) {
	got := handleWithLog(t, maxapi.Update{
		UpdateType: maxapi.UpdateBotStopped,
		ChatID:     777,
		User:       &maxapi.User{UserID: 42, FirstName: "Иван"},
	})

	if !strings.Contains(got, "пользователь остановил бота") {
		t.Errorf("лог = %s, want запись об остановке бота", got)
	}
	if !strings.Contains(got, "chat_id=777") {
		t.Errorf("лог = %s, want chat_id=777", got)
	}
	if !strings.Contains(got, "user_id=42") {
		t.Errorf("лог = %s, want user_id=42", got)
	}
}

// humanUser и botUser — отправители события message_edited. Разделены не для
// красоты: правки самого бота приходят обратно тем же типом события.
func humanUser() *maxapi.User { return &maxapi.User{UserID: 42, FirstName: "Иван"} }
func botUser() *maxapi.User {
	return &maxapi.User{UserID: 1, FirstName: "Демобот", IsBot: true}
}

// editedMessage строит событие message_edited: в нём приезжает полный объект
// сообщения — уже с новым телом.
func editedMessage(text string, sender *maxapi.User) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageEdited,
		Message: &maxapi.Message{
			Sender:    sender,
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body:      maxapi.MessageBody{MID: "mid-1", Text: text},
		},
	}
}

func TestEditedMessageIsAnsweredWithCorrection(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("Привет, бот", humanUser()))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if !strings.Contains(text, "исправили") {
		t.Errorf("текст = %q, want отметку о правке", text)
	}
	if !strings.Contains(text, "Привет, бот") {
		t.Errorf("текст = %q, want новый текст сообщения", text)
	}
}

// POST /answers заменяет сообщение с кнопками — то, которое отправил сам бот, —
// и это возвращается боту как message_edited. Без проверки is_bot каждое
// нажатие кнопки давало бы лишний ответ «Вы исправили: Вы нажали: …».
func TestEditedOwnMessageIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("Вы нажали: Привет", botUser()))

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0: это правка собственного сообщения бота",
			len(sender.messages))
	}
}

func TestEditedMessageWithoutTextIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("", humanUser()))

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0", len(sender.messages))
	}
}

// sender в контракте необязателен, и «отправителя нет» — не то же самое, что
// «пишет человек»: пост в канале приходит от имени канала. Отвечать на такую
// правку значило бы отвечать в канал на собственное сообщение.
func TestEditedMessageWithoutSenderIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("Вы нажали: Привет", nil))

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0: отправитель события неизвестен",
			len(sender.messages))
	}
}

// Правка не перезапускает команду: показать нужно саму правку, иначе она
// неотличима от нового сообщения.
func TestEditedCommandIsEchoedNotExecuted(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("/help", humanUser()))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if !strings.Contains(text, "исправили") || !strings.Contains(text, "/help") {
		t.Errorf("текст = %q, want эхо правки", text)
	}
	if strings.Contains(text, "Доступные команды") {
		t.Errorf("текст = %q, want без справки: правка команду не выполняет", text)
	}
}

// handleWithLog прогоняет событие через бота и возвращает лог обработки.
// Уровень Debug обязателен: ветка default пишет именно на нём.
func handleWithLog(t *testing.T, u maxapi.Update) string {
	t.Helper()

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	New(&fakeSender{}, log).Handle(context.Background(), u)
	return buf.String()
}

// Список подписки и ветки Handle обязаны совпадать: подписка на необработанное
// событие даёт шум, обработчик без подписки не сработает ни разу.
func TestEveryUpdateTypeIsHandled(t *testing.T) {
	types := UpdateTypes()
	if len(types) == 0 {
		t.Fatal("UpdateTypes() пуст")
	}

	for _, ut := range types {
		t.Run(ut, func(t *testing.T) {
			// Событие намеренно пустое: проверяется маршрутизация, а не ответ.
			// Ни один обработчик не должен на этом падать.
			got := handleWithLog(t, maxapi.Update{UpdateType: ut})
			if strings.Contains(got, "событие без обработчика") {
				t.Errorf("тип %q ушёл в default; лог: %s", ut, got)
			}
		})
	}
}

// Контрольный случай: без него проверка выше проходила бы для любого типа,
// замолчи ветка default или изменись её текст.
func TestUnhandledUpdateTypeReachesDefault(t *testing.T) {
	got := handleWithLog(t, maxapi.Update{UpdateType: "chat_title_changed"})

	if !strings.Contains(got, "событие без обработчика") {
		t.Errorf("лог = %s, want запись о необработанном событии", got)
	}
}

// allUpdateTypes — полный список типов событий Max: discriminator.mapping
// объекта Update в max-openapi-official.json. Литерал, а не константы из
// maxapi: сверяться нужно с контрактом, а не с тем, что уже объявлено в
// пакете, — иначе тест ничего не поймает, если константу для нового типа
// забыли завести.
var allUpdateTypes = []string{
	"bot_added",
	"bot_started",
	"bot_stopped",
	"bot_removed",
	"chat_title_changed",
	"dialog_cleared",
	"dialog_muted",
	"dialog_unmuted",
	"dialog_removed",
	"message_callback",
	"message_created",
	"message_edited",
	"message_removed",
	"user_added",
	"user_removed",
}

// TestEveryUpdateTypeIsHandled ловит «подписались на необработанное» — но
// переживает мутацию «убрали тип из UpdateTypes(), оставив ветку в Handle»:
// тип просто выпадает из перебора, и ветка, которая по-прежнему обрабатывает
// его, никого не беспокоит. Это встречная проверка: для каждого типа полного
// реестра, которого нет в UpdateTypes(), событие обязано уходить в default.
// Такая мутация уронит именно этот тест.
func TestTypesOutsideSubscriptionReachDefault(t *testing.T) {
	subscribed := make(map[string]bool)
	for _, ut := range UpdateTypes() {
		subscribed[ut] = true
	}

	for _, ut := range allUpdateTypes {
		if subscribed[ut] {
			continue
		}
		t.Run(ut, func(t *testing.T) {
			got := handleWithLog(t, maxapi.Update{UpdateType: ut})
			if !strings.Contains(got, "событие без обработчика") {
				t.Errorf("тип %q не в UpdateTypes(), но в default не ушёл; лог: %s", ut, got)
			}
		})
	}
}
