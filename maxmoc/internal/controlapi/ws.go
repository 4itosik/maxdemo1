package controlapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"maxmock/internal/events"
	"maxmock/internal/logx"
)

// botFilter описывает, чей поток слушает подключившийся UI: админка берёт
// события всех ботов, веб-чат — одного.
func botFilter(botID int64) string {
	if botID == events.SubscribeAll {
		return "все"
	}
	return strconv.FormatInt(botID, 10)
}

// websocket отдаёт живой поток изменений состояния для веб-интерфейса.
//
// Параметр bot_id ограничивает поток одним ботом — так тестировщики,
// работающие с разными ботами, не видят чужой активности. Без параметра
// приходят события всех ботов (нужно админке).
func (a *API) websocket(w http.ResponseWriter, r *http.Request) {
	botID := events.SubscribeAll
	if raw := r.URL.Query().Get("bot_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "mock.bad_request", "некорректный bot_id")
			return
		}
		botID = id
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Мок живёт в закрытом контуре и открывается по прямому адресу
		// стенда, поэтому проверка Origin только мешала бы.
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.WarnContext(r.Context(), "веб-интерфейс не подключился",
			"remote", r.RemoteAddr, logx.Err(err))
		return
	}
	defer conn.CloseNow()

	// Подключение и отключение печатаются отдельно от прочих запросов: сам
	// запрос длится столько, сколько открыта вкладка, и одной строкой «GET
	// /mock/ws занял сорок минут» в конце не объясняется ничего.
	slog.InfoContext(r.Context(), "веб-интерфейс подключился",
		"bot", botFilter(botID), "remote", r.RemoteAddr)
	defer slog.InfoContext(r.Context(), "веб-интерфейс отключился",
		"bot", botFilter(botID), "remote", r.RemoteAddr)

	ch, cancel := a.bus.Subscribe(botID)
	defer cancel()

	ctx := r.Context()
	// Читатель нужен, чтобы замечать закрытие соединения клиентом: без
	// чтения библиотека не обработает управляющие кадры.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			writeCtx, cancelWrite := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(writeCtx, conn, ev)
			cancelWrite()
			if err != nil {
				return
			}
		case <-ping.C:
			pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			cancelPing()
			if err != nil {
				return
			}
		}
	}
}

// randomHex — 16 случайных байт в hex; используется для токенов файлов,
// загруженных через UI.
func randomHex() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("controlapi: источник случайности недоступен: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// timeOf переводит unix-миллисекунды во время (для заголовков отдачи файла).
func timeOf(ms int64) time.Time { return time.UnixMilli(ms) }
