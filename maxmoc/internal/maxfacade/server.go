// Package maxfacade — HTTP-фасад Max Bot API.
//
// Каждый запрос проходит один и тот же путь: поиск операции в контракте →
// авторизация → валидация запроса по OpenAPI → обработчик → валидация
// собственного ответа → журнал. Обработчики выбираются по operationId из
// самого контракта, поэтому поверхность мока не может разойтись со спекой:
// новой операции в yaml соответствует ветка здесь, а её отсутствие — честный
// 404 mock.not_implemented.
package maxfacade

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/routers"

	"maxmock/internal/config"
	"maxmock/internal/core"
	"maxmock/internal/events"
	"maxmock/internal/logx"
	"maxmock/internal/specs"
	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// maxBodyBytes — предел размера тела запроса Bot API. Файлы сюда не приходят:
// для них есть отдельный upload-эндпоинт.
const maxBodyBytes = 8 << 20

// Server — фасад Bot API.
type Server struct {
	core  *core.Core
	specs *specs.Specs
	store *store.Store
	bus   *events.Bus
	cfg   *config.Config
}

// New собирает фасад.
func New(c *core.Core, sp *specs.Specs, bus *events.Bus, cfg *config.Config) *Server {
	return &Server{core: c, specs: sp, store: c.Store(), bus: bus, cfg: cfg}
}

// recorder буферизует ответ: его нужно проверить по контракту и записать в
// журнал до того, как он уйдёт клиенту.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
	// chatID — диалог операции, если она его касается. Заполняет обработчик:
	// см. noteChat.
	chatID *int64
}

func newRecorder() *recorder {
	return &recorder{header: make(http.Header), status: http.StatusOK}
}

// noteChat отмечает диалог, к которому относится операция.
//
// Вывести его из адреса нельзя: у половины операций диалог определяется телом
// запроса или идентификатором сущности — сообщения, нажатия. Поэтому его
// сообщает обработчик, который всё равно эти сущности резолвит.
func noteChat(w http.ResponseWriter, chatID int64) {
	rec, ok := w.(*recorder)
	if !ok || chatID == 0 {
		return
	}
	rec.chatID = &chatID
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) { r.status = status }

func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *recorder) flush(w http.ResponseWriter) {
	for k, v := range r.header {
		w.Header()[k] = v
	}
	w.WriteHeader(r.status)
	_, _ = w.Write(r.body.Bytes())
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := newRecorder()

	reqBody, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if readErr != nil {
		writeError(rec, http.StatusBadRequest, CodeValidation, "тело запроса не прочитано: "+readErr.Error())
		rec.flush(w)
		s.finish(r, nil, rec, reqBody, start)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(reqBody))

	route, params, bot := s.handle(rec, r, reqBody)
	s.validateOwnResponse(r, route, params, rec, reqBody)
	rec.flush(w)
	s.finish(r, bot, rec, reqBody, start)
}

// handle выполняет запрос, возвращая найденную операцию и авторизованного
// бота (для журнала).
func (s *Server) handle(w http.ResponseWriter, r *http.Request, reqBody []byte) (*routers.Route, map[string]string, *store.Bot) {
	route, params, err := s.specs.FindRoute(r)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotImplemented,
			"операция не реализована моком: "+r.Method+" "+r.URL.Path)
		return nil, nil, nil
	}

	bot, err := s.authorize(r)
	if err != nil {
		writeError(w, statusFor(route, http.StatusUnauthorized), CodeVerifyToken, err.Error())
		return route, params, nil
	}

	if err := s.specs.ValidateRequest(r.Context(), r, route, params); err != nil {
		writeError(w, http.StatusBadRequest, CodeValidation, err.Error())
		return route, params, bot
	}
	// ValidateRequest вернул тело в исходном виде — восстанавливаем читатель
	// для обработчика.
	r.Body = io.NopCloser(bytes.NewReader(reqBody))

	s.dispatch(w, r, route, params, bot, reqBody)
	return route, params, bot
}

// authorize находит бота по токену из заголовка Authorization.
//
// Контракт объявляет две схемы — `BearerAuth` и `access_token` в query, — но
// живой Max не принимает ни одной: токен уходит голым,
// `Authorization: <token>`. Замер прода 2026-08-06 и обоснование выбора — в
// docs/specs/2026-08-06-auth-parity-tz.md. Мок повторяет измеренное поведение
// прода, а не контракт: его назначение — заменить собой prod-адрес, и
// приложение, написанное по документации Max, обязано работать против него
// без единой правки.
func (s *Server) authorize(r *http.Request) (*store.Bot, error) {
	token, err := extractToken(r)
	if err != nil {
		return nil, err
	}
	bot, err := s.store.BotByToken(token)
	if err != nil {
		return nil, errUnauthorized("Invalid access_token")
	}
	return bot, nil
}

// bearerPrefix — схема, которую прод отличает от прочих неверных значений:
// на `Bearer <что угодно>` он отвечает `Malformed access token`, а не
// `Invalid access_token`, — очевидно, пытается разобрать значение как токен
// другого рода. Мок повторяет это различие: именно оно скажет интегратору,
// что он взял схему из спеки, а не из документации. Регистр важен —
// `bearer abc` на проде даёт обычный `Invalid`.
const bearerPrefix = "Bearer "

// extractToken достаёт токен из заголовка Authorization. Значение заголовка
// после TrimSpace целиком и есть токен: разбора схемы нет.
//
// Заголовок имеет безусловный приоритет — при живом заголовке query-параметр
// не смотрится вовсе, как и на проде. Сам по себе `access_token` в query не
// принимается («передача токена через query-параметры больше не
// поддерживается» — dev.max.ru/docs-api), но распознаётся отдельно: глухое
// «нет токена» на валидный токен в URL стоило бы интегратору часа.
func extractToken(r *http.Request) (string, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		if strings.TrimSpace(r.URL.Query().Get("access_token")) != "" {
			return "", errUnauthorized(
				"Query parameter access_token is deprecated, use Authorization header")
		}
		return "", errUnauthorized("No access token")
	}
	if strings.HasPrefix(header, bearerPrefix) {
		return "", errUnauthorized("Malformed access token")
	}
	return header, nil
}

type authError string

func (e authError) Error() string      { return string(e) }
func errUnauthorized(msg string) error { return authError(msg) }

// validateOwnResponse сверяет собственный ответ мока с контрактом.
// Расхождение — ошибка мока, а не клиента: подменяем ответ на 500, чтобы она
// не выглядела как проблема интеграции, и оставляем след в журнале.
func (s *Server) validateOwnResponse(r *http.Request, route *routers.Route, params map[string]string, rec *recorder, reqBody []byte) {
	if !s.cfg.ValidateResponses || route == nil {
		return
	}
	probe := r.Clone(r.Context())
	probe.Body = io.NopCloser(bytes.NewReader(reqBody))
	err := s.specs.ValidateResponse(r.Context(), probe, route, params, rec.status, rec.header, rec.body.Bytes())
	if err == nil {
		return
	}
	slog.ErrorContext(r.Context(), "ответ мока не соответствует контракту",
		"method", r.Method, "path", r.URL.Path, logx.Err(err))
	rec.header = make(http.Header)
	rec.body.Reset()
	rec.status = http.StatusOK
	writeError(rec, http.StatusInternalServerError, CodeInternal,
		"ответ мока не соответствует контракту: "+err.Error())
}

// finish пишет запись журнала, публикует событие для UI и печатает строку в
// консоль.
func (s *Server) finish(r *http.Request, bot *store.Bot, rec *recorder, reqBody []byte, start time.Time) {
	entry := &store.RequestLogEntry{
		Method:       r.Method,
		Path:         r.URL.Path,
		Query:        r.URL.RawQuery,
		ChatID:       rec.chatID,
		Status:       rec.status,
		RequestBody:  string(reqBody),
		ResponseBody: rec.body.String(),
		LatencyMS:    time.Since(start).Milliseconds(),
	}
	var botID int64
	if bot != nil {
		entry.BotID = &bot.ID
		botID = bot.ID
	}
	s.logRequest(r, entry, botID)
	if err := s.store.LogRequest(entry); err != nil {
		slog.ErrorContext(r.Context(), "журнал запросов недоступен", logx.Err(err))
	}
	ev := events.Event{Kind: events.KindRequest, BotID: botID, Payload: entry}
	if rec.chatID != nil {
		ev.ChatID = *rec.chatID
	}
	s.bus.Publish(ev)
}

// logRequest печатает вызов Bot API. Поля берутся из уже собранной записи
// журнала: второго прохода по запросу не нужно, и консоль с UI не разъедутся.
//
// Токен не печатается: он приходит заголовком Authorization, а заголовки в
// строку не попадают вовсе — из query его на всякий случай вычищает logx.
func (s *Server) logRequest(r *http.Request, e *store.RequestLogEntry, botID int64) {
	slog.LogAttrs(r.Context(), logx.LevelForStatus(e.Status), "Bot API",
		slog.String("method", e.Method),
		slog.String("path", e.Path),
		logx.Query(e.Query),
		slog.Int("status", e.Status),
		slog.Int64("ms", e.LatencyMS),
		logx.Int64("bot", botID),
		logx.Int64("chat", chatOf(e)),
		logx.Body("req", []byte(e.RequestBody)),
		logx.Body("resp", []byte(e.ResponseBody)),
	)
}

func chatOf(e *store.RequestLogEntry) int64 {
	if e.ChatID == nil {
		return 0
	}
	return *e.ChatID
}

// dispatch выбирает обработчик по operationId контракта.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, route *routers.Route, params map[string]string, bot *store.Bot, body []byte) {
	q := r.URL.Query()
	switch route.Operation.OperationID {
	case "getMyInfo":
		writeJSON(w, http.StatusOK, s.core.BotInfo(bot))

	case "editMyCommands":
		s.editMyCommands(w, route, bot, body)

	case "getMessages":
		s.getMessages(w, route, bot, q)

	case "sendMessage":
		s.sendMessage(w, route, bot, q, body)

	case "editMessage":
		s.editMessage(w, route, bot, q, body)

	case "deleteMessage":
		s.deleteMessage(w, route, bot, q)

	case "getMessageById":
		s.getMessageByID(w, route, bot, params["messageId"])

	case "answerOnCallback":
		s.answerOnCallback(w, route, bot, q, body)

	case "getSubscriptions":
		s.getSubscriptions(w, route, bot)

	case "subscribe":
		s.subscribe(w, route, bot, body)

	case "unsubscribe":
		s.unsubscribe(w, route, bot, q)

	case "getUploadUrl":
		s.getUploadURL(w, route, bot, q)

	default:
		// Операция описана в контракте, но моком не реализована
		// (PATCH /me/commands, GET /updates, GET /videos/{videoToken}).
		writeError(w, http.StatusNotFound, CodeNotImplemented,
			"операция "+route.Operation.OperationID+" не реализована моком")
	}
}

func (s *Server) fail(w http.ResponseWriter, route *routers.Route, err error) {
	status, code, message := errorResponse(route, err)
	writeError(w, status, code, message)
}

// decodeBody разбирает тело запроса. Форму уже проверила валидация по
// контракту, поэтому ошибка здесь означала бы расхождение схемы и структуры.
func decodeBody(body []byte, v any) error { return json.Unmarshal(body, v) }

func queryInt64(q map[string][]string, name string) *int64 {
	vals, ok := q[name]
	if !ok || len(vals) == 0 || vals[0] == "" {
		return nil
	}
	n, err := strconv.ParseInt(vals[0], 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func (s *Server) editMyCommands(w http.ResponseWriter, route *routers.Route, bot *store.Bot, body []byte) {
	var patch wire.BotCommandsPatch
	if err := decodeBody(body, &patch); err != nil {
		writeError(w, http.StatusBadRequest, CodeValidation, "тело запроса не разобрано: "+err.Error())
		return
	}
	info, err := s.core.EditMyCommands(bot, patch)
	if err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) getMessages(w http.ResponseWriter, route *routers.Route, bot *store.Bot, q map[string][]string) {
	f := store.MessageFilter{
		ChatID: queryInt64(q, "chat_id"),
		From:   queryInt64(q, "from"),
		To:     queryInt64(q, "to"),
	}
	if c := queryInt64(q, "count"); c != nil {
		f.Count = int(*c)
	}
	if vals, ok := q["message_ids"]; ok && len(vals) > 0 && vals[0] != "" {
		// Параметр объявлен массивом со style=form, explode=false —
		// значения приходят через запятую.
		for _, part := range strings.Split(strings.Join(vals, ","), ",") {
			if part = strings.TrimSpace(part); part != "" {
				f.MessageIDs = append(f.MessageIDs, part)
			}
		}
	}
	if f.ChatID != nil {
		noteChat(w, *f.ChatID)
	}
	list, err := s.core.GetMessages(bot, f)
	if err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) sendMessage(w http.ResponseWriter, route *routers.Route, bot *store.Bot, q map[string][]string, body []byte) {
	var req wire.NewMessageBody
	if err := decodeBody(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, CodeValidation, "тело запроса не разобрано: "+err.Error())
		return
	}
	msg, err := s.core.SendMessage(bot, queryInt64(q, "user_id"), queryInt64(q, "chat_id"), req)
	if err != nil {
		s.fail(w, route, err)
		return
	}
	if msg.Recipient.ChatID != nil {
		noteChat(w, *msg.Recipient.ChatID)
	}
	writeJSON(w, http.StatusOK, wire.SendMessageResult{Message: *msg})
}

func (s *Server) editMessage(w http.ResponseWriter, route *routers.Route, bot *store.Bot, q map[string][]string, body []byte) {
	var req wire.NewMessageBody
	if err := decodeBody(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, CodeValidation, "тело запроса не разобрано: "+err.Error())
		return
	}
	mid := ""
	if vals := q["message_id"]; len(vals) > 0 {
		mid = vals[0]
	}
	if rec, err := s.store.MessageByMid(mid); err == nil {
		noteChat(w, rec.ChatID)
	}
	if err := s.core.EditMessage(bot, mid, req); err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, wire.SimpleQueryResult{Success: true})
}

func (s *Server) deleteMessage(w http.ResponseWriter, route *routers.Route, bot *store.Bot, q map[string][]string) {
	mid := ""
	if vals := q["message_id"]; len(vals) > 0 {
		mid = vals[0]
	}
	if rec, err := s.store.MessageByMid(mid); err == nil {
		noteChat(w, rec.ChatID)
	}
	if err := s.core.DeleteMessage(bot, mid); err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, wire.SimpleQueryResult{Success: true})
}

func (s *Server) getMessageByID(w http.ResponseWriter, route *routers.Route, bot *store.Bot, mid string) {
	if rec, err := s.store.MessageByMid(mid); err == nil {
		noteChat(w, rec.ChatID)
	}
	msg, err := s.core.GetMessageByID(bot, mid)
	if err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

func (s *Server) answerOnCallback(w http.ResponseWriter, route *routers.Route, bot *store.Bot, q map[string][]string, body []byte) {
	var ans wire.CallbackAnswer
	if err := decodeBody(body, &ans); err != nil {
		writeError(w, http.StatusBadRequest, CodeValidation, "тело запроса не разобрано: "+err.Error())
		return
	}
	callbackID := ""
	if vals := q["callback_id"]; len(vals) > 0 {
		callbackID = vals[0]
	}
	if cb, err := s.store.CallbackByID(callbackID); err == nil {
		noteChat(w, cb.ChatID)
	}
	if err := s.core.AnswerCallback(bot, callbackID, ans); err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, wire.SimpleQueryResult{Success: true})
}

func (s *Server) getSubscriptions(w http.ResponseWriter, route *routers.Route, bot *store.Bot) {
	list, err := s.core.Subscriptions(bot)
	if err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) subscribe(w http.ResponseWriter, route *routers.Route, bot *store.Bot, body []byte) {
	var req wire.SubscriptionRequestBody
	if err := decodeBody(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, CodeValidation, "тело запроса не разобрано: "+err.Error())
		return
	}
	if err := s.core.Subscribe(bot, req); err != nil {
		s.fail(w, route, err)
		return
	}
	// Пометки про http:// здесь больше нет: контракт объявляет
	// SubscriptionRequestBody.url как `^https://.+$`, и запрос с http-адресом
	// отсеивается валидацией тела до входа в хендлер. Ветка была недостижима.
	writeJSON(w, http.StatusOK, wire.SimpleQueryResult{Success: true})
}

func (s *Server) unsubscribe(w http.ResponseWriter, route *routers.Route, bot *store.Bot, q map[string][]string) {
	url := ""
	if vals := q["url"]; len(vals) > 0 {
		url = vals[0]
	}
	if err := s.core.Unsubscribe(bot, url); err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, wire.SimpleQueryResult{Success: true})
}

func (s *Server) getUploadURL(w http.ResponseWriter, route *routers.Route, bot *store.Bot, q map[string][]string) {
	uploadType := ""
	if vals := q["type"]; len(vals) > 0 {
		uploadType = vals[0]
	}
	endpoint, err := s.core.CreateUpload(bot, uploadType)
	if err != nil {
		s.fail(w, route, err)
		return
	}
	writeJSON(w, http.StatusOK, endpoint)
}
