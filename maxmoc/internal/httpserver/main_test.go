package httpserver

import (
	"os"
	"testing"

	"maxmock/internal/logx"
)

// TestMain глушит консольный лог: иначе прогон печатает строку на каждый
// запрос фикстуры. Тесты, которым лог нужен, поднимают свой приёмник
// (см. captureLog).
func TestMain(m *testing.M) {
	logx.Discard()
	os.Exit(m.Run())
}
