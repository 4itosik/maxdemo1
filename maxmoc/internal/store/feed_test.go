package store

import "testing"

func TestListDialogEventsMergesExchanges(t *testing.T) {
	s := openTemp(t)
	const botID, chatID, otherChat = int64(1), int64(10), int64(20)

	mustLog := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustLog(s.LogDelivery(&DeliveryEntry{TS: 101, BotID: botID, ChatID: ptr(chatID),
		URL: "https://stand", UpdateType: "bot_started", Body: "{}", Attempt: 1, Status: 200,
		ResponseBody: `{"accepted":true}`}))
	mustLog(s.LogRequest(&RequestLogEntry{TS: 102, BotID: ptr(botID), ChatID: ptr(chatID),
		Method: "POST", Path: "/messages", Status: 200}))
	// Вызов вне диалога в ленту не попадает: он относится ко всему боту и
	// живёт на вкладке «Журнал».
	mustLog(s.LogRequest(&RequestLogEntry{TS: 103, BotID: ptr(botID),
		Method: "PATCH", Path: "/me/commands", Status: 200}))
	// Чужой диалог — тем более.
	mustLog(s.LogRequest(&RequestLogEntry{TS: 104, BotID: ptr(botID), ChatID: ptr(otherChat),
		Method: "POST", Path: "/messages", Status: 200}))
	// Действия тестировщика журналируются по-прежнему, но в ленте им места нет.
	mustLog(s.LogUIAction(&UIActionEntry{TS: 105, BotID: botID, ChatID: chatID, Action: "start"}))

	feed, err := s.ListDialogEvents(botID, chatID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 2 {
		t.Fatalf("в ленте %d записей, ожидалось 2: %+v", len(feed), feed)
	}
	// Новые первыми.
	if feed[0].Kind != "request" || feed[0].Request.Path != "/messages" {
		t.Errorf("первым ожидался вызов бота по этому диалогу: %+v", feed[0])
	}
	if feed[1].Kind != "delivery" || feed[1].Delivery.ResponseBody != `{"accepted":true}` {
		t.Errorf("доставка с ответом стенда потеряна: %+v", feed[1])
	}
}

// В одну миллисекунду доставка — причина, вызов бота — следствие. Лента идёт
// от новых к старым, поэтому следствие обязано стоять выше причины: иначе
// ответ бота читается раньше события, которое его вызвало.
func TestListDialogEventsPutsEffectAboveCause(t *testing.T) {
	s := openTemp(t)
	const botID, chatID = int64(1), int64(10)
	if err := s.LogRequest(&RequestLogEntry{TS: 100, BotID: ptr(botID), ChatID: ptr(chatID),
		Method: "POST", Path: "/messages", Status: 200}); err != nil {
		t.Fatal(err)
	}
	if err := s.LogDelivery(&DeliveryEntry{TS: 100, BotID: botID, ChatID: ptr(chatID),
		URL: "https://stand", UpdateType: "message_created", Body: "{}", Attempt: 1, Status: 200}); err != nil {
		t.Fatal(err)
	}

	feed, err := s.ListDialogEvents(botID, chatID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 2 || feed[0].Kind != "request" || feed[1].Kind != "delivery" {
		t.Fatalf("порядок в одну миллисекунду нарушен: %+v", feed)
	}
}

func TestListDialogEventsRespectsLimit(t *testing.T) {
	s := openTemp(t)
	const botID, chatID = int64(1), int64(10)
	for i := range 10 {
		if err := s.LogRequest(&RequestLogEntry{TS: int64(100 + i), BotID: ptr(botID), ChatID: ptr(chatID),
			Method: "POST", Path: "/messages", Status: 200}); err != nil {
			t.Fatal(err)
		}
	}
	feed, err := s.ListDialogEvents(botID, chatID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 3 {
		t.Fatalf("предел не соблюдён: %d записей", len(feed))
	}
	if feed[0].Request.TS != 109 {
		t.Errorf("обрезаны не самые старые записи: первая ts %d", feed[0].Request.TS)
	}
}
