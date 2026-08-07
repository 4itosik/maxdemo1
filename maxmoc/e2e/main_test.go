package e2e

import (
	"os"
	"testing"

	"maxmock/internal/logx"
)

// TestMain глушит консольный лог: сценарий e2e прогоняет через мок десятки
// запросов, и каждый напечатал бы строку поверх вывода теста.
func TestMain(m *testing.M) {
	logx.Discard()
	os.Exit(m.Run())
}
