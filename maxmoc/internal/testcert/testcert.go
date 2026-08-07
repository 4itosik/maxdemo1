// Package testcert выпускает самоподписанные сертификаты для тестов.
//
// В бинарник не попадает: пакет импортируют только _test.go файлы. Нужен
// потому, что проверять TLS осмысленно только на настоящей паре ключей, а
// держать её в репозитории нельзя — секретный ключ в git остаётся там
// навсегда, даже если он «ненастоящий».
package testcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Generate выпускает самоподписанный сертификат на перечисленные имена и
// адреса и кладёт пару PEM-файлов в dir. Первое имя становится Subject CN.
//
// Срок — час: тестам этого достаточно, а случайно утёкший в артефакты
// сборки сертификат протухнет сам.
func Generate(dir string, hosts ...string) (certFile, keyFile string, err error) {
	return generate(dir, time.Now().Add(time.Hour), hosts...)
}

// GenerateExpired выпускает уже истёкший сертификат — для проверок того, как
// код сообщает о просроченной паре.
func GenerateExpired(dir string, hosts ...string) (certFile, keyFile string, err error) {
	return generate(dir, time.Now().Add(-time.Hour), hosts...)
}

func generate(dir string, notAfter time.Time, hosts ...string) (string, string, error) {
	if len(hosts) == 0 {
		return "", "", fmt.Errorf("testcert: нужно хотя бы одно имя")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hosts[0]},
		NotBefore:             notAfter.Add(-2 * time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			continue
		}
		tmpl.DNSNames = append(tmpl.DNSNames, h)
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}

	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := writePEM(certFile, "CERTIFICATE", der, 0o644); err != nil {
		return "", "", err
	}
	if err := writePEM(keyFile, "PRIVATE KEY", keyDER, 0o600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

// WritePEM сохраняет DER-байты сертификата как PEM-файл. Нужен тестам,
// которым нужен только сертификат стенда — без ключа, как CA-бандл.
func WritePEM(path string, der []byte) error {
	return writePEM(path, "CERTIFICATE", der, 0o644)
}

func writePEM(path, blockType string, der []byte, perm os.FileMode) error {
	body := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	return os.WriteFile(path, body, perm)
}
