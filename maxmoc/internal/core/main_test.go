package core

import (
	"os"
	"testing"

	"maxmock/internal/logx"
)

// TestMain глушит консольный лог: иначе прогон печатает строку на каждый
// запрос и каждую доставку.
func TestMain(m *testing.M) {
	logx.Discard()
	os.Exit(m.Run())
}
