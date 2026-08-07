package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newBot(t *testing.T, s *Store) *Bot {
	t.Helper()
	b, err := s.CreateBot("Бот поддержки", "support_bot", "тестовый бот")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBotsCreateAndLookup(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)
	if b.ID == 0 || b.UserID <= 0 || b.Token == "" {
		t.Fatalf("бот создан без идентификаторов: %+v", b)
	}

	byToken, err := s.BotByToken(b.Token)
	if err != nil {
		t.Fatal(err)
	}
	if byToken.ID != b.ID {
		t.Errorf("BotByToken вернул другого бота: %+v", byToken)
	}

	byID, err := s.BotByID(b.ID)
	if err != nil || byID.Token != b.Token {
		t.Errorf("BotByID: %+v %v", byID, err)
	}

	if _, err := s.BotByToken("несуществующий"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ожидался ErrNotFound, получено %v", err)
	}

	bots, err := s.ListBots()
	if err != nil || len(bots) != 1 {
		t.Errorf("ListBots = %v, %v", bots, err)
	}
}

func TestDataSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b := newBot(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	got, err := again.BotByToken(b.Token)
	if err != nil {
		t.Fatalf("после перезапуска бот потерян: %v", err)
	}
	if got.Name != b.Name {
		t.Errorf("бот изменился: %+v", got)
	}
}

func TestClientCreatesDialog(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)

	c, d, err := s.CreateClient(b.ID, ClientInput{FirstName: "Иван", Username: "ivan"})
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID <= 0 || d.ChatID <= 0 {
		t.Fatalf("идентификаторы неположительны: %+v %+v", c, d)
	}
	if d.Started {
		t.Error("новый диалог не должен быть начат")
	}

	byUser, err := s.DialogByUser(b.ID, c.UserID)
	if err != nil || byUser.ChatID != d.ChatID {
		t.Errorf("DialogByUser: %+v %v", byUser, err)
	}
	byChat, err := s.DialogByChatID(d.ChatID)
	if err != nil || byChat.UserID != c.UserID {
		t.Errorf("DialogByChatID: %+v %v", byChat, err)
	}

	changed, err := s.SetDialogStarted(d.ChatID)
	if err != nil || !changed {
		t.Errorf("первый старт диалога: changed=%v err=%v", changed, err)
	}
	changed, err = s.SetDialogStarted(d.ChatID)
	if err != nil || changed {
		t.Error("повторный старт не должен менять состояние — иначе уйдёт второй bot_started")
	}

	clients, err := s.ListClients(b.ID)
	if err != nil || len(clients) != 1 {
		t.Errorf("ListClients = %v, %v", clients, err)
	}
	if _, err := s.ClientByUserID(c.UserID); err != nil {
		t.Errorf("ClientByUserID: %v", err)
	}
}

func TestMessageSeqGrowsWithinChat(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)
	_, d, _ := s.CreateClient(b.ID, ClientInput{FirstName: "Первый", Username: ""})

	prev := int64(0)
	for i := 0; i < 3; i++ {
		m := &Message{ChatID: d.ChatID, BotID: b.ID, Sender: SenderClient, Body: []byte(`{"text":"раз"}`)}
		if err := s.AppendMessage(m, nil); err != nil {
			t.Fatal(err)
		}
		if m.Seq <= prev {
			t.Fatalf("сообщение %d: seq = %d не превысил предыдущий %d", i, m.Seq, prev)
		}
		if m.Mid == "" {
			t.Fatal("mid не назначен")
		}
		prev = m.Seq
	}
}

// Формат seq у живого Max — время создания в старших битах (см. ids.Seq).
// Проверяем именно на записи хранилища: номер выдаётся здесь, и легко
// разъехаться с created_at, если присвоить его раньше времени.
func TestMessageSeqCarriesCreatedAt(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)
	_, d, _ := s.CreateClient(b.ID, ClientInput{FirstName: "Первый", Username: ""})

	m := &Message{ChatID: d.ChatID, BotID: b.ID, Sender: SenderClient,
		Body: []byte(`{"text":"раз"}`), CreatedAt: 1786079712219}
	if err := s.AppendMessage(m, nil); err != nil {
		t.Fatal(err)
	}
	if got := m.Seq >> 16; got != m.CreatedAt {
		t.Errorf("seq >> 16 = %d, а created_at = %d", got, m.CreatedAt)
	}
}

// Хранилище выдаёт номера отдельно по каждому чату. Ставим первому чату
// время далеко в будущем: при общей нумерации второй чат унаследовал бы
// разогнанный счётчик вместо собственного времени.
func TestMessageSeqIsPerChat(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)
	_, d1, _ := s.CreateClient(b.ID, ClientInput{FirstName: "Первый", Username: ""})
	_, d2, _ := s.CreateClient(b.ID, ClientInput{FirstName: "Второй", Username: ""})

	const base = 1786079712219
	for i := 0; i < 3; i++ {
		m := &Message{ChatID: d1.ChatID, BotID: b.ID, Sender: SenderClient,
			Body: []byte(`{"text":"раз"}`), CreatedAt: base + 10000}
		if err := s.AppendMessage(m, nil); err != nil {
			t.Fatal(err)
		}
	}

	m := &Message{ChatID: d2.ChatID, BotID: b.ID, Sender: SenderBot,
		Body: []byte(`{"text":"два"}`), CreatedAt: base}
	if err := s.AppendMessage(m, nil); err != nil {
		t.Fatal(err)
	}
	if got := m.Seq >> 16; got != base {
		t.Fatalf("нумерация чатов не независима: seq >> 16 = %d, ожидалось %d", got, base)
	}
}

func TestMessageFilters(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)
	_, d, _ := s.CreateClient(b.ID, ClientInput{FirstName: "Иван", Username: ""})

	var mids []string
	for i := 0; i < 5; i++ {
		m := &Message{ChatID: d.ChatID, BotID: b.ID, Sender: SenderClient,
			Body: []byte(`{"text":"сообщение"}`), CreatedAt: int64(1000 + i*10)}
		if err := s.AppendMessage(m, nil); err != nil {
			t.Fatal(err)
		}
		mids = append(mids, m.Mid)
	}

	all, err := s.ListMessages(MessageFilter{ChatID: &d.ChatID})
	if err != nil || len(all) != 5 {
		t.Fatalf("по чату: %d записей, %v", len(all), err)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Seq <= all[i].Seq {
			t.Errorf("порядок не «новые первыми»: seq %d стоит перед %d", all[i-1].Seq, all[i].Seq)
		}
	}

	limited, _ := s.ListMessages(MessageFilter{ChatID: &d.ChatID, Count: 2})
	if len(limited) != 2 {
		t.Errorf("Count не применён: %d", len(limited))
	}

	byIDs, _ := s.ListMessages(MessageFilter{MessageIDs: []string{mids[0], mids[3]}})
	if len(byIDs) != 2 {
		t.Errorf("выборка по message_ids: %d", len(byIDs))
	}

	from, to := int64(1010), int64(1030)
	window, _ := s.ListMessages(MessageFilter{ChatID: &d.ChatID, From: &from, To: &to})
	if len(window) != 3 {
		t.Errorf("временное окно: %d записей", len(window))
	}
}

func TestMessageEditAndRemove(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)
	_, d, _ := s.CreateClient(b.ID, ClientInput{FirstName: "Иван", Username: ""})
	m := &Message{ChatID: d.ChatID, BotID: b.ID, Sender: SenderBot, Body: []byte(`{"text":"было"}`)}
	if err := s.AppendMessage(m, nil); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateMessageBody(m.Mid, []byte(`{"text":"стало"}`)); err != nil {
		t.Fatal(err)
	}
	got, err := s.MessageByMid(m.Mid)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != `{"text":"стало"}` {
		t.Errorf("тело не обновлено: %s", got.Body)
	}
	if got.EditedAt == nil {
		t.Error("edited_at не проставлен")
	}

	if err := s.RemoveMessage(m.Mid); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListMessages(MessageFilter{ChatID: &d.ChatID})
	if len(list) != 0 {
		t.Errorf("удалённое сообщение попало в выборку: %d", len(list))
	}
	removed, err := s.MessageByMid(m.Mid)
	if err != nil {
		t.Fatalf("удалённое сообщение должно оставаться доступным по mid: %v", err)
	}
	if removed.Status != StatusRemoved {
		t.Errorf("статус = %q", removed.Status)
	}

	if err := s.RemoveMessage(m.Mid); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
	if err := s.UpdateMessageBody("mid.нет", []byte(`{}`)); !errors.Is(err, ErrNotFound) {
		t.Errorf("правка несуществующего: %v", err)
	}
}

func TestCallbacksAndAttachments(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)
	c, d, _ := s.CreateClient(b.ID, ClientInput{FirstName: "Иван", Username: ""})

	cb := &Callback{BotID: b.ID, ChatID: d.ChatID, Mid: "mid.1", UserID: c.UserID, Payload: "yes"}
	if err := s.SaveCallback(cb); err != nil {
		t.Fatal(err)
	}
	got, err := s.CallbackByID(cb.CallbackID)
	if err != nil || got.Payload != "yes" {
		t.Fatalf("CallbackByID: %+v %v", got, err)
	}
	if got.AnsweredAt != nil {
		t.Error("новый callback не должен быть отвеченным")
	}
	if err := s.MarkCallbackAnswered(cb.CallbackID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.CallbackByID(cb.CallbackID)
	if got.AnsweredAt == nil {
		t.Error("answered_at не проставлен")
	}
	if _, err := s.CallbackByID("нет"); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий callback: %v", err)
	}

	att := &Attachment{BotID: b.ID, Type: "image"}
	if err := s.SaveAttachment(att); err != nil {
		t.Fatal(err)
	}
	if att.Token == "" {
		t.Fatal("токен вложения не назначен")
	}
	stored, err := s.AttachmentByToken(att.Token)
	if err != nil || stored.Uploaded {
		t.Fatalf("вложение до заливки: %+v %v", stored, err)
	}
	if err := s.CompleteAttachment(att.Token, "фото.png", "image/png", "/blobs/x", 128); err != nil {
		t.Fatal(err)
	}
	stored, _ = s.AttachmentByToken(att.Token)
	if !stored.Uploaded || stored.Size != 128 || stored.Filename != "фото.png" {
		t.Errorf("вложение после заливки: %+v", stored)
	}
}

func TestSubscriptions(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)

	sub, err := s.AddSubscription(b.ID, "http://stand.local/hook", []string{"message_created"}, "секрет")
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Wants("message_created") || sub.Wants("bot_started") {
		t.Errorf("фильтр update_types не работает: %+v", sub.UpdateTypes)
	}

	// Повторная подписка на тот же URL обновляет её, а не создаёт вторую.
	if _, err := s.AddSubscription(b.ID, "http://stand.local/hook", nil, ""); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSubscriptions(b.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSubscriptions = %d записей, %v", len(list), err)
	}
	if list[0].UpdateTypes != nil {
		t.Errorf("update_types не сброшен в null: %+v", list[0].UpdateTypes)
	}
	if !list[0].Wants("что_угодно") {
		t.Error("подписка без фильтра должна принимать любые события")
	}

	if err := s.DeleteSubscription(b.ID, "http://stand.local/hook"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubscription(b.ID, "http://stand.local/hook"); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
}

func TestLogsAndPurge(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)

	old := &RequestLogEntry{TS: 1000, BotID: &b.ID, Method: "GET", Path: "/me", Status: 200, LatencyMS: 3}
	fresh := &RequestLogEntry{TS: 9000, BotID: &b.ID, Method: "POST", Path: "/messages", Status: 200, LatencyMS: 7}
	for _, e := range []*RequestLogEntry{old, fresh} {
		if err := s.LogRequest(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LogDelivery(&DeliveryEntry{TS: 1000, BotID: b.ID, URL: "http://x", UpdateType: "message_created",
		Body: "{}", Attempt: 1, Status: 200, DurationMS: 5}); err != nil {
		t.Fatal(err)
	}

	entries, err := s.ListRequestLog(&b.ID, 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("ListRequestLog = %d, %v", len(entries), err)
	}
	if entries[0].Path != "/messages" {
		t.Errorf("порядок журнала не «новые первыми»: %+v", entries[0])
	}
	if all, _ := s.ListRequestLog(nil, 10); len(all) != 2 {
		t.Errorf("журнал по всем ботам: %d", len(all))
	}
	if dl, _ := s.ListDeliveries(b.ID, 10); len(dl) != 1 {
		t.Errorf("ListDeliveries: %d", len(dl))
	}

	n, err := s.Purge(5000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("Purge удалил %d записей, ожидалось 2", n)
	}
	entries, _ = s.ListRequestLog(&b.ID, 10)
	if len(entries) != 1 || entries[0].TS != 9000 {
		t.Errorf("Purge задел свежие записи: %+v", entries)
	}
}

func TestJournalsCarryChatID(t *testing.T) {
	s := openTemp(t)
	chatID := int64(4242)

	req := &RequestLogEntry{Method: "POST", Path: "/messages", Status: 200, ChatID: &chatID}
	if err := s.LogRequest(req); err != nil {
		t.Fatal(err)
	}
	del := &DeliveryEntry{BotID: 1, URL: "https://stand", UpdateType: "message_created",
		Body: "{}", Attempt: 1, Status: 200, ChatID: &chatID}
	if err := s.LogDelivery(del); err != nil {
		t.Fatal(err)
	}

	reqs, err := s.ListRequestLog(nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[0].ChatID == nil || *reqs[0].ChatID != chatID {
		t.Fatalf("chat_id не сохранился в request_log: %+v", reqs)
	}

	dels, err := s.ListDeliveries(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dels) != 1 || dels[0].ChatID == nil || *dels[0].ChatID != chatID {
		t.Fatalf("chat_id не сохранился в webhook_deliveries: %+v", dels)
	}
}

// Ответ стенда на вебхук — вторая половина обмена «max → бот» в ленте
// диалога. Без него видно только «стенд ответил 200».
func TestDeliveryStoresResponseBody(t *testing.T) {
	s := openTemp(t)
	chatID := int64(4242)

	if err := s.LogDelivery(&DeliveryEntry{BotID: 1, ChatID: &chatID, URL: "https://stand",
		UpdateType: "message_created", Body: "{}", Attempt: 1, Status: 200,
		ResponseBody: `{"accepted":true}`}); err != nil {
		t.Fatal(err)
	}

	dels, err := s.ListDeliveries(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dels) != 1 || dels[0].ResponseBody != `{"accepted":true}` {
		t.Fatalf("тело ответа стенда не сохранилось: %+v", dels)
	}
}

// Столбец response_body появился позже журнала доставок: у стендов КЦ в базе
// уже лежат записи без него, и обновление мока обязано их сохранить.
func TestMigrationAddsResponseBodyToOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := old.CreateBot("Бот", "bot", "")
	if err != nil {
		t.Fatal(err)
	}
	chatID := int64(7)
	if err := old.LogDelivery(&DeliveryEntry{BotID: bot.ID, ChatID: &chatID, URL: "https://stand",
		UpdateType: "bot_started", Body: "{}", Attempt: 1, Status: 200}); err != nil {
		t.Fatal(err)
	}
	if _, err := old.DB().Exec(`ALTER TABLE webhook_deliveries DROP COLUMN response_body`); err != nil {
		t.Fatalf("подготовка старой базы: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(path)
	if err != nil {
		t.Fatalf("миграция старой базы не прошла: %v", err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })

	dels, err := upgraded.ListDeliveries(bot.ID, 10)
	if err != nil {
		t.Fatalf("чтение мигрированного журнала: %v", err)
	}
	if len(dels) != 1 || dels[0].UpdateType != "bot_started" || dels[0].ResponseBody != "" {
		t.Fatalf("доставка потеряна или испорчена миграцией: %+v", dels)
	}
}

// TestMigrationAddsChatIDToOldDatabase проверяет главное требование миграций:
// у стендов КЦ в базе живут боты с выданными токенами, и обновление мока не
// имеет права их потерять.
func TestMigrationAddsChatIDToOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := old.CreateBot("Бот", "bot", "")
	if err != nil {
		t.Fatal(err)
	}
	// Имитируем базу предыдущей версии: столбцов ещё нет. Индексы снимаются
	// первыми — SQLite не даёт удалить столбец, на который смотрит индекс.
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_request_log_chat_ts`,
		`DROP INDEX IF EXISTS idx_deliveries_chat_ts`,
		`ALTER TABLE request_log DROP COLUMN chat_id`,
		`ALTER TABLE webhook_deliveries DROP COLUMN chat_id`,
	} {
		if _, err := old.DB().Exec(stmt); err != nil {
			t.Fatalf("подготовка старой базы (%s): %v", stmt, err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(path)
	if err != nil {
		t.Fatalf("миграция старой базы не прошла: %v", err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })

	if _, err := upgraded.BotByID(bot.ID); err != nil {
		t.Fatalf("бот потерян при миграции: %v", err)
	}
	chatID := int64(7)
	if err := upgraded.LogRequest(&RequestLogEntry{Method: "GET", Path: "/me", Status: 200, ChatID: &chatID}); err != nil {
		t.Fatalf("запись в мигрированный журнал: %v", err)
	}
}
