package events

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Event) (Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		return ev, ok
	case <-time.After(time.Second):
		t.Fatal("событие не пришло за секунду")
		return Event{}, false
	}
}

func TestSubscriberGetsOwnBotOnly(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(1)
	defer cancel()

	b.Publish(Event{Kind: KindMessage, BotID: 2, ChatID: 20})
	b.Publish(Event{Kind: KindMessage, BotID: 1, ChatID: 10})

	ev, _ := recv(t, ch)
	if ev.BotID != 1 || ev.ChatID != 10 {
		t.Fatalf("пришло чужое событие: %+v", ev)
	}
	if ev.TS == 0 {
		t.Error("метка времени не проставлена")
	}
}

func TestSubscribeAll(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(SubscribeAll)
	defer cancel()

	b.Publish(Event{Kind: KindRequest, BotID: 7})
	ev, _ := recv(t, ch)
	if ev.BotID != 7 {
		t.Fatalf("подписка на всех ботов пропустила событие: %+v", ev)
	}
}

func TestCancelClosesChannel(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(1)
	if b.Subscribers() != 1 {
		t.Fatalf("подписчиков: %d", b.Subscribers())
	}
	cancel()
	if _, ok := <-ch; ok {
		t.Error("канал не закрыт после отмены")
	}
	if b.Subscribers() != 0 {
		t.Errorf("подписка не снята: %d", b.Subscribers())
	}
	cancel() // повторная отмена безопасна
}

// Медленный подписчик не должен задерживать публикацию: события теряются,
// но Publish возвращается сразу.
func TestPublishNeverBlocks(t *testing.T) {
	b := New()
	_, cancel := b.Subscribe(1)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Publish(Event{Kind: KindMessage, BotID: 1})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish заблокировался на отставшем подписчике")
	}
}

func TestPublishWithoutSubscribers(t *testing.T) {
	New().Publish(Event{Kind: KindMessage, BotID: 1})
}
