// Команда max-mock — эмулятор бот-платформы Max для закрытого контура.
//
// Поднимает три поверхности на одном порту: Max Bot API в корне, служебный
// интерфейс и веб-UI на /mock, доставку webhook-ов — на подписки стендов.
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
	"path/filepath"
	"syscall"
	"time"

	"maxmock/internal/config"
	"maxmock/internal/core"
	"maxmock/internal/events"
	"maxmock/internal/httpserver"
	"maxmock/internal/logx"
	"maxmock/internal/specs"
	"maxmock/internal/store"
	"maxmock/internal/tlsconf"
	"maxmock/internal/webhook"
)

func main() {
	cfgPath := flag.String("config", "", "путь к yaml-конфигу (пусто — значения по умолчанию)")
	logLevel := flag.String("log-level", "", "уровень лога: debug, info, warn, error (пусто — из конфига)")
	flag.Parse()

	if err := run(*cfgPath, *logLevel); err != nil {
		slog.Error("max-mock не запустился", logx.Err(err))
		os.Exit(1)
	}
}

func run(cfgPath, logLevel string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	// Флаг бьёт конфиг: включить подробный лог на одном запуске нужно чаще,
	// чем править ради этого файл на стенде.
	if logLevel != "" {
		cfg.Log.Level = logLevel
	}
	if err := logx.Setup(os.Stderr, cfg.Log.Format, cfg.Log.Level, cfg.Log.BodyLimit); err != nil {
		return err
	}

	sp, err := specs.Load()
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	bus := events.New()
	disp, err := webhook.New(st, sp, bus, cfg.Webhook)
	if err != nil {
		return err
	}
	defer disp.Close()
	c := core.New(st, disp, bus, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go purgeLoop(ctx, st, cfg.LogRetentionDays)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           httpserver.New(c, sp, bus, cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	scheme := "http"
	var certInfo string
	if cfg.TLS.Enabled() {
		// Пара грузится здесь, до начала обслуживания: битый путь, отобранные
		// права или ключ от другого сертификата должны валить старт, а не
		// первое подключение стенда.
		tlsCfg, info, err := tlsconf.ServerConfig(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return err
		}
		srv.TLSConfig = tlsCfg
		scheme = "https"
		certInfo = info.String()
	}

	slog.Info("max-mock запускается",
		"контракт", sp.Version(), "адрес", scheme+"://"+cfg.Listen,
		"уровень_лога", cfg.Log.Level, "предел_тела", logx.Limit())
	if certInfo != "" {
		slog.Info("сертификат", "детали", certInfo)
	}
	if cfg.Webhook.InsecureSkipVerify {
		slog.Warn("проверка сертификатов стендов отключена " +
			"(webhook.insecure_skip_verify), доставка вебхуков уязвима к подмене")
	}
	slog.Info("веб-интерфейс", "админка", cfg.PublicBaseURL+"/mock", "публичный_адрес", cfg.PublicBaseURL)
	// Путь к базе печатается развёрнутым намеренно: он по умолчанию
	// относительный, и запуск из другого каталога молча открывает другой файл.
	// Одна строка в логе отвечает на вопрос «куда делись мои боты».
	slog.Info("состояние", "база", absPath(cfg.DBPath), "файлы", absPath(cfg.BlobDir),
		"боты", describeState(st))

	errCh := make(chan error, 1)
	go func() {
		// Пустые аргументы: пара уже лежит в srv.TLSConfig.
		serve := srv.ListenAndServe
		if cfg.TLS.Enabled() {
			serve = func() error { return srv.ListenAndServeTLS("", "") }
		}
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("останавливаюсь, дожидаюсь доставки событий")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// absPath разворачивает путь для лога; если не вышло — отдаёт как есть.
func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// describeState сообщает, что мок нашёл в базе при старте: пустая база на
// месте ожидаемой — первый признак того, что открыт не тот файл.
func describeState(st *store.Store) string {
	bots, err := st.ListBots()
	if err != nil {
		return "состояние прочитать не удалось: " + err.Error()
	}
	if len(bots) == 0 {
		return "ботов нет — база пустая или новая"
	}
	return fmt.Sprintf("ботов: %d", len(bots))
}

// purgeLoop раз в час чистит журналы старше заданного срока, чтобы
// долгоживущий мок не распухал.
func purgeLoop(ctx context.Context, st *store.Store, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixMilli()
		if n, err := st.Purge(cutoff); err != nil {
			slog.Error("чистка журналов не удалась", logx.Err(err))
		} else if n > 0 {
			slog.Info("журналы почищены", "удалено", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
