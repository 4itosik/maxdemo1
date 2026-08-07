# Простой Max-бот на Go — дизайн

Дата: 2026-08-06
Статус: черновик на ревью

## 1. Цель

Демонстрационный бот для мессенджера MAX на Go: отвечает на команды, показывает
inline-клавиатуру, обрабатывает нажатия кнопок. Получает события через webhook.
Проект учебный — код должен читаться как справочник по Max Bot API, а не прятать
его за абстракциями.

Не входит в объём: вложения (фото/видео/файлы), работа с групповыми чатами и
каналами, база данных, состояние диалога, деплой.

## 2. Исходные данные об API

Источники: `https://dev.max.ru/docs-api` и `max-openapi-official.json` (Max Bot API 0.0.32).

- База: `https://platform-api2.max.ru`
- Авторизация: заголовок `Authorization: <token>`. В OpenAPI объявлен ещё query-параметр
  `access_token`, но и примеры в документации, и официальный SDK используют заголовок.
  Берём заголовок, чтобы токен не попадал в логи прокси.
- Токен: портал [business.max.ru](https://business.max.ru) → создать бота → раздел
  «Интеграция» после модерации.
- Лимиты: 30 rps на домен, не более 2 сообщений в секунду в один чат.
- Webhook требует HTTPS на порту 443 с сертификатом от доверенного УЦ
  (самоподписанные и HTTP не принимаются). Опциональный `secret` приходит в
  заголовке `X-Max-Bot-Api-Secret`.
- При активной webhook-подписке `GET /updates` (long polling) не работает.

Используемые методы:

| Метод | Назначение |
|---|---|
| `GET /me` | информация о боте, проверка токена |
| `PATCH /me/commands` | публикация списка команд |
| `POST /subscriptions` | подписка на webhook |
| `DELETE /subscriptions?url=` | отписка |
| `POST /messages?user_id=\|chat_id=` | отправка сообщения |
| `POST /answers?callback_id=` | ответ на нажатие кнопки |

## 3. Принятые решения

| Решение | Выбор |
|---|---|
| Клиент | свой тонкий HTTP-клиент на stdlib; структуры выписаны вручную по OpenAPI |
| Транспорт | только webhook |
| Структура | пакеты: `cmd/bot`, `internal/maxapi`, `internal/bot`, `internal/webhook` |
| Тесты | юнит-тесты на `net/http/httptest`, разработка по TDD |
| Модуль | `maxbotdemo` |
| Зависимости | только стандартная библиотека |

Кодогенерация из OpenAPI отклонена: из 130 схем нужны ~15, а `oneOf` с
дискриминатором генерируется в неудобный для чтения код.

## 4. Структура проекта

```
maxbotdemo/
├── go.mod                      module maxbotdemo
├── README.md
├── max-openapi-official.json   (уже есть)
├── cmd/bot/main.go
├── internal/maxapi/
│   ├── models.go
│   ├── models_test.go
│   ├── client.go
│   ├── client_test.go
│   ├── messages.go
│   ├── bots.go
│   └── subscriptions.go
├── internal/bot/
│   ├── bot.go
│   ├── bot_test.go
│   └── keyboard.go
└── internal/webhook/
    ├── server.go
    └── server_test.go
```

## 5. Пакет `internal/maxapi`

### 5.1 Модель Update

В API `Update` — это `oneOf` с дискриминатором `update_type`, причём поля лежат
по-разному: у `message_created` адресат находится внутри `message.recipient`, а у
`bot_started` — `chat_id` и `user` на верхнем уровне. Вместо интерфейса и ручной
диспетчеризации используем одну «плоскую» структуру с указателями на
необязательные части. Тот же приём применён в официальном SDK.

```go
type Update struct {
    UpdateType string    `json:"update_type"`
    Timestamp  int64     `json:"timestamp"`
    Message    *Message  `json:"message,omitempty"`
    Callback   *Callback `json:"callback,omitempty"`
    User       *User     `json:"user,omitempty"`
    ChatID     int64     `json:"chat_id,omitempty"`
    Payload    string    `json:"payload,omitempty"`
    UserLocale string    `json:"user_locale,omitempty"`
}
```

Вся нестройность union-типа прячется в четырёх хелперах:

```go
// Target нормализует адресата ответа: берёт из message.recipient,
// иначе из полей верхнего уровня.
func (u Update) Target() Target

// Sender возвращает автора события: message.sender или update.user.
func (u Update) Sender() *User

// Text возвращает текст сообщения либо "" — без паник на nil.
func (u Update) Text() string

// Command разбирает текст вида "/help arg" → ("help", "arg", true).
// Суффикс с id бота, который MAX добавляет в групповых чатах
// ("/help:id-773"), отбрасывается. Не команда → ok == false.
func (u Update) Command() (name, args string, ok bool)
```

```go
type Target struct {
    ChatID int64
    UserID int64
}
```

Константы типов событий: `UpdateMessageCreated`, `UpdateMessageCallback`,
`UpdateBotStarted` (остальные не нужны, но подписка их и не запрашивает).

### 5.2 Прочие структуры

`User`, `Recipient`, `Message`, `MessageBody`, `Callback`, `NewMessageBody`,
`CallbackAnswer`, `SendMessageResult`, `SimpleQueryResult`, `BotInfo`,
`BotCommand`, `Attachment` (только `inline_keyboard`), `Keyboard`, `Button`,
`SubscriptionRequestBody`, `Error`.

Клавиатура передаётся как вложение:

```go
type Attachment struct {
    Type    string `json:"type"`               // "inline_keyboard"
    Payload any    `json:"payload"`
}

type KeyboardPayload struct {
    Buttons [][]Button `json:"buttons"`
}

type Button struct {
    Type    string `json:"type"`               // "callback" | "link"
    Text    string `json:"text"`
    Payload string `json:"payload,omitempty"`  // для callback
    URL     string `json:"url,omitempty"`      // для link
    Intent  string `json:"intent,omitempty"`   // positive | negative | default
}
```

### 5.3 Клиент

```go
type Client struct {
    baseURL string
    token   string
    hc      *http.Client
}

func New(token string, opts ...Option) *Client
func WithBaseURL(u string) Option    // подмена в тестах
func WithHTTPClient(c *http.Client) Option

func (c *Client) do(ctx context.Context, method, path string,
    q url.Values, in, out any) error
```

`do` собирает URL, ставит `Authorization` и `Content-Type: application/json`,
кодирует `in` (если не nil), декодирует ответ в `out` (если не nil).
Не-2xx: тело парсится в `Error{error, code, message}` и оборачивается:

```go
type APIError struct {
    Status  int
    Code    string
    Message string
}

func (e *APIError) Error() string
```

Так вызывающий может отличить 401 (плохой токен) от 429 (лимит). Ретраев нет —
для демо достаточно логирования; отмечено в README как точка роста.

### 5.4 Методы

```go
// bots.go
func (c *Client) GetMyInfo(ctx context.Context) (BotInfo, error)
func (c *Client) SetCommands(ctx context.Context, cmds []BotCommand) error

// messages.go
func (c *Client) SendMessage(ctx context.Context, to Target, body NewMessageBody) (Message, error)
func (c *Client) AnswerOnCallback(ctx context.Context, callbackID string, a CallbackAnswer) error

// subscriptions.go
func (c *Client) Subscribe(ctx context.Context, url, secret string, updateTypes []string) error
func (c *Client) Unsubscribe(ctx context.Context, url string) error
```

`SendMessage` кладёт в query ровно один из `chat_id` / `user_id`: `chat_id`, если
он не нулевой, иначе `user_id`. Если оба нулевые — возвращает ошибку без запроса.

## 6. Пакет `internal/bot`

Логика бота не знает про HTTP. Зависимость объявлена узким интерфейсом на стороне
потребителя:

```go
type Sender interface {
    SendMessage(ctx context.Context, to maxapi.Target, body maxapi.NewMessageBody) (maxapi.Message, error)
    AnswerOnCallback(ctx context.Context, callbackID string, a maxapi.CallbackAnswer) error
}

type Bot struct {
    api Sender
    log *slog.Logger
}

func New(api Sender, log *slog.Logger) *Bot
func (b *Bot) Handle(ctx context.Context, u maxapi.Update)
```

`Handle` не возвращает ошибку: webhook уже ответил 200, и единственная разумная
реакция на сбой — залогировать. Ошибки логируются с `update_type` и целью.

Поведение:

| Событие | Ответ |
|---|---|
| `bot_started` | приветствие по имени + inline-клавиатура |
| `message_created`, `/start` | то же |
| `message_created`, `/help` | список команд текстом |
| `message_created`, `/buttons` | сообщение с клавиатурой |
| `message_created`, прочий текст | эхо: `Вы написали: <текст>` |
| `message_created`, пустой текст | ничего (сообщение без текста — вложение) |
| `message_callback` | `AnswerOnCallback` с изменённым текстом `Вы нажали: <подпись кнопки>` |
| прочие типы | лог на уровне debug, ответа нет |

Клавиатура (`keyboard.go`) — маленький билдер, чтобы не собирать вложенные
литералы в коде обработчиков:

```go
type Keyboard struct{ rows [][]maxapi.Button }

func NewKeyboard() *Keyboard
func (k *Keyboard) Row(bs ...maxapi.Button) *Keyboard
func (k *Keyboard) Attachment() maxapi.Attachment

func CallbackButton(text, payload string) maxapi.Button
func LinkButton(text, url string) maxapi.Button
```

Демо-клавиатура: строка из двух callback-кнопок («Привет», «Пока») и строка с
link-кнопкой на документацию.

Подписи и payload'ы кнопок лежат в одной таблице `callbackButtons`: из неё
собирается клавиатура, и по ней же payload разворачивается обратно в подпись.
Так нужно, потому что в событии `message_callback` MAX присылает **только**
payload — подпись остаётся на стороне клиента. Без таблицы бот отвечал бы на
«Пока» текстом «Вы нажали: bye». Неизвестный payload (кнопка из сообщения,
отправленного прошлой версией бота) выводится как есть.

## 7. Пакет `internal/webhook`

```go
type Handler func(ctx context.Context, u maxapi.Update)

type Server struct { /* secret, handler, log, sem chan struct{}, wg sync.WaitGroup */ }

func New(secret string, h Handler, log *slog.Logger) *Server
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (s *Server) Wait()   // дождаться обработчиков при shutdown
```

Порядок в `ServeHTTP`:

1. Метод не `POST` → 405.
2. Если `secret` задан и `X-Max-Bot-Api-Secret` не совпадает
   (сравнение через `subtle.ConstantTimeCompare`) → 401.
3. Тело не разбирается в `Update` → 400.
4. Иначе `200 OK` **сразу**, обработка — в горутине.

Ответ отдаётся до обработки намеренно: MAX ждёт быстрый 200, иначе повторит
доставку. Число одновременных обработчиков ограничено буферизованным каналом на
32; при переполнении горутина ждёт слот. `sync.WaitGroup` позволяет дождаться
незавершённых обработчиков при остановке. Контекст обработчика — свой,
`context.WithTimeout(30s)`, а не контекст запроса: тот отменяется сразу после
ответа.

Дедупликации нет. Бот без состояния, повторная доставка максимум продублирует
эхо. Записано в README как известное ограничение.

## 8. `cmd/bot`

Конфигурация из переменных окружения:

| Переменная | Обяз. | По умолчанию | Назначение |
|---|---|---|---|
| `MAX_BOT_TOKEN` | да | — | токен бота; принимается и как `BOT_TOKEN` |
| `WEBHOOK_URL` | да | — | публичный `https://…/webhook` |
| `WEBHOOK_SECRET` | нет | — | `^[a-zA-Z0-9_-]{5,256}$` |
| `LISTEN_ADDR` | нет | `:8080` | адрес локального сервера |
| `MAX_API_CA_FILE` | нет | — | корневой сертификат для TLS (см. §8.1) |
| `MAX_API_BASE_URL` | нет | `https://platform-api2.max.ru` | подмена в тестах |
| `UNSUBSCRIBE_ON_EXIT` | нет | не задана | `1` — отписаться от webhook при остановке |

Отсутствие обязательной переменной или `WEBHOOK_URL` не на `https://` —
завершение с понятным сообщением, без запросов к API. Путь webhook-сервера
берётся из `WEBHOOK_URL`, чтобы локальный маршрут совпадал с тем, что увидит Max.

### 8.1 Доверие к сертификату Max

`*.max.ru` подписан цепочкой Минцифры («Russian Trusted Root CA»), которой нет
в системном наборе macOS и большинства Linux-образов. Без неё любой запрос к API
падает с `x509: certificate signed by unknown authority`.

`maxapi.NewWithRootCA(token, caFile, opts...)` добавляет корень из PEM-файла к
копии системного пула и подкладывает результат в `tls.Config.RootCAs` своего
`http.Client`. Системное хранилище не меняется — доверие ограничено этой
программой. Если `MAX_API_CA_FILE` не задан, используется обычный `maxapi.New`.

Последовательность запуска:

1. `GET /me` — проверка токена, лог `first_name` и `username`.
2. `PATCH /me/commands` — публикация `start`, `help`, `buttons`.
3. Синхронизация подписки: `GET /subscriptions`, затем
   `DELETE /subscriptions?url=` для каждого чужого адреса и
   `POST /subscriptions` — если своего адреса среди подписок ещё нет.
   Типы событий: `message_created`, `message_callback`, `bot_started`.
4. `ListenAndServe` на `LISTEN_ADDR`, маршрут `POST /webhook`.
5. `SIGINT`/`SIGTERM`: `http.Server.Shutdown` с таймаутом 10 с, затем
   `Server.Wait()`. Отписка вызывается, только если `UNSUBSCRIBE_ON_EXIT=1` —
   иначе следующий запуск с тем же адресом просто увидит подписку на шаге 3.

Шаг 3 устроен именно так, потому что `POST /subscriptions` **не заменяет**
подписку, а добавляет ещё одну. Без уборки после нескольких запусков с разными
адресами (например, с новым туннелем) Max рассылает каждое событие на все
подписки сразу, и бот отвечает по разу на каждую живую.

Логирование — `log/slog`, текстовый handler, уровень `info`.

## 9. Тесты

Только stdlib: `testing` + `net/http/httptest`.

**`maxapi/models_test.go`** — разбор JSON-примеров реальных событий
(`message_created` в диалоге, `message_callback`, `bot_started`):
`Target()` даёт верные `ChatID`/`UserID`, `Text()` не паникует на сообщении без
тела, `Command()` разбирает `/help`, `/help arg`, `/help:id-773`, а на обычном
тексте и на строке `не /команда` возвращает `ok == false`.

**`cmd/bot/register_test.go`** — заглушка API, ведущая список подписок так же,
как настоящий сервер (`POST` добавляет, а не заменяет): порядок вызовов при
чистом старте, снятие чужих подписок, отсутствие повторной подписки на свой
адрес, остановка при неверном токене и при отказе в подписке.

**`cmd/bot/config_test.go`** — значения по умолчанию, переопределения,
`BOT_TOKEN` как запасное имя токена, путь webhook из `WEBHOOK_URL`, отказ при
пустом токене, HTTP-адресе, адресе без пути и неверном формате секрета.

**`maxapi/client_test.go`** — заглушка API на `httptest.NewServer`:
- `SendMessage` шлёт `POST /messages?chat_id=…`, заголовок `Authorization`
  равен токену, тело — ожидаемый JSON;
- при заданном только `UserID` в query уходит `user_id`;
- при нулевом `Target` запрос не отправляется, возвращается ошибка;
- `AnswerOnCallback` шлёт `POST /answers?callback_id=…`;
- `SetCommands` шлёт `PATCH /me/commands`;
- `Subscribe` шлёт `POST /subscriptions` с url, secret и update_types;
- ответ 400 с телом `{"code":"...","message":"..."}` превращается в `*APIError`
  с этими полями и `Status == 400`.

**`bot/bot_test.go`** — таблица «входящий Update → ожидаемые вызовы» на
фейковом `Sender`, записывающем аргументы. Покрывает все строки таблицы из
раздела 6, включая «пустой текст → вызовов нет».

**`webhook/server_test.go`**:
- `GET` → 405;
- secret задан, заголовка нет → 401;
- secret задан, значение неверное → 401;
- битый JSON → 400, handler не вызван;
- валидный запрос → 200 и handler получает разобранный `Update`
  (синхронизация через канал в тесте).

## 10. Запуск и проверка

```sh
brew install cloudflared
cloudflared tunnel --protocol http2 --url http://localhost:8080

MAX_BOT_TOKEN=… \
WEBHOOK_URL=https://xxxx.trycloudflare.com/webhook \
WEBHOOK_SECRET=dev-secret-123 \
MAX_API_CA_FILE=certs/russian_trusted_root_ca.cer \
go run ./cmd/bot
```

`--protocol http2` обязателен, если исходящий UDP закрыт: по умолчанию
`cloudflared` ходит через QUIC, и при закрытом UDP туннель молча не поднимается —
адрес выдаётся, но не отвечает.

Проверка без туннеля — POST фейкового `Update` на `http://localhost:8080/webhook`
с корректным заголовком секрета; бот попытается отправить ответ в реальный API.

## 11. Что подтвердилось на живом боте

Проверено на боте `@id352831874756_bot` 2026-08-06:

- **Имя команды — без ведущего слэша.** `GET /me` после `PATCH /me/commands`
  вернул `start`, `help`, `buttons` — открытый вопрос закрыт.
- **`trycloudflare.com` проходит валидацию Max.** `POST /subscriptions` принял
  адрес туннеля; риск отказа из-за TLS не подтвердился.
- **`POST /subscriptions` не заменяет подписку, а добавляет.** Обнаружено
  вживую: после двух запусков с разными адресами в `GET /subscriptions` оказалось
  две записи. Исправлено синхронизацией подписок (§8, шаг 3).
- **Max не проверяет доступность webhook при подписке.** Подписка была принята
  на адрес туннеля, который ни разу не подключился к Cloudflare.
- **TLS требует корня Минцифры** (§8.1) — в исходном дизайне не было учтено.
- **`message.recipient.user_id` в диалоге — это сам бот**, а не собеседник:
  в логе он совпал с `user_id` из `GET /me`. Отвечать нужно по `chat_id`;
  `SendMessage` так и делает, поэтому проблема не проявилась.
- **В `message_callback` нет подписи кнопки** — только `payload`. Первая версия
  печатала его напрямую, и на «Пока» бот отвечал «Вы нажали: bye». Тест это
  пропустил, потому что проверял ровно то поведение, которое было написано.
  Исправлено таблицей `callbackButtons` (§6).
- **Сквозной сценарий пройден:** `/start`, произвольный текст и обе
  callback-кнопки — все события дошли и получили ответ.

## 12. Оставшиеся риски

- **Токен бота не должен попадать в репозиторий.** Передаётся только через
  окружение; в `.gitignore` добавлен `.env`.
- **Быстрый туннель не для продакшена.** Адрес `trycloudflare.com` меняется при
  каждом перезапуске, гарантий доступности нет. Для боевого бота нужен свой хост
  с валидным TLS.
- **Фолбэка на long polling нет** — решение принято сознательно. Если webhook
  недоступен, бот не получит событий вообще.
