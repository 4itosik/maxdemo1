package store

import "database/sql"

// RequestLogEntry — запись журнала входящих вызовов Bot API.
type RequestLogEntry struct {
	ID    int64  `json:"id"`
	TS    int64  `json:"ts"`
	BotID *int64 `json:"bot_id,omitempty"`
	// ChatID — диалог, к которому относится запись. nil у операций, которые
	// диалога не касаются: GET /me, PATCH /me/commands, подписки, загрузки.
	ChatID       *int64 `json:"chat_id,omitempty"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	Query        string `json:"query,omitempty"`
	Status       int    `json:"status"`
	RequestBody  string `json:"request_body,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
	LatencyMS    int64  `json:"latency_ms"`
	Error        string `json:"error,omitempty"`
}

// DeliveryEntry — запись журнала исходящих webhook-доставок.
// Status == 0 означает, что ответа не было (сетевая ошибка, таймаут или
// отказ на валидации — тогда заполнен Error).
type DeliveryEntry struct {
	ID    int64 `json:"id"`
	TS    int64 `json:"ts"`
	BotID int64 `json:"bot_id"`
	// ChatID — диалог, к которому относится запись. nil у операций, которые
	// диалога не касаются: GET /me, PATCH /me/commands, подписки, загрузки.
	ChatID         *int64 `json:"chat_id,omitempty"`
	SubscriptionID *int64 `json:"subscription_id,omitempty"`
	URL            string `json:"url"`
	UpdateType     string `json:"update_type"`
	Body           string `json:"body"`
	Attempt        int    `json:"attempt"`
	Status         int    `json:"status"`
	Error          string `json:"error,omitempty"`
	// ResponseBody — тело ответа стенда, обрезанное по потолку диспетчера.
	// Пусто и когда стенд ответил без тела, и когда ответа не было вовсе;
	// различает эти случаи Status.
	ResponseBody string `json:"response_body,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
}

// LogRequest сохраняет запись о входящем вызове.
func (s *Store) LogRequest(e *RequestLogEntry) error {
	if e.TS == 0 {
		e.TS = nowMS()
	}
	res, err := s.db.Exec(
		`INSERT INTO request_log (ts, bot_id, chat_id, method, path, query, status, request_body, response_body, latency_ms, error)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.TS, e.BotID, e.ChatID, e.Method, e.Path, e.Query, e.Status, e.RequestBody, e.ResponseBody, e.LatencyMS, e.Error)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// LogDelivery сохраняет запись о попытке доставки события.
func (s *Store) LogDelivery(e *DeliveryEntry) error {
	if e.TS == 0 {
		e.TS = nowMS()
	}
	res, err := s.db.Exec(
		`INSERT INTO webhook_deliveries (ts, bot_id, chat_id, subscription_id, url, update_type, body, attempt, status, error, response_body, duration_ms)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.TS, e.BotID, e.ChatID, e.SubscriptionID, e.URL, e.UpdateType, e.Body, e.Attempt, e.Status, e.Error,
		e.ResponseBody, e.DurationMS)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// ListRequestLog возвращает последние записи журнала, новые первыми.
// botID == nil — по всем ботам.
func (s *Store) ListRequestLog(botID *int64, limit int) ([]RequestLogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, ts, bot_id, chat_id, method, path, query, status, request_body, response_body, latency_ms, error
	      FROM request_log`
	args := []any{}
	if botID != nil {
		q += ` WHERE bot_id = ?`
		args = append(args, *botID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}

// ListDeliveries возвращает последние доставки бота, новые первыми.
func (s *Store) ListDeliveries(botID int64, limit int) ([]DeliveryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, ts, bot_id, chat_id, subscription_id, url, update_type, body, attempt, status, error, response_body, duration_ms
		 FROM webhook_deliveries WHERE bot_id = ? ORDER BY id DESC LIMIT ?`, botID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// scanDeliveries разбирает выборку журнала доставок.
func scanDeliveries(rows *sql.Rows) ([]DeliveryEntry, error) {
	out := []DeliveryEntry{}
	for rows.Next() {
		var e DeliveryEntry
		var sub, chat sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TS, &e.BotID, &chat, &sub, &e.URL, &e.UpdateType, &e.Body,
			&e.Attempt, &e.Status, &e.Error, &e.ResponseBody, &e.DurationMS); err != nil {
			return nil, err
		}
		if sub.Valid {
			e.SubscriptionID = &sub.Int64
		}
		if chat.Valid {
			e.ChatID = &chat.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// scanRequests разбирает выборку журнала вызовов Bot API.
func scanRequests(rows *sql.Rows) ([]RequestLogEntry, error) {
	out := []RequestLogEntry{}
	for rows.Next() {
		var e RequestLogEntry
		var bot, chat sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TS, &bot, &chat, &e.Method, &e.Path, &e.Query, &e.Status,
			&e.RequestBody, &e.ResponseBody, &e.LatencyMS, &e.Error); err != nil {
			return nil, err
		}
		if bot.Valid {
			e.BotID = &bot.Int64
		}
		if chat.Valid {
			e.ChatID = &chat.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UIActionEntry — запись журнала действий тестировщика в веб-чате.
// Detail хранит тело запроса control-API как есть: лента должна показывать,
// что тестировщик на самом деле отправил, а не то, во что мок это превратил.
type UIActionEntry struct {
	ID     int64  `json:"id"`
	TS     int64  `json:"ts"`
	BotID  int64  `json:"bot_id"`
	ChatID int64  `json:"chat_id"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// LogUIAction сохраняет действие тестировщика.
//
// Читателя в веб-чате у этого журнала больше нет: лента диалога показывает
// только обмены между моком и ботом, а отказ действия тестировщик видит
// алертом сразу, в момент клика. Записи остаются следом для разбора — их
// достают запросом к базе.
func (s *Store) LogUIAction(e *UIActionEntry) error {
	if e.TS == 0 {
		e.TS = nowMS()
	}
	res, err := s.db.Exec(
		`INSERT INTO ui_actions (ts, bot_id, chat_id, action, detail, error) VALUES (?,?,?,?,?,?)`,
		e.TS, e.BotID, e.ChatID, e.Action, e.Detail, e.Error)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// ListUIActions возвращает последние действия в диалоге, новые первыми.
func (s *Store) ListUIActions(chatID int64, limit int) ([]UIActionEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, ts, bot_id, chat_id, action, detail, error
		 FROM ui_actions WHERE chat_id = ? ORDER BY id DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UIActionEntry{}
	for rows.Next() {
		var e UIActionEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.BotID, &e.ChatID, &e.Action, &e.Detail, &e.Error); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Purge удаляет журналы старше указанного момента (unix-мс) и возвращает
// число удалённых записей. Нужен, чтобы долгоживущий мок не распухал.
func (s *Store) Purge(olderThanMS int64) (int64, error) {
	var total int64
	for _, table := range []string{"request_log", "webhook_deliveries", "ui_actions"} {
		res, err := s.db.Exec(`DELETE FROM `+table+` WHERE ts < ?`, olderThanMS)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}
