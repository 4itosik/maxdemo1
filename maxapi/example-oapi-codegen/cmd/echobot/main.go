// Команда echobot — сквозной пример использования maxapi/gen/oapi-codegen.
//
// Делает ровно то же, что ../../../example-quicktype/cmd/echobot на структурах
// quicktype: обновляет подписку, слушает webhook, отвечает эхом. Пара нужна
// для сравнения двух вариантов кодогенерации на одной задаче.
//
// Запуск:
//
//	export MAX_BOT_TOKEN=…            # токен бота из @MasterBot
//	export MAX_WEBHOOK_URL=https://…  # публичный HTTPS-адрес этого процесса
//	export MAX_WEBHOOK_SECRET=…       # 5..256 символов [A-Za-z0-9_-]
//	go run ./cmd/echobot
//
// Флаг -api направляет бота на другой адрес Bot API — например на локальный
// мок из ../../../maxmoc, который принимает webhook и по обычному http:
//
//	go run ./cmd/echobot -api http://localhost:8099 -addr :9099
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example-oapi-codegen/maxclient"
	"example-oapi-codegen/webhook"
	"stash.sigma.sbrf.ru/scpl/oapi/maxapi"
)

func main() {
	if err := run(); err != nil {
		slog.Error("echobot остановлен", "error", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "адрес, который слушает процесс")
	api := flag.String("api", maxclient.DefaultBaseURL, "базовый адрес Max Bot API")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	token, err := mustEnv("MAX_BOT_TOKEN")
	if err != nil {
		return err
	}
	publicURL, err := mustEnv("MAX_WEBHOOK_URL")
	if err != nil {
		return err
	}
	secret, err := mustEnv("MAX_WEBHOOK_SECRET")
	if err != nil {
		return err
	}

	client := maxclient.New(token, maxclient.WithBaseURL(*api))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- сгенерированный клиент: обновление подписок ----------------------
	subscribeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	removed, err := client.EnsureSubscription(subscribeCtx, maxapi.SubscriptionRequestBody{
		Url:         publicURL,
		Secret:      &secret,
		UpdateTypes: &[]maxapi.UpdateType{"message_created", "message_callback"},
	})
	if err != nil {
		return fmt.Errorf("обновление подписок: %w", err)
	}
	log.Info("подписка обновлена", "api", *api, "url", publicURL, "снято_чужих", len(removed))

	// --- webhook-сервер: приём событий ------------------------------------
	server := webhook.New(secret, webhook.Handler{
		MessageCreated:  echoMessage(client, log),
		MessageCallback: echoCallback(client, log),
		Other: func(_ context.Context, updateType string, _ any) error {
			log.Info("событие без обработчика", "update_type", updateType)
			return nil
		},
	}, log)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("слушаю webhook", "addr", *addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("останавливаюсь")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

// echoMessage отвечает тем же текстом в тот же чат.
func echoMessage(client *maxclient.Client, log *slog.Logger) func(context.Context, maxapi.MessageCreatedUpdate) error {
	return func(ctx context.Context, u maxapi.MessageCreatedUpdate) error {
		text := u.Message.Body.Text
		if text == nil || *text == "" {
			log.Info("сообщение без текста — пропускаю", "mid", u.Message.Body.Mid)
			return nil
		}
		chatID := u.Message.Recipient.ChatId
		if chatID == nil {
			log.Warn("у сообщения нет chat_id", "mid", u.Message.Body.Mid)
			return nil
		}

		result, err := client.SendMessage(ctx, maxclient.ToChat(*chatID), maxclient.TextMessage("Вы написали: "+*text))
		if err != nil {
			return fmt.Errorf("ответ в чат %d: %w", *chatID, err)
		}
		log.Info("ответил", "chat_id", *chatID, "mid", result.Message.Body.Mid)
		return nil
	}
}

// echoCallback отвечает payload-ом нажатой кнопки.
func echoCallback(client *maxclient.Client, log *slog.Logger) func(context.Context, maxapi.MessageCallbackUpdate) error {
	return func(ctx context.Context, u maxapi.MessageCallbackUpdate) error {
		payload := "(пусто)"
		if u.Callback.Payload != nil {
			payload = *u.Callback.Payload
		}
		// В message_callback сообщение необязательно: кнопку могли нажать в
		// сообщении, уже удалённом к моменту доставки события.
		if u.Message == nil || u.Message.Recipient.ChatId == nil {
			log.Info("нажатие без сообщения", "callback_id", u.Callback.CallbackId)
			return nil
		}
		chatID := *u.Message.Recipient.ChatId
		if _, err := client.SendMessage(ctx, maxclient.ToChat(chatID), maxclient.TextMessage("Нажата кнопка: "+payload)); err != nil {
			return fmt.Errorf("ответ на нажатие в чате %d: %w", chatID, err)
		}
		log.Info("ответил на нажатие", "chat_id", chatID, "payload", payload)
		return nil
	}
}

func mustEnv(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("переменная окружения %s не задана", name)
	}
	return value, nil
}
