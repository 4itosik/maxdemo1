package jsonlog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

// logPayload пишет значение в JSON-лог и возвращает разобранную запись.
// Проверять MarshalJSON напрямую мало: смысл типа в том, как он выглядит
// внутри строки лога, а её собирает slog.
func logPayload(t *testing.T, v Raw) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("запись", "payload", v)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("строка лога не разбирается как JSON: %v (%s)", err, buf.String())
	}
	return rec
}

func TestValidJSONIsEmbeddedAsObject(t *testing.T) {
	rec := logPayload(t, Raw(`{"text":"привет","n":1}`))

	payload, ok := rec["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want объект", rec["payload"])
	}
	if payload["text"] != "привет" {
		t.Errorf(`payload["text"] = %#v, want "привет"`, payload["text"])
	}
	if payload["n"] != float64(1) {
		t.Errorf(`payload["n"] = %#v, want 1`, payload["n"])
	}
}

// Вложенные объекты не должны схлопываться в строку: именно так приезжает
// тело события с сообщением и вложениями.
func TestNestedObjectSurvives(t *testing.T) {
	rec := logPayload(t, Raw(`{"message":{"body":{"text":"привет"}}}`))

	payload, ok := rec["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want объект", rec["payload"])
	}
	message, ok := payload["message"].(map[string]any)
	if !ok {
		t.Fatalf(`payload["message"] = %#v, want объект`, payload["message"])
	}
	body, ok := message["body"].(map[string]any)
	if !ok {
		t.Fatalf(`message["body"] = %#v, want объект`, message["body"])
	}
	if body["text"] != "привет" {
		t.Errorf(`body["text"] = %#v, want "привет"`, body["text"])
	}
}

func TestNonJSONBecomesString(t *testing.T) {
	rec := logPayload(t, Raw(`{"update_type":`))

	if rec["payload"] != `{"update_type":` {
		t.Errorf("payload = %#v, want строку с исходным текстом", rec["payload"])
	}
}

func TestEmptyBecomesNull(t *testing.T) {
	rec := logPayload(t, Raw(nil))

	v, ok := rec["payload"]
	if !ok {
		t.Fatal("в записи нет поля payload, want null")
	}
	if v != nil {
		t.Errorf("payload = %#v, want null", v)
	}
}
