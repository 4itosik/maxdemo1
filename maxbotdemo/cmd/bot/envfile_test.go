package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeEnvFile кладёт файл во временный каталог и возвращает путь к нему.
func writeEnvFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".bot.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("запись файла: %v", err)
	}
	return path
}

func TestParseEnvFileReadsPairs(t *testing.T) {
	vars, err := parseEnvFile(strings.NewReader(
		"# профиль max-mock\n" +
			"\n" +
			"MAX_BOT_TOKEN=bot.123\n" +
			"  LISTEN_ADDR = :8081 \n" +
			"export WEBHOOK_SECRET='mock-secret-123'\n" +
			"WEBHOOK_URL=\"http://localhost:8081/max/v1.0/webhooks/b3d4a7f0-1c2e-4f6a-9d8b-5e0c7a1f2b34\"\n" +
			"MAX_API_BASE_URL=http://localhost:8080/?a=1\r\n"))
	if err != nil {
		t.Fatalf("parseEnvFile: %v", err)
	}

	want := map[string]string{
		"MAX_BOT_TOKEN":    "bot.123",
		"LISTEN_ADDR":      ":8081",
		"WEBHOOK_SECRET":   "mock-secret-123",
		"WEBHOOK_URL":      "http://localhost:8081/max/v1.0/webhooks/b3d4a7f0-1c2e-4f6a-9d8b-5e0c7a1f2b34",
		"MAX_API_BASE_URL": "http://localhost:8080/?a=1",
	}
	if len(vars) != len(want) {
		t.Fatalf("прочитано %d переменных, want %d: %v", len(vars), len(want), vars)
	}
	for k, v := range want {
		if vars[k] != v {
			t.Errorf("%s = %q, want %q", k, vars[k], v)
		}
	}
}

// Строка без `=` — почти всегда опечатка. Молча пропустить её значит
// запустить бота с недостающей переменной и искать причину в логе.
func TestParseEnvFileRejectsLineWithoutEquals(t *testing.T) {
	_, err := parseEnvFile(strings.NewReader("MAX_BOT_TOKEN=bot.123\nWEBHOOK_SECRET\n"))
	if err == nil {
		t.Fatal("parseEnvFile вернул nil, want ошибку")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("ошибка = %q, want указание номера строки", err)
	}
}

func TestParseEnvFileRejectsEmptyName(t *testing.T) {
	if _, err := parseEnvFile(strings.NewReader("=значение\n")); err == nil {
		t.Fatal("parseEnvFile вернул nil, want ошибку про пустое имя")
	}
}

func TestReadEnvFileReportsMissingFile(t *testing.T) {
	_, err := readEnvFile(filepath.Join(t.TempDir(), "нет-такого.env"))
	if err == nil {
		t.Fatal("readEnvFile вернул nil, want ошибку")
	}
	if !strings.Contains(err.Error(), "нет-такого.env") {
		t.Errorf("ошибка = %q, want упоминание пути", err)
	}
}

// Переменная, заданная в окружении, перебивает файл: README предлагает
// подставлять адрес туннеля перед командой, и это должно работать.
func TestEnvFileDoesNotOverrideEnvironment(t *testing.T) {
	path := writeEnvFile(t, "MAX_BOT_TOKEN=из-файла\nWEBHOOK_SECRET=из-файла\n")
	vars, err := readEnvFile(path)
	if err != nil {
		t.Fatalf("readEnvFile: %v", err)
	}

	env := map[string]string{"MAX_BOT_TOKEN": "из-окружения"}
	getenv := envFirst(envOf(env), vars)

	if got := getenv("MAX_BOT_TOKEN"); got != "из-окружения" {
		t.Errorf("MAX_BOT_TOKEN = %q, want значение из окружения", got)
	}
	if got := getenv("WEBHOOK_SECRET"); got != "из-файла" {
		t.Errorf("WEBHOOK_SECRET = %q, want значение из файла", got)
	}
	if got := getenv("LISTEN_ADDR"); got != "" {
		t.Errorf("LISTEN_ADDR = %q, want пустую строку", got)
	}
}

// Файл проходит весь путь до конфига: это и есть `./main .bot.env`.
func TestLoadConfigFromEnvFile(t *testing.T) {
	path := writeEnvFile(t, "MAX_BOT_TOKEN=bot.123\n"+
		"WEBHOOK_URL=https://example.test"+webhookPathSample+"\n")
	vars, err := readEnvFile(path)
	if err != nil {
		t.Fatalf("readEnvFile: %v", err)
	}

	cfg, err := loadConfig(envFirst(envOf(nil), vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Token != "bot.123" {
		t.Errorf("Token = %q, want %q", cfg.Token, "bot.123")
	}
	if cfg.WebhookPath != webhookPathSample {
		t.Errorf("WebhookPath = %q, want %q", cfg.WebhookPath, webhookPathSample)
	}
}

func TestEnvironmentWithoutArgumentsUsesProcessEnv(t *testing.T) {
	t.Setenv("MAX_BOT_TOKEN", "из-окружения")

	getenv, err := environment(nil)
	if err != nil {
		t.Fatalf("environment: %v", err)
	}
	if got := getenv("MAX_BOT_TOKEN"); got != "из-окружения" {
		t.Errorf("MAX_BOT_TOKEN = %q, want значение из окружения", got)
	}
}

func TestEnvironmentReadsFileArgument(t *testing.T) {
	path := writeEnvFile(t, "MAX_BOT_TOKEN=из-файла\n")

	getenv, err := environment([]string{path})
	if err != nil {
		t.Fatalf("environment: %v", err)
	}
	if got := getenv("MAX_BOT_TOKEN"); got != "из-файла" {
		t.Errorf("MAX_BOT_TOKEN = %q, want значение из файла", got)
	}
}

func TestEnvironmentRejectsUnreadableFile(t *testing.T) {
	if _, err := environment([]string{filepath.Join(t.TempDir(), "нет.env")}); err == nil {
		t.Fatal("environment вернул nil, want ошибку про недоступный файл")
	}
}

// Второй аргумент — почти наверняка опечатка или лишний флаг; принять его
// молча значит запустить бота не с тем файлом.
func TestEnvironmentRejectsExtraArguments(t *testing.T) {
	_, err := environment([]string{".bot.env", "лишнее"})
	if err == nil {
		t.Fatal("environment вернул nil, want ошибку про лишний аргумент")
	}
	if !strings.Contains(err.Error(), "лишнее") {
		t.Errorf("ошибка = %q, want упоминание лишнего аргумента", err)
	}
}
