# Паритет авторизации с живым Max — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** мок принимает токен ровно так, как живой Max — голым в заголовке
`Authorization`, — и отвергает обе схемы, объявленные в контракте.

**Architecture:** правка локальна: `extractToken` перестаёт разбирать схему и
читать query, ошибка авторизации получает собственный код `verify.token`,
настройка `strict_bearer_auth` исчезает вместе со своим предметом. Слой
OpenAPI-валидации не трогаем: `noAuth` уже отключает проверку security, поэтому
голый токен не будет отвергнут как нарушение контракта. Смена схемы ломает все
вызовы Bot API в тестах и скриптах — они правятся тем же коммитом, иначе дерево
не будет зелёным ни в одной промежуточной точке.

**Tech Stack:** Go 1.22+, `net/http`, `kin-openapi`, `go test ./...`, bash-smoke.

**ТЗ:** `docs/specs/2026-08-06-auth-parity-tz.md` — там же замеры прода и скрипт
для их повторного снятия.

## Global Constraints

- Репозиторий: `/Users/aay/endeavors/ailearn/maxmoc` (Go-модуль `maxmock`).
- Работать в ветке `auth-parity`, отведённой от `main`.
- Тексты сообщений об ошибке авторизации — **дословно английские**, как на
  проде: `No access token`, `Malformed access token`, `Invalid access_token`.
  Не переводить, не менять регистр, не дополнять. Остальные сообщения мока
  остаются русскими.
- Код ошибки авторизации — `verify.token`, **без** префикса `mock.`: он
  скопирован с прода, а не выдуман моком.
- `Bearer ` распознаётся регистрозависимо, с пробелом. `bearer abc` — это
  обычный неверный токен.
- Статус ответа — `statusFor(route, http.StatusUnauthorized)`, то есть `401`:
  он объявлен в контракте для всех операций.
- Артефакт `api/openapi.MaxBotApi.yaml` не правится: он принадлежит `../maxapi`.
- Комментарии и документация — на русском, как во всём репозитории.

---

## Файлы

| Файл | Что с ним |
|---|---|
| `internal/maxfacade/server.go` | `extractToken`/`authorize` переписаны, комментарии заменены |
| `internal/maxfacade/errors.go` | `CodeVerifyToken` вместо `CodeUnauthorized` |
| `internal/maxfacade/server_test.go` | таблица из 10 случаев приёмки вместо трёх старых тестов; хелпер `do` |
| `internal/httpserver/server_test.go` | 4 вызова с `Bearer ` |
| `internal/specs/specs_test.go` | 1 заголовок в хелпере `postMessages` |
| `internal/specs/specs.go` | два комментария (`noAuth`, `normalizeAuthSchemes`) |
| `internal/config/config.go` | удалить поле `StrictBearerAuth` |
| `e2e/e2e_test.go`, `e2e/harness_test.go` | схема в хелпере, проверка query-токена |
| `scripts/smoke.sh` | заголовок в `auth=(…)` |
| `config.example.yaml` | убрать блок `strict_bearer_auth` |
| `README.md` | раздел авторизации, таблица кодов, новый раздел о расхождении с прод-API |
| `docs/ЗАПУСК.md` | шаги 2–3, пример конфига, строка в разборе неполадок |
| `web/static/admin.html` | подпись под формой регистрации бота |
| `docs/specs/2026-08-05-max-mock-design.md` | пометка «устарело» в §6 и в обзорной таблице |

---

## Task 0: Ветка

- [ ] **Step 1: Отвести ветку от `main`**

```bash
cd /Users/aay/endeavors/ailearn/maxmoc
git checkout -b auth-parity
git status --short   # ожидается только ?? docs/specs/2026-08-06-auth-parity-tz.md
```

- [ ] **Step 2: Зафиксировать ТЗ и этот план в истории**

ТЗ пока не в git — без него последующие коммиты будут ссылаться в пустоту.

```bash
git add docs/specs/2026-08-06-auth-parity-tz.md docs/plans/2026-08-06-auth-parity.md
git commit -m "docs: ТЗ и план паритета авторизации с живым Max"
```

---

## Task 1: Авторизация по голому заголовку

Один коммит: поведение и все его вызовы. Разделить нельзя — тесты и скрипты
шлют `Bearer <token>`, и после смены поведения они падают все разом.

**Files:**
- Modify: `internal/maxfacade/errors.go:15-33`
- Modify: `internal/maxfacade/server.go:104-108`, `internal/maxfacade/server.go:122-169`
- Test: `internal/maxfacade/server_test.go:65-68`, `:131-209`
- Modify: `internal/httpserver/server_test.go:212,213,272,371`
- Modify: `internal/specs/specs_test.go:54`
- Modify: `e2e/harness_test.go:203-205`, `e2e/e2e_test.go:50-56`
- Modify: `scripts/smoke.sh:64`

**Interfaces:**
- Produces: `maxfacade.CodeVerifyToken = "verify.token"` — константа кода
  ошибки авторизации, используется в тестах пакета.
- Produces: `func extractToken(r *http.Request) (string, error)` — свободная
  функция (не метод `*Server`): конфигурация ей больше не нужна.
- Удаляется: `maxfacade.CodeUnauthorized`. Ссылок вне пакета нет.

- [ ] **Step 1: Написать таблицу приёмки — 10 случаев**

В `internal/maxfacade/server_test.go` добавить новый тест (старые пока не
трогаем — они удаляются на шаге 3):

```go
// Паритет авторизации с живым Max: токен передаётся голым в заголовке
// Authorization. Схема Bearer и query-параметр access_token отвергаются —
// контракт объявляет обе, но прод не принимает ни одной.
//
// Ожидаемые коды, статусы и тексты — замер прода 2026-08-06, а не домысел:
// docs/specs/2026-08-06-auth-parity-tz.md.
func TestAuthorizationParity(t *testing.T) {
	f := newFixture(t)
	token := f.bot.Token

	cases := []struct {
		name    string
		auth    string
		query   string
		status  int
		message string
	}{
		{"заголовка нет", "", "", http.StatusUnauthorized, "No access token"},
		{"пробелы вместо токена", "   ", "", http.StatusUnauthorized, "No access token"},
		{"Bearer с валидным токеном", "Bearer " + token, "", http.StatusUnauthorized, "Malformed access token"},
		{"Bearer с мусором", "Bearer abc", "", http.StatusUnauthorized, "Malformed access token"},
		{"схема в нижнем регистре", "bearer abc", "", http.StatusUnauthorized, "Invalid access_token"},
		{"Bearer без пробела", "Bearer", "", http.StatusUnauthorized, "Invalid access_token"},
		{"голый токен", token, "", http.StatusOK, ""},
		{"голый токен в пробелах", "  " + token + "  ", "", http.StatusOK, ""},
		{"заголовок побеждает query", token, "?access_token=мусор", http.StatusOK, ""},
		{"только query", "", "?access_token=" + token, http.StatusUnauthorized,
			"Query parameter access_token is deprecated, use Authorization header"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := f.doAuth(t, "GET", "/me"+tc.query, "", tc.auth)
			if resp.StatusCode != tc.status {
				t.Fatalf("статус %d, ожидался %d: %s", resp.StatusCode, tc.status, body)
			}
			if tc.status == http.StatusOK {
				return
			}
			e := decodeError(t, body)
			if e.Code != CodeVerifyToken {
				t.Errorf("код %q, ожидался %q", e.Code, CodeVerifyToken)
			}
			if e.Message != tc.message {
				t.Errorf("сообщение %q, ожидалось %q", e.Message, tc.message)
			}
		})
	}
}
```

- [ ] **Step 2: Убедиться, что тест не компилируется по отсутствию `CodeVerifyToken`**

```bash
cd /Users/aay/endeavors/ailearn/maxmoc
go test ./internal/maxfacade/ -run TestAuthorizationParity
```

Ожидается: `undefined: CodeVerifyToken`. Это и есть «красный» — константы кода
ошибки, который обязан вернуть мок, ещё нет.

- [ ] **Step 3: Удалить три устаревших теста авторизации**

Из `internal/maxfacade/server_test.go` удалить целиком:
- `TestAuthorization` (строки 131–157) — проверяет разбор схемы `Bearer`,
  включая приём `bearer <token>` как валидного;
- комментарий и `TestQueryTokenIsAcceptedByDefault` (строки 159–188) —
  утверждает, что токен в query принимается;
- комментарий и `TestStrictBearerAuthRejectsQueryToken` (строки 190–209) —
  проверяет удаляемую настройку.

Их предмет целиком покрыт `TestAuthorizationParity`.

- [ ] **Step 4: Завести код `verify.token` в `errors.go`**

В `internal/maxfacade/errors.go` из блока `const` (строка 24) удалить
`CodeUnauthorized` и добавить после блока:

```go
// CodeVerifyToken — код ошибки авторизации. Единственный код мока без
// префикса `mock.`: и он, и англоязычные тексты его сообщений дословно
// скопированы с живого Max (замер 2026-08-06, docs/specs/2026-08-06-auth-parity-tz.md).
// Клиент вправе завязаться на code, и мок обязан отдать тот же самый.
const CodeVerifyToken = "verify.token"
```

- [ ] **Step 5: Переписать авторизацию в `server.go`**

Строка 106 — код ошибки:

```go
		writeError(w, statusFor(route, http.StatusUnauthorized), CodeVerifyToken, err.Error())
```

Строки 122–169 (`authorize`, `extractToken`, `authError`) заменить целиком на:

```go
// authorize находит бота по токену из заголовка Authorization.
//
// Контракт объявляет две схемы — `BearerAuth` и `access_token` в query, — но
// живой Max не принимает ни одной: токен уходит голым, `Authorization: <token>`.
// Замер прода 2026-08-06 и обоснование выбора — в
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
```

- [ ] **Step 6: Хелпер `do` шлёт голый токен**

`internal/maxfacade/server_test.go:65-68`:

```go
func (f *fixture) do(t *testing.T, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	return f.doAuth(t, method, path, body, f.bot.Token)
}
```

- [ ] **Step 7: Прогнать тесты пакета — все 10 случаев зелёные**

```bash
go test ./internal/maxfacade/ -run TestAuthorizationParity -v
go test ./internal/maxfacade/
```

Ожидается: PASS, десять подтестов.

- [ ] **Step 8: Поправить остальные вызовы Bot API в тестах**

`internal/httpserver/server_test.go` — четыре места, всюду убрать `"Bearer " + `:

```go
	f.req(t, "GET", "/me", "", map[string]string{"Authorization": first["token"].(string)})          // :212
	f.req(t, "GET", "/subscriptions", "", map[string]string{"Authorization": second["token"].(string)}) // :213
	auth := map[string]string{"Authorization": token}                                                 // :272
	auth := map[string]string{"Authorization": bot["token"].(string)}                                 // :371
```

`internal/specs/specs_test.go:54` — заголовок в хелпере `postMessages`
(валидацию security отключает `noAuth`, значение здесь произвольное, но пусть
не вводит в заблуждение):

```go
	r.Header.Set("Authorization", "x")
```

`e2e/harness_test.go:203-205`:

```go
	if token != "" {
		req.Header.Set("Authorization", token)
	}
```

`e2e/e2e_test.go:50-56` — проверка «токен в query» переворачивается: вместо
«обе схемы дают один результат» проверяем, что query-токен отвергнут:

```go
	// Контракт объявляет ещё две схемы — Bearer и access_token в query, — но
	// живой Max отвергает обе (замер 2026-08-06). Мок повторяет прод, и
	// сквозной тест это фиксирует: иначе расхождение всплывёт у интегратора.
	if status, raw := m.call(t, "GET", "/me?access_token="+token, "", ""); status != http.StatusUnauthorized {
		t.Fatalf("токен в query должен отвергаться: %d %s", status, raw)
	}
```

(`m.call`, а не `m.mustCall`: последний валит тест на любом не-200.
`net/http` в `e2e_test.go` уже импортирован.)

`scripts/smoke.sh:64`:

```bash
auth=(-H "Authorization: ${TOKEN}" -H 'Content-Type: application/json')
```

- [ ] **Step 9: Полный прогон**

```bash
go build ./...
go test ./...
make e2e
make smoke
```

Ожидается: всё PASS. `make smoke` печатает сценарий и завершается без
`curl: (22)`.

Если `go build` ругается на неиспользуемый импорт `config` в `maxfacade` —
проверить: `s.cfg` всё ещё нужен в `validateOwnResponse` (`ValidateResponses`),
импорт остаётся.

- [ ] **Step 10: Коммит**

```bash
git add internal/maxfacade internal/httpserver internal/specs e2e scripts/smoke.sh
git commit -m "feat: авторизация по голому заголовку Authorization, как на живом Max

Контракт объявляет BearerAuth и access_token в query; замер прода 2026-08-06
показал, что API отвергает обе схемы и принимает голый токен в заголовке.
Мок повторяет прод: его назначение — заменить собой prod-адрес.

Ошибка авторизации отдаёт код verify.token и англоязычные тексты прода
(No access token / Malformed access token / Invalid access_token) —
они скопированы, а не придуманы, и клиент вправе на них завязаться."
```

---

## Task 2: Удалить настройку `strict_bearer_auth`

Настройка означала «принимать только заголовок, запретить токен в query».
После Task 1 query-токен запрещён всегда, а Bearer не принимается вовсе — у
настройки не осталось предмета. Поле уже не читается ниоткуда, поэтому задача
отделима: её можно принять или отклонить независимо от Task 1.

**Files:**
- Modify: `internal/config/config.go:31-35`
- Modify: `config.example.yaml:19-23`
- Modify: `docs/ЗАПУСК.md:196`

**Interfaces:**
- Удаляется: поле `config.Config.StrictBearerAuth`. Ссылок в коде после Task 1
  нет — проверяется grep'ом на шаге 1.

- [ ] **Step 1: Убедиться, что поле больше нигде не читается**

```bash
cd /Users/aay/endeavors/ailearn/maxmoc
grep -rn "StrictBearerAuth\|strict_bearer_auth" --include="*.go" .
```

Ожидается: единственная строка — объявление в `internal/config/config.go:35`.
Если найдётся что-то ещё — сначала разобраться, Task 1 выполнен не полностью.

- [ ] **Step 2: Удалить поле из структуры**

`internal/config/config.go` — удалить строки 31–35 (комментарий и поле),
оставив `Webhook` в структуре:

```go
	// ValidateResponses — валидировать ли собственные ответы против
	// OpenAPI. Ловит ошибки в wire-структурах до того, как их увидит КЦ.
	ValidateResponses bool    `yaml:"validate_responses"`
	Webhook           Webhook `yaml:"webhook"`
}
```

Значения по умолчанию в `Default()` менять не нужно: поле там не задавалось.

- [ ] **Step 3: Убрать блок из примера конфигурации**

`config.example.yaml` — удалить строки 19–23 (комментарий и
`strict_bearer_auth: false`) вместе с пустой строкой перед `webhook:`, чтобы
не осталось двойного отступа.

- [ ] **Step 4: Убрать строку из примера в ЗАПУСК.md**

`docs/ЗАПУСК.md:196` — удалить строку

```
strict_bearer_auth: false   # true — запретить токен в query
```

- [ ] **Step 5: Проверить, что старый конфиг всё ещё запускается**

Неизвестные ключи yaml мок игнорирует — старый `config.yaml` со снятой
настройкой не должен ломать запуск. Проверяем явно:

```bash
cd /Users/aay/endeavors/ailearn/maxmoc
printf 'listen: ":8099"\nstrict_bearer_auth: true\n' > /tmp/legacy-config.yaml
go run ./cmd/max-mock -config /tmp/legacy-config.yaml &
sleep 2
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8099/me
kill %1
rm -f /tmp/legacy-config.yaml
```

Ожидается: сервис поднялся без ошибки разбора конфига, `curl` вернул `401`.
Прибрать за собой созданную базу, если она легла в рабочий каталог:
`git status --short` не должен показывать новых файлов (`max-mock.db*` в
`.gitignore` — проверить).

- [ ] **Step 6: Прогон и коммит**

```bash
go test ./...
git add internal/config/config.go config.example.yaml docs/ЗАПУСК.md
git commit -m "refactor: убрана настройка strict_bearer_auth

Она означала «только заголовок, без токена в query». Токен в query запрещён
безусловно, Bearer не принимается вовсе — предмета у настройки не осталось.
Неизвестные ключи yaml игнорируются, поэтому старые config.yaml запускаются."
```

---

## Task 3: Документация

Обещание README — «переезд с прода на мок это замена базового адреса, и больше
ничего» — теперь наконец выполняется. Документация должна говорить то же самое,
и там же должно быть зафиксировано, почему мок расходится с собственным
контрактом.

**Files:**
- Modify: `README.md:70-80` (авторизация), `:90-105` (таблица кодов), `:133-145` (новый раздел)
- Modify: `docs/ЗАПУСК.md:88-107`, `:279`
- Modify: `web/static/admin.html:26-29`
- Modify: `internal/specs/specs.go:38-41`, `:92-96`
- Modify: `docs/specs/2026-08-05-max-mock-design.md:35`, `:104-114`

- [ ] **Step 1: README — раздел «Адреса и авторизация»**

Строки 70–80 (от «Авторизация — оба способа…» до «…истории браузера).»)
заменить на:

```markdown
Авторизация — голый токен в заголовке, как на живом Max:

```
Authorization: <access_token>
```

Ни схема `Bearer`, ни query-параметр `access_token` не принимаются, хотя
контракт объявляет обе: живой API отвергает и то, и другое. Замеры и
обоснование — в разделе [«Контракт против живого API»](#контракт-против-живого-api).
```

- [ ] **Step 2: README — таблица кодов ошибок**

Строку 93 (`| `mock.unauthorized` | нет токена, токен неизвестен или передан в
query |`) удалить. После таблицы (после строки 100, перед абзацем «**Статус
выбирается по контракту операции.**») вставить:

```markdown
| `verify.token` | нет токена, токен неизвестен или передан не так, как ждёт Max |

Последняя строка — единственный код без префикса `mock.`, и это намеренно: и
код, и англоязычные тексты его сообщений (`No access token`,
`Malformed access token`, `Invalid access_token`) дословно скопированы с
живого Max. Клиент вправе завязаться на них, и мок обязан отдать те же самые.
```

(Строка таблицы добавляется последней строкой таблицы — то есть после
`| `mock.internal` | … |`, а поясняющий абзац — следом.)

- [ ] **Step 3: README — новый раздел о расхождении с прод-API**

После раздела «### Три компенсации дефектов артефакта» (после строки 145,
перед «## Вложения») вставить:

```markdown
### Контракт против живого API

Три правки выше чинят спеку по её же смыслу: она противоречила сама себе.
С авторизацией случай другой — спека внутренне непротиворечива, она просто
отстала от API. Замер `GET /me` на проде токеном рабочего бота (2026-08-06):

| Форма | Живой Max | Что объявляет контракт |
|---|---|---|
| `Authorization: <token>` | `200` | не объявлена |
| `Authorization: Bearer <token>` | `401` | `BearerAuth` |
| `?access_token=<token>` | `401` | `ApiKeyAuth` |

Документация Max говорит то же: «Передача токена через query-параметры больше
не поддерживается — используйте заголовок `Authorization: <token>`».

Мок выбирает измеренное поведение прода, потому что его назначение — заменить
прод, а не контракт. Артефакт `api/openapi.MaxBotApi.yaml` при этом не
правится: он принадлежит [`maxapi`](../maxapi) и переписан с официальной схемы
Max, где `BearerAuth` нет вовсе, а `access_token` в query объявлен.

Поведение измерено, а не выведено, — значит, может измениться. Скрипт для
повторного замера лежит в `docs/specs/2026-08-06-auth-parity-tz.md`; если
эталон разойдётся с таблицей приёмки оттуда, правится мок, а не таблица.
```

- [ ] **Step 4: ЗАПУСК.md — шаги 2 и 3**

Строки 88–98. Абзац «Больше ничего менять не нужно…» и проверку связи
заменить на:

```markdown
Больше ничего менять не нужно: пути (`/me`, `/messages`, `/subscriptions`,
`/answers`, `/uploads`, `/me/commands`) и авторизация совпадают с
документацией Max — токен уходит голым в заголовке `Authorization`, без схемы
`Bearer` и без `?access_token=` в query (живой Max их тоже не принимает).

Проверить связь:

```bash
curl -s http://localhost:8080/me -H "Authorization: <token>" | jq .
```
```

Строка 106 (шаг 3, подписка) — тот же заголовок:

```bash
curl -s -X POST http://localhost:8080/subscriptions \
  -H "Authorization: <token>" -H 'Content-Type: application/json' \
  -d '{"url":"http://<адрес-стенда>/webhook","secret":"stand-secret-123"}' | jq .
```

- [ ] **Step 5: ЗАПУСК.md — разбор неполадок**

Строку 279 заменить на:

```markdown
| `401` с кодом `verify.token` | токена нет, он неверен или передан не так. В заголовке должен быть ровно токен: `Authorization: <token>` — без `Bearer` и без `?access_token=` в URL. Текст в поле `message` различает случаи: `No access token`, `Malformed access token` (передана схема `Bearer`), `Invalid access_token` (токен неизвестен). Токен виден в админке |
```

- [ ] **Step 6: Админка — подпись под формой**

`web/static/admin.html:26-29`:

```html
    <p class="muted" style="margin-bottom:0">
      После регистрации выдаётся access_token — его КЦ передаёт в заголовке
      <code>Authorization: &lt;token&gt;</code>, голым, без схемы
      «Bearer»: так же, как на живом Max.
    </p>
```

- [ ] **Step 7: Комментарии в `internal/specs/specs.go`**

Строки 38–41 — комментарий к `noAuth` утверждает «только Bearer»:

```go
// noAuth отключает проверку security-требований: авторизацию мок выполняет
// сам, по измеренному поведению прода (голый токен в заголовке Authorization,
// см. maxfacade), а kin-openapi без этой функции считает любой запрос
// с описанным security неавторизованным.
```

Строки 92–96 — комментарий к `normalizeAuthSchemes` ссылается на
регистронезависимый разбор, которого больше нет:

```go
// normalizeAuthSchemes приводит имя http-схемы авторизации к нижнему регистру.
// Артефакт объявляет `BearerAuth.scheme: Bearer`, тогда как реестр HTTP
// Authentication Schemes (RFC 7235) содержит имена в нижнем регистре и
// валидатор требует именно их. На поведение мока это не влияет: схему
// `BearerAuth` он не принимает вовсе (см. maxfacade), а правка нужна лишь
// затем, чтобы документ прошёл собственную валидацию.
```

- [ ] **Step 8: Пометить устаревшим §6 дизайна от 2026-08-05**

Дизайн-документ — запись принятых решений, переписывать его задним числом
нельзя, но и оставлять читателя с неверным описанием тоже.

`docs/specs/2026-08-05-max-mock-design.md:35` — строка обзорной таблицы:

```markdown
| Авторизация | Голый токен в заголовке `Authorization` — по измеренному поведению прода (пересмотрено 2026-08-06, см. §6) |
```

`docs/specs/2026-08-05-max-mock-design.md:104` — сразу под заголовком
«## 6. Авторизация» вставить:

```markdown
> **Пересмотрено 2026-08-06.** Замер живого Max показал, что API не принимает
> ни одну из объявленных в контракте схем: токен уходит голым в заголовке
> `Authorization`, а `Bearer` и `access_token` в query дают `401`. Мок
> приведён к поведению прода, настройка `strict_bearer_auth` удалена.
> Действующее описание — `docs/specs/2026-08-06-auth-parity-tz.md`.
> Ниже — решение, каким оно было принято при проектировании.
```

- [ ] **Step 9: Проверить, что в документации не осталось старой схемы**

```bash
cd /Users/aay/endeavors/ailearn/maxmoc
grep -rn "Bearer\|access_token=\|strict_bearer" README.md docs/ЗАПУСК.md web/static/ config.example.yaml
```

Ожидается: упоминания `Bearer` только там, где объясняется, что схема **не**
принимается (README — раздел о расхождении и таблица кодов, ЗАПУСК — шаг 2 и
разбор неполадок, admin.html — подпись). Ни одного примера `curl` с
`Authorization: Bearer`. Ни одного `strict_bearer`.

- [ ] **Step 10: Коммит**

```bash
git add README.md docs/ЗАПУСК.md web/static/admin.html internal/specs/specs.go docs/specs/2026-08-05-max-mock-design.md
git commit -m "docs: одна схема авторизации вместо двух, расхождение с живым API зафиксировано"
```

---

## Task 4: Сквозная проверка демонстрационным ботом

Главный критерий приёмки из ТЗ: `../maxbotdemo`, написанный по документации Max
и проверенный на живом API, доходит до `GET /me` → `PATCH /me/commands` →
`POST /subscriptions` без единой правки клиента API. Демобот шлёт
`Authorization: <token>` (`../maxbotdemo/internal/maxapi/client.go:100`) — до
правки он получал `401` на первом же вызове.

**Files:**
- Ничего не меняется. Задача — проверка; при провале правится мок.

- [ ] **Step 1: Поднять мок на временной базе**

```bash
cd /Users/aay/endeavors/ailearn/maxmoc
go build -o /tmp/max-mock ./cmd/max-mock
MAXMOCK_DB_PATH=/tmp/parity-check.db MAXMOCK_BLOB_DIR=/tmp/parity-blobs \
  MAXMOCK_LISTEN=:8080 /tmp/max-mock &
sleep 2
```

- [ ] **Step 2: Зарегистрировать бота и забрать токен**

```bash
TOKEN=$(curl -sf -X POST http://localhost:8080/mock/api/bots \
  -H 'Content-Type: application/json' \
  -d '{"name":"Демобот","username":"demo_bot"}' | jq -r .token)
echo "$TOKEN"
```

- [ ] **Step 3: Запустить демобота против мока**

`WEBHOOK_URL` обязан быть `https://…` с непустым путём — это проверка самого
демобота, доставка событий в этой задаче не проверяется. `LISTEN_ADDR`
уводится с `:8080`, занятого моком.

```bash
cd /Users/aay/endeavors/ailearn/maxbotdemo
MAX_BOT_TOKEN="$TOKEN" \
MAX_API_BASE_URL=http://localhost:8080 \
WEBHOOK_URL=https://stand.local/webhook \
WEBHOOK_SECRET=stand-secret-123 \
LISTEN_ADDR=:8081 \
timeout 15 go run ./cmd/bot
```

Ожидается в логе: `бот зарегистрирован name=Демобот …`, затем
`слушаем события addr=:8081 path=/webhook`, и выход по таймауту. Ни одного
`401`, ни одной ошибки регистрации.

- [ ] **Step 4: Подтвердить по журналу мока, что дошли все три вызова**

```bash
curl -sf 'http://localhost:8080/mock/api/log?limit=50' \
  | jq -r '.[] | "\(.status) \(.method) \(.path)"'
```

Ожидается среди записей: `200 GET /me`, `200 PATCH /me/commands`,
`200 GET /subscriptions`, `200 POST /subscriptions`. Ни одной записи со
статусом `401`.

(Маршрут — `GET /mock/api/log`, `internal/controlapi/api.go:43`; `limit` по
умолчанию 100.)

- [ ] **Step 5: Прибрать за собой**

```bash
kill %1 2>/dev/null || pkill -f /tmp/max-mock
rm -rf /tmp/max-mock /tmp/parity-check.db* /tmp/parity-blobs
cd /Users/aay/endeavors/ailearn/maxmoc && git status --short
```

`git status` должен быть чистым: временная база и блобы жили в `/tmp`.

- [ ] **Step 6: Финальный прогон всего**

```bash
cd /Users/aay/endeavors/ailearn/maxmoc
go vet ./...
go test ./...
make e2e
make smoke
```

Ожидается: всё PASS.

- [ ] **Step 7: Записать результат проверки в ТЗ**

В `docs/specs/2026-08-06-auth-parity-tz.md`, в конце раздела «## Приёмка»,
после абзаца «Сквозная проверка: …», добавить строку с фактическим
результатом — дату и что именно прошло. Формулировка пишется по итогу шагов
3–4, а не заранее.

```bash
git add docs/specs/2026-08-06-auth-parity-tz.md
git commit -m "docs: отметка о пройденной сквозной проверке демоботом"
```

---

## Завершение

- [ ] Свести ветку `auth-parity` в `main` (см. superpowers:finishing-a-development-branch).
- [ ] Обновить память проекта: в `maxmoc-project.md` описание авторизации
      («обе схемы контракта») больше не верно — заменить на голый заголовок
      и сослаться на ТЗ 2026-08-06.
