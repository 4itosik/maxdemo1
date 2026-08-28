# Пример использования `maxapi/gen/oapi-codegen`

Тот же echo-бот, что в [`../example-quicktype`](../example-quicktype), но на структурах
oapi-codegen. Пара нужна, чтобы выбрать генератор по факту: одна задача, один
и тот же записанный с живого Max образец, одинаковые тесты.

```
example-oapi-codegen/
├── maxclient/   HTTP-клиент: подписки и отправка сообщений
├── webhook/     приём событий и разбор через ValueByDiscriminator()
└── cmd/echobot/ связка: подписался → слушает → отвечает
```

Отдельный Go-модуль с `replace stash.sigma.sbrf.ru/scpl/oapi/maxapi => ../gen/oapi-codegen`.

```bash
go test ./...                     # сети не касается
make example-oapi-codegen                 # из maxapi/: сгенерировать и проверить
go run ./cmd/echobot -api http://localhost:8099 -addr :9099   # против maxmoc
```

HTTP-слой написан руками — так же, как в паре на quicktype. oapi-codegen
клиент генерировать умеет (`client: true`, ещё 3344 строки и 20 методов), но
тогда сравнивать было бы нечего: у quicktype клиента нет вовсе. При
`models: true` сравниваются именно модели, ради которых генератор и берут.

## Сколько экономит генератор — измерено

```
часть                quicktype   oapi-codegen   разница
клиент                     155            155        +0
webhook-сервер              94             92        -2
echobot                    130            130        +0
ИТОГО                      379            377        -2
```

Цифры печатает `make compare-gen`. **Две строки из 379** — вот вся экономия
рукописного кода.

Файлы клиента различаются только именами полей (`sub.Url` против `sub.URL`) и
путём импорта; структурно они совпадают построчно. `cmd/echobot` совпадает
полностью. Различается один-единственный метод — `dispatch()` в
webhook-сервере, и различается не объёмом, а формой:

```go
// quicktype: тело разбирается дважды — сперва ради update_type,
// потом в конкретный вариант.
var flat maxapi.Update
json.Unmarshal(body, &flat)
switch string(flat.UpdateType) {
case "message_created":
    var update maxapi.MessageCreatedUpdate
    json.Unmarshal(body, &update)      // второй разбор того же тела
    return s.handler.MessageCreated(ctx, update)
...

// oapi-codegen: разбор один, дальше type switch.
var update maxapi.Update
json.Unmarshal(body, &update)
value, err := update.ValueByDiscriminator()
switch typed := value.(type) {
case maxapi.MessageCreatedUpdate:
    return s.handler.MessageCreated(ctx, typed)
...
```

Выигрыш реальный, но узкий: перечень из шестнадцати `case` со строковыми
литералами заменён на type switch, который проверяет компилятор. Добавление
семнадцатого типа события в контракт у quicktype требует правки строкового
литерала здесь, у oapi-codegen — только новой ветки switch, а забытая ветка
уйдёт в `Other`. Строк это не экономит.

**Вывод: по объёму рукописного кода генераторы неразличимы.** Выбирать надо
по другому — см. [`../README.md`](../README.md), раздел «Два варианта
кодогенерации».

## Мелочи, которые стоит знать заранее

- Необязательные массивы приезжают указателем на слайс: `*[]Attachment`.
  Читается неприятно — `len(*attachments)` после проверки на nil.
- Поля именуются по-Go-шному из JSON: `chat_id` → `ChatId` (не `ChatID`),
  `url` → `Url` (не `URL`). У quicktype — `ChatID`, `URL`, что идиоматичнее.
- `UpdateUnified` в `gen/oapi-codegen` отсутствует: генерация идёт из одного
  api-документа, а эта схема есть только в webhook-документе.
- Зависимость `github.com/oapi-codegen/runtime` остаётся и без клиента:
  `runtime.JSONMerge` вызывается 174 раза в методах `From*`, которые собирают
  union из варианта, подставляя поле-дискриминатор.
