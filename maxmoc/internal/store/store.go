// Package store — персистентное состояние мока в SQLite (без cgo).
//
// Пул ограничен одним соединением: мок не нагрузочный стенд, а сериализация
// доступа полностью снимает вопрос «database is locked» и делает порядок
// записей предсказуемым (важно для seq сообщений и для журнала).
package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// ErrNotFound — запрошенной записи нет.
var ErrNotFound = errors.New("запись не найдена")

// ErrConflict — запись с таким идентификатором уже есть.
var ErrConflict = errors.New("запись уже существует")

// Store — хранилище мока.
type Store struct {
	db *sql.DB
}

// Open открывает (создавая при необходимости) базу и применяет схему.
// path ":memory:" даёт базу в памяти — используется в тестах.
func Open(path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	if path == ":memory:" {
		dsn = "file::memory:?_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("открытие базы %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("подключение к базе %s: %w", path, err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("применение схемы: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrations — доработки схемы для баз, созданных предыдущими версиями мока.
// Для свежей базы всё это уже сделано в schema.sql, поэтому ошибка «столбец
// уже существует» — ожидаемый исход, а не сбой.
var migrations = []string{
	`ALTER TABLE bots ADD COLUMN commands TEXT NOT NULL DEFAULT '[]'`,
	// Карточка клиента: фамилия уезжает к КЦ в схеме User, телефон и
	// координаты — только вложениями contact и location.
	`ALTER TABLE clients ADD COLUMN last_name TEXT`,
	`ALTER TABLE clients ADD COLUMN phone TEXT`,
	`ALTER TABLE clients ADD COLUMN latitude REAL`,
	`ALTER TABLE clients ADD COLUMN longitude REAL`,
	// Лента событий диалога: журналы жили в разрезе бота, и связать вызов
	// Bot API или доставку с конкретным диалогом было нечем.
	`ALTER TABLE request_log ADD COLUMN chat_id INTEGER`,
	`ALTER TABLE webhook_deliveries ADD COLUMN chat_id INTEGER`,
	// Ответ стенда на вебхук: раньше тело сливалось в io.Discard, и в ленте
	// было видно только «стенд ответил 200», а что именно он вернул — нет.
	`ALTER TABLE webhook_deliveries ADD COLUMN response_body TEXT NOT NULL DEFAULT ''`,
	// Индексы идут миграцией, а не schema.sql: схема выполняется до миграций,
	// и на старой базе индекс по ещё не добавленному столбцу упал бы.
	`CREATE INDEX IF NOT EXISTS idx_request_log_chat_ts ON request_log(chat_id, ts DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_deliveries_chat_ts ON webhook_deliveries(chat_id, ts DESC)`,
}

func migrate(db *sql.DB) error {
	for _, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("миграция %q: %w", stmt, err)
		}
	}
	return nil
}

// Close закрывает базу.
func (s *Store) Close() error { return s.db.Close() }

// DB даёт доступ к соединению — нужен служебным операциям вне DAO.
func (s *Store) DB() *sql.DB { return s.db }

// nowMS — текущее время в unix-миллисекундах (формат таймстемпов Max).
func nowMS() int64 { return time.Now().UnixMilli() }

// nullString превращает пустую строку в SQL NULL.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
