// Package specs загружает оба OpenAPI-контракта и даёт валидацию против них:
// входящих запросов Bot API, собственных ответов и исходящих webhook-тел.
package specs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"maxmock/api"
)

// ErrRouteNotFound — путь/метод отсутствует в контракте Bot API.
var ErrRouteNotFound = errors.New("маршрут не описан в контракте")

// Specs — загруженные контракты и построенные по ним роутеры.
type Specs struct {
	BotAPI  *openapi3.T
	Webhook *openapi3.T

	botRouter     routers.Router
	webhookRouter routers.Router
	webhookURL    string
	webhookMethod string
}

// noAuth отключает проверку security-требований: авторизацию мок выполняет
// сам, по измеренному поведению прода (голый токен в заголовке Authorization,
// см. maxfacade), а kin-openapi без этой функции считает любой запрос с
// описанным security неавторизованным.
func noAuth(context.Context, *openapi3filter.AuthenticationInput) error { return nil }

// ecmaMatcher адаптирует regexp2 под интерфейс kin-openapi.
type ecmaMatcher struct{ re *regexp2.Regexp }

func (m ecmaMatcher) MatchString(s string) bool {
	ok, err := m.re.MatchString(s)
	return err == nil && ok
}

// compileRegex компилирует `pattern` из спека движком с поддержкой
// ECMAScript-синтаксиса. Стандартный regexp Go построен на RE2 и не понимает
// опережающие проверки, а контракт их использует: `callback_id` объявлен как
// `^(?!\s*$).+` («не пробельная строка»).
func compileRegex(expr string) (openapi3.RegexMatcher, error) {
	re, err := regexp2.Compile(expr, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return ecmaMatcher{re: re}, nil
}

func requestOptions() *openapi3filter.Options {
	return &openapi3filter.Options{
		AuthenticationFunc: noAuth,
		RegexCompiler:      compileRegex,
	}
}

func loadDoc(name string) (*openapi3.T, error) {
	b, err := api.FS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("чтение %s: %w", name, err)
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(b)
	if err != nil {
		return nil, fmt.Errorf("разбор %s: %w", name, err)
	}
	normalizeAuthSchemes(doc)
	// Примеры в спеке снабжены ремарками и не обязаны проходить валидацию схем.
	if err := doc.Validate(loader.Context,
		openapi3.DisableExamplesValidation(),
		openapi3.SetRegexCompiler(compileRegex),
	); err != nil {
		return nil, fmt.Errorf("валидация документа %s: %w", name, err)
	}
	return doc, nil
}

// normalizeAuthSchemes приводит имя http-схемы авторизации к нижнему регистру.
// Артефакт объявляет `BearerAuth.scheme: Bearer`, тогда как реестр HTTP
// Authentication Schemes (RFC 7235) содержит имена в нижнем регистре и
// валидатор требует именно их. На поведение мока это не влияет: схему
// `BearerAuth` он не принимает вовсе (см. maxfacade), а правка нужна лишь
// затем, чтобы документ прошёл собственную валидацию.
func normalizeAuthSchemes(doc *openapi3.T) {
	if doc.Components == nil {
		return
	}
	for _, ref := range doc.Components.SecuritySchemes {
		if ref != nil && ref.Value != nil && ref.Value.Type == "http" {
			ref.Value.Scheme = strings.ToLower(ref.Value.Scheme)
		}
	}
}

// Load читает оба контракта из embed.FS и строит роутеры.
func Load() (*Specs, error) {
	bot, err := loadDoc(api.BotAPIFile)
	if err != nil {
		return nil, err
	}
	wh, err := loadDoc(api.WebhookFile)
	if err != nil {
		return nil, err
	}

	// Мок слушает на собственном адресе, а не на platform-api2.max.ru: если
	// оставить servers, роутер начнёт сверять хост запроса с хостом из спека
	// и не найдёт ни одного маршрута.
	bot.Servers = nil
	wh.Servers = nil

	botRouter, err := gorillamux.NewRouter(bot)
	if err != nil {
		return nil, fmt.Errorf("роутер Bot API: %w", err)
	}
	whRouter, err := gorillamux.NewRouter(wh)
	if err != nil {
		return nil, fmt.Errorf("роутер webhook: %w", err)
	}

	s := &Specs{BotAPI: bot, Webhook: wh, botRouter: botRouter, webhookRouter: whRouter}

	// В webhook-контракте ровно один путь и один метод — находим их один раз.
	for path, item := range wh.Paths.Map() {
		for method := range item.Operations() {
			s.webhookURL, s.webhookMethod = "http://webhook.local"+path, method
		}
	}
	if s.webhookMethod == "" {
		return nil, errors.New("в webhook-контракте не найдено ни одной операции")
	}
	return s, nil
}

// Version возвращает версию контракта Bot API.
func (s *Specs) Version() string { return s.BotAPI.Info.Version }

// FindRoute ищет операцию контракта, соответствующую запросу.
// Возвращает ErrRouteNotFound, если такой операции нет.
func (s *Specs) FindRoute(r *http.Request) (*routers.Route, map[string]string, error) {
	route, params, err := s.botRouter.FindRoute(r)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s %s", ErrRouteNotFound, r.Method, r.URL.Path)
	}
	return route, params, nil
}

func requestInput(r *http.Request, route *routers.Route, params map[string]string) *openapi3filter.RequestValidationInput {
	return &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: params,
		Route:      route,
		Options:    requestOptions(),
	}
}

// ValidateRequest проверяет параметры и тело запроса против контракта.
//
// После валидации r.Body содержит ровно те байты, что прислал клиент.
// Это не бесплатно: kin-openapi по ходу валидации подменяет тело собственной
// пересборкой разобранного JSON — с применёнными `default` и переупорядоченными
// ключами. Обработчику нужен оригинал: значения по умолчанию (`notify`,
// `disable_link_preview`) мок подставляет сам, а различие «поле пришло со
// значением null» и «поля не было» в пересборке теряется.
func (s *Specs) ValidateRequest(ctx context.Context, r *http.Request, route *routers.Route, params map[string]string) error {
	var raw []byte
	if r.Body != nil && r.Body != http.NoBody {
		var err error
		if raw, err = io.ReadAll(r.Body); err != nil {
			return fmt.Errorf("чтение тела запроса: %w", err)
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		defer func() { r.Body = io.NopCloser(bytes.NewReader(raw)) }()
	}
	return openapi3filter.ValidateRequest(ctx, requestInput(r, route, params))
}

// ValidateResponse проверяет собственный ответ мока против контракта.
// Статусы, не описанные в спеке (например, 400 — его в контракте Max нет),
// пропускаются: их форму гарантирует схема Error на стороне мока.
func (s *Specs) ValidateResponse(ctx context.Context, r *http.Request, route *routers.Route, params map[string]string, status int, header http.Header, body []byte) error {
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: requestInput(r, route, params),
		Status:                 status,
		Header:                 header,
		Body:                   io.NopCloser(bytes.NewReader(body)),
		Options:                requestOptions(),
	}
	return openapi3filter.ValidateResponse(ctx, input)
}

// ValidateWebhookBody проверяет тело исходящего события против контракта
// webhook-эндпоинта (openapi.MaxBotWebhook.yaml).
func (s *Specs) ValidateWebhookBody(ctx context.Context, body []byte) error {
	req, err := http.NewRequest(s.webhookMethod, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	route, params, err := s.webhookRouter.FindRoute(req)
	if err != nil {
		return fmt.Errorf("маршрут webhook не найден: %w", err)
	}
	return openapi3filter.ValidateRequest(ctx, &openapi3filter.RequestValidationInput{
		Request:     req,
		PathParams:  params,
		QueryParams: url.Values{},
		Route:       route,
		Options:     requestOptions(),
	})
}
