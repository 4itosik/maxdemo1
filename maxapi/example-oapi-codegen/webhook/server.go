// Package webhook — приём событий Max Bot API на своём HTTPS-эндпоинте.
//
// Парный пакет ../../example-quicktype/webhook делает то же самое поверх структур
// quicktype. Разница — в разборе тела, и она здесь единственная существенная.
//
// # Как разбирается тело
//
// Тип maxapi.Update, порождённый oapi-codegen, — настоящий дискриминированный
// union: внутри сырой JSON, а рядом сгенерированы Discriminator() и
// ValueByDiscriminator() на все шестнадцать типов событий. Разбор — один
// вызов, дальше type switch по конкретным типам.
//
// В варианте quicktype этого нет: там Update — плоская структура со всеми
// полями всех вариантов, а диспетчеризацию приходится писать руками
// (двухшаговый разбор: сперва прочитать update_type, потом разобрать то же
// тело второй раз в нужный тип). Здесь эти ~30 строк заменены на вызов
// сгенерированного метода, и добавление семнадцатого типа события в контракт
// не требует правок в этом файле вообще.
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

	"stash.sigma.sbrf.ru/scpl/oapi/maxapi"
)

// maxBodySize ограничивает тело запроса: крупнейшее событие — message_created
// с вложениями и разметкой, мегабайт покрывает с запасом.
const maxBodySize = 1 << 20

// SecretHeader — заголовок, в котором Max присылает secret из подписки
// (SubscriptionRequestBody.secret в контракте).
const SecretHeader = "X-Max-Bot-Api-Secret"

// Handler — реакции на события. Любое поле может быть nil.
type Handler struct {
	MessageCreated  func(context.Context, maxapi.MessageCreatedUpdate) error
	MessageCallback func(context.Context, maxapi.MessageCallbackUpdate) error
	// Other вызывается на остальные четырнадцать типов: разобранный вариант
	// отдаётся как есть, тип определяется в потребителе.
	Other func(ctx context.Context, updateType string, value any) error
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

var errMalformed = errors.New("тело не разобрано")

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "только POST", http.StatusMethodNotAllowed)
		return
	}

	// Сравнение постоянного времени: секрет общий, утечка по времени ответа
	// позволила бы подобрать его побайтово.
	if s.secret != "" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(SecretHeader)), []byte(s.secret)) != 1 {
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
		// Max повторяет доставку на не-2xx. Битое тело при повторе останется
		// битым, поэтому подтверждаем его и пишем в лог; на ошибку обработчика
		// отвечаем 500, чтобы событие приехало снова.
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

// dispatch разбирает тело сгенерированным дискриминатором и раздаёт по
// обработчикам.
func (s *Server) dispatch(ctx context.Context, body []byte) error {
	var update maxapi.Update
	if err := json.Unmarshal(body, &update); err != nil {
		return fmt.Errorf("%w как maxapi.Update: %w", errMalformed, err)
	}

	// Один вызов вместо рукописного switch по update_type.
	value, err := update.ValueByDiscriminator()
	if err != nil {
		return fmt.Errorf("%w: дискриминатор: %w", errMalformed, err)
	}

	switch typed := value.(type) {
	case maxapi.MessageCreatedUpdate:
		if s.handler.MessageCreated != nil {
			return s.handler.MessageCreated(ctx, typed)
		}
	case maxapi.MessageCallbackUpdate:
		if s.handler.MessageCallback != nil {
			return s.handler.MessageCallback(ctx, typed)
		}
	}

	if s.handler.Other != nil {
		kind, err := update.Discriminator()
		if err != nil {
			return fmt.Errorf("%w: дискриминатор: %w", errMalformed, err)
		}
		return s.handler.Other(ctx, kind, value)
	}
	kind, _ := update.Discriminator()
	return fmt.Errorf("%w: %q", ErrUnknownUpdateType, kind)
}
