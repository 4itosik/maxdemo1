// Package maxclient — HTTP-клиент к Max Bot API поверх структур oapi-codegen.
//
// Парный пакет ../../example-quicktype/maxclient делает ровно то же самое поверх
// структур quicktype. Пара нужна для сравнения двух вариантов кодогенерации:
// HTTP-слой здесь и там написан руками и по одному образцу, поэтому вся
// разница между файлами — это разница в сгенерированных типах, и ничто иное.
//
// Клиент oapi-codegen генерировать умеет (`client: true` в
// gen/oapi-codegen/oapi-codegen.yaml), но тогда сравнивать было бы нечего: у
// quicktype клиента нет вовсе.
package maxclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"stash.sigma.sbrf.ru/scpl/oapi/maxapi"
)

// DefaultBaseURL — адрес Max Bot API из контракта (`servers`).
const DefaultBaseURL = "https://platform-api2.max.ru"

// Client выполняет запросы к Max Bot API.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// Option настраивает клиента.
type Option func(*Client)

// WithBaseURL подменяет адрес API — нужно для тестов против httptest.Server.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient задаёт свой http.Client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.hc = hc }
}

// New создаёт клиента с токеном бота.
func New(token string, opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		token:   token,
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// APIError — ответ Max Bot API с кодом вне 2xx.
//
// Тело таких ответов описано в контракте схемой Error, поэтому разбирается в
// maxapi.Error, а не в map[string]any.
type APIError struct {
	StatusCode int
	Body       maxapi.Error
}

func (e *APIError) Error() string {
	if e.Body.Error != nil && *e.Body.Error != "" {
		return fmt.Sprintf("max api: %d %s (%s): %s", e.StatusCode, e.Body.Code, *e.Body.Error, e.Body.Message)
	}
	return fmt.Sprintf("max api: %d %s: %s", e.StatusCode, e.Body.Code, e.Body.Message)
}

// do выполняет запрос и разбирает ответ в out.
//
// Токен уходит в заголовке Authorization. Контракт допускает и query-параметр
// access_token (securitySchemes.ApiKeyAuth), но query попадает в access-логи
// прокси, а заголовок — нет.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("сериализация тела %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(encoded)
	}

	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("сборка запроса %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("запрос %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("чтение ответа %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		// Тело может оказаться не-JSON (страница от прокси) — тогда код и
		// сообщение останутся пустыми, но статус сохранится.
		_ = json.Unmarshal(payload, &apiErr.Body)
		return apiErr
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("разбор ответа %s %s: %w", method, path, err)
	}
	return nil
}

// GetSubscriptions возвращает текущие подписки бота: GET /subscriptions.
func (c *Client) GetSubscriptions(ctx context.Context) (maxapi.GetSubscriptionsResult, error) {
	var out maxapi.GetSubscriptionsResult
	err := c.do(ctx, http.MethodGet, "/subscriptions", nil, nil, &out)
	return out, err
}

// Subscribe подписывает бота на webhook: POST /subscriptions.
func (c *Client) Subscribe(ctx context.Context, body maxapi.SubscriptionRequestBody) (maxapi.SimpleQueryResult, error) {
	var out maxapi.SimpleQueryResult
	err := c.do(ctx, http.MethodPost, "/subscriptions", nil, body, &out)
	return out, err
}

// Unsubscribe снимает подписку с указанного URL: DELETE /subscriptions?url=...
func (c *Client) Unsubscribe(ctx context.Context, webhookURL string) (maxapi.SimpleQueryResult, error) {
	var out maxapi.SimpleQueryResult
	err := c.do(ctx, http.MethodDelete, "/subscriptions", url.Values{"url": {webhookURL}}, nil, &out)
	return out, err
}

// EnsureSubscription приводит подписки бота к желаемой: снимает все чужие URL
// и подписывает нужный. Возвращает список снятых URL.
//
// Отдельный метод, потому что «обновить подписку» в Max Bot API — не одна
// операция: PUT для подписок в контракте нет, есть GET/POST/DELETE. Повторный
// POST на тот же URL безопасен и обновляет update_types и secret, поэтому
// подписка ставится всегда, а удаляются только посторонние адреса.
func (c *Client) EnsureSubscription(ctx context.Context, want maxapi.SubscriptionRequestBody) ([]string, error) {
	current, err := c.GetSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("чтение текущих подписок: %w", err)
	}

	var removed []string
	for _, sub := range current.Subscriptions {
		if sub.Url == want.Url {
			continue
		}
		if _, err := c.Unsubscribe(ctx, sub.Url); err != nil {
			return removed, fmt.Errorf("снятие подписки %s: %w", sub.Url, err)
		}
		removed = append(removed, sub.Url)
	}

	if _, err := c.Subscribe(ctx, want); err != nil {
		return removed, fmt.Errorf("подписка на %s: %w", want.Url, err)
	}
	return removed, nil
}

// Target — адресат сообщения: пользователь или чат.
//
// В контракте это два взаимоисключающих необязательных query-параметра
// (user_id и chat_id у POST /messages), что в Go выражается конструкторами
// ToUser/ToChat: так нельзя случайно задать оба или ни одного.
type Target struct {
	userID *int64
	chatID *int64
}

// ToUser адресует сообщение пользователю.
func ToUser(id int64) Target { return Target{userID: &id} }

// ToChat адресует сообщение чату.
func ToChat(id int64) Target { return Target{chatID: &id} }

func (t Target) query() (url.Values, error) {
	switch {
	case t.userID != nil:
		return url.Values{"user_id": {strconv.FormatInt(*t.userID, 10)}}, nil
	case t.chatID != nil:
		return url.Values{"chat_id": {strconv.FormatInt(*t.chatID, 10)}}, nil
	default:
		return nil, fmt.Errorf("адресат не задан: используйте ToUser или ToChat")
	}
}

// SendMessage отправляет сообщение: POST /messages?user_id=… | chat_id=…
func (c *Client) SendMessage(ctx context.Context, to Target, body maxapi.NewMessageBody) (maxapi.SendMessageResult, error) {
	var out maxapi.SendMessageResult
	query, err := to.query()
	if err != nil {
		return out, err
	}
	err = c.do(ctx, http.MethodPost, "/messages", query, body, &out)
	return out, err
}

// TextMessage собирает тело простого текстового сообщения.
//
// Нужен потому, что Text в NewMessageBody — *string: поле объявлено nullable,
// и указатель отличает «нет текста» от пустой строки.
func TextMessage(text string) maxapi.NewMessageBody {
	return maxapi.NewMessageBody{Text: &text}
}
