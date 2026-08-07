package maxapi

import (
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTLSStub поднимает HTTPS-заглушку с самоподписанным сертификатом и
// возвращает её адрес и сертификат в формате PEM. Так воспроизводится
// ситуация с Max API, чей корневой УЦ (Минцифры) не входит в системный набор.
func newTLSStub(t *testing.T) (addr string, certPEM []byte) {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"user_id":1,"first_name":"Демобот","username":"demo_bot","is_bot":true}`)
	}))
	t.Cleanup(srv.Close)

	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: srv.Certificate().Raw,
	})
	return srv.URL, certPEM
}

func TestUntrustedCertificateIsRejectedByDefault(t *testing.T) {
	addr, _ := newTLSStub(t)
	client := New("test-token", WithBaseURL(addr))

	_, err := client.GetMyInfo(context.Background())

	if err == nil {
		t.Fatal("GetMyInfo вернул nil, want ошибку проверки сертификата")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("ошибка = %q, want ошибку проверки сертификата", err)
	}
}

func TestExtraRootCAMakesCertificateTrusted(t *testing.T) {
	addr, certPEM := newTLSStub(t)

	caFile := filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(caFile, certPEM, 0o600); err != nil {
		t.Fatalf("запись сертификата: %v", err)
	}

	client, err := NewWithRootCA("test-token", caFile, WithBaseURL(addr))
	if err != nil {
		t.Fatalf("NewWithRootCA: %v", err)
	}

	info, err := client.GetMyInfo(context.Background())
	if err != nil {
		t.Fatalf("GetMyInfo: %v", err)
	}
	if info.Username != "demo_bot" {
		t.Errorf("Username = %q, want %q", info.Username, "demo_bot")
	}
}

func TestNewWithRootCAReportsMissingFile(t *testing.T) {
	_, err := NewWithRootCA("test-token", filepath.Join(t.TempDir(), "нет.pem"))

	if err == nil {
		t.Fatal("NewWithRootCA вернул nil, want ошибку")
	}
}

func TestNewWithRootCAReportsInvalidPEM(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "битый.pem")
	if err := os.WriteFile(caFile, []byte("это не сертификат"), 0o600); err != nil {
		t.Fatalf("запись файла: %v", err)
	}

	_, err := NewWithRootCA("test-token", caFile)

	if err == nil {
		t.Fatal("NewWithRootCA вернул nil, want ошибку")
	}
	if !strings.Contains(err.Error(), "сертификат") {
		t.Errorf("ошибка = %q, want упоминание сертификата", err)
	}
}
