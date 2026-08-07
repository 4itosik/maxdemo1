package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"maxmock/internal/ids"
)

// Кто отправил сообщение.
const (
	SenderBot    = "bot"
	SenderClient = "client"
)

// Состояние сообщения.
const (
	StatusActive  = "active"
	StatusRemoved = "removed"
)

// Message — сообщение диалога. Body хранит JSON тела (wire.MessageBody):
// хранилище не разбирает содержимое, чтобы форма тела оставалась делом
// доменного слоя и контракта.
type Message struct {
	Mid       string `json:"mid"`
	ChatID    int64  `json:"chat_id"`
	BotID     int64  `json:"bot_id"`
	Seq       int64  `json:"seq"`
	Sender    string `json:"sender"`
	Body      []byte `json:"body"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
	EditedAt  *int64 `json:"edited_at,omitempty"`
}

const messageColumns = `mid, chat_id, bot_id, seq, sender, body, status, created_at, edited_at`

func scanMessage(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var body string
	var editedAt sql.NullInt64
	if err := row.Scan(&m.Mid, &m.ChatID, &m.BotID, &m.Seq, &m.Sender, &body, &m.Status, &m.CreatedAt, &editedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m.Body = []byte(body)
	if editedAt.Valid {
		m.EditedAt = &editedAt.Int64
	}
	return &m, nil
}

// AppendMessage добавляет сообщение в диалог, назначая ему порядковый номер
// и mid. Номер выдаётся в транзакции, поэтому пропусков и коллизий не бывает.
//
// Тело собирает buildBody: mid и seq входят в само тело сообщения
// (MessageBody.mid, MessageBody.seq), а известны они только после выдачи
// номера. Ошибка buildBody откатывает транзакцию — в диалоге не остаётся
// сообщения-пустышки.
//
// ВАЖНО: buildBody вызывается внутри транзакции и НЕ должен обращаться к
// хранилищу. Пул ограничен одним соединением, и такой вызов встанет намертво,
// ожидая соединение, занятое этой же транзакцией.
func (s *Store) AppendMessage(m *Message, buildBody func(mid string, seq int64) ([]byte, error)) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var maxSeq int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE chat_id = ?`, m.ChatID).Scan(&maxSeq); err != nil {
		return fmt.Errorf("выдача seq: %w", err)
	}
	if m.Status == "" {
		m.Status = StatusActive
	}
	// Время проставляется до выдачи номера: живой Max упаковывает его в
	// старшие биты seq, и присвоить номер раньше времени значило бы развести
	// created_at и seq одного и того же сообщения.
	if m.CreatedAt == 0 {
		m.CreatedAt = nowMS()
	}
	m.Seq = ids.Seq(m.CreatedAt, maxSeq)
	m.Mid = ids.Mid(m.ChatID, m.Seq)
	if buildBody != nil {
		body, err := buildBody(m.Mid, m.Seq)
		if err != nil {
			return err
		}
		m.Body = body
	}
	if _, err := tx.Exec(
		`INSERT INTO messages (`+messageColumns+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		m.Mid, m.ChatID, m.BotID, m.Seq, m.Sender, string(m.Body), m.Status, m.CreatedAt, m.EditedAt); err != nil {
		return fmt.Errorf("сохранение сообщения: %w", err)
	}
	return tx.Commit()
}

// MessageByMid возвращает сообщение по его идентификатору, включая удалённые:
// GET /messages/{messageId} по удалённому сообщению должен ответить осмысленно,
// а не «не найдено».
func (s *Store) MessageByMid(mid string) (*Message, error) {
	return scanMessage(s.db.QueryRow(`SELECT `+messageColumns+` FROM messages WHERE mid = ?`, mid))
}

// MessageFilter — параметры выборки GET /messages.
type MessageFilter struct {
	ChatID     *int64
	MessageIDs []string
	From       *int64 // нижняя граница created_at, мс
	To         *int64 // верхняя граница created_at, мс
	Count      int    // максимум записей; 0 — значение по умолчанию (50)
}

// ListMessages возвращает активные сообщения по фильтру, новые первыми
// (как отдаёт реальный Max).
func (s *Store) ListMessages(f MessageFilter) ([]Message, error) {
	where := []string{`status = ?`}
	args := []any{StatusActive}
	if f.ChatID != nil {
		where = append(where, `chat_id = ?`)
		args = append(args, *f.ChatID)
	}
	if len(f.MessageIDs) > 0 {
		where = append(where, `mid IN (`+strings.TrimSuffix(strings.Repeat("?,", len(f.MessageIDs)), ",")+`)`)
		for _, id := range f.MessageIDs {
			args = append(args, id)
		}
	}
	if f.From != nil {
		where = append(where, `created_at >= ?`)
		args = append(args, *f.From)
	}
	if f.To != nil {
		where = append(where, `created_at <= ?`)
		args = append(args, *f.To)
	}
	count := f.Count
	if count <= 0 {
		count = 50
	}
	args = append(args, count)

	rows, err := s.db.Query(
		`SELECT `+messageColumns+` FROM messages WHERE `+strings.Join(where, " AND ")+
			` ORDER BY created_at DESC, seq DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// UpdateMessageBody заменяет тело сообщения (правка ботом или клиентом).
func (s *Store) UpdateMessageBody(mid string, body []byte) error {
	res, err := s.db.Exec(`UPDATE messages SET body = ?, edited_at = ? WHERE mid = ? AND status = ?`,
		string(body), nowMS(), mid, StatusActive)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

// RemoveMessage помечает сообщение удалённым.
func (s *Store) RemoveMessage(mid string) error {
	res, err := s.db.Exec(`UPDATE messages SET status = ? WHERE mid = ? AND status = ?`,
		StatusRemoved, mid, StatusActive)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

func affectedOrNotFound(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Callback — зарегистрированное нажатие кнопки.
type Callback struct {
	CallbackID string `json:"callback_id"`
	BotID      int64  `json:"bot_id"`
	ChatID     int64  `json:"chat_id"`
	Mid        string `json:"mid"`
	UserID     int64  `json:"user_id"`
	Payload    string `json:"payload"`
	CreatedAt  int64  `json:"created_at"`
	AnsweredAt *int64 `json:"answered_at,omitempty"`
}

// SaveCallback сохраняет нажатие и возвращает его идентификатор.
func (s *Store) SaveCallback(c *Callback) error {
	if c.CallbackID == "" {
		c.CallbackID = ids.NewCallbackID()
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = nowMS()
	}
	_, err := s.db.Exec(
		`INSERT INTO callbacks (callback_id, bot_id, chat_id, mid, user_id, payload, created_at) VALUES (?,?,?,?,?,?,?)`,
		c.CallbackID, c.BotID, c.ChatID, c.Mid, c.UserID, c.Payload, c.CreatedAt)
	return err
}

const callbackColumns = `callback_id, bot_id, chat_id, mid, user_id, payload, created_at, answered_at`

func scanCallback(row interface{ Scan(...any) error }) (*Callback, error) {
	var c Callback
	var answered sql.NullInt64
	err := row.Scan(&c.CallbackID, &c.BotID, &c.ChatID, &c.Mid, &c.UserID, &c.Payload, &c.CreatedAt, &answered)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if answered.Valid {
		c.AnsweredAt = &answered.Int64
	}
	return &c, nil
}

// CallbackByID находит нажатие по идентификатору из POST /answers.
func (s *Store) CallbackByID(id string) (*Callback, error) {
	return scanCallback(s.db.QueryRow(`SELECT `+callbackColumns+` FROM callbacks WHERE callback_id = ?`, id))
}

// LastCallback возвращает последнее нажатие кнопки у бота. Нужен тестам и
// диагностике: снаружи идентификатор нажатия виден только в событии.
func (s *Store) LastCallback(botID int64) (*Callback, error) {
	return scanCallback(s.db.QueryRow(
		`SELECT `+callbackColumns+` FROM callbacks WHERE bot_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`,
		botID))
}

// MarkCallbackAnswered отмечает, что бот ответил на нажатие.
func (s *Store) MarkCallbackAnswered(id string) error {
	res, err := s.db.Exec(`UPDATE callbacks SET answered_at = ? WHERE callback_id = ?`, nowMS(), id)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

// Attachment — метаданные вложения; сам файл лежит в blob-каталоге по Path.
type Attachment struct {
	Token     string `json:"token"`
	BotID     int64  `json:"bot_id"`
	Type      string `json:"type"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	Path      string `json:"path"`
	Uploaded  bool   `json:"uploaded"`
	CreatedAt int64  `json:"created_at"`
}

// SaveAttachment создаёт запись о будущем или уже загруженном вложении.
func (s *Store) SaveAttachment(a *Attachment) error {
	if a.Token == "" {
		a.Token = ids.NewUploadToken()
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = nowMS()
	}
	uploaded := 0
	if a.Uploaded {
		uploaded = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO attachments (token, bot_id, type, filename, mime, size, path, uploaded, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		a.Token, a.BotID, a.Type, a.Filename, a.Mime, a.Size, a.Path, uploaded, a.CreatedAt)
	return err
}

// CompleteAttachment фиксирует факт заливки файла.
func (s *Store) CompleteAttachment(token, filename, mime, path string, size int64) error {
	res, err := s.db.Exec(
		`UPDATE attachments SET filename = ?, mime = ?, path = ?, size = ?, uploaded = 1 WHERE token = ?`,
		filename, mime, path, size, token)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

// AttachmentByToken находит вложение по токену.
func (s *Store) AttachmentByToken(token string) (*Attachment, error) {
	var a Attachment
	var uploaded int
	err := s.db.QueryRow(
		`SELECT token, bot_id, type, filename, mime, size, path, uploaded, created_at FROM attachments WHERE token = ?`, token).
		Scan(&a.Token, &a.BotID, &a.Type, &a.Filename, &a.Mime, &a.Size, &a.Path, &uploaded, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.Uploaded = uploaded != 0
	return &a, nil
}
