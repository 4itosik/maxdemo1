# maxmoc — план реализации (13 задач)

**Цель:** Go-сервис `max-mock` — stateful-эмулятор Bot API мессенджера Max для
закрытого контура: принимает запросы КЦ-платформы, доставляет webhook-события,
даёт тестировщику веб-чат и админку ботов.

**Спека:** `docs/superpowers/specs/2026-08-05-max-mock-design.md` (maxapi).
**Репозиторий:** `/Users/aay/endeavors/ailearn/maxmoc`, Go-модуль `maxmock`.
**Версия контракта:** 0.0.32.

## Global Constraints

- Go ≥ 1.22, без cgo. Runtime-зависимости: `modernc.org/sqlite`,
  `github.com/getkin/kin-openapi`, `gopkg.in/yaml.v3`, `github.com/coder/websocket`.
  Кодогенерации нет — wire-структуры пишутся руками (см. §12 спеки).
- Все ответы Bot API — `application/json`; тело ошибки всегда
  `{"code": "...", "message": "..."}` (схема `Error`, поле `error` не заполняем).
- Коды недокументированных ситуаций — с префиксом `mock.`: `mock.validation`,
  `mock.unauthorized`, `mock.not_implemented`, `mock.chat.not_found`,
  `mock.callback.not_found`, `mock.message.not_found`, `mock.internal`.
- Авторизация — обе схемы контракта: `Authorization: Bearer <token>` и
  `access_token` в query; приоритет у заголовка. Отсутствие или неверный токен
  → 401 `mock.unauthorized`.
- Все таймстемпы в wire-объектах — unix-миллисекунды.
- Каждая задача: тесты → реализация → `go build ./... && go test ./...` зелёные
  → `git commit`.
- Динамические поля (timestamp, id) в тестах сравниваются по форме, не по значению.

## Структура

```
maxmoc/
├── api/{openapi.MaxBotApi.yaml,openapi.MaxBotWebhook.yaml,embed.go}
├── cmd/max-mock/main.go
├── internal/
│   ├── config/      yaml + env
│   ├── ids/         chat_id, user_id, mid, токены
│   ├── wire/        структуры Bot API и Update (руками)
│   ├── specs/       kin-openapi: валидация request/response/webhook
│   ├── store/       SQLite: схема + DAO
│   ├── events/      шина событий
│   ├── webhook/     диспетчер доставки
│   ├── core/        доменная логика
│   ├── maxfacade/   HTTP-фасад Bot API
│   ├── controlapi/  /mock/api/*, /mock/ws, /mock/upload, /mock/files
│   └── httpserver/  сборка роутеров + embed web
├── web/             index.html (админка), chat.html, app.js, style.css
├── e2e/             тесты полного цикла
├── scripts/smoke.sh
└── config.example.yaml, Makefile, README.md
```

## Справочник контракта (сверено с yaml)

| operationId | Метод/путь | Параметры | Тело → 200 |
|---|---|---|---|
| `getMyInfo` | GET /me | — | → `BotInfo` |
| `editMyCommands` | PATCH /me/commands | — | `BotCommandsPatch` → `BotCommandsInfo` |
| `getMessages` | GET /messages | `chat_id`,`message_ids`,`from`,`to`,`count` (опц.) | → `MessageList` |
| `sendMessage` | POST /messages | `user_id`,`chat_id`,`disable_link_preview` (опц.) | `NewMessageBody` → `SendMessageResult` |
| `editMessage` | PUT /messages | `message_id` (обяз.) | `NewMessageBody` → `SimpleQueryResult` |
| `deleteMessage` | DELETE /messages | `message_id` (обяз.) | → `SimpleQueryResult` |
| `getMessageById` | GET /messages/{messageId} | path | → `Message` |
| `answerOnCallback` | POST /answers | `callback_id` (обяз.) | `CallbackAnswer` → `SimpleQueryResult` |
| `getSubscriptions` | GET /subscriptions | — | → `GetSubscriptionsResult` |
| `subscribe` | POST /subscriptions | — | `SubscriptionRequestBody` → `SimpleQueryResult` |
| `unsubscribe` | DELETE /subscriptions | `url` (обяз.) | → `SimpleQueryResult` |
| `getUploadUrl` | POST /uploads | `type` (обяз.: image/video/audio/file) | → `UploadEndpoint` |

404 `mock.not_implemented`: `GET /updates`, `GET /videos/{videoToken}`,
`/chats*` и любой неизвестный путь.

Формы схем (`!` = required, `?` = nullable):

- `Error`: `{error?, code!, message!}`
- `NewMessageBody`: `{text!?, attachments!?, link!?, notify?, format?}` —
  text/attachments/link **required, но nullable**
- `SendMessageResult{message!}`; `SimpleQueryResult{success!, message?}`
- `Message{sender?, recipient!, timestamp!, link?, body!, stat?, url?}`
- `MessageBody{mid!, seq!, text!?, attachments?, markup?}`
- `Recipient{chat_id!?, chat_type!, user_id!?}`; `chat_type` = `"dialog"`
- `User{user_id!, first_name!, last_name?, username!?, is_bot!, last_activity_time!, name!?}`
- `BotInfo` = `User` + `{description?, avatar_url?, full_avatar_url?, commands?}`
- `Subscription{url!, time!, update_types!?}`;
  `SubscriptionRequestBody{url!, update_types?, secret?}` (secret `^[a-zA-Z0-9_-]{5,256}$`)
- `UploadEndpoint{url!, token?}`; `CallbackAnswer{message?}`
- `Callback{timestamp!, callback_id!, payload?, user!}`
- `InlineKeyboardAttachmentRequest{type:"inline_keyboard", payload{buttons!: Button[][]}}`
  → в `Message` превращается в `InlineKeyboardAttachment{type, payload: Keyboard{buttons}}`

Исходящие Update (все с `update_type` и `timestamp` в ms):

- `message_created{message!, user_locale?}`
- `message_callback{callback!, message!?, user_locale?}`
- `message_edited{message!}`
- `message_removed{message_id!, chat_id!, user_id!}`
- `bot_started{chat_id!, user!, payload?, user_locale?}`

При заданном `secret` каждый webhook-POST несёт `X-Max-Bot-Api-Secret`.

---

## Task 1 — Каркас

**Files:** `go.mod`, `.gitignore`, `internal/config/{config.go,config_test.go}`,
`config.example.yaml`, `Makefile`, `cmd/max-mock/main.go`

`config.Config{Listen, PublicBaseURL, DBPath, BlobDir string; LogRetentionDays int;
ValidateResponses bool; Webhook{TimeoutSec, Retries int; BackoffSec []int}}`.
`Load("")` → дефолты (`:8080`, `http://localhost:8080`, `max-mock.db`, `blobs`,
14, true, 10/2/[1,5]). Env: `MAXMOCK_LISTEN`, `MAXMOCK_PUBLIC_BASE_URL`,
`MAXMOCK_DB_PATH`, `MAXMOCK_BLOB_DIR`.

**Тесты:** дефолты; yaml-файл + env-переопределение поверх него.
**Приёмка:** `./max-mock` поднимается, `GET /healthz` → `ok`.

## Task 2 — Спеки и валидация

**Files:** `api/embed.go`, `internal/specs/{specs.go,specs_test.go}`

`specs.Load() (*Specs, error)` грузит оба документа из `embed.FS`.
Методы:
- `FindRoute(*http.Request) (*routers.Route, map[string]string, error)`
- `ValidateRequest(ctx, *http.Request) error` — параметры + тело против
  `openapi.MaxBotApi.yaml`; `AuthenticationFunc` всегда `nil` (авторизацию
  делаем сами). Тело восстанавливается для последующего чтения.
- `ValidateResponse(ctx, route, status int, body []byte) error`
- `ValidateWebhookBody(ctx, body []byte) error` — против `openapi.MaxBotWebhook.yaml`

**Тесты:** валидный `POST /messages` проходит; тело без `text` — нет; валидный
`message_removed` проходит webhook-валидацию, `timestamp:"строка"` — нет;
`ValidateResponse` ловит `Message` без `recipient`.

## Task 3 — ids и wire-структуры

**Files:** `internal/ids/{ids.go,ids_test.go}`, `internal/wire/{wire.go,wire_test.go}`

`ids`: `NewChatID()`, `NewUserID()` (положительные int64), `Mid(chatID, seq)` →
`mid.<16hex><8hex>` (детерминирован), `NewToken(prefix)` → `<prefix>.<32hex>`,
`NewCallbackID()`.

`wire`: `User`, `Recipient`, `MessageBody`, `Message`, `BotInfo`,
`SimpleQueryResult`, `SendMessageResult`, `MessageList`, `Subscription`,
`GetSubscriptionsResult`, `UploadEndpoint`, `Callback`, `Error`,
`NewMessageBody` (с `json.RawMessage` для attachments/link и флагами
присутствия), `Attachment`, `Button`, `Keyboard`, и пять Update-типов.
Правило: `required+nullable` поля сериализуются **без** `omitempty` (`text: null`,
а не отсутствие ключа); `required` необнуляемые — тоже без `omitempty`.

**Тесты:** маршалинг `Message` содержит `"text":null` при пустом тексте и не
теряет `recipient`; `NewMessageBody` различает «поле отсутствует» и «поле null»;
маршалинг каждого Update-типа проходит `specs.ValidateWebhookBody`.

## Task 4 — Хранилище: схема, боты, клиенты, диалоги

**Files:** `internal/store/{store.go,schema.sql,bots.go,dialogs.go,store_test.go}`

`store.Open(path)` — `modernc.org/sqlite`, `PRAGMA journal_mode=WAL`,
`foreign_keys=ON`, применение схемы, `ErrNotFound`.
Таблицы (все с `created_at` ms): `bots`, `clients`, `dialogs`, `messages`,
`subscriptions`, `attachments`, `request_log`, `webhook_deliveries`, `callbacks`.

DAO: `CreateBot(name, username) (*Bot, error)`, `ListBots`, `BotByToken`,
`BotByID`; `CreateClient(botID, name)` (создаёт клиента и диалог),
`ListClients(botID)`, `ClientByID`; `DialogByChatID`, `DialogByUserID(botID,userID)`,
`MarkDialogStarted`.

**Тесты:** повторное `Open` того же файла видит записи; `BotByToken` находит;
создание клиента заводит диалог с положительным `chat_id`.

## Task 5 — Хранилище: сообщения, подписки, журналы

**Files:** `internal/store/{messages.go,subscriptions.go,logs.go,*_test.go}`

`AppendMessage(msg)` — присваивает `seq` (монотонно в рамках чата) и `mid`;
`MessageByMid`, `ListMessages(filter{ChatID, MessageIDs, From, To, Count})`,
`UpdateMessageBody(mid, body)`, `MarkMessageRemoved(mid)`.
`SaveCallback(cb)`, `CallbackByID`, `MarkCallbackAnswered`.
`SaveAttachment(att)`, `AttachmentByToken`.
`AddSubscription(botID, url, updateTypes, secret)` (upsert по url+bot),
`ListSubscriptions(botID)`, `DeleteSubscription(botID, url)`.
`LogRequest(entry)`, `LogDelivery(entry)`, `ListRequestLog(botID, limit)`,
`ListDeliveries(botID, limit)`, `Purge(olderThan)`.

**Тесты:** `seq` растёт в пределах чата и независим между чатами; фильтры
`ListMessages` (по chat_id, по message_ids, по count); удалённое сообщение не
попадает в список, но `MessageByMid` возвращает его со статусом `removed`;
`Purge` чистит старое и не трогает свежее.

## Task 6 — Шина событий

**Files:** `internal/events/{bus.go,bus_test.go}`

`bus.Publish(ev Event)`; `bus.Subscribe(botID) (<-chan Event, cancel func())`.
`Event{Kind string, BotID int64, ChatID int64, Payload any}` — виды:
`message`, `message_edited`, `message_removed`, `dialog`, `delivery`, `request`.
Неблокирующая доставка: медленный подписчик получает drop, а не тормозит систему.

**Тесты:** подписчик получает опубликованное событие своего бота и не получает
чужого; `cancel` закрывает канал; publish без подписчиков не блокирует.

## Task 7 — Диспетчер webhook-ов

**Files:** `internal/webhook/{dispatcher.go,dispatcher_test.go}`

`New(store, specs, cfg, bus) *Dispatcher`; `d.Deliver(botID, chatID int64, update any)`.
Порядок: по одной последовательной очереди на `chatID`, разные чаты параллельно.
Для каждой подписки бота: фильтр `update_types`; сериализация; **валидация
против webhook-спека** (не прошло — доставку не делаем, пишем отказ в журнал);
POST с `Content-Type: application/json` и `X-Max-Bot-Api-Secret` при наличии
секрета; таймаут; ретраи при 5xx/сетевой ошибке с бэкоффом; каждая попытка →
`webhook_deliveries` + событие `delivery` в шину.

**Тесты (httptest-стенд):** доставка приходит с корректным телом и заголовком
секрета; фильтр `update_types` отсекает; 500 у стенда вызывает ретрай, затем
успех; невалидный Update не уходит вовсе; события одного чата приходят по
порядку.

## Task 8 — Доменная логика

**Files:** `internal/core/{core.go,messages.go,client.go,*_test.go}`

Операции бота (вызываются фасадом):
`SendMessage(bot, userID/chatID, NewMessageBody) (*wire.Message, error)` —
создаёт сообщение отправителя-бота, регистрирует callback-токены кнопок,
публикует в шину; `EditMessage(bot, mid, body)`; `DeleteMessage(bot, mid)`;
`AnswerCallback(bot, callbackID, CallbackAnswer)` — при наличии `message`
редактирует исходное сообщение; `GetMessages`, `GetMessageByID`.

Операции клиента (вызываются control-API):
`ClientStart(chatID)` → `bot_started`; `ClientSendText(chatID, text)` и
`ClientSendAttachment(chatID, token)` → `message_created`;
`ClientPressButton(chatID, mid, payload)` → создаёт `callback` и шлёт
`message_callback`; `ClientEdit(chatID, mid, text)` → `message_edited`;
`ClientDelete(chatID, mid)` → `message_removed`.

Ошибки домена: `ErrChatNotFound`, `ErrMessageNotFound`, `ErrCallbackNotFound`,
`ErrForbidden` (бот правит/удаляет чужое сообщение в диалоге).

**Тесты:** отправка боту неизвестного `user_id` → `ErrChatNotFound`; кнопки
сообщения регистрируются как callback-и; `AnswerCallback` с `message` меняет
тело исходного сообщения и порождает `message_edited`; бот не может удалить
сообщение клиента; действия клиента порождают ровно один Update нужного типа.

## Task 9 — Фасад: middleware и ошибки

**Files:** `internal/maxfacade/{errors.go,middleware.go,middleware_test.go}`

`writeError(w, status, code, message)` — тело `wire.Error`.
Цепочка: recover → чтение и буферизация тела → `specs.FindRoute`
(не нашли → 404 `mock.not_implemented`) → авторизация (`Bearer`, иначе 401
`mock.unauthorized`; `access_token` в query тоже 401) → `specs.ValidateRequest`
(не прошло → 400 `mock.validation`) → handler → при `ValidateResponses`
валидация ответа (не прошло → 500 `mock.internal` + запись в журнал) →
`request_log` + событие `request` в шину.

**Тесты:** запрос без заголовка → 401 с телом `Error`; `?access_token=…` → 401;
битое тело → 400 `mock.validation`; неизвестный путь → 404
`mock.not_implemented`; успешный запрос попадает в `request_log`.

## Task 10 — Фасад: операции Bot API

**Files:** `internal/maxfacade/{server.go,server_test.go}`

Диспетчеризация по `operationId` контракта для 12 операций; всё остальное —
404 `mock.not_implemented`. `POST /uploads` выдаёт
`{url: "{public_base_url}/mock/upload/{token}", token}`.

**Тесты (httptest поверх всего стека):** `GET /me` отдаёт бота с токеном
запроса; `POST /messages?user_id=…` создаёт сообщение, отдаёт
`SendMessageResult`; `PUT`/`DELETE /messages` работают; `GET /messages` фильтрует;
`GET /messages/{mid}` → 404 `mock.message.not_found` на неизвестном id;
подписки создаются/читаются/удаляются; `PATCH /me/commands` сохраняет команды
и они видны в `GET /me`; `GET /updates` → 404 `mock.not_implemented`.

## Task 11 — Control-API, upload, WebSocket

**Files:** `internal/controlapi/{api.go,upload.go,ws.go,*_test.go}`

REST `/mock/api/`: `GET,POST /bots`, `GET /bots/{id}`,
`GET /bots/{id}/subscriptions`, `GET,POST /bots/{id}/clients`,
`GET /bots/{id}/log`, `GET /bots/{id}/deliveries`,
`GET /dialogs/{chatId}/messages`, `POST /dialogs/{chatId}/actions`
(`{action: start|send|press|edit|delete, ...}`).

`POST /mock/upload/{token}` — multipart, сохраняет blob, отвечает
`{"photos":{"…":{"token":…}}}` для image и `{"token":…}` иначе.
`GET /mock/files/{token}` — отдаёт blob.
`GET /mock/ws?bot_id=…` — стрим событий шины (`coder/websocket`).

**Тесты:** создание бота отдаёт токен; действие клиента `send` порождает
доставку на стенд; upload → полученный токен принимается в `POST /messages`;
WS отдаёт событие после действия.

## Task 12 — Веб-UI

**Files:** `web/{index.html,chat.html,app.js,style.css}`,
`internal/httpserver/{server.go,server_test.go}`

`httpserver.New(...)` собирает: Bot API в корне, `/mock/api/*`, `/mock/ws`,
`/mock/upload/*`, `/mock/files/*`, `/mock` → админка, `/mock/chat/{botId}` → чат,
`/healthz`. Порядок роутов: специфичные `/mock/...` раньше корневого фасада.

Админка: таблица ботов, форма создания, токен с кнопкой «копировать», подписки,
общий журнал. Чат: список клиентов + создание, лента сообщений с рендером
инлайн-кнопок (клик → `press`), поле ввода, загрузка файла, правка/удаление
своих сообщений, кнопка «Начать», панель журнала; живое обновление по WS.

**Тесты:** `GET /mock` отдаёт HTML 200; `GET /mock/chat/1` отдаёт HTML;
`GET /me` через тот же сервер по-прежнему работает (роуты не конфликтуют).

## Task 13 — e2e, smoke, README

**Files:** `e2e/e2e_test.go`, `scripts/smoke.sh`, `README.md`

E2E-сценарий на реальном сервере (`httptest.NewServer` поверх `httpserver`) +
фейковый стенд, принимающий webhook-и и **валидирующий каждое тело** против
webhook-спека:

1. создать бота через control-API, получить токен;
2. `POST /subscriptions` с секретом → 200;
3. создать клиента, нажать «Начать» → стенд получил `bot_started`;
4. клиент шлёт текст → `message_created`;
5. бот отвечает `POST /messages` с инлайн-клавиатурой → 200;
6. клиент жмёт кнопку → `message_callback` с `payload`;
7. `POST /answers` с `message` → исходное сообщение изменилось, пришёл
   `message_edited`;
8. `PUT /messages` и `DELETE /messages` → `message_edited`, `message_removed`;
9. `POST /uploads` → загрузка файла на выданный URL → отправка вложения
   `POST /messages` → 200, вложение видно в `GET /messages`;
10. `GET /me`, `GET /subscriptions`, `DELETE /subscriptions` → согласованы.

Все ответы мока валидируются против API-спека, все webhook-тела — против
webhook-спека. `scripts/smoke.sh` — тот же путь через `curl` для ручной проверки.
README: назначение, сборка, запуск, оба UI, конвенции `mock.*`, обновление спеков.
