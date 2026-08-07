package store

import "sort"

// DialogEvent — строка ленты событий диалога: один обмен «запрос → ответ»
// между моком и ботом. Заполнен ровно один указатель, какой именно — говорит
// Kind.
//
// Два источника не сведены в одну таблицу намеренно: у вызова Bot API это
// метод, путь, статус и латентность, у доставки — url, тип события и номер
// попытки. Общая схема либо потеряла бы типизацию, либо превратила бы одно
// поле в свалку; слияние двух-трёх сотен записей по времени стоит дешевле.
type DialogEvent struct {
	Kind     string           `json:"kind"` // delivery | request
	TS       int64            `json:"ts"`
	Delivery *DeliveryEntry   `json:"delivery,omitempty"`
	Request  *RequestLogEntry `json:"request,omitempty"`
}

// Порядок видов внутри одной миллисекунды: доставка порождает вызов бота.
// Лента идёт от новых к старым, поэтому больший ранг оказывается выше — то
// есть следствие стоит над причиной.
var feedRank = map[string]int{"delivery": 2, "request": 3}

// ListDialogEvents собирает ленту диалога: доставки событий на стенд и вызовы
// Bot API от бота — в общей хронологии, парами «запрос → ответ».
//
// Действий тестировщика в ленте нет: он только что совершил их сам, а отказ
// увидел алертом в момент клика. В журнале ui_actions они остаются.
func (s *Store) ListDialogEvents(botID, chatID int64, limit int) ([]DialogEvent, error) {
	if limit <= 0 {
		limit = 200
	}

	deliveries, err := s.listDialogDeliveries(chatID, limit)
	if err != nil {
		return nil, err
	}
	requests, err := s.listDialogRequests(botID, chatID, limit)
	if err != nil {
		return nil, err
	}

	out := make([]DialogEvent, 0, len(deliveries)+len(requests))
	for i := range deliveries {
		out = append(out, DialogEvent{Kind: "delivery", TS: deliveries[i].TS, Delivery: &deliveries[i]})
	}
	for i := range requests {
		out = append(out, DialogEvent{Kind: "request", TS: requests[i].TS, Request: &requests[i]})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TS != out[j].TS {
			return out[i].TS > out[j].TS
		}
		return feedRank[out[i].Kind] > feedRank[out[j].Kind]
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// listDialogDeliveries — доставки, привязанные к диалогу.
func (s *Store) listDialogDeliveries(chatID int64, limit int) ([]DeliveryEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, ts, bot_id, chat_id, subscription_id, url, update_type, body, attempt, status, error, response_body, duration_ms
		 FROM webhook_deliveries WHERE chat_id = ? ORDER BY id DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// listDialogRequests — вызовы бота по этому диалогу.
//
// Вызовы вне диалога (GET /me, PATCH /me/commands, подписки, загрузки) сюда
// не попадают: они относятся ко всему боту и живут на вкладке «Журнал». В
// ленте диалога они были шумом — обменом, к этому разговору не относящимся.
func (s *Store) listDialogRequests(botID, chatID int64, limit int) ([]RequestLogEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, ts, bot_id, chat_id, method, path, query, status, request_body, response_body, latency_ms, error
		 FROM request_log
		 WHERE bot_id = ? AND chat_id = ?
		 ORDER BY id DESC LIMIT ?`, botID, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}
