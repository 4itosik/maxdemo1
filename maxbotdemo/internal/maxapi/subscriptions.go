package maxapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// GetSubscriptions возвращает текущие webhook-подписки бота
// (GET /subscriptions).
func (c *Client) GetSubscriptions(ctx context.Context) ([]Subscription, error) {
	var res GetSubscriptionsResult
	if err := c.do(ctx, http.MethodGet, "/subscriptions", nil, nil, &res); err != nil {
		return nil, err
	}
	return res.Subscriptions, nil
}

// Subscribe настраивает доставку событий на webhook-endpoint (POST /subscriptions).
//
// URL должен быть доступен по HTTPS на порту 443 с сертификатом доверенного
// удостоверяющего центра — исключение сделано только для петлевого адреса,
// на котором живёт эмулятор max-mock (см. CheckWebhookURL). Если secret не
// пуст, Max будет присылать его в заголовке X-Max-Bot-Api-Secret. Повторный
// вызов перезаписывает подписку.
func (c *Client) Subscribe(ctx context.Context, webhookURL, secret string, updateTypes []string) error {
	if err := CheckWebhookURL(webhookURL); err != nil {
		return fmt.Errorf("Subscribe: %w", err)
	}

	body := SubscriptionRequestBody{
		URL:         webhookURL,
		UpdateTypes: updateTypes,
		Secret:      secret,
	}
	return c.do(ctx, http.MethodPost, "/subscriptions", nil, body, nil)
}

// Unsubscribe отключает доставку событий на указанный URL
// (DELETE /subscriptions?url=).
func (c *Client) Unsubscribe(ctx context.Context, webhookURL string) error {
	if webhookURL == "" {
		return errors.New("Unsubscribe: пустой URL")
	}

	q := url.Values{"url": {webhookURL}}
	return c.do(ctx, http.MethodDelete, "/subscriptions", q, nil, nil)
}
