// Команда bot — демонстрационный бот для мессенджера Max.
//
// Бот получает события через webhook: при старте он подписывает адрес из
// WEBHOOK_URL и поднимает локальный HTTP-сервер, на который Max шлёт события.
// Наружу сервер должен быть выставлен по HTTPS на порту 443 — например,
// туннелем `cloudflared tunnel --url http://localhost:8080`.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"maxbotdemo/internal/bot"
	"maxbotdemo/internal/maxapi"
	"maxbotdemo/internal/webhook"
)

// shutdownTimeout — сколько ждём завершения текущих HTTP-запросов.
const shutdownTimeout = 10 * time.Second

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil {
		log.Error("бот остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	getenv, err := environment(os.Args[1:])
	if err != nil {
		return err
	}

	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api, err := newAPIClient(cfg, log)
	if err != nil {
		return err
	}

	info, err := register(ctx, api, cfg)
	if err != nil {
		return err
	}
	log.Info("бот зарегистрирован",
		"name", info.FirstName, "username", info.Username, "webhook", cfg.WebhookURL)

	demo := bot.New(api, log)
	hook := webhook.New(cfg.Secret, demo.Handle, log)

	mux := http.NewServeMux()
	mux.Handle(cfg.WebhookPath, hook)
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("слушаем события", "addr", cfg.ListenAddr, "path", cfg.WebhookPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return fmt.Errorf("HTTP-сервер: %w", err)
	case <-ctx.Done():
		log.Info("получен сигнал остановки")
	}

	return shutdown(srv, hook, api, cfg, log)
}

// newAPIClient создаёт клиента Max API. Если задан MAX_API_CA_FILE, клиент
// дополнительно доверяет корневому сертификату из этого файла: *.max.ru
// подписан цепочкой Минцифры, которой нет в системном наборе macOS.
func newAPIClient(cfg config, log *slog.Logger) (*maxapi.Client, error) {
	if cfg.CAFile == "" {
		return maxapi.New(cfg.Token,
			maxapi.WithBaseURL(cfg.BaseURL), maxapi.WithLogger(log)), nil
	}
	return maxapi.NewWithRootCA(cfg.Token, cfg.CAFile,
		maxapi.WithBaseURL(cfg.BaseURL), maxapi.WithLogger(log))
}

// register выполняет стартовую последовательность: проверяет токен,
// публикует команды и настраивает webhook-подписку.
func register(ctx context.Context, api *maxapi.Client, cfg config) (maxapi.BotInfo, error) {
	info, err := api.GetMyInfo(ctx)
	if err != nil {
		return maxapi.BotInfo{}, fmt.Errorf("проверка токена (GET /me): %w", err)
	}

	if err := api.SetCommands(ctx, bot.Commands()); err != nil {
		return maxapi.BotInfo{}, fmt.Errorf("публикация команд: %w", err)
	}

	if err := syncSubscription(ctx, api, cfg); err != nil {
		return maxapi.BotInfo{}, err
	}

	return info, nil
}

// syncSubscription приводит подписки бота к одной — на cfg.WebhookURL с
// набором событий из bot.UpdateTypes().
//
// Подписка опознаётся по URL: чужие адреса снимаются, а свой обновляется одним
// POST. Так описано в документации метода — «чтобы обновить подписку на
// события, используйте POST /subscriptions», — и DELETE перед ним только
// открыл бы окно, в котором события падают в никуда. Несколько подписок сразу
// возникают от разных адресов: новый туннель — новый URL, и MAX начинает слать
// каждое событие на все живые адреса.
func syncSubscription(ctx context.Context, api *maxapi.Client, cfg config) error {
	existing, err := api.GetSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("чтение подписок: %w", err)
	}

	want := bot.UpdateTypes()
	var current []string
	found := false
	for _, s := range existing {
		if s.URL == cfg.WebhookURL {
			current, found = s.UpdateTypes, true
			continue
		}
		if err := api.Unsubscribe(ctx, s.URL); err != nil {
			return fmt.Errorf("снятие устаревшей подписки %s: %w", s.URL, err)
		}
	}

	// Сверять нужно и набор событий: иначе расширенный список не доедет до
	// MAX — запись с нужным адресом уже есть, — и новые события не придут
	// ни разу, причём молча.
	if found && sameSet(current, want) {
		return nil
	}

	if err := api.Subscribe(ctx, cfg.WebhookURL, cfg.Secret, want); err != nil {
		return fmt.Errorf("подписка на webhook %s: %w", cfg.WebhookURL, err)
	}
	return nil
}

// sameSet сообщает, что списки состоят из одних и тех же элементов. Порядок
// update_types в ответе API контрактом не оговорён, полагаться на него нельзя.
// Повторы считаются по количеству — контракт объявляет update_types
// uniqueItems, так что до этого дойти не должно, но и молча схлопывать дубли
// незачем.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	count := make(map[string]int, len(a))
	for _, v := range a {
		count[v]++
	}
	for _, v := range b {
		count[v]--
		if count[v] < 0 {
			return false
		}
	}
	return true
}

// shutdown останавливает HTTP-сервер, дожидается обработчиков и при
// необходимости снимает подписку.
func shutdown(srv *http.Server, hook *webhook.Server, api *maxapi.Client, cfg config, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("сервер остановлен принудительно", "error", err)
	}
	hook.Wait()

	// По умолчанию подписку не снимаем: следующий запуск с тем же адресом
	// увидит её в GET /subscriptions и не станет подписываться повторно.
	if cfg.UnsubscribeOnExit {
		if err := api.Unsubscribe(ctx, cfg.WebhookURL); err != nil {
			log.Error("не удалось снять подписку", "error", err)
		} else {
			log.Info("подписка снята", "webhook", cfg.WebhookURL)
		}
	}

	log.Info("бот остановлен")
	return nil
}
