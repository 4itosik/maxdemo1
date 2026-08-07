package maxapi

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// NewWithRootCA создаёт клиента, доверяющего дополнительному корневому
// сертификату из файла (PEM) — в дополнение к системным.
//
// Это нужно, потому что *.max.ru подписан цепочкой Минцифры («Russian Trusted
// Root CA»), которой нет в системном наборе macOS и большинства Linux-образов.
// Сертификат подключается только к этому клиенту: системное хранилище не
// меняется, остальные программы ничего не замечают.
func NewWithRootCA(token, caFile string, opts ...Option) (*Client, error) {
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("чтение корневого сертификата: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("файл %s не содержит сертификатов в формате PEM", caFile)
	}

	hc := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	opts = append([]Option{WithHTTPClient(hc)}, opts...)
	return New(token, opts...), nil
}
