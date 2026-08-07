package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestClientKeepsAllCardFields(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)

	lat, lon := 55.751244, 37.618423
	c, _, err := s.CreateClient(b.ID, ClientInput{
		FirstName: "Иван", LastName: "Петров", Username: "ivan",
		Phone: "+79001234567", Latitude: &lat, Longitude: &lon,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.ClientByUserID(c.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstName != "Иван" || got.LastName != "Петров" || got.Username != "ivan" {
		t.Errorf("имя не сохранилось: %+v", got)
	}
	if got.Phone != "+79001234567" {
		t.Errorf("телефон не сохранился: %q", got.Phone)
	}
	if got.Latitude == nil || got.Longitude == nil || *got.Latitude != lat || *got.Longitude != lon {
		t.Errorf("координаты не сохранились: %+v", got)
	}

	list, err := s.ListClients(b.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListClients: %v, %v", list, err)
	}
	if list[0].Phone != "+79001234567" || list[0].LastName != "Петров" {
		t.Errorf("список отдаёт неполную карточку: %+v", list[0])
	}
}

// Пустые поля должны читаться как пустые, а не как «NULL сломал скан».
func TestClientWithoutOptionalFields(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)

	c, _, err := s.CreateClient(b.ID, ClientInput{FirstName: "Аноним"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ClientByUserID(c.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastName != "" || got.Username != "" || got.Phone != "" {
		t.Errorf("пустые поля прочитаны неверно: %+v", got)
	}
	if got.Latitude != nil || got.Longitude != nil {
		t.Errorf("координат не задавали, а они есть: %+v", got)
	}
}

// Заданный user_id — то, чем КЦ связывает клиента мока со своей записью
// абонента, поэтому он обязан использоваться как есть.
func TestClientWithExplicitUserID(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)

	const want = 1234567890123
	c, d, err := s.CreateClient(b.ID, ClientInput{UserID: ptr(int64(want)), FirstName: "Иван"})
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != want {
		t.Fatalf("user_id = %d, ожидался %d", c.UserID, want)
	}
	if d.UserID != want {
		t.Errorf("диалог открыт на другого пользователя: %d", d.UserID)
	}
}

// Молча подменить занятый идентификатор нельзя: это сорвёт ровно ту связку,
// ради которой его задают.
func TestClientWithTakenUserIDIsConflict(t *testing.T) {
	s := openTemp(t)
	b := newBot(t, s)

	const id = 555000555
	if _, _, err := s.CreateClient(b.ID, ClientInput{UserID: ptr(int64(id)), FirstName: "Первый"}); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.CreateClient(b.ID, ClientInput{UserID: ptr(int64(id)), FirstName: "Второй"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("ожидался ErrConflict, получено: %v", err)
	}

	// Первый клиент не должен пострадать от неудачной попытки.
	got, err := s.ClientByUserID(id)
	if err != nil || got.FirstName != "Первый" {
		t.Errorf("исходный клиент повреждён: %+v, %v", got, err)
	}
}

// База, созданная предыдущей версией мока, должна открываться без потери
// ботов и переписки: обновление в контуре — это замена бинарника.
func TestMigrationAddsCardColumnsToOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// Таблица клиентов в том виде, в каком её создавали до этой правки.
	// foreign_keys не включаем: бота в старой базе тут нет, а проверяем мы
	// миграцию клиентов.
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE clients (
		user_id    INTEGER PRIMARY KEY,
		bot_id     INTEGER NOT NULL,
		first_name TEXT NOT NULL,
		username   TEXT,
		created_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(
		`INSERT INTO clients (user_id, bot_id, first_name, username, created_at) VALUES (777, 1, 'Старый', 'old', 1)`,
	); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("старая база не открылась: %v", err)
	}
	defer func() { _ = s.Close() }()

	got, err := s.ClientByUserID(777)
	if err != nil {
		t.Fatalf("клиент из старой базы потерян: %v", err)
	}
	if got.FirstName != "Старый" || got.Username != "old" {
		t.Errorf("данные старого клиента изменились: %+v", got)
	}
	if got.LastName != "" || got.Phone != "" || got.Latitude != nil {
		t.Errorf("новые поля старого клиента должны быть пустыми: %+v", got)
	}
}

func ptr[T any](v T) *T { return &v }
