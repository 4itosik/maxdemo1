package store

import (
	"database/sql"
	"errors"
	"fmt"

	"maxmock/internal/ids"
)

// Client — виртуальный пользователь Max, роль которого играет тестировщик.
//
// Телефон и координаты в схеме `User` контракта отсутствуют: до бота они
// доезжают только вложениями `contact` и `location`. Здесь они лежат потому,
// что кнопка «поделиться контактом» обязана взять их откуда-то, а придумывать
// на лету значит ломать повторяемость тестов КЦ.
type Client struct {
	UserID    int64  `json:"user_id"`
	BotID     int64  `json:"bot_id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Phone     string `json:"phone"`
	// Координаты по умолчанию для «поделиться геопозицией».
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	CreatedAt int64    `json:"created_at"`
}

// ClientInput — данные для создания клиента.
//
// Структура вместо позиционных аргументов: шесть подряд идущих строк —
// приглашение перепутать имя с фамилией.
type ClientInput struct {
	// UserID задан — используется как есть, иначе генерируется. Позволяет
	// воспроизвести на моке конкретного абонента из тестовых данных КЦ.
	UserID    *int64
	FirstName string
	LastName  string
	Username  string
	Phone     string
	Latitude  *float64
	Longitude *float64
}

// Dialog — диалог «клиент ↔ бот».
type Dialog struct {
	ChatID    int64 `json:"chat_id"`
	BotID     int64 `json:"bot_id"`
	UserID    int64 `json:"user_id"`
	Started   bool  `json:"started"`
	CreatedAt int64 `json:"created_at"`
}

// clientColumns — порядок столбцов, на который рассчитывает scanClient.
const clientColumns = `user_id, bot_id, first_name, last_name, username, phone, latitude, longitude, created_at`

func scanClient(row interface{ Scan(...any) error }) (*Client, error) {
	var c Client
	var lastName, username, phone sql.NullString
	var lat, lon sql.NullFloat64
	if err := row.Scan(&c.UserID, &c.BotID, &c.FirstName, &lastName, &username, &phone,
		&lat, &lon, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.LastName, c.Username, c.Phone = lastName.String, username.String, phone.String
	if lat.Valid {
		c.Latitude = &lat.Float64
	}
	if lon.Valid {
		c.Longitude = &lon.Float64
	}
	return &c, nil
}

func scanDialog(row interface{ Scan(...any) error }) (*Dialog, error) {
	var d Dialog
	var started int
	if err := row.Scan(&d.ChatID, &d.BotID, &d.UserID, &started, &d.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Started = started != 0
	return &d, nil
}

// CreateClient заводит клиента и сразу открывает ему диалог с ботом:
// в Max диалог существует до первого сообщения, «Начать» лишь помечает его
// начатым и порождает bot_started.
func (s *Store) CreateClient(botID int64, in ClientInput) (*Client, *Dialog, error) {
	userID := ids.NewUserID()
	if in.UserID != nil {
		// Занятый идентификатор отвергается, а не подменяется молча: подмена
		// сорвала бы ровно ту связку с записью абонента, ради которой его и
		// задают.
		if _, err := s.ClientByUserID(*in.UserID); err == nil {
			return nil, nil, fmt.Errorf("%w: клиент с user_id %d уже существует", ErrConflict, *in.UserID)
		} else if !errors.Is(err, ErrNotFound) {
			return nil, nil, err
		}
		userID = *in.UserID
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := nowMS()
	c := &Client{
		UserID: userID, BotID: botID,
		FirstName: in.FirstName, LastName: in.LastName, Username: in.Username,
		Phone: in.Phone, Latitude: in.Latitude, Longitude: in.Longitude,
		CreatedAt: now,
	}
	if _, err := tx.Exec(
		`INSERT INTO clients (user_id, bot_id, first_name, last_name, username, phone, latitude, longitude, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		c.UserID, c.BotID, c.FirstName, nullString(c.LastName), nullString(c.Username),
		nullString(c.Phone), c.Latitude, c.Longitude, c.CreatedAt); err != nil {
		return nil, nil, fmt.Errorf("создание клиента: %w", err)
	}

	d := &Dialog{ChatID: ids.NewChatID(), BotID: botID, UserID: c.UserID, CreatedAt: now}
	if _, err := tx.Exec(
		`INSERT INTO dialogs (chat_id, bot_id, user_id, started, created_at) VALUES (?,?,?,0,?)`,
		d.ChatID, d.BotID, d.UserID, d.CreatedAt); err != nil {
		return nil, nil, fmt.Errorf("создание диалога: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return c, d, nil
}

// ListClients возвращает клиентов бота в порядке создания.
func (s *Store) ListClients(botID int64) ([]Client, error) {
	rows, err := s.db.Query(
		`SELECT `+clientColumns+` FROM clients WHERE bot_id = ? ORDER BY created_at, user_id`,
		botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Client{}
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ClientByUserID находит клиента по его user_id.
func (s *Store) ClientByUserID(userID int64) (*Client, error) {
	return scanClient(s.db.QueryRow(
		`SELECT `+clientColumns+` FROM clients WHERE user_id = ?`, userID))
}

const dialogColumns = `chat_id, bot_id, user_id, started, created_at`

// DialogByChatID находит диалог по chat_id.
func (s *Store) DialogByChatID(chatID int64) (*Dialog, error) {
	return scanDialog(s.db.QueryRow(`SELECT `+dialogColumns+` FROM dialogs WHERE chat_id = ?`, chatID))
}

// DialogByUser находит диалог бота с конкретным пользователем.
func (s *Store) DialogByUser(botID, userID int64) (*Dialog, error) {
	return scanDialog(s.db.QueryRow(
		`SELECT `+dialogColumns+` FROM dialogs WHERE bot_id = ? AND user_id = ?`, botID, userID))
}

// ListDialogs возвращает диалоги бота.
func (s *Store) ListDialogs(botID int64) ([]Dialog, error) {
	rows, err := s.db.Query(`SELECT `+dialogColumns+` FROM dialogs WHERE bot_id = ? ORDER BY created_at, chat_id`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dialog{}
	for rows.Next() {
		d, err := scanDialog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// SetDialogStarted помечает диалог начатым (нажата кнопка «Начать»).
// Возвращает true, если состояние изменилось: повторное нажатие не должно
// порождать второй bot_started.
func (s *Store) SetDialogStarted(chatID int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE dialogs SET started = 1 WHERE chat_id = ? AND started = 0`, chatID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetDialogStopped помечает диалог остановленным (нажата кнопка «Остановить»).
// Возвращает true, если состояние изменилось: повторная остановка не должна
// порождать второй bot_stopped.
func (s *Store) SetDialogStopped(chatID int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE dialogs SET started = 0 WHERE chat_id = ? AND started = 1`, chatID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
