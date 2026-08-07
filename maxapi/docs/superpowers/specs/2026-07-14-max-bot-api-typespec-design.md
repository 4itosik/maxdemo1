# TypeSpec-описание Max Bot API — дизайн

Дата: 2026-07-14
Статус: утверждён

## Цель

Рукописное TypeSpec-описание всего Max Bot API (https://dev.max.ru/docs-api), из которого
компилируется эталонная OpenAPI 3 спецификация. Официальная OpenAPI-схема
(репозиторий `max-messenger-bot/max-bot-api-schemas`, снимок `schema_2026_07_10.json`,
API v0.0.32) используется **только как справочник и эталон для сверки** — TypeSpec пишем
вручную, это одновременно учебное освоение TypeSpec на реальном API.

## Объём

Весь API целиком: 28 операций в 5 группах (bots, chats, messages, subscriptions, upload),
~128 моделей, включая 6 дискриминированных union-типов:

- `Attachment` (по `type`): image, video, audio, file, sticker, contact, inline_keyboard, share, location
- `AttachmentRequest` (по `type`): те же 9 видов — то, что отправляет бот
- `Button` (по `type`): callback, link, request_geo_location, request_contact, open_app, message, clipboard
- `ReplyButton` (по `type`): message, user_geo_location, user_contact
- `MarkupElement` (по `type`): strong, emphasized, monospaced, link, strikethrough, underline, user_mention, heading, highlighted, quote
- `Update` (по `update_type`): 15 типов webhook-событий

## Структура проекта

```
maxapi/
├── package.json          # @typespec/compiler, @typespec/http, @typespec/openapi3
├── tspconfig.yaml        # эмиттер openapi3 → tsp-output/openapi.yaml
├── main.tsp              # @service "Max Bot API", сервер https://platform-api2.max.ru, auth
├── common.tsp            # Error, SimpleQueryResult, Image, обёртки ответов (401/403/404/405/500)
├── models/
│   ├── users.tsp         # User, UserWithPhoto, BotInfo, BotCommand, BotPatch
│   ├── chats.tsp         # Chat, ChatType, ChatStatus, ChatMember, ChatAdmin, списки
│   ├── messages.tsp      # Message, MessageBody, NewMessageBody, LinkedMessage и др.
│   ├── attachments.tsp   # union Attachment + payload-модели
│   ├── attachment-requests.tsp  # union AttachmentRequest
│   ├── keyboard.tsp      # Keyboard, union Button, union ReplyButton
│   ├── markup.tsp        # union MarkupElement
│   └── updates.tsp       # union Update — 15 типов событий
├── routes/
│   ├── bots.tsp          # GET /me (getMyInfo)
│   ├── chats.tsp         # 16 операций над чатами и участниками
│   ├── messages.tsp      # 7 операций: /messages, /messages/{id}, /videos/{token}, /answers
│   ├── subscriptions.tsp # /subscriptions (webhooks) + GET /updates (long polling)
│   └── uploads.tsp       # POST /uploads (getUploadUrl)
└── scripts/
    └── compare.py        # семантическая сверка сгенерированного OpenAPI с официальным
```

## Ключевые решения

1. **Union-типы через `@discriminator`**: базовая модель + наследники через `extends`;
   эмиттер генерирует `discriminator.mapping` в OpenAPI, как в оригинале.
2. **Аутентификация — обе схемы**: `ApiKeyAuth` в query-параметре `access_token`
   (как в официальной схеме; помечаем устаревшей) и `BearerAuth` через заголовок
   `Authorization` (актуальная рекомендация документации).
3. **Роуты через `interface`** с `@tag` по группам. Имена операций в интерфейсах
   берём совпадающими с официальными `operationId` (getMyInfo, sendMessage,
   getUpdates и т.д.), чтобы эмиттер выдал их без дополнительных декораторов;
   `@operationId` используем только там, где эмиттер добавляет префикс интерфейса.
4. **`@doc` на русском языке** — формулировки переносим из документации dev.max.ru.
5. **Все ограничения переносятся полностью**. В официальной схеме 114 объявленных
   ограничений: длины строк (`maxLength`/`minLength`), форматы целых (`int64` для
   идентификаторов/таймстемпов/маркеров, `int32` для счётчиков и позиций разметки,
   `double` для координат), размеры массивов (`maxItems`/`minItems`), диапазоны
   query-параметров (`minimum`/`maximum`) и паттерны. В TypeSpec выражаем их
   декораторами `@maxLength`/`@minLength`, `@minValue`/`@maxValue`,
   `@maxItems`/`@minItems`, `@pattern` и типами `int32`/`int64`/`float64`.
   Три известные неаккуратности оригинала исправляем осознанно и документируем
   в README: строковый `maxItems: "100"` на элементах `user_ids` (переносим как
   `@maxItems(100)` на массив), `minLength: 1` на массивах кнопок (переносим как
   `@minItems(1)`), `pattern` на integer path-параметрах `chatId` (опускаем).
6. **Критерий готовности — семантическая сверка**: `scripts/compare.py` нормализует
   обе спеки (разворачивает `$ref`) и сравнивает пути, методы, параметры,
   обязательность и типы полей, значения enum. Имена схем и композиция могут
   отличаться. Готово, когда список расхождений пуст либо содержит только
   осознанные отличия, задокументированные в README.

## Порядок реализации

1. Каркас проекта (package.json, tspconfig, main.tsp) — компилируется пустым.
2. common.tsp + models/users.tsp.
3. models/attachments.tsp, keyboard.tsp, markup.tsp, attachment-requests.tsp (самый сложный блок).
4. models/messages.tsp, chats.tsp, updates.tsp.
5. routes/* — все 28 операций.
6. scripts/compare.py, сверка, доводка до нулевых расхождений.

После каждого шага — `tsp compile .` (сборка обязана проходить).

## Вне объёма (YAGNI)

- Генерация клиентов/SDK.
- Версионирование через `@typespec/versioning`.
- Кастомные эмиттеры.
