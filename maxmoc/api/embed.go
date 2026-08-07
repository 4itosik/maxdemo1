// Package api содержит OpenAPI-артефакты репозитория maxapi — единственный
// источник правды о контракте Max Bot API. Файлы обновляются через
// `make sync-specs`; версия контракта — 0.0.32.
package api

import "embed"

//go:embed openapi.MaxBotApi.yaml openapi.MaxBotWebhook.yaml
var FS embed.FS

// Имена встроенных документов.
const (
	BotAPIFile  = "openapi.MaxBotApi.yaml"
	WebhookFile = "openapi.MaxBotWebhook.yaml"
)
