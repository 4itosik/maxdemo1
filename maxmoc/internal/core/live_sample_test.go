package core

import (
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
	"time"

	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// Форма события, сверенная с записью живого Max.
//
// Образец — message_created, которым пользователь ответил на кнопку
// request_contact:
//
//	{"update_type":"message_created","timestamp":1786079712219,
//	 "message":{"recipient":{"chat_id":442209522,"chat_type":"dialog","user_id":271648516},
//	  "timestamp":1786079712219,
//	  "body":{"mid":"mid.000000001a5b94f2019fdaa593db1d43","seq":117052520019991875,
//	          "text":"","attachments":[{"type":"contact","payload":{…}}]},
//	  "sender":{"user_id":174756854,"first_name":"Артём","last_name":"",
//	            "is_bot":false,"last_activity_time":1786079712000,"name":"Артём"}},
//	 "user_locale":"ru"}
//
// Тесты ниже закрепляют те его черты, в которых мок от прода расходился.
// Расхождение в моке опаснее, чем кажется: клиент КЦ пишется против мока, а
// работать будет против прода.

// digest достаёт из webhook-события карту по цепочке ключей.
func digest(t *testing.T, ev map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := ev
	for _, key := range path {
		next, ok := cur[key].(map[string]any)
		if !ok {
			t.Fatalf("в событии нет объекта %q: %v", key, cur[key])
		}
		cur = next
	}
	return cur
}

// bareClient заводит клиента с одним лишь именем и телефоном: без фамилии и
// без username — то есть в том виде, в каком пользователь приехал в образце.
func bareClient(t *testing.T, f *fixture) int64 {
	t.Helper()
	_, d, err := f.core.CreateClient(f.bot.ID, store.ClientInput{
		FirstName: "Артём", Phone: "+79639986193",
	})
	if err != nil {
		t.Fatal(err)
	}
	return d.ChatID
}

// createdEvent отправляет контакт от лица клиента и возвращает единственное
// событие message_created — сообщение без текста, но с вложением.
func createdEvent(t *testing.T, f *fixture, chatID int64) map[string]any {
	t.Helper()
	if _, err := f.core.ClientSendContact(chatID); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()
	evs := f.stand.ofType(wire.UpdateMessageCreated)
	if len(evs) != 1 {
		t.Fatalf("событий message_created: %d, ожидалось 1", len(evs))
	}
	return evs[0]
}

// Сообщение без текста живой Max отдаёт с пустой строкой. Клиент, который
// зовёт text.trim() или text.length, обязан вести себя на моке так же, как на
// проде, — а null и "" разводят его поведение.
func TestMessageWithoutTextCarriesEmptyString(t *testing.T) {
	f := newFixture(t)
	body := digest(t, createdEvent(t, f, bareClient(t, f)), "message", "body")

	text, ok := body["text"]
	if !ok {
		t.Fatal("в теле сообщения нет поля text")
	}
	if text != "" {
		t.Errorf("text = %#v, а живой Max присылает пустую строку", text)
	}
}

// У пользователя без публичного имени живой Max поле username не присылает
// вовсе. Спека объявляет его обязательным, то есть прод нарушает собственный
// контракт: клиент, чья модель сгенерирована из спеки, на таком событии
// упадёт. Мок обязан показывать это, а не подставлять null.
func TestUserWithoutUsernameOmitsField(t *testing.T) {
	f := newFixture(t)
	ev := createdEvent(t, f, bareClient(t, f))

	sender := digest(t, ev, "message", "sender")
	if v, ok := sender["username"]; ok {
		t.Errorf("sender.username = %#v, а живой Max поле опускает", v)
	}
	// max_info во вложении описывает того же пользователя той же схемой —
	// разъехаться они не должны.
	maxInfo := digest(t, ev, "message", "body")["attachments"].([]any)[0].(map[string]any)
	maxInfo = digest(t, maxInfo, "payload", "max_info")
	if v, ok := maxInfo["username"]; ok {
		t.Errorf("max_info.username = %#v, а живой Max поле опускает", v)
	}
}

// Фамилию живой Max присылает всегда, даже пустую: `"last_name":""`.
func TestUserCarriesLastNameEvenWhenEmpty(t *testing.T) {
	f := newFixture(t)
	sender := digest(t, createdEvent(t, f, bareClient(t, f)), "message", "sender")

	lastName, ok := sender["last_name"]
	if !ok {
		t.Fatal("в sender нет поля last_name")
	}
	if lastName != "" {
		t.Errorf("last_name = %#v, ожидалась пустая строка", lastName)
	}
}

// Для ботов контракт фамилию не описывает («Для ботов это поле не
// возвращается»), и присылать её вместе с клиентской нельзя.
func TestBotUserHasNoLastName(t *testing.T) {
	f := newFixture(t)
	raw, err := json.Marshal(botUser(f.bot))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if v, ok := fields["last_name"]; ok {
		t.Errorf("у бота есть last_name = %#v", v)
	}
}

// Карточка контакта с прода, собранная для того же клиента, что заводит
// bareClient. Сверяется целиком: порядок строк, регистр параметра TYPE и
// PRODID — всё это части формата, по которым разбирают карточку на стороне КЦ.
func TestContactVcardMatchesLiveFormat(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.ClientSendContact(bareClient(t, f))
	if err != nil {
		t.Fatal(err)
	}
	p := contactPayload(t, msg)

	const want = "BEGIN:VCARD\r\n" +
		"VERSION:3.0\r\n" +
		"PRODID:ez-vcard 0.10.3\r\n" +
		"TEL;TYPE=cell:79639986193\r\n" +
		"FN:Артём\r\n" +
		"END:VCARD\r\n"
	if p.VcfInfo == nil {
		t.Fatal("vcf_info пуст")
	}
	if *p.VcfInfo != want {
		t.Errorf("карточка разошлась с продом:\nполучено: %q\nобразец:  %q", *p.VcfInfo, want)
	}
}

// В образце с прода hash — 64 hex-символа (полный SHA-256); мок обрезал его
// вдвое. Клиент, у которого под это поле заведена колонка char(64), на моке
// молча получал бы половину.
func TestContactHashIsFullLength(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.ClientSendContact(bareClient(t, f))
	if err != nil {
		t.Fatal(err)
	}
	p := contactPayload(t, msg)

	if p.Hash == nil {
		t.Fatal("hash пуст")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(*p.Hash) {
		t.Errorf("hash = %q, ожидались 64 hex-символа", *p.Hash)
	}
}

// Телефон живой Max отдаёт в карточке нормализованным: только цифры, с семёрки.
func TestPhoneNormalization(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"+79639986193", "79639986193"},
		{"79639986193", "79639986193"},
		{"89639986193", "79639986193"},
		{"+7 (963) 998-61-93", "79639986193"},
		{"8919331122", "8919331122"},      // не 11 цифр — трогать нечего
		{"+380501234567", "380501234567"}, // не российский — только чистка
	} {
		if got := normalizePhone(tc.in); got != tc.want {
			t.Errorf("normalizePhone(%q) = %q, ожидалось %q", tc.in, got, tc.want)
		}
	}
}

// Клавиатура из образца: ровно та, что уходит в POST /messages, и ровно та,
// что прод возвращает в теле ответа. Отличие одно — `quick` у геокнопки.
const (
	keyboardSent = `{"buttons":[
		[{"type":"callback","text":"Привет","payload":"hello"},
		 {"type":"callback","text":"Пока","payload":"bye"}],
		[{"type":"request_contact","text":"Поделиться номером"}],
		[{"type":"request_geo_location","text":"Отправить геопозицию"}],
		[{"type":"link","text":"Документация","url":"https://dev.max.ru/docs-api"}]]}`

	keyboardReturnedByProd = `{"buttons":[
		[{"payload":"hello","text":"Привет","type":"callback"},
		 {"payload":"bye","text":"Пока","type":"callback"}],
		[{"text":"Поделиться номером","type":"request_contact"}],
		[{"text":"Отправить геопозицию","quick":false,"type":"request_geo_location"}],
		[{"url":"https://dev.max.ru/docs-api","text":"Документация","type":"link"}]]}`
)

// Прод достраивает у кнопки request_geo_location умолчание `quick: false` —
// контракт объявляет его через `default`, и в теле сообщения оно
// материализовано. Остальные кнопки возвращаются как присланы: у них поля
// `quick` в схеме нет вовсе.
//
// Сверяется вся клавиатура целиком, разобранная в структуры: порядок ключей
// внутри объекта ни на что не влияет, а вот лишнее или потерянное поле — да.
func TestKeyboardMatchesProdRoundTrip(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, wire.NewMessageBody{
		Text:        wire.Ptr("Нажмите любую кнопку:"),
		Attachments: []wire.AttachmentRequest{{Type: wire.AttachmentInlineKeyboard, Payload: []byte(keyboardSent)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Body.Attachments) != 1 {
		t.Fatalf("вложений: %d, ожидалось 1", len(msg.Body.Attachments))
	}

	var got, want any
	if err := json.Unmarshal(msg.Body.Attachments[0].Payload, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(keyboardReturnedByProd), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		t.Errorf("клавиатура разошлась с продом:\nполучено: %s\nобразец:  %s", gotJSON, wantJSON)
	}
}

// В образце message_callback с прода timestamp события и timestamp самого
// нажатия совпадают (оба 1786081600397) — как и в message_created, событие
// описывает только что случившееся действие.
//
// Проверяется на серии по той же причине, что и в message_created: два
// обращения к часам расходятся только на границе миллисекунды.
func TestCallbackEventTimestampMatchesCallback(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, keyboardBody("Нажмите любую кнопку:", "bye"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := f.core.ClientPressButton(f.chatID, msg.Body.Mid, "bye"); err != nil {
			t.Fatal(err)
		}
	}
	f.disp.Wait()

	evs := f.stand.ofType(wire.UpdateMessageCallback)
	if len(evs) != 50 {
		t.Fatalf("событий message_callback: %d, ожидалось 50", len(evs))
	}
	for i, ev := range evs {
		outer, ok := ev["timestamp"].(float64)
		if !ok {
			t.Fatalf("событие %d: timestamp = %#v", i, ev["timestamp"])
		}
		inner, ok := digest(t, ev, "callback")["timestamp"].(float64)
		if !ok {
			t.Fatalf("событие %d: timestamp нажатия = %#v", i, digest(t, ev, "callback")["timestamp"])
		}
		if outer != inner {
			t.Fatalf("событие %d: timestamp события = %d, нажатия = %d",
				i, int64(outer), int64(inner))
		}
	}
}

// Живой Max не присылает боту message_edited на правку, которую бот сделал
// сам. Замер: нажатие кнопки на проде, ответ через POST /answers, лог
// подержан после ответа — пришёл только `200 {"success":true}`, события не
// было. Контракт говорит то же: «Вы получите это событие, как только
// **пользователь** отредактирует сообщение».
//
// Это то же правило, что и для message_created: собственное действие бота ему
// не возвращается, иначе отвечающий бот зациклится на своём эхо. Веб-чат
// событие получает всегда — он играет роль клиента и обязан видеть, что текст
// сообщения изменился.
func TestBotOwnEditDoesNotReachStand(t *testing.T) {
	f := newFixture(t)
	ch, unsubscribe := f.bus.Subscribe(f.bot.ID)
	defer unsubscribe()

	msg, err := f.core.SendMessage(f.bot, &f.client.UserID, nil, keyboardBody("Нажмите любую кнопку:", "bye"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.core.ClientPressButton(f.chatID, msg.Body.Mid, "bye"); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()
	callbackID := f.stand.ofType(wire.UpdateMessageCallback)[0]["callback"].(map[string]any)["callback_id"].(string)

	answer := wire.CallbackAnswer{Message: &wire.NewMessageBody{Text: wire.Ptr("Вы нажали: Пока")}}
	if err := f.core.AnswerCallback(f.bot, callbackID, answer); err != nil {
		t.Fatal(err)
	}
	// Прямая правка — тот же случай: действие бота, а не пользователя.
	if err := f.core.EditMessage(f.bot, msg.Body.Mid, wire.NewMessageBody{Text: wire.Ptr("Заявка принята")}); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if got := f.stand.ofType(wire.UpdateMessageEdited); len(got) != 0 {
		t.Errorf("боту доставлено %d событий message_edited о его же правках, want 0", len(got))
	}

	// Правки при этом применились и видны веб-чату.
	updated, err := f.core.GetMessageByID(f.bot, msg.Body.Mid)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body.Text == nil || *updated.Body.Text != "Заявка принята" {
		t.Errorf("текст сообщения не изменён: %v", updated.Body.Text)
	}
	edits := 0
	for {
		select {
		case <-ch:
			edits++
			continue
		default:
		}
		break
	}
	// Отправка, нажатие и две правки — веб-чат обязан увидеть каждое звено.
	if edits < 4 {
		t.Errorf("веб-чат получил %d событий, ожидалось не меньше 4", edits)
	}
}

// А правку клиента боту доставлять надо — ровно её контракт и описывает.
func TestClientEditReachesStand(t *testing.T) {
	f := newFixture(t)
	msg, err := f.core.ClientSendMessage(f.chatID, "было", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.core.ClientEditMessage(f.chatID, msg.Body.Mid, "стало"); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	if got := f.stand.ofType(wire.UpdateMessageEdited); len(got) != 1 {
		t.Errorf("боту доставлено %d событий message_edited о правке клиента, want 1", len(got))
	}
}

// У бота, в отличие от пользователя, живой Max отдаёт last_activity_time с
// миллисекундами: в ответе POST /messages 1786081137409 при timestamp
// сообщения 1786081137396.
//
// Момент выбирается заведомо внутри секунды и с запасом до её конца: на
// круглой миллисекунде округление неотличимо от его отсутствия.
func TestBotLastActivityKeepsMilliseconds(t *testing.T) {
	f := newFixture(t)
	var before int64
	for {
		before = time.Now().UnixMilli()
		if ms := before % 1000; ms != 0 && ms < 900 {
			break
		}
	}
	if last := botUser(f.bot).LastActivityTime; last < before {
		t.Errorf("last_activity_time бота = %d, а часы показывали уже %d — время округлено", last, before)
	}
}

// У пользователя живой Max отдаёт last_activity_time с точностью до секунды:
// 1786079712000 и 1786081137000 в двух образцах.
func TestLastActivityTimeIsWholeSeconds(t *testing.T) {
	f := newFixture(t)
	sender := digest(t, createdEvent(t, f, bareClient(t, f)), "message", "sender")

	last, ok := sender["last_activity_time"].(float64)
	if !ok {
		t.Fatalf("last_activity_time = %#v", sender["last_activity_time"])
	}
	if int64(last)%1000 != 0 {
		t.Errorf("last_activity_time = %d — не округлён до секунды", int64(last))
	}
}

// В образце timestamp события и timestamp сообщения совпадают: событие
// message_created описывает только что созданное сообщение, и расходиться им
// не на чем.
//
// Проверяется на серии: два независимых обращения к часам расходятся не
// каждый раз, а только когда между ними проходит граница миллисекунды, и на
// одном сообщении тест ничего не доказывал бы.
func TestCreatedEventTimestampMatchesMessage(t *testing.T) {
	f := newFixture(t)
	chatID := bareClient(t, f)
	for i := 0; i < 50; i++ {
		if _, err := f.core.ClientSendContact(chatID); err != nil {
			t.Fatal(err)
		}
	}
	f.disp.Wait()

	evs := f.stand.ofType(wire.UpdateMessageCreated)
	if len(evs) != 50 {
		t.Fatalf("событий message_created: %d, ожидалось 50", len(evs))
	}
	for i, ev := range evs {
		outer, ok := ev["timestamp"].(float64)
		if !ok {
			t.Fatalf("событие %d: timestamp = %#v", i, ev["timestamp"])
		}
		inner, ok := digest(t, ev, "message")["timestamp"].(float64)
		if !ok {
			t.Fatalf("событие %d: timestamp сообщения = %#v", i, digest(t, ev, "message")["timestamp"])
		}
		if outer != inner {
			t.Fatalf("событие %d: timestamp события = %d, сообщения = %d",
				i, int64(outer), int64(inner))
		}
	}
}
