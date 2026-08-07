package main

import (
	"strings"
	"testing"
)

// envOf делает из карты функцию чтения переменных окружения.
func envOf(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func validEnv() map[string]string {
	return map[string]string{
		"MAX_BOT_TOKEN": "token-123",
		"WEBHOOK_URL":   "https://example.test/webhook",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(envOf(validEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.Token != "token-123" {
		t.Errorf("Token = %q, want %q", cfg.Token, "token-123")
	}
	if cfg.WebhookURL != "https://example.test/webhook" {
		t.Errorf("WebhookURL = %q", cfg.WebhookURL)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.WebhookPath != "/webhook" {
		t.Errorf("WebhookPath = %q, want %q", cfg.WebhookPath, "/webhook")
	}
	if cfg.Secret != "" {
		t.Errorf("Secret = %q, want пустую строку", cfg.Secret)
	}
	if cfg.UnsubscribeOnExit {
		t.Error("UnsubscribeOnExit = true, want false")
	}
	if cfg.BaseURL == "" {
		t.Error("BaseURL пуст, want адрес Max API по умолчанию")
	}
	if cfg.CAFile != "" {
		t.Errorf("CAFile = %q, want пустую строку", cfg.CAFile)
	}
}

func TestLoadConfigReadsCAFile(t *testing.T) {
	vars := validEnv()
	vars["MAX_API_CA_FILE"] = "certs/russian_trusted_root_ca.pem"

	cfg, err := loadConfig(envOf(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.CAFile != "certs/russian_trusted_root_ca.pem" {
		t.Errorf("CAFile = %q", cfg.CAFile)
	}
}

func TestLoadConfigAcceptsBotTokenAsFallback(t *testing.T) {
	vars := validEnv()
	delete(vars, "MAX_BOT_TOKEN")
	vars["BOT_TOKEN"] = "из-BOT_TOKEN"

	cfg, err := loadConfig(envOf(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.Token != "из-BOT_TOKEN" {
		t.Errorf("Token = %q, want %q", cfg.Token, "из-BOT_TOKEN")
	}
}

func TestLoadConfigPrefersMaxBotToken(t *testing.T) {
	vars := validEnv()
	vars["BOT_TOKEN"] = "запасной"

	cfg, err := loadConfig(envOf(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.Token != "token-123" {
		t.Errorf("Token = %q, want значение из MAX_BOT_TOKEN", cfg.Token)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	vars := validEnv()
	vars["LISTEN_ADDR"] = "127.0.0.1:9999"
	vars["WEBHOOK_SECRET"] = "top-secret_1"
	vars["MAX_API_BASE_URL"] = "http://localhost:1234"
	vars["UNSUBSCRIBE_ON_EXIT"] = "1"

	cfg, err := loadConfig(envOf(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.ListenAddr != "127.0.0.1:9999" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.Secret != "top-secret_1" {
		t.Errorf("Secret = %q", cfg.Secret)
	}
	if cfg.BaseURL != "http://localhost:1234" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if !cfg.UnsubscribeOnExit {
		t.Error("UnsubscribeOnExit = false, want true")
	}
}

func TestLoadConfigDerivesWebhookPathFromURL(t *testing.T) {
	vars := validEnv()
	vars["WEBHOOK_URL"] = "https://example.test/hooks/max"

	cfg, err := loadConfig(envOf(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.WebhookPath != "/hooks/max" {
		t.Errorf("WebhookPath = %q, want %q", cfg.WebhookPath, "/hooks/max")
	}
}

// Профиль max-mock: эмулятор слушает петлевой адрес по HTTP, и подписка на
// него должна проходить — туннеля с сертификатом в этом сценарии нет.
func TestLoadConfigAcceptsLoopbackHTTPWebhook(t *testing.T) {
	vars := validEnv()
	vars["WEBHOOK_URL"] = "http://localhost:8081/webhook"

	cfg, err := loadConfig(envOf(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.WebhookPath != "/webhook" {
		t.Errorf("WebhookPath = %q, want %q", cfg.WebhookPath, "/webhook")
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{
			name:    "нет токена",
			mutate:  func(v map[string]string) { delete(v, "MAX_BOT_TOKEN") },
			wantErr: "MAX_BOT_TOKEN",
		},
		{
			name:    "нет URL",
			mutate:  func(v map[string]string) { delete(v, "WEBHOOK_URL") },
			wantErr: "WEBHOOK_URL",
		},
		{
			name:    "URL по HTTP",
			mutate:  func(v map[string]string) { v["WEBHOOK_URL"] = "http://example.test/webhook" },
			wantErr: "https",
		},
		{
			name:    "URL без пути",
			mutate:  func(v map[string]string) { v["WEBHOOK_URL"] = "https://example.test" },
			wantErr: "путь",
		},
		{
			name:    "битый URL",
			mutate:  func(v map[string]string) { v["WEBHOOK_URL"] = "https://exa mple.test/webhook" },
			wantErr: "WEBHOOK_URL",
		},
		{
			name:    "короткий секрет",
			mutate:  func(v map[string]string) { v["WEBHOOK_SECRET"] = "abc" },
			wantErr: "WEBHOOK_SECRET",
		},
		{
			name:    "секрет с недопустимыми символами",
			mutate:  func(v map[string]string) { v["WEBHOOK_SECRET"] = "секрет-на-русском" },
			wantErr: "WEBHOOK_SECRET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := validEnv()
			tt.mutate(vars)

			_, err := loadConfig(envOf(vars))
			if err == nil {
				t.Fatal("loadConfig вернул nil, want ошибку")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ошибка = %q, want упоминание %q", err, tt.wantErr)
			}
		})
	}
}
