package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"maxmock/internal/ids"
)

// Bot — зарегистрированный в моке бот.
//
// Commands хранится сырым JSON: форму команд задаёт контракт (BotCommand), и
// хранилище не обязано её знать. json.RawMessage, а не []byte, — чтобы список
// попадал в ответы control-API объектом, а не строкой в base64.
type Bot struct {
	ID          int64           `json:"id"`
	UserID      int64           `json:"user_id"`
	Name        string          `json:"name"`
	Username    string          `json:"username"`
	Token       string          `json:"token"`
	Description string          `json:"description"`
	Commands    json.RawMessage `json:"commands"`
	CreatedAt   int64           `json:"created_at"`
}

const botColumns = `id, user_id, name, username, token, description, commands, created_at`

func scanBot(row interface{ Scan(...any) error }) (*Bot, error) {
	var b Bot
	var commands string
	if err := row.Scan(&b.ID, &b.UserID, &b.Name, &b.Username, &b.Token, &b.Description,
		&commands, &b.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if commands == "" {
		commands = "[]"
	}
	b.Commands = json.RawMessage(commands)
	return &b, nil
}

// CreateBot регистрирует бота и выдаёт ему access_token.
func (s *Store) CreateBot(name, username, description string) (*Bot, error) {
	b := &Bot{
		UserID:      ids.NewUserID(),
		Name:        name,
		Username:    username,
		Token:       ids.NewBotToken(),
		Description: description,
		Commands:    json.RawMessage("[]"),
		CreatedAt:   nowMS(),
	}
	res, err := s.db.Exec(
		`INSERT INTO bots (user_id, name, username, token, description, commands, created_at) VALUES (?,?,?,?,?,?,?)`,
		b.UserID, b.Name, b.Username, b.Token, b.Description, string(b.Commands), b.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("создание бота: %w", err)
	}
	if b.ID, err = res.LastInsertId(); err != nil {
		return nil, err
	}
	return b, nil
}

// UpdateBotCommands заменяет список команд бота (PATCH /me/commands).
func (s *Store) UpdateBotCommands(botID int64, commands json.RawMessage) error {
	if len(commands) == 0 {
		commands = json.RawMessage("[]")
	}
	res, err := s.db.Exec(`UPDATE bots SET commands = ? WHERE id = ?`, string(commands), botID)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

// ListBots возвращает всех ботов в порядке регистрации.
func (s *Store) ListBots() ([]Bot, error) {
	rows, err := s.db.Query(`SELECT ` + botColumns + ` FROM bots ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bot{}
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// BotByID находит бота по внутреннему идентификатору.
func (s *Store) BotByID(id int64) (*Bot, error) {
	return scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM bots WHERE id = ?`, id))
}

// BotByToken находит бота по access_token — так авторизуются запросы Bot API.
func (s *Store) BotByToken(token string) (*Bot, error) {
	return scanBot(s.db.QueryRow(`SELECT `+botColumns+` FROM bots WHERE token = ?`, token))
}
