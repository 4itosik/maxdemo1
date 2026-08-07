# ТЗ: паритет авторизации с живым Max

**Дата замера прода:** 2026-08-06 · **Версия контракта:** 0.0.32 ·
**Затрагивает:** `internal/maxfacade`, `internal/config`, документацию, тесты

## Задача

README мока обещает: «Переезд приложения с прода на мок — это замена базового
адреса `https://platform-api2.max.ru` на адрес мока, и больше ничего».
Обещание не выполняется. Приложение, написанное по документации Max и
проверенное на живом API, получает от мока `401` на первом же `GET /me`.

Причина — авторизация. Мок принимает ровно те две схемы, которые живой Max
отвергает, и отвергает единственную, которую тот принимает.

| Форма | Живой Max | Мок сегодня |
|---|---|---|
| `Authorization: <token>` | `200` | `401 mock.unauthorized` |
| `Authorization: Bearer <token>` | `401` | `200` |
| `?access_token=<token>` | `401` | `200` |

Расхождение обнаружено при подключении демонстрационного бота
(`../maxbotdemo`), написанного по официальной документации и проверенного на
живом Max.

## Откуда известно

**1. Документация.** [dev.max.ru/docs-api](https://dev.max.ru/docs-api):

> Передача токена через query-параметры больше не поддерживается — используйте
> заголовок `Authorization: <token>`

**2. Замеры на живом API** (2026-08-06, токен рабочего бота):

```sh
curl -H "Authorization: $TOKEN"        https://platform-api2.max.ru/me   # 200
curl -H "Authorization: Bearer $TOKEN" https://platform-api2.max.ru/me   # 401
curl "https://platform-api2.max.ru/me?access_token=$TOKEN"               # 401
```

**3. Контракты расходятся между собой и с реальностью.** `api/openapi.MaxBotApi.yaml`
объявляет `ApiKeyAuth` (query) и `BearerAuth`. Официальный артефакт той же
версии 0.0.32 (`../maxbotdemo/max-openapi-official.json`) объявляет только
`access_token` в query, а `BearerAuth` в нём нет вовсе. Живой Max не принимает
ни одну из трёх объявленных схем.

Это не «дефект артефакта» в том смысле, в каком им были три уже описанные в
README правки: там спека противоречила сама себе, и её чинили по её же смыслу.
Здесь спека внутренне непротиворечива — она просто отстала от API. Мок выбирает
измеренное поведение прода, потому что его назначение — заменить прод, а не
контракт.

## Целевое поведение

Токен извлекается только из заголовка `Authorization`. Разбор схемы не
выполняется: значение заголовка после `TrimSpace` **целиком** является токеном.

```
если заголовок Authorization отсутствует или пуст после TrimSpace:
    401 verify.token · "No access token"

если значение начинается с "Bearer " (точный регистр, с пробелом):
    401 verify.token · "Malformed access token"

иначе:
    token := TrimSpace(значение)
    бот := store.BotByToken(token)
    если бота нет:
        401 verify.token · "Invalid access_token"
    иначе:
        авторизован

query-параметр access_token учитывается только когда заголовка нет:
    401 verify.token · "Query parameter access_token is deprecated, use Authorization header"
```

Порядок важен: заголовок имеет безусловный приоритет, query-параметр при живом
заголовке игнорируется целиком — на проде валидный заголовок вместе с мусором в
`?access_token=` даёт `200`.

Особый случай `Bearer ` не выдуман: прод отличает его от прочих неверных
значений (`Malformed`, а не `Invalid`) — очевидно, пытается разобрать значение
как токен другого рода. Мок повторяет это различие, потому что именно оно
скажет интегратору, что он взял схему из спеки, а не из документации.
Регистрозависимо: `bearer abc` на проде — обычный `Invalid`.

## Ошибки: код `verify.token` без префикса `mock.`

Конвенция мока помечает префиксом `mock.` то, что мок **домыслил** сверх
контракта. Здесь наоборот: код и текст дословно скопированы с прода, вплоть до
английского языка сообщений (остальные сообщения мока — русские). Клиент вправе
завязаться на `code`, и мок обязан отдать тот же самый.

Сейчас `writeError` в этом месте получает константу `CodeUnauthorized`
(`internal/maxfacade/server.go:106`), а текст — из `authError`. Ошибка
авторизации должна нести **свой** код: либо `authError` становится структурой
`{code, message}`, либо в `errors.go` появляется `CodeVerifyToken =
"verify.token"` и передаётся явно. Статус остаётся `statusFor(route,
http.StatusUnauthorized)` — `401` объявлен в контракте для всех операций.

`mock.unauthorized` после правки не используется ни в одном пути авторизации
Bot API. Константу удалить вместе с упоминанием в таблице кодов README.

## Конфигурация: `strict_bearer_auth` удаляется

Настройка означала «принимать только заголовок, запретить токен в query».
После правки query-токен запрещён всегда, а Bearer не принимается вовсе, —
у настройки не остаётся предмета.

Удалить: поле `StrictBearerAuth` (`internal/config/config.go:35`), ветку в
`extractToken`, блок в `config.example.yaml:19-23`, описание в `README.md:78`
и в `docs/ЗАПУСК.md:196`. Неизвестные поля yaml мок игнорирует, поэтому старый
`config.yaml` со `strict_bearer_auth` продолжит запускаться.

## Объём правок

**Код**

| Файл | Что |
|---|---|
| `internal/maxfacade/server.go:122-164` | `extractToken` по алгоритму выше; переписать комментарий к `authorize` |
| `internal/maxfacade/errors.go:24` | `CodeVerifyToken`; убрать `CodeUnauthorized` |
| `internal/config/config.go:31-35` | убрать `StrictBearerAuth` |
| `internal/specs/specs.go:39` | комментарий к `noAuth` утверждает «авторизацию мок выполняет сам (только Bearer)» — теперь неверно |

Слой OpenAPI-валидации трогать не нужно: `noAuth` отключает проверку
security-требований, поэтому голый токен не будет отвергнут как нарушение
контракта.

**Тесты и скрипты**

| Файл | Упоминаний `Bearer`/`access_token` |
|---|---|
| `internal/maxfacade/server_test.go` | 10 — включая `TestStrictBearerAuthRejectsQueryToken` (удалить) и хелпер `doAuth` |
| `internal/httpserver/server_test.go` | 4 |
| `e2e/e2e_test.go`, `e2e/harness_test.go` | 4 |
| `scripts/smoke.sh` | 1 |

**Документация**

| Файл | Что |
|---|---|
| `README.md:59-81` | раздел «Адреса и авторизация»: одна схема вместо двух |
| `README.md:90-105` | таблица кодов: убрать `mock.unauthorized` (строка 93), добавить `verify.token` |
| `README.md:133` | новый раздел рядом с «Тремя компенсациями дефектов артефакта»: расхождение контракта с живым API, с таблицей замеров и датой |
| `docs/ЗАПУСК.md:88-107` | шаги 2 и 3: `Authorization: <token>` |
| `docs/ЗАПУСК.md:279` | строка про `401` в разборе неполадок |
| `config.example.yaml` | убрать `strict_bearer_auth` |
| `web/static/admin.html:27-28` | подпись под таблицей ботов |

## Приёмка

Десять случаев, по одному тесту на строку. Ожидаемые ответы — измеренные
ответы прода, а не домысел.

| # | `Authorization` | query | Ожидается |
|---|---|---|---|
| 1 | заголовка нет | — | `401` · `verify.token` · `No access token` |
| 2 | пустой / только пробелы | — | `401` · `verify.token` · `No access token` |
| 3 | `Bearer <валидный>` | — | `401` · `verify.token` · `Malformed access token` |
| 4 | `Bearer abc` | — | `401` · `verify.token` · `Malformed access token` |
| 5 | `bearer abc` | — | `401` · `verify.token` · `Invalid access_token` |
| 6 | `Bearer` (без пробела) | — | `401` · `verify.token` · `Invalid access_token` |
| 7 | `<валидный>` | — | `200` |
| 8 | `  <валидный>  ` | — | `200` — пробелы по краям обрезаются |
| 9 | `<валидный>` | `access_token=мусор` | `200` — заголовок выигрывает |
| 10 | заголовка нет | `access_token=<валидный>` | `401` · `verify.token` · `Query parameter access_token is deprecated, use Authorization header` |

Сквозная проверка: `make e2e` и `make smoke` проходят после правки скриптов;
`../maxbotdemo` с `MAX_API_BASE_URL=http://localhost:8080` доходит до
`GET /me` → `PATCH /me/commands` → `POST /subscriptions` без единой правки
клиента API.

**Выполнено 2026-08-06.** Десять случаев приёмки — `TestAuthorizationParity`
(`internal/maxfacade/server_test.go`), по подтесту на строку таблицы. `go vet`,
`go test ./...`, `make e2e`, `make smoke` — зелёные. Демобот, запущенный без
единой правки (`MAX_BOT_TOKEN` от мока, `MAX_API_BASE_URL=http://localhost:8080`,
`LISTEN_ADDR=:8081`), дошёл до `бот зарегистрирован`; в журнале мока —
`200 GET /me`, `200 PATCH /me/commands`, `200 GET /subscriptions`,
`200 POST /subscriptions`, ни одного `401`.

## Как переснять эталон

Поведение прода измерено, а не выведено из документации, — значит, может
измениться. Скрипт для повторного замера (нужны рабочий токен и корневой
сертификат Минцифры):

```sh
TOKEN='<токен живого бота>'
CA='russian_trusted_root_ca.cer'   # gu-st.ru/content/Other/doc/russian_trusted_root_ca.cer
API='https://platform-api2.max.ru/me'

for h in "$TOKEN" "Bearer $TOKEN" "Bearer abc" "bearer abc" "Bearer" "abc def" ""; do
  printf '%-24s ' "[$h]"
  curl -s -w ' [%{http_code}]\n' --cacert "$CA" -H "Authorization: $h" "$API" | head -c 120
done
curl -s -w ' [%{http_code}]\n' --cacert "$CA" "$API?access_token=$TOKEN" | head -c 120
```

Если эталон разойдётся с таблицей приёмки — правится мок, а не таблица.

## Вне объёма

- Служебная поверхность `/mock/*` (админка, веб-чат, control-API, загрузка
  файлов, WebSocket) — авторизации не имеет и не получает.
- Доставка событий на подписки: секрет уходит в `X-Max-Bot-Api-Secret`,
  заголовка `Authorization` там нет.
- Приём `http://` в подписках — уже реализован, не меняется.
- Обновление `api/openapi.MaxBotApi.yaml`: артефакт принадлежит `../maxapi`,
  и мок правит не его, а своё поведение. Расхождение фиксируется в README.
