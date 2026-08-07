package maxapi

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// CheckWebhookURL проверяет, годится ли адрес для подписки на события.
//
// Max доставляет события только по HTTPS на порт 443, поэтому http://
// разрешён единственным исключением — для петлевого хоста. На нём живёт
// локальный эмулятор max-mock, который принимает подписку по HTTP; живому
// Max петлевой адрес недоступен, так что подписать его на http:// по ошибке
// нельзя.
func CheckWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("адрес webhook не разбирается: %w", err)
	}

	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopbackHost(u.Hostname()):
	default:
		return fmt.Errorf("адрес webhook %q должен начинаться с https:// — Max не принимает webhook по HTTP; http:// допустим только для петлевого адреса (localhost, 127.0.0.1, ::1)", raw)
	}

	if u.Host == "" {
		return fmt.Errorf("адрес webhook %q не содержит имени хоста", raw)
	}
	return nil
}

// isLoopbackHost сообщает, указывает ли хост на эту же машину.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
