package maxapi

import (
	"strings"
	"testing"
)

func TestCheckWebhookURLAccepts(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"https", "https://example.test/webhook"},
		{"http на localhost", "http://localhost:8081/webhook"},
		{"http на 127.0.0.1", "http://127.0.0.1:8081/webhook"},
		{"http на ::1", "http://[::1]:8081/webhook"},
		{"http на localhost без порта", "http://localhost/webhook"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckWebhookURL(tt.raw); err != nil {
				t.Errorf("CheckWebhookURL(%q) = %v, want nil", tt.raw, err)
			}
		})
	}
}

func TestCheckWebhookURLRejects(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"http на чужой хост", "http://example.test/webhook", "https"},
		{"http на внешний адрес", "http://203.0.113.10:8081/webhook", "https"},
		{"чужая схема", "ftp://localhost/webhook", "https"},
		{"без схемы", "example.test/webhook", "https"},
		{"без хоста", "https:///webhook", "хост"},
		{"не разбирается", "https://exa mple.test/webhook", "адрес"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckWebhookURL(tt.raw)
			if err == nil {
				t.Fatalf("CheckWebhookURL(%q) = nil, want ошибку", tt.raw)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ошибка = %q, want упоминание %q", err, tt.wantErr)
			}
		})
	}
}
