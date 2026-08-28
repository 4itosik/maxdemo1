package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"maxbotdemo/internal/maxapi"
)

// secretPattern повторяет ограничение Max на поле secret подписки.
var secretPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{5,256}$`)

// webhookPathPattern повторяет путь webhook-эндпоинта из контракта
// (`openapi.MaxBotWebhook.yaml`): `/max/v1.0/webhooks/{integrationId}`, где
// последний сегмент — идентификатор интеграции, UUID из 36 символов
// (hex-группы 8-4-4-4-12 через дефисы).
//
// Путь проверяется на старте, до регистрации подписки: адрес с другим путём
// платформа примет, и расхождение с контрактом обнаружилось бы уже на
// периметре — отвергнутыми или ушедшими не туда событиями.
var webhookPathPattern = regexp.MustCompile(
	`^/max/v1\.0/webhooks/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// config — настройки бота, читаемые из переменных окружения.
type config struct {
	Token             string // MAX_BOT_TOKEN
	WebhookURL        string // WEBHOOK_URL — публичный https-адрес
	WebhookPath       string // путь из WebhookURL, на нём слушает локальный сервер
	Secret            string // WEBHOOK_SECRET
	ListenAddr        string // LISTEN_ADDR
	BaseURL           string // MAX_API_BASE_URL
	CAFile            string // MAX_API_CA_FILE — корневой сертификат Минцифры
	UnsubscribeOnExit bool   // UNSUBSCRIBE_ON_EXIT
}

// loadConfig читает и проверяет настройки. getenv позволяет подменить
// источник в тестах.
func loadConfig(getenv func(string) string) (config, error) {
	// BOT_TOKEN — запасное имя: под ним токен фигурирует в примерах
	// официального SDK, и .env часто уже заполнен именно так.
	token := strings.TrimSpace(getenv("MAX_BOT_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(getenv("BOT_TOKEN"))
	}

	cfg := config{
		Token:             token,
		WebhookURL:        strings.TrimSpace(getenv("WEBHOOK_URL")),
		Secret:            strings.TrimSpace(getenv("WEBHOOK_SECRET")),
		ListenAddr:        strings.TrimSpace(getenv("LISTEN_ADDR")),
		BaseURL:           strings.TrimSpace(getenv("MAX_API_BASE_URL")),
		CAFile:            strings.TrimSpace(getenv("MAX_API_CA_FILE")),
		UnsubscribeOnExit: getenv("UNSUBSCRIBE_ON_EXIT") == "1",
	}

	if cfg.Token == "" {
		return config{}, fmt.Errorf("не задан MAX_BOT_TOKEN (или BOT_TOKEN) — токен бота из раздела «Интеграция» на business.max.ru")
	}
	if cfg.WebhookURL == "" {
		return config{}, fmt.Errorf("не задан WEBHOOK_URL — публичный адрес, на который Max будет слать события")
	}

	path, err := webhookPath(cfg.WebhookURL)
	if err != nil {
		return config{}, err
	}
	cfg.WebhookPath = path

	if cfg.Secret != "" && !secretPattern.MatchString(cfg.Secret) {
		return config{}, fmt.Errorf("WEBHOOK_SECRET должен состоять из 5–256 символов A-Z, a-z, 0-9, дефиса и подчёркивания")
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = maxapi.DefaultBaseURL
	}

	return cfg, nil
}

// webhookPath проверяет URL и возвращает его путь: локальный сервер должен
// слушать ровно тот путь, который увидит Max.
func webhookPath(raw string) (string, error) {
	if err := maxapi.CheckWebhookURL(raw); err != nil {
		return "", fmt.Errorf("WEBHOOK_URL: %w", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("WEBHOOK_URL не разбирается как адрес: %w", err)
	}
	if !webhookPathPattern.MatchString(u.Path) {
		return "", fmt.Errorf("WEBHOOK_URL: путь %q не соответствует контракту "+
			"/max/v1.0/webhooks/{integrationId} — последний сегмент должен быть "+
			"идентификатором интеграции (UUID, 36 символов). Пример: "+
			"https://example.com/max/v1.0/webhooks/b3d4a7f0-1c2e-4f6a-9d8b-5e0c7a1f2b34",
			u.Path)
	}
	return u.Path, nil
}
