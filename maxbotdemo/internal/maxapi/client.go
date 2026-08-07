// Package maxapi — тонкий клиент к Max Bot API (https://dev.max.ru/docs-api).
//
// Клиент намеренно покрывает только то, что нужно демонстрационному боту:
// информация о боте, команды, подписка на webhook, отправка сообщений и ответ
// на нажатие кнопки.
package maxapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"maxbotdemo/internal/jsonlog"
)

// DefaultBaseURL — адрес Max Bot API.
const DefaultBaseURL = "https://platform-api2.max.ru"

// Client выполняет запросы к Max Bot API.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
	log     *slog.Logger
}

// Option настраивает клиента.
type Option func(*Client)

// WithBaseURL подменяет адрес API — нужно для тестов.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient задаёт свой http.Client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.hc = hc }
}

// WithLogger включает запись запросов и ответов API в лог.
func WithLogger(log *slog.Logger) Option {
	return func(c *Client) { c.log = log }
}

// New создаёт клиента с указанным токеном бота.
func New(token string, opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		token:   token,
		hc:      &http.Client{Timeout: 30 * time.Second},
		// Молчащий логгер, а не nil: так do не обрастает проверками.
		log: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// APIError — ошибка, возвращённая Max API. Status позволяет отличить
// неверный токен (401) от превышения лимита (429).
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("max api: %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("max api: %d: %s", e.Status, e.Message)
}

// errorBody — тело ответа при ошибке (схема Error в OpenAPI).
type errorBody struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// do выполняет запрос к API. Токен уходит в заголовке Authorization: OpenAPI
// допускает и query-параметр access_token, но заголовок не попадает в логи
// прокси-серверов. in и out могут быть nil, если тело не нужно.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var encoded []byte
	var body io.Reader
	if in != nil {
		var err error
		encoded, err = json.Marshal(in)
		if err != nil {
			return fmt.Errorf("кодирование тела запроса: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("создание запроса: %w", err)
	}
	req.Header.Set("Authorization", c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Заголовки не логируются: в Authorization лежит токен.
	c.log.Info("запрос к API",
		"method", method, "path", path, "query", q.Encode(), "payload", jsonlog.Raw(redact(encoded)))

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("чтение ответа %s %s: %w", method, path, err)
	}

	// До проверки статуса: тело отказа в логе полезнее свёрнутого *APIError.
	c.log.Info("ответ API",
		"method", method, "path", path, "status", resp.StatusCode, "payload", jsonlog.Raw(redact(raw)))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(resp.StatusCode, raw)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("разбор ответа %s %s: %w", method, path, err)
	}
	return nil
}

// redactedFields — поля тела, которым в логе не место. secret уходит в
// POST /subscriptions и по смыслу равен паролю.
var redactedFields = []string{"secret"}

// redact подменяет значения чувствительных полей на "***". Тело, которое не
// разбирается как JSON-объект, возвращается как есть: секретов в нём быть не
// может, а потерять его хуже, чем напечатать.
func redact(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw
	}

	found := false
	for _, name := range redactedFields {
		if _, ok := fields[name]; ok {
			fields[name] = json.RawMessage(`"***"`)
			found = true
		}
	}
	if !found {
		return raw
	}

	out, err := json.Marshal(fields)
	if err != nil {
		// Собрать обратно только что разобранное нечему помешать, но если
		// вдруг — лучше потерять запись, чем напечатать секрет.
		return []byte(`{"redacted":true}`)
	}
	return out
}

// newAPIError строит *APIError из тела ответа. Если тело не JSON — кладёт его
// в Message как есть, чтобы причина не потерялась.
func newAPIError(status int, raw []byte) *APIError {
	e := &APIError{Status: status}

	var parsed errorBody
	if err := json.Unmarshal(raw, &parsed); err == nil && (parsed.Code != "" || parsed.Message != "") {
		e.Code = parsed.Code
		e.Message = parsed.Message
		return e
	}

	e.Message = strings.TrimSpace(string(raw))
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}
