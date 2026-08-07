// Package tlsconf собирает конфигурации TLS для двух сторон обмена: слушателя
// мока и клиента доставки вебхуков.
//
// Пакет ничего не знает ни про мок, ни про его конфигурацию: на входе пути к
// файлам, на выходе *tls.Config. Это позволяет проверять его на настоящих
// сертификатах, не поднимая сервис.
package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"
)

// CertInfo — краткое описание сертификата сервера для стартового лога.
//
// В закрытом контуре это первый ответ на «почему браузер ругается» и «почему
// стенд не подключается»: имена в SAN и срок годности видны сразу, без
// openssl на боевом хосте.
type CertInfo struct {
	Subject  string
	Hosts    []string // DNS-имена и IP-адреса из SAN
	NotAfter time.Time
}

// String даёт строку для лога.
func (i CertInfo) String() string {
	s := i.Subject
	if s == "" {
		s = "без CN"
	}
	if len(i.Hosts) > 0 {
		s += " (SAN: " + strings.Join(i.Hosts, ", ") + ")"
	}
	return s + ", действителен до " + i.NotAfter.Format("2006-01-02")
}

// ServerConfig грузит пару cert/key и собирает конфигурацию слушателя.
//
// Загрузка выполняется здесь, а не внутри ListenAndServeTLS, чтобы битый
// путь, отобранные права или ключ от другого сертификата были видны при
// старте, а не при первом подключении стенда.
func ServerConfig(certFile, keyFile string) (*tls.Config, CertInfo, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, CertInfo{}, fmt.Errorf("сертификат TLS (%s + %s): %w", certFile, keyFile, err)
	}
	leaf := pair.Leaf
	if leaf == nil {
		if leaf, err = x509.ParseCertificate(pair.Certificate[0]); err != nil {
			return nil, CertInfo{}, fmt.Errorf("разбор сертификата %s: %w", certFile, err)
		}
	}

	info := CertInfo{Subject: leaf.Subject.CommonName, NotAfter: leaf.NotAfter}
	info.Hosts = append(info.Hosts, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		info.Hosts = append(info.Hosts, ip.String())
	}

	// MinVersion задан явно: у сервера Go нижняя граница по умолчанию ниже
	// TLS 1.2, и полагаться на её изменение в будущих версиях не стоит.
	cfg := &tls.Config{
		Certificates: []tls.Certificate{pair},
		MinVersion:   tls.VersionTLS12,
	}
	return cfg, info, nil
}

// ClientConfig собирает конфигурацию клиента доставки вебхуков.
//
// Пустой caFile при выключенном insecure даёт nil — то есть штатную проверку
// по системным корневым сертификатам.
func ClientConfig(caFile string, insecure bool) (*tls.Config, error) {
	if insecure {
		// Осознанная аварийная мера: вызывающий код обязан предупредить о
		// ней в логе. caFile при этом не читается — проверять всё равно
		// нечего.
		return &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, nil //nolint:gosec // включается только явной настройкой
	}
	if caFile == "" {
		return nil, nil
	}

	body, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("CA-файл для проверки стендов: %w", err)
	}
	// CA добавляется к системному пулу, а не заменяет его: в контуре часть
	// стендов обычно живёт на внутреннем CA, часть — на публичном
	// сертификате, и замена пула тихо ломает вторые.
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		// Экзотический образ без набора корневых. Один лишь CA из файла —
		// это по-прежнему рабочая конфигурация, а не повод не стартовать.
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(body) {
		return nil, fmt.Errorf("CA-файл %s: не найдено ни одного сертификата PEM", caFile)
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}
