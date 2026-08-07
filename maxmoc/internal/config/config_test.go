package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" {
		t.Errorf("Listen = %q", c.Listen)
	}
	if c.PublicBaseURL != "http://localhost:8080" {
		t.Errorf("PublicBaseURL = %q", c.PublicBaseURL)
	}
	if c.DBPath != "max-mock.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if c.BlobDir != "blobs" {
		t.Errorf("BlobDir = %q", c.BlobDir)
	}
	if c.LogRetentionDays != 14 {
		t.Errorf("LogRetentionDays = %d", c.LogRetentionDays)
	}
	if !c.ValidateResponses {
		t.Error("ValidateResponses должен быть включён по умолчанию")
	}
	if c.Webhook.TimeoutSec != 10 || c.Webhook.Retries != 2 {
		t.Errorf("Webhook = %+v", c.Webhook)
	}
	if len(c.Webhook.BackoffSec) != 2 {
		t.Errorf("BackoffSec = %v", c.Webhook.BackoffSec)
	}
}

func TestLoadFileOverridesDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	body := "listen: \":9090\"\nlog_retention_days: 3\nvalidate_responses: false\nwebhook:\n  timeout_sec: 5\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":9090" || c.LogRetentionDays != 3 || c.Webhook.TimeoutSec != 5 {
		t.Fatalf("файл не применён: %+v", c)
	}
	if c.ValidateResponses {
		t.Error("validate_responses: false из файла не применён")
	}
	// не заданные в файле поля остаются дефолтными
	if c.DBPath != "max-mock.db" || c.Webhook.Retries != 2 {
		t.Fatalf("дефолты затёрты: %+v", c)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte("db_path: \"from-file.db\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAXMOCK_DB_PATH", "/tmp/from-env.db")
	t.Setenv("MAXMOCK_LISTEN", ":7777")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.DBPath != "/tmp/from-env.db" {
		t.Errorf("env не перекрыл файл: %q", c.DBPath)
	}
	if c.Listen != ":7777" {
		t.Errorf("MAXMOCK_LISTEN не применён: %q", c.Listen)
	}
}

func TestLoadMissingFileIsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "нет.yaml")); err == nil {
		t.Fatal("ожидалась ошибка на отсутствующем файле")
	}
}

// write кладёт конфиг во временный файл и возвращает путь.
func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTLSDisabledByDefault(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.TLS.Enabled() {
		t.Error("без сертификата в конфиге TLS должен быть выключен")
	}
	if c.Webhook.CAFile != "" || c.Webhook.InsecureSkipVerify {
		t.Errorf("проверка стендов по умолчанию должна быть штатной: %+v", c.Webhook)
	}
}

func TestTLSFromFile(t *testing.T) {
	p := write(t, "public_base_url: \"https://mock.local:8443\"\ntls:\n  cert_file: \"/etc/c.pem\"\n  key_file: \"/etc/k.pem\"\nwebhook:\n  ca_file: \"/etc/ca.pem\"\n  insecure_skip_verify: true\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.TLS.Enabled() || c.TLS.CertFile != "/etc/c.pem" || c.TLS.KeyFile != "/etc/k.pem" {
		t.Errorf("блок tls не применён: %+v", c.TLS)
	}
	if c.Webhook.CAFile != "/etc/ca.pem" || !c.Webhook.InsecureSkipVerify {
		t.Errorf("настройки доверия не применены: %+v", c.Webhook)
	}
}

func TestTLSEnvOverridesFile(t *testing.T) {
	p := write(t, "public_base_url: \"https://mock.local:8443\"\ntls:\n  cert_file: \"/from-file.pem\"\n  key_file: \"/from-file.key\"\n")
	t.Setenv("MAXMOCK_TLS_CERT_FILE", "/from-env.pem")
	t.Setenv("MAXMOCK_WEBHOOK_CA_FILE", "/from-env-ca.pem")
	t.Setenv("MAXMOCK_WEBHOOK_INSECURE_SKIP_VERIFY", "yes")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.TLS.CertFile != "/from-env.pem" || c.TLS.KeyFile != "/from-file.key" {
		t.Errorf("env не перекрыл файл: %+v", c.TLS)
	}
	if c.Webhook.CAFile != "/from-env-ca.pem" || !c.Webhook.InsecureSkipVerify {
		t.Errorf("env для доставки не применён: %+v", c.Webhook)
	}
}

func TestInsecureEnvRejectsGarbage(t *testing.T) {
	t.Setenv("MAXMOCK_WEBHOOK_INSECURE_SKIP_VERIFY", "нет")
	if _, err := Load(""); err == nil {
		t.Fatal("непонятное значение булевой переменной должно быть ошибкой, а не молчаливым «да»")
	}
}

func TestInsecureEnvOffValues(t *testing.T) {
	p := write(t, "webhook:\n  insecure_skip_verify: true\n")
	t.Setenv("MAXMOCK_WEBHOOK_INSECURE_SKIP_VERIFY", "false")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Webhook.InsecureSkipVerify {
		t.Error("env со значением false должен выключать флаг из файла")
	}
}

// Половина пары — самая частая ошибка при переносе конфига, и она обязана
// останавливать запуск, а не оставлять мок молча на HTTP.
func TestHalfOfTLSPairIsError(t *testing.T) {
	for _, body := range []string{
		"tls:\n  cert_file: \"/etc/c.pem\"\n",
		"tls:\n  key_file: \"/etc/k.pem\"\n",
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("ожидалась ошибка на половине пары:\n%s", body)
		}
	}
}

func TestTLSWithHTTPPublicBaseURLIsError(t *testing.T) {
	p := write(t, "public_base_url: \"http://mock.local:8443\"\ntls:\n  cert_file: \"/etc/c.pem\"\n  key_file: \"/etc/k.pem\"\n")
	_, err := Load(p)
	if err == nil {
		t.Fatal("http:// в public_base_url при включённом TLS должен останавливать запуск")
	}
	if !strings.Contains(err.Error(), "public_base_url") {
		t.Errorf("сообщение не называет поле: %v", err)
	}
}

// Обратное сочетание законно: мок за прокси, который сам терминирует TLS.
func TestHTTPSPublicBaseURLWithoutTLSIsAllowed(t *testing.T) {
	p := write(t, "public_base_url: \"https://mock.local\"\n")
	if _, err := Load(p); err != nil {
		t.Fatalf("https в адресе без своего TLS — законная конфигурация за прокси: %v", err)
	}
}
