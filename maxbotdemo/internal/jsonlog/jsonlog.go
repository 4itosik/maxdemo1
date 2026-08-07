// Package jsonlog кладёт сырой JSON в лог так, чтобы он остался JSON'ом, а не
// строкой с экранированными кавычками.
package jsonlog

import "encoding/json"

// Raw — сырое тело запроса или ответа, пригодное как значение атрибута slog.
//
// slog.JSONHandler кодирует такое значение через json.Marshal, поэтому тело
// возвращается как есть и вкладывается в строку лога объектом. Побочно
// json.Marshal сжимает отступы и переводы строк — читать лог это не мешает.
type Raw []byte

// MarshalJSON всегда возвращает валидный JSON: строка лога не должна ломаться
// из-за того, что в неё попало.
func (r Raw) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		// Тела нет вовсе — например, у GET /me.
		return []byte("null"), nil
	}
	if !json.Valid(r) {
		// Не JSON: обрезанное тело или мусор от чужого запроса. Кладём
		// текстом, чтобы причина не потерялась.
		return json.Marshal(string(r))
	}
	return r, nil
}
