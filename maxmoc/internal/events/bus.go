// Package events — внутренняя шина изменений состояния мока.
//
// Единственный потребитель шины — веб-UI (через WebSocket): она нужна, чтобы
// тестировщик видел ответ КЦ в чате мгновенно. Поэтому доставка неблокирующая:
// подписчик, который не успевает читать, теряет события, но никогда не
// задерживает обработку HTTP-запроса.
package events

import (
	"sync"
	"time"
)

// Виды событий шины.
const (
	KindMessage        = "message"         // создано сообщение
	KindMessageEdited  = "message_edited"  // сообщение отредактировано
	KindMessageRemoved = "message_removed" // сообщение удалено
	KindDialog         = "dialog"          // изменилось состояние диалога
	KindClient         = "client"          // создан клиент
	KindDelivery       = "delivery"        // попытка доставки webhook-а
	KindRequest        = "request"         // входящий вызов Bot API
	KindUIAction       = "ui_action"       // действие тестировщика в веб-чате
	KindBot            = "bot"             // изменилась карточка бота (команды)
)

// Event — единица потока изменений.
type Event struct {
	Kind    string `json:"kind"`
	BotID   int64  `json:"bot_id"`
	ChatID  int64  `json:"chat_id,omitempty"`
	TS      int64  `json:"ts"`
	Payload any    `json:"payload,omitempty"`
}

// SubscribeAll — значение botID, означающее «события всех ботов».
const SubscribeAll int64 = 0

type subscriber struct {
	botID int64
	ch    chan Event
}

// Bus — шина событий.
type Bus struct {
	mu     sync.Mutex
	nextID int64
	subs   map[int64]*subscriber
}

// New создаёт пустую шину.
func New() *Bus {
	return &Bus{subs: make(map[int64]*subscriber)}
}

// Subscribe открывает поток событий бота (SubscribeAll — всех ботов).
// Возвращённая функция закрывает подписку; вызывать её обязательно.
func (b *Bus) Subscribe(botID int64) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	s := &subscriber{botID: botID, ch: make(chan Event, 64)}
	b.subs[id] = s
	return s.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if cur, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(cur.ch)
		}
	}
}

// Publish рассылает событие подходящим подписчикам, не блокируясь.
func (b *Bus) Publish(ev Event) {
	if ev.TS == 0 {
		ev.TS = time.Now().UnixMilli()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if s.botID != SubscribeAll && s.botID != ev.BotID {
			continue
		}
		select {
		case s.ch <- ev:
		default: // подписчик отстал — событие теряется, состояние он перечитает из API
		}
	}
}

// Subscribers возвращает число активных подписок (для диагностики).
func (b *Bus) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
