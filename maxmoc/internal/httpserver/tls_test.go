package httpserver

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"maxmock/internal/testcert"
	"maxmock/internal/tlsconf"
)

// Сквозная проверка входящего TLS: мок целиком поднят на своём слушателе с
// настоящей парой ключей, и обе половины tlsconf работают друг с другом.
func TestServesOverTLS(t *testing.T) {
	f := newFixture(t)

	dir := t.TempDir()
	// Сертификат самоподписанный, поэтому он же служит CA-бандлом клиенту.
	certFile, keyFile, err := testcert.Generate(dir, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg, info, err := tlsconf.ServerConfig(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Hosts) != 1 || info.Hosts[0] != "127.0.0.1" {
		t.Fatalf("SAN сертификата: %v", info.Hosts)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: f.handler, TLSConfig: tlsCfg, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })

	base := "https://" + ln.Addr().String()

	t.Run("клиент без CA не проходит", func(t *testing.T) {
		if _, err := http.DefaultClient.Get(base + "/healthz"); err == nil {
			t.Error("самоподписанный сертификат принят без CA")
		}
	})

	t.Run("клиент с CA получает ответ", func(t *testing.T) {
		clientCfg, err := tlsconf.ClientConfig(certFile, false)
		if err != nil {
			t.Fatal(err)
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = clientCfg
		c := &http.Client{Transport: tr, Timeout: 5 * time.Second}

		resp, err := c.Get(base + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || string(body) != "ok" {
			t.Errorf("healthz по TLS: %d %q", resp.StatusCode, body)
		}
	})

	// Решение «только HTTPS на том же порту» проверяется явно: обычный HTTP
	// на этот порт не отвечает, и стенд, забывший сменить схему, узнаёт об
	// этом сразу.
	t.Run("обычный HTTP на порт TLS не проходит", func(t *testing.T) {
		resp, err := http.DefaultClient.Get("http://" + ln.Addr().String() + "/healthz")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusOK && strings.Contains(string(body), "ok") {
			t.Error("порт с TLS ответил на обычный HTTP")
		}
	})
}
