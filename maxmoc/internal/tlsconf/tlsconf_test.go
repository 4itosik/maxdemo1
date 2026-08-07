package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maxmock/internal/testcert"
)

func TestServerConfigLoadsPair(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, err := testcert.Generate(dir, "max-mock.stand.local", "10.0.0.7")
	if err != nil {
		t.Fatal(err)
	}

	cfg, info, err := ServerConfig(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("сертификатов в конфигурации: %d", len(cfg.Certificates))
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, ожидалась TLS 1.2: у сервера Go граница по умолчанию ниже", cfg.MinVersion)
	}
	if info.Subject != "max-mock.stand.local" {
		t.Errorf("Subject = %q", info.Subject)
	}
	if len(info.Hosts) != 2 || info.Hosts[0] != "max-mock.stand.local" || info.Hosts[1] != "10.0.0.7" {
		t.Errorf("Hosts = %v, ожидались имя и адрес из SAN", info.Hosts)
	}
	if time.Until(info.NotAfter) <= 0 {
		t.Errorf("NotAfter = %s, ожидался срок в будущем", info.NotAfter)
	}
	// Строка уходит в стартовый лог — в ней должно хватать данных, чтобы
	// понять, почему стенд не доверяет моку, не запуская openssl.
	if s := info.String(); !strings.Contains(s, "max-mock.stand.local") || !strings.Contains(s, "10.0.0.7") {
		t.Errorf("описание сертификата не содержит SAN: %q", s)
	}
}

func TestServerConfigReportsMissingFile(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, err := testcert.Generate(dir, "mock.local")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ServerConfig(filepath.Join(dir, "нет.pem"), keyFile); err == nil {
		t.Error("ожидалась ошибка на отсутствующем сертификате")
	}
	if _, _, err := ServerConfig(certFile, filepath.Join(dir, "нет.pem")); err == nil {
		t.Error("ожидалась ошибка на отсутствующем ключе")
	}
}

func TestServerConfigReportsBrokenKey(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, err := testcert.Generate(dir, "mock.local")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("не ключ вовсе"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = ServerConfig(certFile, keyFile)
	if err == nil {
		t.Fatal("ожидалась ошибка на битом ключе")
	}
	// Путь в сообщении обязателен: в контуре пару чаще всего собирают из
	// файлов разного происхождения, и надо знать, какой из двух виноват.
	if !strings.Contains(err.Error(), keyFile) {
		t.Errorf("в ошибке нет пути к ключу: %v", err)
	}
}

// Ключ от другого сертификата — типовая ошибка при ручном копировании
// файлов на стенд, и она должна ловиться на старте, а не при первом
// подключении.
func TestServerConfigRejectsMismatchedPair(t *testing.T) {
	certFile, _, err := testcert.Generate(t.TempDir(), "mock.local")
	if err != nil {
		t.Fatal(err)
	}
	_, otherKey, err := testcert.Generate(t.TempDir(), "mock.local")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ServerConfig(certFile, otherKey); err == nil {
		t.Error("пара из разных сертификатов принята")
	}
}

func TestClientConfigWithoutCAUsesSystemRoots(t *testing.T) {
	cfg, err := ClientConfig("", false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Errorf("без ca_file ожидалась штатная проверка (nil), получено %+v", cfg)
	}
}

func TestClientConfigTrustsCAFromFile(t *testing.T) {
	stand := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stand.Close()

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := testcert.WritePEM(caFile, stand.Certificate().Raw); err != nil {
		t.Fatal(err)
	}

	// Без CA стенду с самоподписанным сертификатом доверять нечем.
	if _, err := get(t, nil, stand.URL); err == nil {
		t.Error("соединение без CA должно отвергаться")
	}

	cfg, err := ClientConfig(caFile, false)
	if err != nil {
		t.Fatal(err)
	}
	status, err := get(t, cfg, stand.URL)
	if err != nil {
		t.Fatalf("с CA из файла соединение должно проходить: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("статус: %d", status)
	}
}

// Системные корни не должны теряться при добавлении своего CA: в контуре
// часть стендов живёт на внутреннем CA, часть — на публичном сертификате.
//
// Пересчитать пул нельзя — Subjects() для системного пула документированно
// пуст, — поэтому сравниваем с пулом, где лежит только наш CA: если пулы
// различаются, значит в собранном есть что-то ещё.
func TestClientConfigKeepsSystemRoots(t *testing.T) {
	if sys, err := x509.SystemCertPool(); err != nil || sys == nil || sys.Equal(x509.NewCertPool()) {
		t.Skip("на этой машине нет системных корневых сертификатов — дополнять нечего")
	}

	certFile, _, err := testcert.Generate(t.TempDir(), "stand.local")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, body, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ClientConfig(caFile, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("пул корневых не собран")
	}
	onlyOurCA := x509.NewCertPool()
	if !onlyOurCA.AppendCertsFromPEM(body) {
		t.Fatal("CA не разобран")
	}
	if cfg.RootCAs.Equal(onlyOurCA) {
		t.Error("в пуле только CA из файла: системные корни заменены, а не дополнены")
	}
}

func TestClientConfigRejectsGarbageCA(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte("это не PEM"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ClientConfig(caFile, false); err == nil {
		t.Error("ожидалась ошибка на файле без сертификатов")
	}
	if _, err := ClientConfig(filepath.Join(t.TempDir(), "нет.pem"), false); err == nil {
		t.Error("ожидалась ошибка на отсутствующем CA-файле")
	}
}

func TestClientConfigInsecureSkipsVerification(t *testing.T) {
	stand := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stand.Close()

	cfg, err := ClientConfig("", true)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify не выставлен")
	}
	if _, err := get(t, cfg, stand.URL); err != nil {
		t.Errorf("с отключённой проверкой соединение должно проходить: %v", err)
	}
}

func get(t *testing.T, cfg *tls.Config, url string) (int, error) {
	t.Helper()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = cfg
	c := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
