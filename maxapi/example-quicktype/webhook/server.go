// Package webhook — приём событий Max Bot API на своём HTTPS-эндпоинте.
//
// Контракт эндпоинта описан отдельным документом, openapi.MaxBotWebhook.yaml:
// один POST / с телом WebhookUpdate и ответом 200.
//
// # Зачем здесь диспетчер
//
// Это то место, где сгенерированных структур не хватает. quicktype не
// порождает UnmarshalJSON с диспетчеризацией по дискриминатору, поэтому
// maxapi.Update — плоская структура: поля всех шестнадцати типов событий
// собраны в одну и почти все необязательны. Для сервера это неудобно:
// обрабатывая message_created, приходится помнить, что Message там указатель,
// хотя в самом событии он обязателен.
//
// Разбор идёт в два шага:
//
//  1. тело разбирается в maxapi.Update, чтобы прочитать update_type;
//  2. ТО ЖЕ тело разбирается второй раз в конкретный тип варианта —
//     maxapi.MessageCreatedUpdate, maxapi.MessageCallbackUpdate, — где
//     обязательные поля объявлены значениями, а не указателями.
//
// Второй разбор стоит дёшево (тело уже в памяти) и снимает весь пласт проверок
// на nil. Разобраны два типа событий, нужные echo-боту; остальные четырнадцать
// доводятся до Handler.Other и дописываются по тому же образцу.
package webhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	maxapi "maxapi-quicktype"
)

// maxBodySize ограничивает тело запроса. Крупнейшее событие — message_created
// с вложениями и разметкой; мегабайт с большим запасом это покрывает, а
// произвольно большое тело читать в память нельзя.
const maxBodySize = 1 << 20

// SecretHeader — заголовок, в котором Max присылает secret из подписки
// (см. SubscriptionRequestBody.secret в контракте API).
const SecretHeader = "X-Max-Bot-Api-Secret"

// Handler — реакции на события. Любое поле может быть nil.
type Handler struct {
	// MessageCreated вызывается на новое сообщение.
	MessageCreated func(context.Context, maxapi.MessageCreatedUpdate) error
	// MessageCallback вызывается на нажатие инлайн-кнопки.
	MessageCallback func(context.Context, maxapi.MessageCallbackUpdate) error
	// Other вызывается на остальные четырнадцать типов событий: тип уже
	// прочитан, тело отдаётся плоским maxapi.Update.
	Other func(context.Context, maxapi.Update) error
}

// Server — http.Handler, принимающий события Max Bot API.
type Server struct {
	secret  string
	handler Handler
	log     *slog.Logger
}

// New создаёт сервер. Пустой secret отключает проверку заголовка — так можно
// только локально: без него эндпоинт примет событие от кого угодно.
func New(secret string, handler Handler, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{secret: secret, handler: handler, log: log}
}

// ErrUnknownUpdateType возвращается, когда событие не разобрано ни одним
// обработчиком.
var ErrUnknownUpdateType = errors.New("нет обработчика для типа события")

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "только POST", http.StatusMethodNotAllowed)
		return
	}

	// Сравнение постоянного времени: секрет — общий, утечка по времени
	// ответа позволила бы подобрать его побайтово.
	if s.secret != "" {
		got := r.Header.Get(SecretHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.secret)) != 1 {
			s.log.Warn("отклонено: неверный секрет", "remote", r.RemoteAddr)
			http.Error(w, "неверный секрет", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
		s.log.Warn("отклонено: тело не прочитано", "error", err)
		http.Error(w, "тело не прочитано", http.StatusBadRequest)
		return
	}

	if err := s.dispatch(r.Context(), body); err != nil {
		// Max повторяет доставку на не-2xx. Ошибка разбора при повторе
		// воспроизведётся, поэтому такое тело подтверждаем и пишем в лог;
		// на ошибку обработчика отвечаем 500, чтобы событие приехало снова.
		if errors.Is(err, errMalformed) || errors.Is(err, ErrUnknownUpdateType) {
			s.log.Warn("событие пропущено", "error", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		s.log.Error("обработчик вернул ошибку", "error", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

var errMalformed = errors.New("тело не разобрано")

// dispatch — двухшаговый разбор, описанный в докстринге пакета.
func (s *Server) dispatch(ctx context.Context, body []byte) error {
	// Шаг 1: плоский Update, чтобы узнать тип.
	var flat maxapi.Update
	if err := json.Unmarshal(body, &flat); err != nil {
		return fmt.Errorf("%w как maxapi.Update: %w", errMalformed, err)
	}

	// Шаг 2: то же тело в конкретный вариант.
	switch string(flat.UpdateType) {
	case "message_created":
		if s.handler.MessageCreated == nil {
			break
		}
		var update maxapi.MessageCreatedUpdate
		if err := json.Unmarshal(body, &update); err != nil {
			return fmt.Errorf("%w как maxapi.MessageCreatedUpdate: %w", errMalformed, err)
		}
		return s.handler.MessageCreated(ctx, update)

	case "message_callback":
		if s.handler.MessageCallback == nil {
			break
		}
		var update maxapi.MessageCallbackUpdate
		if err := json.Unmarshal(body, &update); err != nil {
			return fmt.Errorf("%w как maxapi.MessageCallbackUpdate: %w", errMalformed, err)
		}
		return s.handler.MessageCallback(ctx, update)
	}

	if s.handler.Other != nil {
		return s.handler.Other(ctx, flat)
	}
	return fmt.Errorf("%w: %q", ErrUnknownUpdateType, flat.UpdateType)
}
