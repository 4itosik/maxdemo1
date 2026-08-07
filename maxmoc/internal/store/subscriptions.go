package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Subscription — webhook-подписка бота.
// UpdateTypes == nil означает «все типы событий» (поле в контракте nullable).
type Subscription struct {
	ID          int64    `json:"id"`
	BotID       int64    `json:"bot_id"`
	URL         string   `json:"url"`
	UpdateTypes []string `json:"update_types"`
	Secret      string   `json:"secret,omitempty"`
	CreatedAt   int64    `json:"created_at"`
}

// Wants сообщает, подходит ли событие под фильтр подписки.
func (s Subscription) Wants(updateType string) bool {
	if s.UpdateTypes == nil {
		return true
	}
	for _, t := range s.UpdateTypes {
		if t == updateType {
			return true
		}
	}
	return false
}

func scanSubscription(row interface{ Scan(...any) error }) (*Subscription, error) {
	var s Subscription
	var types sql.NullString
	if err := row.Scan(&s.ID, &s.BotID, &s.URL, &types, &s.Secret, &s.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if types.Valid {
		if err := json.Unmarshal([]byte(types.String), &s.UpdateTypes); err != nil {
			return nil, err
		}
	}
	return &s, nil
}

const subscriptionColumns = `id, bot_id, url, update_types, secret, created_at`

// AddSubscription создаёт подписку или обновляет существующую с тем же URL:
// повторный POST /subscriptions с тем же адресом в Max не плодит дубликаты.
func (s *Store) AddSubscription(botID int64, url string, updateTypes []string, secret string) (*Subscription, error) {
	var types any
	if updateTypes != nil {
		b, err := json.Marshal(updateTypes)
		if err != nil {
			return nil, err
		}
		types = string(b)
	}
	now := nowMS()
	if _, err := s.db.Exec(
		`INSERT INTO subscriptions (bot_id, url, update_types, secret, created_at) VALUES (?,?,?,?,?)
		 ON CONFLICT(bot_id, url) DO UPDATE SET update_types = excluded.update_types, secret = excluded.secret`,
		botID, url, types, secret, now); err != nil {
		return nil, err
	}
	return scanSubscription(s.db.QueryRow(
		`SELECT `+subscriptionColumns+` FROM subscriptions WHERE bot_id = ? AND url = ?`, botID, url))
}

// ListSubscriptions возвращает подписки бота.
func (s *Store) ListSubscriptions(botID int64) ([]Subscription, error) {
	rows, err := s.db.Query(`SELECT `+subscriptionColumns+` FROM subscriptions WHERE bot_id = ? ORDER BY id`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sub)
	}
	return out, rows.Err()
}

// DeleteSubscription удаляет подписку по URL.
func (s *Store) DeleteSubscription(botID int64, url string) error {
	res, err := s.db.Exec(`DELETE FROM subscriptions WHERE bot_id = ? AND url = ?`, botID, url)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}
