// Package config — конфигурация max-mock: yaml-файл поверх дефолтов,
// env-переменные поверх файла.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Webhook — параметры доставки событий на подписки.
type Webhook struct {
	TimeoutSec int   `yaml:"timeout_sec"`
	Retries    int   `yaml:"retries"`
	BackoffSec []int `yaml:"backoff_sec"`
	// CAFile — сертификат удостоверяющего центра контура, которым подписаны
	// сертификаты стендов КЦ. Пусто — проверка по системным корневым.
	CAFile string `yaml:"ca_file"`
	// InsecureSkipVerify отключает проверку сертификатов стендов целиком.
	// Аварийная мера на случай, когда CA взять неоткуда, а демонстрация
	// сегодня; сервис предупреждает о ней при каждом старте.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
}

// Log — консольный лог сервиса.
//
// Он не заменяет журналы в базе, которые читает UI: те хранят историю, этот
// показывает, что мок жив и что именно сейчас через него прошло.
type Log struct {
	// Format — text (key=value, для глаз) или json (для сборщика логов).
	Format string `yaml:"format"`
	// Level — debug, info, warn или error. На debug добавляются отдача
	// статики, страниц и /healthz.
	Level string `yaml:"level"`
	// BodyLimit — сколько байт тела запроса или ответа попадает в строку.
	// 0 — не печатать тела вовсе.
	BodyLimit int `yaml:"body_limit"`
}

// TLS — сертификат, с которым мок слушает свой порт. Заданные оба поля
// включают HTTPS; пустой блок оставляет обычный HTTP.
type TLS struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Enabled сообщает, поднимать ли слушатель по TLS.
func (t TLS) Enabled() bool { return t.CertFile != "" && t.KeyFile != "" }

// Config — полная конфигурация сервиса.
type Config struct {
	Listen        string `yaml:"listen"`
	PublicBaseURL string `yaml:"public_base_url"`
	DBPath        string `yaml:"db_path"`
	BlobDir       string `yaml:"blob_dir"`
	// LogRetentionDays — через сколько дней чистить request_log и
	// webhook_deliveries. 0 — не чистить.
	LogRetentionDays int `yaml:"log_retention_days"`
	// ValidateResponses — валидировать ли собственные ответы против
	// OpenAPI. Ловит ошибки в wire-структурах до того, как их увидит КЦ.
	ValidateResponses bool    `yaml:"validate_responses"`
	Log               Log     `yaml:"log"`
	TLS               TLS     `yaml:"tls"`
	Webhook           Webhook `yaml:"webhook"`
}

// Default возвращает конфигурацию по умолчанию.
func Default() *Config {
	return &Config{
		Listen:            ":8080",
		PublicBaseURL:     "http://localhost:8080",
		DBPath:            "max-mock.db",
		BlobDir:           "blobs",
		LogRetentionDays:  14,
		ValidateResponses: true,
		Log:               Log{Format: "text", Level: "info", BodyLimit: 4096},
		Webhook:           Webhook{TimeoutSec: 10, Retries: 2, BackoffSec: []int{1, 5}},
	}
}

// Load читает конфигурацию: дефолты ← yaml-файл (если path не пуст) ← env.
func Load(path string) (*Config, error) {
	c := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("чтение конфига: %w", err)
		}
		if err := yaml.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("разбор конфига %s: %w", path, err)
		}
	}
	for env, dst := range map[string]*string{
		"MAXMOCK_LISTEN":          &c.Listen,
		"MAXMOCK_PUBLIC_BASE_URL": &c.PublicBaseURL,
		"MAXMOCK_DB_PATH":         &c.DBPath,
		"MAXMOCK_BLOB_DIR":        &c.BlobDir,
		"MAXMOCK_TLS_CERT_FILE":   &c.TLS.CertFile,
		"MAXMOCK_TLS_KEY_FILE":    &c.TLS.KeyFile,
		"MAXMOCK_WEBHOOK_CA_FILE": &c.Webhook.CAFile,
		"MAXMOCK_LOG_FORMAT":      &c.Log.Format,
		"MAXMOCK_LOG_LEVEL":       &c.Log.Level,
	} {
		if v := os.Getenv(env); v != "" {
			*dst = v
		}
	}
	if err := envBool("MAXMOCK_WEBHOOK_INSECURE_SKIP_VERIFY", &c.Webhook.InsecureSkipVerify); err != nil {
		return nil, err
	}
	if err := envInt("MAXMOCK_LOG_BODY_LIMIT", &c.Log.BodyLimit); err != nil {
		return nil, err
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// validate ловит сочетания настроек, которые иначе проявятся поздно и не на
// той стороне: у КЦ, в виде необъяснимого отказа.
func (c *Config) validate() error {
	switch {
	case c.TLS.CertFile != "" && c.TLS.KeyFile == "":
		return fmt.Errorf("TLS: задан tls.cert_file, но не задан tls.key_file (или MAXMOCK_TLS_KEY_FILE)")
	case c.TLS.KeyFile != "" && c.TLS.CertFile == "":
		return fmt.Errorf("TLS: задан tls.key_file, но не задан tls.cert_file (или MAXMOCK_TLS_CERT_FILE)")
	}
	// Обратное сочетание — https в адресе при выключенном TLS — законно: так
	// выглядит мок за терминирующим прокси.
	if c.TLS.Enabled() && strings.HasPrefix(c.PublicBaseURL, "http://") {
		return fmt.Errorf(
			"TLS включён, а public_base_url = %q начинается с http://: "+
				"стенды получат upload-адреса и ссылки на вложения по схеме, которой на порту больше нет, "+
				"и не смогут скачать файлы. Замените схему на https://",
			c.PublicBaseURL)
	}
	return nil
}

// envInt читает целочисленную переменную окружения. Отрицательное значение
// отвергается: все числовые настройки здесь — размеры и сроки.
func envInt(name string, dst *int) error {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fmt.Errorf("%s=%q: ожидалось неотрицательное целое", name, v)
	}
	*dst = n
	return nil
}

// envBool читает булеву переменную окружения. Пустая — оставляет значение
// как есть; непонятная — ошибка, потому что «MAXMOCK_..._VERIFY=нет» молча
// означало бы «да».
func envBool(name string, dst *bool) error {
	v := os.Getenv(name)
	if v == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		*dst = true
	case "0", "false", "no", "off":
		*dst = false
	default:
		return fmt.Errorf("%s=%q: ожидалось 1/0, true/false, yes/no или on/off", name, v)
	}
	return nil
}
