// Package controlapi — служебный интерфейс мока на префиксе /mock.
//
// Здесь живёт всё, чего нет в Max Bot API: регистрация ботов, создание
// виртуальных клиентов, действия «за клиента», журналы, приём файлов и
// живой поток изменений для UI. Префикс выбран так, чтобы ни один путь не
// пересекался с поверхностью Bot API.
package controlapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"maxmock/internal/core"
	"maxmock/internal/events"
	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// API — служебный интерфейс.
type API struct {
	core *core.Core
	bus  *events.Bus
}

// New собирает control-API.
func New(c *core.Core, bus *events.Bus) *API {
	return &API{core: c, bus: bus}
}

// Mux — то, во что регистрируются маршруты. Интерфейс вместо *http.ServeMux
// нужен httpserver: он подставляет обёртку, которая по дороге навешивает на
// каждый обработчик строку консольного лога.
type Mux interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// Routes регистрирует служебные маршруты в переданном мультиплексоре.
func (a *API) Routes(mux Mux) {
	mux.HandleFunc("GET /mock/api/bots", a.listBots)
	mux.HandleFunc("POST /mock/api/bots", a.createBot)
	mux.HandleFunc("GET /mock/api/bots/{id}", a.getBot)
	mux.HandleFunc("GET /mock/api/bots/{id}/subscriptions", a.listSubscriptions)
	mux.HandleFunc("GET /mock/api/bots/{id}/clients", a.listClients)
	mux.HandleFunc("POST /mock/api/bots/{id}/clients", a.createClient)
	mux.HandleFunc("GET /mock/api/bots/{id}/log", a.requestLog)
	mux.HandleFunc("GET /mock/api/bots/{id}/deliveries", a.deliveries)
	mux.HandleFunc("POST /mock/api/bots/{id}/files", a.uploadFromUI)
	mux.HandleFunc("GET /mock/api/log", a.globalLog)
	mux.HandleFunc("GET /mock/api/dialogs/{chatId}/messages", a.dialogMessages)
	mux.HandleFunc("GET /mock/api/dialogs/{chatId}/events", a.dialogEvents)
	mux.HandleFunc("POST /mock/api/dialogs/{chatId}/actions", a.dialogAction)

	mux.HandleFunc("POST /mock/upload/{token}", a.uploadFromBotPlatform)
	mux.HandleFunc("PUT /mock/upload/{token}", a.uploadFromBotPlatform)
	mux.HandleFunc("GET /mock/files/{token}", a.serveFile)
	mux.HandleFunc("GET /mock/ws", a.websocket)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, wire.Error{Code: code, Message: message})
}

// fail переводит доменную ошибку в служебный ответ. Коды здесь те же
// `mock.*`, что и в фасаде: control-API — часть мока, а не эмуляция Max.
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrChatNotFound):
		writeErr(w, http.StatusNotFound, "mock.chat.not_found", "диалог не найден")
	case errors.Is(err, core.ErrMessageNotFound):
		writeErr(w, http.StatusNotFound, "mock.message.not_found", "сообщение не найдено")
	case errors.Is(err, core.ErrAttachmentNotFound):
		writeErr(w, http.StatusNotFound, "mock.attachment.not_found", "вложение не найдено")
	case errors.Is(err, core.ErrForbidden):
		writeErr(w, http.StatusForbidden, "mock.forbidden", "операция запрещена")
	case errors.Is(err, core.ErrBadRequest):
		writeErr(w, http.StatusBadRequest, "mock.bad_request", err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "mock.not_found", "запись не найдена")
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "mock.conflict", err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "mock.internal", err.Error())
	}
}

// pathInt64 читает числовой параметр пути.
func pathInt64(r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return v, err == nil
}

func queryInt(r *http.Request, name string, def int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return v
}

func (a *API) listBots(w http.ResponseWriter, _ *http.Request) {
	bots, err := a.core.Store().ListBots()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bots)
}

func (a *API) createBot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Username    string `json:"username"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "тело запроса не разобрано: "+err.Error())
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "не задано имя бота")
		return
	}
	if req.Username == "" {
		req.Username = "bot"
	}
	bot, err := a.core.Store().CreateBot(req.Name, req.Username, req.Description)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bot)
}

// bot читает бота из пути, отвечая ошибкой, если его нет.
func (a *API) bot(w http.ResponseWriter, r *http.Request) (*store.Bot, bool) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "некорректный идентификатор бота")
		return nil, false
	}
	bot, err := a.core.Store().BotByID(id)
	if err != nil {
		fail(w, err)
		return nil, false
	}
	return bot, true
}

func (a *API) getBot(w http.ResponseWriter, r *http.Request) {
	if bot, ok := a.bot(w, r); ok {
		writeJSON(w, http.StatusOK, bot)
	}
}

func (a *API) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	bot, ok := a.bot(w, r)
	if !ok {
		return
	}
	subs, err := a.core.Store().ListSubscriptions(bot.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

func (a *API) listClients(w http.ResponseWriter, r *http.Request) {
	bot, ok := a.bot(w, r)
	if !ok {
		return
	}
	clients, err := a.core.Store().ListClients(bot.ID)
	if err != nil {
		fail(w, err)
		return
	}
	dialogs, err := a.core.Store().ListDialogs(bot.ID)
	if err != nil {
		fail(w, err)
		return
	}
	chatByUser := make(map[int64]store.Dialog, len(dialogs))
	for _, d := range dialogs {
		chatByUser[d.UserID] = d
	}

	type entry struct {
		store.Client
		ChatID  int64 `json:"chat_id"`
		Started bool  `json:"started"`
	}
	out := make([]entry, 0, len(clients))
	for _, cl := range clients {
		d := chatByUser[cl.UserID]
		out = append(out, entry{Client: cl, ChatID: d.ChatID, Started: d.Started})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createClient(w http.ResponseWriter, r *http.Request) {
	bot, ok := a.bot(w, r)
	if !ok {
		return
	}
	var req struct {
		UserID    *int64   `json:"user_id"`
		FirstName string   `json:"first_name"`
		LastName  string   `json:"last_name"`
		Username  string   `json:"username"`
		Phone     string   `json:"phone"`
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "тело запроса не разобрано: "+err.Error())
		return
	}
	if req.FirstName == "" {
		req.FirstName = "Клиент"
	}
	cl, d, err := a.core.CreateClient(bot.ID, store.ClientInput{
		UserID: req.UserID, FirstName: req.FirstName, LastName: req.LastName,
		Username: req.Username, Phone: req.Phone,
		Latitude: req.Latitude, Longitude: req.Longitude,
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.logAction(bot.ID, d.ChatID, "create_client", nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{"client": cl, "dialog": d})
}

func (a *API) requestLog(w http.ResponseWriter, r *http.Request) {
	bot, ok := a.bot(w, r)
	if !ok {
		return
	}
	entries, err := a.core.Store().ListRequestLog(&bot.ID, queryInt(r, "limit", 100))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a *API) globalLog(w http.ResponseWriter, r *http.Request) {
	entries, err := a.core.Store().ListRequestLog(nil, queryInt(r, "limit", 100))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a *API) deliveries(w http.ResponseWriter, r *http.Request) {
	bot, ok := a.bot(w, r)
	if !ok {
		return
	}
	entries, err := a.core.Store().ListDeliveries(bot.ID, queryInt(r, "limit", 100))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a *API) dialogMessages(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathInt64(r, "chatId")
	if !ok {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "некорректный chat_id")
		return
	}
	msgs, err := a.core.DialogMessages(chatID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// dialogEvents отдаёт ленту диалога: действия тестировщика, доставки на
// стенд и вызовы Bot API от бота в общей хронологии.
func (a *API) dialogEvents(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathInt64(r, "chatId")
	if !ok {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "некорректный chat_id")
		return
	}
	// store.ErrNotFound переводим в core.ErrChatNotFound, как и в dialogAction:
	// это тот же путь /mock/api/dialogs/{chatId}/*, и клиент вправе ждать
	// одинаковый код на несуществующий диалог, а не разный по эндпоинту.
	d, err := a.core.Store().DialogByChatID(chatID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = core.ErrChatNotFound
		}
		fail(w, err)
		return
	}
	list, err := a.core.Store().ListDialogEvents(d.BotID, chatID, queryInt(r, "limit", 200))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// logAction записывает действие тестировщика в журнал диалога. botID
// приходит от вызывающего — диалог к этому моменту уже резолвлен
// (в dialogAction — до вызова ядра, в createClient — при поиске бота),
// повторный SELECT на каждое действие не нужен.
//
// Пишется и неудачное действие, с причиной: «нажал, ничего не произошло» —
// самая частая жалоба, и в ленте она должна стоять рядом с объяснением.
// Сбой самого журнала действие не отменяет: журнал — диагностика, а не
// часть операции.
func (a *API) logAction(botID, chatID int64, action string, detail []byte, actionErr error) {
	e := &store.UIActionEntry{
		BotID: botID, ChatID: chatID, Action: action, Detail: string(detail),
	}
	if actionErr != nil {
		e.Error = actionErr.Error()
	}
	if err := a.core.Store().LogUIAction(e); err != nil {
		return
	}
	a.bus.Publish(events.Event{
		Kind: events.KindUIAction, BotID: botID, ChatID: chatID, Payload: e,
	})
}

// dialogAction выполняет действие тестировщика в роли клиента.
func (a *API) dialogAction(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathInt64(r, "chatId")
	if !ok {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "некорректный chat_id")
		return
	}
	// Диалог резолвим один раз здесь же: logAction получит botID готовым, а
	// несуществующий чат отсечётся сразу, а не после того, как до него
	// доберётся ядро — иначе именно неудачное действие (самый нужный случай
	// для журнала) осталось бы незаписанным.
	d, err := a.core.Store().DialogByChatID(chatID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = core.ErrChatNotFound
		}
		fail(w, err)
		return
	}
	// Тело читаем целиком: оно уходит в журнал как есть.
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "тело запроса не прочитано: "+err.Error())
		return
	}
	var req struct {
		Action      string   `json:"action"`
		Text        string   `json:"text"`
		Mid         string   `json:"mid"`
		Payload     string   `json:"payload"`
		Attachments []string `json:"attachments"`
		// Координаты действия send_location; не заданы — берутся из карточки.
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "тело запроса не разобрано: "+err.Error())
		return
	}

	var (
		msg    *wire.Message
		actErr error
	)
	switch req.Action {
	case "start":
		actErr = a.core.ClientStart(chatID, req.Payload)
	case "stop":
		actErr = a.core.ClientStop(chatID)
	case "send":
		msg, actErr = a.core.ClientSendMessage(chatID, req.Text, req.Attachments)
	case "send_contact":
		msg, actErr = a.core.ClientSendContact(chatID)
	case "send_location":
		msg, actErr = a.core.ClientSendLocation(chatID, req.Latitude, req.Longitude)
	case "press":
		actErr = a.core.ClientPressButton(chatID, req.Mid, req.Payload)
	case "edit":
		actErr = a.core.ClientEditMessage(chatID, req.Mid, req.Text)
	case "delete":
		actErr = a.core.ClientDeleteMessage(chatID, req.Mid)
	default:
		writeErr(w, http.StatusBadRequest, "mock.bad_request",
			"неизвестное действие "+strconv.Quote(req.Action)+
				"; допустимы start, stop, send, send_contact, send_location, press, edit, delete")
		return
	}

	a.logAction(d.BotID, chatID, req.Action, raw, actErr)

	if actErr != nil {
		fail(w, actErr)
		return
	}
	if msg != nil {
		writeJSON(w, http.StatusOK, msg)
		return
	}
	writeJSON(w, http.StatusOK, wire.SimpleQueryResult{Success: true})
}
