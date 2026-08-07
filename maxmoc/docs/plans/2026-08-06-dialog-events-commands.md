# Старт/стоп диалога, лента событий и меню команд — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Дать тестировщику послать `bot_started`/`bot_stopped` в любой момент, увидеть ход разговора одной лентой и нажимать команды бота вместо набора их руками.

**Architecture:** `bot_stopped` добавляется как обычное исходящее событие ядра. Журналы остаются раздельными, но получают `chat_id`; действия из веб-чата пишутся в новую таблицу `ui_actions`; control-API отдаёт слитую по времени ленту диалога. Меню команд рисуется из `bot.commands`, которое control-API уже отдаёт.

**Tech Stack:** Go 1.22+ без cgo, SQLite (`modernc.org/sqlite`), `net/http` + `http.ServeMux`, ванильный JS без сборки, `kin-openapi` для валидации по контракту.

**Спека:** `docs/specs/2026-08-06-dialog-events-commands-design.md`

## Global Constraints

- Комментарии, сообщения об ошибках и коммиты — по-русски. Комментарий объясняет **почему**, а не пересказывает код.
- Каждое исходящее событие обязано пройти `ValidateWebhookBody` по `api/openapi.MaxBotWebhook.yaml`. Тесты ядра валидируют события автоматически через фейковый стенд — новые события проверяются тем же путём.
- Мок не выдумывает поведение живого Max. Неизмеренное расхождение фиксируется в README, а не реализуется наугад.
- Миграции существующих баз обязательны: у стендов КЦ в базе живут боты с выданными токенами. Терять их нельзя.
- Тесты гоняются `make test`, сквозные — `make e2e`.
- Каждый коммит заканчивается строкой `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## Структура файлов

| Файл | Ответственность | Задачи |
|---|---|---|
| `internal/wire/wire.go` | `BotStoppedUpdate`, константа типа события | 1 |
| `internal/store/dialogs.go` | `SetDialogStopped` | 1 |
| `internal/core/client.go` | `ClientStop` | 1 |
| `internal/controlapi/api.go` | действие `stop`, журналирование действий UI, эндпоинт ленты | 2, 6, 7 |
| `web/static/chat.html`, `chat.js`, `style.css` | кнопка-переключатель, вкладка «События», чипы команд | 2, 8, 9 |
| `internal/store/schema.sql`, `store.go`, `logs.go` | `chat_id` в журналах, таблица `ui_actions`, чтение ленты | 3, 6, 7 |
| `internal/maxfacade/server.go` | `noteChat` и проставление диалога по операциям | 4 |
| `internal/webhook/dispatcher.go` | `chat_id` в доставках, запись «нет подписки» | 5 |
| `internal/events/bus.go` | виды событий `ui_action`, `bot` | 6, 9 |
| `internal/core/core.go` | публикация изменения команд | 9 |
| `e2e/e2e_test.go`, `README.md`, `docs/ЗАПУСК.md` | сквозной сценарий и документация | 10 |

---

### Task 1: Событие `bot_stopped` в ядре

**Files:**
- Modify: `internal/wire/wire.go:17-21` (константы), `internal/wire/wire.go:463-470` (рядом с `BotStartedUpdate`)
- Modify: `internal/store/dialogs.go:199-209`
- Modify: `internal/core/client.go:32-60`
- Test: `internal/core/core_test.go`

**Interfaces:**
- Consumes: `store.SetDialogStarted`, `core.dialogContext`, `core.clientUser`, `core.publish` — всё уже есть.
- Produces: `wire.UpdateBotStopped` (строка `"bot_stopped"`), `wire.BotStoppedUpdate`, `(*store.Store).SetDialogStopped(chatID int64) (bool, error)`, `(*core.Core).ClientStop(chatID int64) error`.

- [ ] **Step 1: Написать падающий тест**

В `internal/core/core_test.go` рядом с `TestClientStartOnlyOnce`:

```go
func TestClientStopAndRestart(t *testing.T) {
	f := newFixture(t)
	if err := f.core.ClientStart(f.chatID, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.core.ClientStop(f.chatID); err != nil {
		t.Fatal(err)
	}
	// Повторная остановка молчит: выключенного бота нельзя выключить дважды.
	if err := f.core.ClientStop(f.chatID); err != nil {
		t.Fatal(err)
	}
	// А вот старт после остановки обязан сработать: контракт описывает
	// bot_started как событие, которое приходит, когда пользователь
	// «начнёт или возобновит общение с ботом».
	if err := f.core.ClientStart(f.chatID, ""); err != nil {
		t.Fatal(err)
	}
	f.disp.Wait()

	stopped := f.stand.ofType(wire.UpdateBotStopped)
	if len(stopped) != 1 {
		t.Fatalf("bot_stopped доставлен %d раз, ожидался один", len(stopped))
	}
	if int64(stopped[0]["chat_id"].(float64)) != f.chatID {
		t.Errorf("chat_id в bot_stopped: %v", stopped[0]["chat_id"])
	}
	user, ok := stopped[0]["user"].(map[string]any)
	if !ok {
		t.Fatalf("в bot_stopped нет пользователя: %v", stopped[0])
	}
	if int64(user["user_id"].(float64)) != f.client.UserID {
		t.Errorf("user_id в bot_stopped: %v", user["user_id"])
	}
	if len(f.stand.ofType(wire.UpdateBotStarted)) != 2 {
		t.Errorf("bot_started после возобновления: %d, ожидалось 2",
			len(f.stand.ofType(wire.UpdateBotStarted)))
	}
}
```

- [ ] **Step 2: Убедиться, что тест не компилируется**

Run: `go test ./internal/core/ -run TestClientStopAndRestart`
Expected: FAIL — `undefined: wire.UpdateBotStopped`, `f.core.ClientStop undefined`

- [ ] **Step 3: Добавить тип события в контрактные структуры**

В `internal/wire/wire.go` в блок констант рядом с `UpdateBotStarted`:

```go
	UpdateBotStopped      = "bot_stopped"
```

И структуру рядом с `BotStartedUpdate`:

```go
// BotStoppedUpdate — пользователь остановил бота в его настройках.
// Payload-а у события нет: диплинк бывает только у запуска.
type BotStoppedUpdate struct {
	UpdateBase
	ChatID     int64   `json:"chat_id"`
	User       User    `json:"user"`
	UserLocale *string `json:"user_locale,omitempty"`
}
```

- [ ] **Step 4: Добавить `SetDialogStopped`**

В `internal/store/dialogs.go` следом за `SetDialogStarted`:

```go
// SetDialogStopped помечает диалог остановленным (нажата кнопка «Остановить»).
// Возвращает true, если состояние изменилось: повторная остановка не должна
// порождать второй bot_stopped.
func (s *Store) SetDialogStopped(chatID int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE dialogs SET started = 0 WHERE chat_id = ? AND started = 1`, chatID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
```

- [ ] **Step 5: Добавить `ClientStop`**

В `internal/core/client.go` сразу после `ClientStart`:

```go
// ClientStop — клиент остановил бота в его настройках. Повторная остановка
// ничего не делает: выключенного бота нельзя выключить второй раз.
//
// Остановленный диалог не запрещает боту слать в него сообщения. Как ведёт
// себя живой Max — не измерено, а мок не выдумывает поведение: запрет
// появится, когда будет замер.
func (c *Core) ClientStop(chatID int64) error {
	d, bot, cl, err := c.dialogContext(chatID)
	if err != nil {
		return err
	}
	changed, err := c.store.SetDialogStopped(d.ChatID)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	upd := wire.BotStoppedUpdate{
		UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateBotStopped, Timestamp: nowMS()},
		ChatID:     d.ChatID,
		User:       clientUser(cl),
		UserLocale: wire.Ptr("ru"),
	}
	c.publish(events.KindDialog, bot.ID, d.ChatID,
		map[string]any{"chat_id": d.ChatID, "started": false},
		wire.UpdateBotStopped, upd)
	return nil
}
```

- [ ] **Step 6: Прогнать тесты**

Run: `go test ./internal/core/ ./internal/store/ ./internal/wire/`
Expected: PASS. Фейковый стенд в фикстуре сам сверяет тело `bot_stopped` с `openapi.MaxBotWebhook.yaml` — если структура разошлась со схемой, тест упадёт с текстом расхождения.

- [ ] **Step 7: Коммит**

```bash
git add internal/wire/wire.go internal/store/dialogs.go internal/core/client.go internal/core/core_test.go
git commit -m "feat: событие bot_stopped и возобновление диалога

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Действие `stop` и кнопка-переключатель в веб-чате

**Files:**
- Modify: `internal/controlapi/api.go:305-320` (switch в `dialogAction`)
- Modify: `web/static/chat.js:107-116` (`selectClient`), `web/static/chat.js:320-325` (обработчик кнопки)
- Modify: `web/static/chat.html:42`
- Test: `internal/httpserver/server_test.go`

**Interfaces:**
- Consumes: `(*core.Core).ClientStop` из задачи 1.
- Produces: действие `"stop"` у `POST /mock/api/dialogs/{chatId}/actions`; элемент `#start-btn`, который переключает подпись между «Начать» и «Остановить».

- [ ] **Step 1: Написать падающий тест**

В `internal/httpserver/server_test.go`:

```go
func TestDialogStopAction(t *testing.T) {
	f := newFixture(t)
	bot, chatID := f.seedBotAndClient(t)
	_ = bot

	f.postAction(t, chatID, `{"action":"start"}`, http.StatusOK)
	f.postAction(t, chatID, `{"action":"stop"}`, http.StatusOK)

	d, err := f.core.Store().DialogByChatID(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Started {
		t.Error("после действия stop диалог остался начатым")
	}
}
```

Если в фикстуре `internal/httpserver/server_test.go` ещё нет хелперов `seedBotAndClient` и `postAction`, добавить их там же:

```go
// seedBotAndClient заводит бота с клиентом и возвращает chat_id их диалога.
func (f *fixture) seedBotAndClient(t *testing.T) (*store.Bot, int64) {
	t.Helper()
	bot, err := f.core.Store().CreateBot("Бот поддержки", "support_bot", "демо")
	if err != nil {
		t.Fatal(err)
	}
	_, d, err := f.core.CreateClient(bot.ID, store.ClientInput{FirstName: "Иван"})
	if err != nil {
		t.Fatal(err)
	}
	return bot, d.ChatID
}

// postAction выполняет действие тестировщика и проверяет код ответа.
func (f *fixture) postAction(t *testing.T, chatID int64, body string, want int) []byte {
	t.Helper()
	resp, err := http.Post(
		f.srv.URL+"/mock/api/dialogs/"+strconv.FormatInt(chatID, 10)+"/actions",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("действие %s: статус %d, ожидался %d; тело %s", body, resp.StatusCode, want, out)
	}
	return out
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/httpserver/ -run TestDialogStopAction`
Expected: FAIL — действие `stop` неизвестно, статус 400 вместо 200

- [ ] **Step 3: Добавить ветку в control-API**

В `internal/controlapi/api.go`, в `switch req.Action` сразу после ветки `"start"`:

```go
	case "stop":
		err = a.core.ClientStop(chatID)
```

И в тексте ошибки для неизвестного действия дописать `stop` в перечень допустимых:

```go
		writeErr(w, http.StatusBadRequest, "mock.bad_request",
			"неизвестное действие "+strconv.Quote(req.Action)+
				"; допустимы start, stop, send, send_contact, send_location, press, edit, delete")
```

- [ ] **Step 4: Прогнать тест**

Run: `go test ./internal/httpserver/ -run TestDialogStopAction`
Expected: PASS

- [ ] **Step 5: Переключатель в разметке**

`web/static/chat.html`, строка 42 — убрать `hidden`, подпись выставляется скриптом:

```html
      <button type="button" id="start-btn" class="secondary">Начать</button>
```

- [ ] **Step 6: Переключатель в скрипте**

В `web/static/chat.js` заменить строку `$('start-btn').hidden = state.started;` в `selectClient` на вызов новой функции и добавить саму функцию рядом:

```js
// renderStartButton держит подпись кнопки в согласии с состоянием диалога.
// Кнопка не прячется после первого нажатия: bot_started приходит и при
// возобновлении общения, а проверять это нужно, не заводя нового клиента.
function renderStartButton() {
  const btn = $('start-btn');
  btn.textContent = state.started ? 'Остановить' : 'Начать';
  btn.title = state.started
    ? 'Остановить бота в настройках — уйдёт bot_stopped'
    : 'Начать разговор — уйдёт bot_started';
}
```

Вызов в `selectClient` вместо прежней строки:

```js
  renderStartButton();
```

И обработчик вместо прежнего:

```js
$('start-btn').onclick = async () => {
  await act({action: state.started ? 'stop' : 'start'});
  // Состояние берётся из ответа сервера, а не инвертируется на месте:
  // act() гасит ошибку алертом и наружу её не отдаёт, так что по факту
  // вызова судить об успехе нельзя, а подпись кнопки — единственный
  // постоянный признак того, начат диалог или нет.
  //
  // Обновление списка прикрыто отдельно: подпись обязана быть перерисована
  // даже когда список не доехал, иначе она замрёт на значении до клика —
  // ровно то расхождение, ради которого состояние и берётся с сервера.
  try {
    await loadClients();
  } catch (err) {
    alert('Список клиентов не обновлён: ' + err.message);
  }
  const client = currentClient();
  state.started = client ? client.started : false;
  renderStartButton();
};
```

- [ ] **Step 7: Проверить руками**

Run: `make run`, открыть `/mock`, завести бота и клиента, зайти в чат.
Expected: кнопка показывает «Начать»; после нажатия — «Остановить», бейдж «не начат» пропал; после второго нажатия бейдж вернулся, а кнопка снова «Начать». На вкладке «Доставки» видны `bot_started` и `bot_stopped`.

- [ ] **Step 8: Коммит**

```bash
git add internal/controlapi/api.go internal/httpserver/server_test.go web/static/chat.html web/static/chat.js
git commit -m "feat: действие stop и кнопка-переключатель в веб-чате

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `chat_id` в журналах

**Files:**
- Modify: `internal/store/schema.sql:83-113`
- Modify: `internal/store/store.go:61-70` (список `migrations`)
- Modify: `internal/store/logs.go` (структуры, `LogRequest`, `LogDelivery`, `ListRequestLog`, `ListDeliveries`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: поля `RequestLogEntry.ChatID *int64` и `DeliveryEntry.ChatID *int64` (JSON — `chat_id`, опускается при `nil`). Задачи 4, 5 и 7 заполняют и читают именно их.

**Ловушка, из-за которой ломаются существующие базы:** `store.Open` сначала выполняет `schema.sql` целиком, и только потом `migrate`. Значит `CREATE INDEX ... ON request_log(chat_id, …)` в `schema.sql` упадёт на старой базе — столбца там ещё нет. Индексы по новым столбцам ставятся **в список миграций, после `ALTER TABLE`**; `CREATE INDEX IF NOT EXISTS` идемпотентен и на свежей базе просто отработает вторым.

- [ ] **Step 1: Написать падающий тест**

В `internal/store/store_test.go`:

```go
func TestJournalsCarryChatID(t *testing.T) {
	s := openStore(t)
	chatID := int64(4242)

	req := &RequestLogEntry{Method: "POST", Path: "/messages", Status: 200, ChatID: &chatID}
	if err := s.LogRequest(req); err != nil {
		t.Fatal(err)
	}
	del := &DeliveryEntry{BotID: 1, URL: "https://stand", UpdateType: "message_created",
		Body: "{}", Attempt: 1, Status: 200, ChatID: &chatID}
	if err := s.LogDelivery(del); err != nil {
		t.Fatal(err)
	}

	reqs, err := s.ListRequestLog(nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[0].ChatID == nil || *reqs[0].ChatID != chatID {
		t.Fatalf("chat_id не сохранился в request_log: %+v", reqs)
	}

	dels, err := s.ListDeliveries(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dels) != 1 || dels[0].ChatID == nil || *dels[0].ChatID != chatID {
		t.Fatalf("chat_id не сохранился в webhook_deliveries: %+v", dels)
	}
}

// TestMigrationAddsChatIDToOldDatabase проверяет главное требование миграций:
// у стендов КЦ в базе живут боты с выданными токенами, и обновление мока не
// имеет права их потерять.
func TestMigrationAddsChatIDToOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := old.CreateBot("Бот", "bot", "")
	if err != nil {
		t.Fatal(err)
	}
	// Имитируем базу предыдущей версии: столбцов ещё нет. Индексы снимаются
	// первыми — SQLite не даёт удалить столбец, на который смотрит индекс.
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_request_log_chat_ts`,
		`DROP INDEX IF EXISTS idx_deliveries_chat_ts`,
		`ALTER TABLE request_log DROP COLUMN chat_id`,
		`ALTER TABLE webhook_deliveries DROP COLUMN chat_id`,
	} {
		if _, err := old.DB().Exec(stmt); err != nil {
			t.Fatalf("подготовка старой базы (%s): %v", stmt, err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(path)
	if err != nil {
		t.Fatalf("миграция старой базы не прошла: %v", err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })

	if _, err := upgraded.BotByID(bot.ID); err != nil {
		t.Fatalf("бот потерян при миграции: %v", err)
	}
	chatID := int64(7)
	if err := upgraded.LogRequest(&RequestLogEntry{Method: "GET", Path: "/me", Status: 200, ChatID: &chatID}); err != nil {
		t.Fatalf("запись в мигрированный журнал: %v", err)
	}
}
```

Если хелпера `openStore` в файле нет, добавить:

```go
func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
```

- [ ] **Step 2: Убедиться, что тест не компилируется**

Run: `go test ./internal/store/ -run 'TestJournalsCarryChatID|TestMigrationAddsChatID'`
Expected: FAIL — `unknown field ChatID in struct literal`

- [ ] **Step 3: Добавить столбцы в схему**

В `internal/store/schema.sql` — в `CREATE TABLE request_log` после `bot_id`:

```sql
    chat_id       INTEGER,
```

в `CREATE TABLE webhook_deliveries` после `bot_id`:

```sql
    chat_id         INTEGER,
```

Индексы по `chat_id` здесь **не создавать** — см. ловушку выше.

- [ ] **Step 4: Добавить миграции**

В `internal/store/store.go`, в конец списка `migrations`:

```go
	// Лента событий диалога: журналы жили в разрезе бота, и связать вызов
	// Bot API или доставку с конкретным диалогом было нечем.
	`ALTER TABLE request_log ADD COLUMN chat_id INTEGER`,
	`ALTER TABLE webhook_deliveries ADD COLUMN chat_id INTEGER`,
	// Индексы идут миграцией, а не schema.sql: схема выполняется до миграций,
	// и на старой базе индекс по ещё не добавленному столбцу упал бы.
	`CREATE INDEX IF NOT EXISTS idx_request_log_chat_ts ON request_log(chat_id, ts DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_deliveries_chat_ts ON webhook_deliveries(chat_id, ts DESC)`,
```

- [ ] **Step 5: Провести `chat_id` через структуры и запросы**

В `internal/store/logs.go` — поле в обеих структурах сразу после `BotID`:

```go
	// ChatID — диалог, к которому относится запись. nil у операций, которые
	// диалога не касаются: GET /me, PATCH /me/commands, подписки, загрузки.
	ChatID *int64 `json:"chat_id,omitempty"`
```

`LogRequest` — столбец и значение:

```go
	res, err := s.db.Exec(
		`INSERT INTO request_log (ts, bot_id, chat_id, method, path, query, status, request_body, response_body, latency_ms, error)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.TS, e.BotID, e.ChatID, e.Method, e.Path, e.Query, e.Status, e.RequestBody, e.ResponseBody, e.LatencyMS, e.Error)
```

`LogDelivery`:

```go
	res, err := s.db.Exec(
		`INSERT INTO webhook_deliveries (ts, bot_id, chat_id, subscription_id, url, update_type, body, attempt, status, error, duration_ms)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.TS, e.BotID, e.ChatID, e.SubscriptionID, e.URL, e.UpdateType, e.Body, e.Attempt, e.Status, e.Error, e.DurationMS)
```

`ListRequestLog` — столбец в выборке и разбор:

```go
	q := `SELECT id, ts, bot_id, chat_id, method, path, query, status, request_body, response_body, latency_ms, error
	      FROM request_log`
```

```go
		var e RequestLogEntry
		var bot, chat sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TS, &bot, &chat, &e.Method, &e.Path, &e.Query, &e.Status,
			&e.RequestBody, &e.ResponseBody, &e.LatencyMS, &e.Error); err != nil {
			return nil, err
		}
		if bot.Valid {
			e.BotID = &bot.Int64
		}
		if chat.Valid {
			e.ChatID = &chat.Int64
		}
```

`ListDeliveries` — так же:

```go
		`SELECT id, ts, bot_id, chat_id, subscription_id, url, update_type, body, attempt, status, error, duration_ms
		 FROM webhook_deliveries WHERE bot_id = ? ORDER BY id DESC LIMIT ?`, botID, limit)
```

```go
		var e DeliveryEntry
		var sub, chat sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TS, &e.BotID, &chat, &sub, &e.URL, &e.UpdateType, &e.Body,
			&e.Attempt, &e.Status, &e.Error, &e.DurationMS); err != nil {
			return nil, err
		}
		if sub.Valid {
			e.SubscriptionID = &sub.Int64
		}
		if chat.Valid {
			e.ChatID = &chat.Int64
		}
```

- [ ] **Step 6: Прогнать тесты**

Run: `go test ./internal/store/`
Expected: PASS, включая оба новых теста

- [ ] **Step 7: Коммит**

```bash
git add internal/store/schema.sql internal/store/store.go internal/store/logs.go internal/store/store_test.go
git commit -m "feat: chat_id в журналах вызовов и доставок

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Фасад проставляет диалог операции

**Files:**
- Modify: `internal/maxfacade/server.go:51-59` (`recorder`), `:203-219` (`finish`), обработчики операций
- Test: `internal/maxfacade/server_test.go`

**Interfaces:**
- Consumes: `store.RequestLogEntry.ChatID` из задачи 3.
- Produces: `noteChat(w http.ResponseWriter, chatID int64)` — внутренний хелпер пакета `maxfacade`.

- [ ] **Step 1: Написать падающий тест**

В `internal/maxfacade/server_test.go`:

```go
func TestRequestLogCarriesChatID(t *testing.T) {
	f := newFixture(t)

	// Операция с диалогом: chat_id обязан оказаться в журнале.
	f.do(t, http.MethodPost, "/messages?chat_id="+strconv.FormatInt(f.chatID, 10),
		`{"text":"привет","attachments":null,"link":null}`)
	// Операция без диалога: chat_id обязан остаться пустым.
	f.do(t, http.MethodGet, "/me", "")

	entries, err := f.store.ListRequestLog(&f.bot.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]*store.RequestLogEntry{}
	for i := range entries {
		byPath[entries[i].Path] = &entries[i]
	}

	send := byPath["/messages"]
	if send == nil || send.ChatID == nil || *send.ChatID != f.chatID {
		t.Errorf("POST /messages без chat_id в журнале: %+v", send)
	}
	if me := byPath["/me"]; me == nil || me.ChatID != nil {
		t.Errorf("GET /me диалога не касается, chat_id должен быть пуст: %+v", me)
	}
}

func TestAnswerLogCarriesChatIDFromCallback(t *testing.T) {
	f := newFixture(t)
	if err := f.core.ClientStart(f.chatID, ""); err != nil {
		t.Fatal(err)
	}
	// Клавиатуру отправляем через сам фасад: тест фасада должен ходить его
	// собственной поверхностью, а не звать ядро в обход.
	_, sentBody := f.do(t, http.MethodPost,
		"/messages?chat_id="+strconv.FormatInt(f.chatID, 10),
		`{"text":"нажмите","attachments":[{"type":"inline_keyboard","payload":{"buttons":[[{"type":"callback","text":"Привет","payload":"hello"}]]}}],"link":null}`)
	var sent wire.SendMessageResult
	if err := json.Unmarshal(sentBody, &sent); err != nil {
		t.Fatalf("ответ POST /messages не разобран: %v; тело %s", err, sentBody)
	}
	if err := f.core.ClientPressButton(f.chatID, sent.Message.Body.Mid, "hello"); err != nil {
		t.Fatal(err)
	}
	cb, err := f.store.LastCallback(f.bot.ID)
	if err != nil {
		t.Fatal(err)
	}

	f.do(t, http.MethodPost, "/answers?callback_id="+cb.CallbackID, `{}`)

	entries, err := f.store.ListRequestLog(&f.bot.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Path != "/answers" {
			continue
		}
		if e.ChatID == nil || *e.ChatID != f.chatID {
			t.Fatalf("POST /answers: диалог не выведен из callback_id: %+v", e)
		}
		return
	}
	t.Fatal("вызов /answers не попал в журнал")
}
```

Если `(*store.Store).LastCallback` нет, добавить в `internal/store/messages.go`:

```go
// LastCallback возвращает последнее нажатие кнопки у бота. Нужен тестам и
// диагностике: снаружи идентификатор нажатия виден только в событии.
func (s *Store) LastCallback(botID int64) (*Callback, error) {
	return scanCallback(s.db.QueryRow(
		`SELECT `+callbackColumns+` FROM callbacks WHERE bot_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`,
		botID))
}
```

(имена `callbackColumns` и `scanCallback` уже используются в `CallbackByID` — переиспользовать их.)

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/maxfacade/ -run 'ChatID'`
Expected: FAIL — `chat_id` в журнале пуст

- [ ] **Step 3: Завести ячейку в `recorder`**

В `internal/maxfacade/server.go` в структуру `recorder`:

```go
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
	// chatID — диалог операции, если она его касается. Заполняет обработчик:
	// см. noteChat.
	chatID *int64
}
```

И хелпер рядом с `newRecorder`:

```go
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
```

- [ ] **Step 4: Писать диалог в журнал**

В `finish`, при сборке `entry`:

```go
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
```

И в событие шины добавить диалог, чтобы лента обновлялась только у нужного чата:

```go
	ev := events.Event{Kind: events.KindRequest, BotID: botID, Payload: entry}
	if rec.chatID != nil {
		ev.ChatID = *rec.chatID
	}
	s.bus.Publish(ev)
```

- [ ] **Step 5: Проставить диалог в обработчиках**

`getMessages` — сразу после сборки фильтра:

```go
	if f.ChatID != nil {
		noteChat(w, *f.ChatID)
	}
```

`sendMessage` — после успешного вызова ядра (диалог мог быть найден по `user_id`, и достоверен он только в готовом сообщении):

```go
	if msg.Recipient.ChatID != nil {
		noteChat(w, *msg.Recipient.ChatID)
	}
```

`editMessage`, `deleteMessage` — после того, как `mid` извлечён из query, до вызова ядра (после удаления сообщение уже не найти):

```go
	if rec, err := s.store.MessageByMid(mid); err == nil {
		noteChat(w, rec.ChatID)
	}
```

`getMessageByID` — в начале, по параметру пути:

```go
	if rec, err := s.store.MessageByMid(mid); err == nil {
		noteChat(w, rec.ChatID)
	}
```

`answerOnCallback` — после извлечения `callbackID`, до вызова ядра:

```go
	if cb, err := s.store.CallbackByID(callbackID); err == nil {
		noteChat(w, cb.ChatID)
	}
```

Остальные операции (`getMyInfo`, `editMyCommands`, подписки, `getUploadUrl`) диалога не касаются и `noteChat` не зовут — это и даёт им пустой `chat_id`.

- [ ] **Step 6: Прогнать тесты**

Run: `go test ./internal/maxfacade/ ./internal/store/`
Expected: PASS

- [ ] **Step 7: Коммит**

```bash
git add internal/maxfacade/server.go internal/maxfacade/server_test.go internal/store/messages.go
git commit -m "feat: фасад проставляет диалог операции в журнал

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Диспетчер пишет диалог и недоставленные события

**Files:**
- Modify: `internal/webhook/dispatcher.go:89-146` (`Deliver`, `logRejected`, `process`, `deliverTo`)
- Test: `internal/webhook/dispatcher_test.go`

**Interfaces:**
- Consumes: `store.DeliveryEntry.ChatID` из задачи 3.
- Produces: запись в `webhook_deliveries` с `URL == ""`, `Status == 0`, `Attempt == 0` и `Error == "нет подписки на этот тип события"` для каждого события, которое никуда не ушло.

- [ ] **Step 1: Написать падающий тест**

В `internal/webhook/dispatcher_test.go`:

```go
// TestSkippedEventIsLogged — событие, на тип которого стенд не подписан, не
// должно пропадать бесследно: иначе «мок не отправил» неотличимо от
// «стенд не подписан», и разбор упирается в тупик.
func TestSkippedEventIsLogged(t *testing.T) {
	d, st, botID := newDispatcherFixture(t)
	if _, err := st.AddSubscription(botID, "https://stand.test/hook",
		[]string{"message_created"}, ""); err != nil {
		t.Fatal(err)
	}

	// bot_started, а не message_edited: тело простое, и тест проверяет
	// отсутствие подписки, а не форму события. Deliver валидирует тело по
	// контракту синхронно, поэтому лишние поля здесь только мешали бы.
	chatID := int64(99)
	if err := d.Deliver(botID, chatID, "bot_started", map[string]any{
		"update_type": "bot_started",
		"timestamp":   int64(1),
		"chat_id":     chatID,
		"user": map[string]any{
			"user_id":            int64(5),
			"first_name":         "Иван",
			"is_bot":             false,
			"last_activity_time": int64(1),
		},
	}); err != nil {
		t.Fatal(err)
	}
	d.Wait()

	entries, err := st.ListDeliveries(botID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("записей о доставке: %d, ожидалась одна", len(entries))
	}
	e := entries[0]
	if e.URL != "" || e.Status != 0 {
		t.Errorf("недоставленное событие записано как отправленное: %+v", e)
	}
	if !strings.Contains(e.Error, "нет подписки") {
		t.Errorf("причина не записана: %q", e.Error)
	}
	if e.ChatID == nil || *e.ChatID != chatID {
		t.Errorf("chat_id в записи о недоставке: %+v", e.ChatID)
	}
}
```

Фикстуру `newDispatcherFixture` взять из уже существующих тестов файла; если её нет — собрать по образцу `internal/core/core_test.go:newFixture` (store в `t.TempDir()`, `specs.Load()`, `events.New()`, `webhook.New` с `Retries: 0`), возвращая диспетчер, хранилище и `bot.ID`.

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/webhook/ -run TestSkippedEventIsLogged`
Expected: FAIL — записей о доставке 0, ожидалась одна

- [ ] **Step 3: Провести диалог в записи о доставке**

В `deliverTo`, при сборке `entry`:

```go
		entry := &store.DeliveryEntry{
			BotID: j.botID, ChatID: &j.chatID, SubscriptionID: &sub.ID, URL: sub.URL,
			UpdateType: j.updateType, Body: string(j.body),
			Attempt: attempt, Status: status, DurationMS: dur.Milliseconds(),
		}
```

- [ ] **Step 4: Записывать недоставленное**

`process` получает признак того, что событие никому не ушло:

```go
func (d *Dispatcher) process(j job) {
	subs, err := d.store.ListSubscriptions(j.botID)
	if err != nil {
		return
	}
	delivered := false
	for _, sub := range subs {
		if !sub.Wants(j.updateType) {
			continue
		}
		delivered = true
		d.deliverTo(sub, j)
	}
	if !delivered {
		d.logSkipped(j)
	}
}

// logSkipped фиксирует событие, которое сгенерировано, но никуда не ушло:
// подписки на этот тип нет либо подписок нет вовсе. Без такой записи событие
// пропадает бесследно, и «мок не отправил» выглядит неотличимо от «стенд не
// подписан» — на этом разбор интеграции и застревает.
func (d *Dispatcher) logSkipped(j job) {
	e := &store.DeliveryEntry{
		BotID: j.botID, ChatID: &j.chatID, UpdateType: j.updateType,
		Body: string(j.body), Error: "нет подписки на этот тип события",
	}
	if err := d.store.LogDelivery(e); err != nil {
		return
	}
	d.bus.Publish(events.Event{
		Kind: events.KindDelivery, BotID: j.botID, ChatID: j.chatID, Payload: e,
	})
}
```

- [ ] **Step 5: Провести диалог в запись об отказе валидации**

`logRejected` вызывается из `Deliver`, где `chatID` есть, — добавить параметр и поле:

```go
// logRejected фиксирует отказ от доставки невалидного события.
func (d *Dispatcher) logRejected(botID, chatID int64, updateType string, body []byte, cause error) {
	e := &store.DeliveryEntry{
		BotID: botID, ChatID: &chatID, URL: "", UpdateType: updateType, Body: string(body),
```

(остальное тело метода не меняется). В `Deliver` — вызов с новым аргументом:

```go
		d.logRejected(botID, chatID, updateType, body, err)
```

- [ ] **Step 6: Прогнать тесты**

Run: `go test ./internal/webhook/ ./internal/core/`
Expected: PASS

- [ ] **Step 7: Коммит**

```bash
git add internal/webhook/dispatcher.go internal/webhook/dispatcher_test.go
git commit -m "feat: журнал доставок знает диалог и недоставленные события

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Журнал действий тестировщика

**Files:**
- Modify: `internal/store/schema.sql` (новая таблица), `internal/store/logs.go` (структура, запись, чтение, `Purge`)
- Modify: `internal/events/bus.go:15-23`
- Modify: `internal/controlapi/api.go` (`dialogAction`, `createClient`)
- Test: `internal/httpserver/server_test.go`

**Interfaces:**
- Produces: `store.UIActionEntry`, `(*store.Store).LogUIAction(*UIActionEntry) error`, `(*store.Store).ListUIActions(chatID int64, limit int) ([]UIActionEntry, error)`, `events.KindUIAction == "ui_action"`.

- [ ] **Step 1: Написать падающий тест**

В `internal/httpserver/server_test.go`:

```go
func TestUIActionsAreJournalled(t *testing.T) {
	f := newFixture(t)
	_, chatID := f.seedBotAndClient(t)

	f.postAction(t, chatID, `{"action":"start"}`, http.StatusOK)
	f.postAction(t, chatID, `{"action":"send","text":"/start"}`, http.StatusOK)
	// Неудачное действие тоже обязано попасть в журнал: «нажал, ничего не
	// произошло» — самая частая жалоба, и причина должна быть видна.
	f.postAction(t, chatID, `{"action":"press","mid":"mid.нет","payload":"hello"}`, http.StatusNotFound)

	entries, err := f.core.Store().ListUIActions(chatID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("записей о действиях: %d, ожидалось 3: %+v", len(entries), entries)
	}
	// Новые первыми.
	if entries[0].Action != "press" || entries[0].Error == "" {
		t.Errorf("неудачное действие записано без причины: %+v", entries[0])
	}
	if entries[1].Action != "send" || !strings.Contains(entries[1].Detail, "/start") {
		t.Errorf("параметры действия не сохранены: %+v", entries[1])
	}
	if entries[2].Action != "start" {
		t.Errorf("порядок записей нарушен: %+v", entries)
	}
}
```

- [ ] **Step 2: Убедиться, что тест не компилируется**

Run: `go test ./internal/httpserver/ -run TestUIActionsAreJournalled`
Expected: FAIL — `ListUIActions undefined`

- [ ] **Step 3: Завести таблицу**

В конец `internal/store/schema.sql`:

```sql
-- Действия тестировщика в веб-чате. Отдельная таблица, а не общая с журналом
-- вызовов: у действия нет ни метода с путём, ни url с попыткой, и половина
-- чужих столбцов стояла бы пустой.
CREATE TABLE IF NOT EXISTS ui_actions (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    ts      INTEGER NOT NULL,
    bot_id  INTEGER NOT NULL,
    chat_id INTEGER NOT NULL,
    action  TEXT    NOT NULL,
    detail  TEXT    NOT NULL DEFAULT '',
    error   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ui_actions_chat_ts ON ui_actions(chat_id, ts DESC);
```

Новая таблица идёт именно в `schema.sql`: `CREATE TABLE IF NOT EXISTS` безопасен и для свежей базы, и для старой, — в отличие от индекса по новому столбцу из задачи 3.

- [ ] **Step 4: Структура, запись и чтение**

В `internal/store/logs.go`:

```go
// UIActionEntry — запись журнала действий тестировщика в веб-чате.
// Detail хранит тело запроса control-API как есть: лента должна показывать,
// что тестировщик на самом деле отправил, а не то, во что мок это превратил.
type UIActionEntry struct {
	ID     int64  `json:"id"`
	TS     int64  `json:"ts"`
	BotID  int64  `json:"bot_id"`
	ChatID int64  `json:"chat_id"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// LogUIAction сохраняет действие тестировщика.
func (s *Store) LogUIAction(e *UIActionEntry) error {
	if e.TS == 0 {
		e.TS = nowMS()
	}
	res, err := s.db.Exec(
		`INSERT INTO ui_actions (ts, bot_id, chat_id, action, detail, error) VALUES (?,?,?,?,?,?)`,
		e.TS, e.BotID, e.ChatID, e.Action, e.Detail, e.Error)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// ListUIActions возвращает последние действия в диалоге, новые первыми.
func (s *Store) ListUIActions(chatID int64, limit int) ([]UIActionEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, ts, bot_id, chat_id, action, detail, error
		 FROM ui_actions WHERE chat_id = ? ORDER BY id DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UIActionEntry{}
	for rows.Next() {
		var e UIActionEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.BotID, &e.ChatID, &e.Action, &e.Detail, &e.Error); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
```

И в `Purge` — третья таблица, иначе долгоживущий мок распухнет:

```go
	for _, table := range []string{"request_log", "webhook_deliveries", "ui_actions"} {
```

- [ ] **Step 5: Новый вид события шины**

В `internal/events/bus.go`, в блок констант:

```go
	KindUIAction       = "ui_action"       // действие тестировщика в веб-чате
```

- [ ] **Step 6: Писать действия из control-API**

В `internal/controlapi/api.go` добавить хелпер рядом с `dialogAction`:

```go
// logAction записывает действие тестировщика в журнал диалога. botID
// приходит от вызывающего — диалог к этому моменту уже резолвлен
// (в dialogAction — до вызова ядра, в createClient — при поиске бота),
// повторный SELECT на каждое действие не нужен.
//
// Пишется и неудачное действие, с причиной: «нажал, ничего не произошло» —
// самая частая жалоба, и в ленте она должна стоять рядом с объяснением.
// Сбой самого журнала действие не отменяет: журнал — диагностика, а не
// часть операции.
func (a *API) logAction(botID, chatID int64, action string, detail []byte, actionErr error) {
	e := &store.UIActionEntry{
		BotID: botID, ChatID: chatID, Action: action, Detail: string(detail),
	}
	if actionErr != nil {
		e.Error = actionErr.Error()
	}
	if err := a.core.Store().LogUIAction(e); err != nil {
		return
	}
	a.bus.Publish(events.Event{
		Kind: events.KindUIAction, BotID: botID, ChatID: chatID, Payload: e,
	})
}
```

Диалог резолвится **один раз**, в начале `dialogAction`, а не внутри `logAction`:
если резолвить его заново на записи журнала, то при несуществующем `chat_id`
основной вызов уже упадёт с `core.ErrChatNotFound`, а повторный резолв внутри
`logAction` наткнётся на ту же нехватку диалога и тихо откажется писать —
получится, что именно неудачное действие (самый нужный случай для журнала) не
попадёт в журнал. Резолв даёт готовый `botID` для `logAction` и заодно убирает
лишний `SELECT` на каждое действие.

Ловушка: `DialogByChatID` при отсутствии диалога возвращает `store.ErrNotFound`,
а `fail` превращает его в 404 с кодом `mock.not_found` — тогда как контракт
control-API для несуществующего чата всегда отвечал 404 с кодом
`mock.chat.not_found` (через `core.ErrChatNotFound`). Ошибку нужно явно
перевести, иначе код ответа тихо поменяется:

```go
	d, err := a.core.Store().DialogByChatID(chatID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = core.ErrChatNotFound
		}
		fail(w, err)
		return
	}
```

Переписать `dialogAction` так, чтобы у всех веток был один выход — иначе журнал придётся звать из семи мест:

```go
func (a *API) dialogAction(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathInt64(r, "chatId")
	if !ok {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "некорректный chat_id")
		return
	}
	d, err := a.core.Store().DialogByChatID(chatID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = core.ErrChatNotFound
		}
		fail(w, err)
		return
	}
	// Тело читаем целиком: оно уходит в журнал как есть.
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "тело запроса не прочитано: "+err.Error())
		return
	}
	var req struct {
		Action      string   `json:"action"`
		Text        string   `json:"text"`
		Mid         string   `json:"mid"`
		Payload     string   `json:"payload"`
		Attachments []string `json:"attachments"`
		// Координаты действия send_location; не заданы — берутся из карточки.
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "тело запроса не разобрано: "+err.Error())
		return
	}

	var (
		msg    *wire.Message
		actErr error
	)
	switch req.Action {
	case "start":
		actErr = a.core.ClientStart(chatID, req.Payload)
	case "stop":
		actErr = a.core.ClientStop(chatID)
	case "send":
		msg, actErr = a.core.ClientSendMessage(chatID, req.Text, req.Attachments)
	case "send_contact":
		msg, actErr = a.core.ClientSendContact(chatID)
	case "send_location":
		msg, actErr = a.core.ClientSendLocation(chatID, req.Latitude, req.Longitude)
	case "press":
		actErr = a.core.ClientPressButton(chatID, req.Mid, req.Payload)
	case "edit":
		actErr = a.core.ClientEditMessage(chatID, req.Mid, req.Text)
	case "delete":
		actErr = a.core.ClientDeleteMessage(chatID, req.Mid)
	default:
		writeErr(w, http.StatusBadRequest, "mock.bad_request",
			"неизвестное действие "+strconv.Quote(req.Action)+
				"; допустимы start, stop, send, send_contact, send_location, press, edit, delete")
		return
	}

	a.logAction(d.BotID, chatID, req.Action, raw, actErr)

	if actErr != nil {
		fail(w, actErr)
		return
	}
	if msg != nil {
		writeJSON(w, http.StatusOK, msg)
		return
	}
	writeJSON(w, http.StatusOK, wire.SimpleQueryResult{Success: true})
}
```

Добавить `"io"` в импорты пакета.

В `createClient` бот уже резолвлен раньше (`bot, ok := a.bot(w, r)`) — его и
передать в `logAction`, без повторного поиска. Сразу после успешного
создания клиента — запись о его заведении, чтобы лента диалога начиналась с
появления клиента:

```go
	a.logAction(bot.ID, d.ChatID, "create_client", nil, nil)
```

(`bot`, `d` — уже резолвленный бот и созданный диалог; если переменные называются иначе, использовать их имена.)

- [ ] **Step 7: Прогнать тесты**

Run: `go test ./internal/store/ ./internal/httpserver/ ./internal/controlapi/`
Expected: PASS

- [ ] **Step 8: Коммит**

```bash
git add internal/store/schema.sql internal/store/logs.go internal/events/bus.go internal/controlapi/api.go internal/httpserver/server_test.go
git commit -m "feat: журнал действий тестировщика в веб-чате

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Лента событий диалога

**Files:**
- Create: `internal/store/feed.go`
- Modify: `internal/controlapi/api.go:33-51` (маршруты), новый обработчик
- Test: `internal/store/feed_test.go`

**Interfaces:**
- Consumes: `UIActionEntry`, `DeliveryEntry`, `RequestLogEntry` с `chat_id` из задач 3 и 6.
- Produces: `store.DialogEvent`, `(*store.Store).ListDialogEvents(botID, chatID int64, limit int) ([]DialogEvent, error)`, эндпоинт `GET /mock/api/dialogs/{chatId}/events?limit=`.

- [ ] **Step 1: Написать падающий тест**

Создать `internal/store/feed_test.go`:

```go
package store

import "testing"

func TestListDialogEventsMergesThreeSources(t *testing.T) {
	s := openStore(t)
	const botID, chatID, otherChat = int64(1), int64(10), int64(20)

	mustLog := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustLog(s.LogUIAction(&UIActionEntry{TS: 100, BotID: botID, ChatID: chatID, Action: "start"}))
	mustLog(s.LogDelivery(&DeliveryEntry{TS: 101, BotID: botID, ChatID: &[]int64{chatID}[0],
		URL: "https://stand", UpdateType: "bot_started", Body: "{}", Attempt: 1, Status: 200}))
	mustLog(s.LogRequest(&RequestLogEntry{TS: 102, BotID: &[]int64{botID}[0], ChatID: &[]int64{chatID}[0],
		Method: "POST", Path: "/messages", Status: 200}))
	// Вызов без диалога того же бота обязан попасть в ленту: PATCH
	// /me/commands наполняет меню команд, и смотрят на него отсюда.
	mustLog(s.LogRequest(&RequestLogEntry{TS: 103, BotID: &[]int64{botID}[0],
		Method: "PATCH", Path: "/me/commands", Status: 200}))
	// Чужой диалог в ленту попадать не должен.
	mustLog(s.LogUIAction(&UIActionEntry{TS: 104, BotID: botID, ChatID: otherChat, Action: "send"}))

	feed, err := s.ListDialogEvents(botID, chatID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 4 {
		t.Fatalf("в ленте %d записей, ожидалось 4: %+v", len(feed), feed)
	}
	// Новые первыми.
	wantKinds := []string{"request", "request", "delivery", "ui"}
	for i, want := range wantKinds {
		if feed[i].Kind != want {
			t.Errorf("запись %d: вид %q, ожидался %q", i, feed[i].Kind, want)
		}
	}
	if feed[0].Request == nil || feed[0].Request.Path != "/me/commands" {
		t.Errorf("вызов без диалога не подмешан в ленту: %+v", feed[0])
	}
	if feed[3].UI == nil || feed[3].UI.Action != "start" {
		t.Errorf("действие тестировщика потеряно: %+v", feed[3])
	}
}

func TestListDialogEventsRespectsLimit(t *testing.T) {
	s := openStore(t)
	const botID, chatID = int64(1), int64(10)
	for i := 0; i < 10; i++ {
		if err := s.LogUIAction(&UIActionEntry{
			TS: int64(100 + i), BotID: botID, ChatID: chatID, Action: "send"}); err != nil {
			t.Fatal(err)
		}
	}
	feed, err := s.ListDialogEvents(botID, chatID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 3 {
		t.Fatalf("предел не соблюдён: %d записей", len(feed))
	}
	if feed[0].UI.TS != 109 {
		t.Errorf("обрезаны не самые старые записи: первая ts %d", feed[0].UI.TS)
	}
}
```

- [ ] **Step 2: Убедиться, что тест не компилируется**

Run: `go test ./internal/store/ -run TestListDialogEvents`
Expected: FAIL — `s.ListDialogEvents undefined`

- [ ] **Step 3: Реализовать ленту**

Создать `internal/store/feed.go`:

```go
package store

import "sort"

// DialogEvent — строка ленты событий диалога. Заполнен ровно один указатель,
// какой именно — говорит Kind.
//
// Три источника не сведены в одну таблицу намеренно: у вызова Bot API это
// метод, путь, статус и латентность, у доставки — url, тип события и номер
// попытки, у действия тестировщика — свои параметры. Общая схема либо
// потеряла бы типизацию, либо превратила бы одно поле в свалку; слияние
// двух-трёх сотен записей по времени стоит дешевле.
type DialogEvent struct {
	Kind     string           `json:"kind"` // ui | delivery | request
	TS       int64            `json:"ts"`
	UI       *UIActionEntry   `json:"ui,omitempty"`
	Delivery *DeliveryEntry   `json:"delivery,omitempty"`
	Request  *RequestLogEntry `json:"request,omitempty"`
}

// Порядок видов внутри одной миллисекунды: действие тестировщика порождает
// доставку, доставка — вызов бота. Лента идёт от новых к старым, поэтому
// больший ранг оказывается выше.
var feedRank = map[string]int{"ui": 1, "delivery": 2, "request": 3}

// ListDialogEvents собирает ленту диалога: действия тестировщика, доставки
// событий на стенд и вызовы Bot API от бота — в общей хронологии.
//
// Вызовы бота без диалога (GET /me, PATCH /me/commands, подписки) берутся у
// того же бота: относятся они ко всему боту, но смотрят на них отсюда.
func (s *Store) ListDialogEvents(botID, chatID int64, limit int) ([]DialogEvent, error) {
	if limit <= 0 {
		limit = 200
	}

	actions, err := s.ListUIActions(chatID, limit)
	if err != nil {
		return nil, err
	}
	deliveries, err := s.listDialogDeliveries(chatID, limit)
	if err != nil {
		return nil, err
	}
	requests, err := s.listDialogRequests(botID, chatID, limit)
	if err != nil {
		return nil, err
	}

	out := make([]DialogEvent, 0, len(actions)+len(deliveries)+len(requests))
	for i := range actions {
		out = append(out, DialogEvent{Kind: "ui", TS: actions[i].TS, UI: &actions[i]})
	}
	for i := range deliveries {
		out = append(out, DialogEvent{Kind: "delivery", TS: deliveries[i].TS, Delivery: &deliveries[i]})
	}
	for i := range requests {
		out = append(out, DialogEvent{Kind: "request", TS: requests[i].TS, Request: &requests[i]})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TS != out[j].TS {
			return out[i].TS > out[j].TS
		}
		return feedRank[out[i].Kind] > feedRank[out[j].Kind]
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// listDialogDeliveries — доставки, привязанные к диалогу.
func (s *Store) listDialogDeliveries(chatID int64, limit int) ([]DeliveryEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, ts, bot_id, chat_id, subscription_id, url, update_type, body, attempt, status, error, duration_ms
		 FROM webhook_deliveries WHERE chat_id = ? ORDER BY id DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// listDialogRequests — вызовы бота по этому диалогу плюс те, что диалога не
// касаются вовсе.
func (s *Store) listDialogRequests(botID, chatID int64, limit int) ([]RequestLogEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, ts, bot_id, chat_id, method, path, query, status, request_body, response_body, latency_ms, error
		 FROM request_log
		 WHERE bot_id = ? AND (chat_id = ? OR chat_id IS NULL)
		 ORDER BY id DESC LIMIT ?`, botID, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}
```

Разбор строк вынести из `ListDeliveries` и `ListRequestLog` в `internal/store/logs.go`, чтобы одинаковый скан не жил в двух местах:

```go
// scanDeliveries разбирает выборку журнала доставок.
func scanDeliveries(rows *sql.Rows) ([]DeliveryEntry, error) {
	out := []DeliveryEntry{}
	for rows.Next() {
		var e DeliveryEntry
		var sub, chat sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TS, &e.BotID, &chat, &sub, &e.URL, &e.UpdateType, &e.Body,
			&e.Attempt, &e.Status, &e.Error, &e.DurationMS); err != nil {
			return nil, err
		}
		if sub.Valid {
			e.SubscriptionID = &sub.Int64
		}
		if chat.Valid {
			e.ChatID = &chat.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// scanRequests разбирает выборку журнала вызовов Bot API.
func scanRequests(rows *sql.Rows) ([]RequestLogEntry, error) {
	out := []RequestLogEntry{}
	for rows.Next() {
		var e RequestLogEntry
		var bot, chat sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TS, &bot, &chat, &e.Method, &e.Path, &e.Query, &e.Status,
			&e.RequestBody, &e.ResponseBody, &e.LatencyMS, &e.Error); err != nil {
			return nil, err
		}
		if bot.Valid {
			e.BotID = &bot.Int64
		}
		if chat.Valid {
			e.ChatID = &chat.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
```

`ListRequestLog` и `ListDeliveries` после этого заканчиваются `return scanRequests(rows)` и `return scanDeliveries(rows)`.

- [ ] **Step 4: Прогнать тест хранилища**

Run: `go test ./internal/store/`
Expected: PASS

- [ ] **Step 5: Открыть ленту наружу**

В `internal/controlapi/api.go` — маршрут рядом с `dialogMessages`:

```go
	mux.HandleFunc("GET /mock/api/dialogs/{chatId}/events", a.dialogEvents)
```

И обработчик:

```go
func (a *API) dialogEvents(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathInt64(r, "chatId")
	if !ok {
		writeErr(w, http.StatusBadRequest, "mock.bad_request", "некорректный chat_id")
		return
	}
	d, err := a.core.Store().DialogByChatID(chatID)
	if err != nil {
		fail(w, err)
		return
	}
	list, err := a.core.Store().ListDialogEvents(d.BotID, chatID, queryInt(r, "limit", 200))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
```

- [ ] **Step 6: Проверить эндпоинт тестом**

В `internal/httpserver/server_test.go`:

```go
func TestDialogEventsEndpoint(t *testing.T) {
	f := newFixture(t)
	_, chatID := f.seedBotAndClient(t)
	f.postAction(t, chatID, `{"action":"start"}`, http.StatusOK)

	resp, err := http.Get(f.srv.URL + "/mock/api/dialogs/" + strconv.FormatInt(chatID, 10) + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("статус %d", resp.StatusCode)
	}
	var feed []store.DialogEvent
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		t.Fatal(err)
	}
	if len(feed) == 0 {
		t.Fatal("лента пуста, ожидалось действие start")
	}
	if feed[len(feed)-1].Kind != "ui" || feed[len(feed)-1].UI.Action != "start" {
		t.Errorf("первым в диалоге ожидалось действие start: %+v", feed[len(feed)-1])
	}
}
```

Run: `go test ./internal/httpserver/ -run TestDialogEventsEndpoint`
Expected: PASS

- [ ] **Step 7: Коммит**

```bash
git add internal/store/feed.go internal/store/feed_test.go internal/store/logs.go internal/controlapi/api.go internal/httpserver/server_test.go
git commit -m "feat: лента событий диалога

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Вкладка «События» в веб-чате

**Files:**
- Modify: `web/static/chat.html:30-39`
- Modify: `web/static/chat.js:362-448` (панели, отрисовка, вкладки, WebSocket)

**Interfaces:**
- Consumes: `GET /mock/api/dialogs/{chatId}/events` из задачи 7; виды событий шины `ui_action`, `delivery`, `request`.

- [ ] **Step 1: Добавить вкладку и панель в разметку**

`web/static/chat.html` — кнопка второй в списке вкладок:

```html
      <div class="tabs">
        <button data-tab="chat" class="active">Чат</button>
        <button data-tab="events">События</button>
        <button data-tab="log">Журнал</button>
        <button data-tab="deliveries">Доставки</button>
      </div>
```

И панель рядом с существующими:

```html
    <div id="events-panel" class="log-panel"></div>
```

- [ ] **Step 2: Отрисовка ленты**

В `web/static/chat.js` рядом с `renderLog` добавить:

```js
// Подписи действий тестировщика. В журнале лежит имя действия control-API —
// человеку нужна фраза, а не «send_location».
const UI_ACTION_LABELS = {
  create_client: 'клиент создан',
  start: 'нажал «Начать»',
  stop: 'нажал «Остановить»',
  send: 'отправил сообщение',
  send_contact: 'поделился контактом',
  send_location: 'отправил геопозицию',
  press: 'нажал кнопку',
  edit: 'отредактировал сообщение',
  delete: 'удалил сообщение',
};

// renderEvents рисует ленту диалога: действие тестировщика, отправку события
// на стенд и вызов Bot API от бота — в одной хронологии. Порознь их читать
// нельзя: причина и следствие оказываются на разных вкладках.
function renderEvents(box, entries) {
  box.textContent = '';
  if (!entries.length) {
    box.append(el('div', {className: 'empty', textContent: 'Пока пусто.'}));
    return;
  }
  for (const e of entries) {
    box.append(eventRow(e));
  }
}

function eventRow(e) {
  if (e.kind === 'ui') {
    const ui = e.ui;
    return feedRow('UI', ui.error ? 0 : 200,
      UI_ACTION_LABELS[ui.action] || ui.action, [ui.error, ui.detail], e.ts);
  }
  if (e.kind === 'delivery') {
    const d = e.delivery;
    // url пуст — событие никуда не ушло: нет подписки или отказ валидации.
    const target = d.url || 'не отправлено';
    const attempt = d.attempt ? ` (попытка ${d.attempt})` : '';
    return feedRow('→ стенд', d.status, `${d.update_type} → ${target}${attempt}`,
      [d.error, d.body], e.ts);
  }
  const q = e.request;
  const scope = q.chat_id ? '' : ' · без диалога';
  return feedRow('← бот', q.status,
    `${q.method} ${q.path}${q.query ? '?' + q.query : ''}${scope}`,
    [q.request_body, q.response_body], e.ts);
}

function feedRow(direction, code, title, payloads, ts) {
  const row = el('div', {className: 'log-entry'}, [
    el('div', {className: 'row'}, [
      el('span', {className: 'badge', textContent: direction}),
      el('span', {className: 'badge ' + (code >= 200 && code < 400 ? 'ok' : 'err'),
                  textContent: code ? String(code) : '—'}),
      el('code', {textContent: title}),
      el('span', {className: 'muted', textContent: timeOf(ts)}),
    ]),
  ]);
  for (const payload of payloads) {
    if (payload) row.append(el('pre', {textContent: pretty(payload)}));
  }
  return row;
}
```

- [ ] **Step 3: Подключить панель**

В объект `panels` добавить вторым ключом:

```js
  events: async () => {
    const box = $('events-panel');
    // Диалога ещё нет (свежий бот без клиентов) — /dialogs/null/events только
    // упадёт запросом. Показываем ту же фразу, что и заголовок диалога до
    // выбора клиента, а не тишину с необработанной ошибкой.
    if (state.chatID === null) {
      box.textContent = '';
      box.append(el('div', {className: 'empty', textContent: 'Выберите клиента.'}));
      return;
    }
    const entries = await getJSON(`/mock/api/dialogs/${state.chatID}/events?limit=200`);
    renderEvents(box, entries);
  },
```

В обработчике вкладок — переключение новой панели:

```js
    $('events-panel').classList.toggle('active', activeTab === 'events');
```

- [ ] **Step 4: Живое обновление**

В `ws.onmessage` — обновлять ленту на любое из трёх событий её источников:

```js
    // chat_id у события шины помечен omitempty: у вызовов бота вне диалога
    // (GET /me, PATCH /me/commands, подписки, загрузки) его нет вовсе, и
    // event.chat_id читается как undefined. Лента задачи 7 показывает такие
    // вызовы в каждом диалоге бота (chat_id IS NULL в SQL) — значит и живьём
    // они обязаны обновлять ленту независимо от выбранного диалога. Событие
    // с chat_id по-прежнему обновляет только свой диалог: чужой чат
    // перерисовывать не нужно.
    if (activeTab === 'events' && ['ui_action', 'delivery', 'request'].includes(event.kind) &&
        (event.chat_id === undefined || event.chat_id === state.chatID)) {
      await panels.events();
    }
```

- [ ] **Step 5: Проверить руками**

Run: `make run`, открыть чат, нажать «Начать», написать сообщение, нажать инлайн-кнопку из ответа бота.
Expected: на вкладке «События» строки идут сверху вниз от новых к старым; у каждого действия тестировщика есть строка `UI`, у каждого события — строка `→ стенд`, у каждого вызова бота — `← бот` с телом запроса. Событие, на которое стенд не подписан, показано с прочерком вместо кода и пояснением «нет подписки на этот тип события».

- [ ] **Step 6: Коммит**

```bash
git add web/static/chat.html web/static/chat.js
git commit -m "feat: вкладка «События» — лента диалога в одной хронологии

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Меню команд бота

**Files:**
- Modify: `internal/core/core.go:120-137` (`EditMyCommands`)
- Modify: `internal/events/bus.go:15-23`
- Modify: `web/static/chat.html:41` (перед формой), `web/static/chat.js:64-68` (`loadBot`), WebSocket
- Modify: `web/static/style.css`
- Test: `internal/core/core_test.go`

**Interfaces:**
- Consumes: поле `commands` из `GET /mock/api/bots/{id}` — control-API отдаёт его уже сейчас (`store.Bot.Commands`).
- Produces: `events.KindBot == "bot"`; функция `renderCommands(commands)` в `chat.js`.

- [ ] **Step 1: Написать падающий тест**

В `internal/core/core_test.go`:

```go
// TestEditMyCommandsNotifiesUI — меню команд в веб-чате рисуется из того, что
// бот опубликовал. Без события шины полоса обновлялась бы только перезагрузкой
// страницы, и «команды не появились» выглядело бы как дефект мока.
func TestEditMyCommandsNotifiesUI(t *testing.T) {
	f := newFixture(t)
	ch, cancel := f.bus.Subscribe(f.bot.ID)
	defer cancel()

	// Патч собирается разбором тела, а не литералом: признак «поле commands
	// пришло» у BotCommandsPatch неэкспортируемый и выставляется только в
	// UnmarshalJSON. Литерал дал бы CommandsSet() == false, EditMyCommands
	// вышел бы по ветке «ничего не менять», и тест падал бы не по делу.
	var patch wire.BotCommandsPatch
	if err := json.Unmarshal([]byte(
		`{"commands":[{"name":"start","description":"Начать разговор"}]}`), &patch); err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.EditMyCommands(f.bot, patch); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-ch:
		if ev.Kind != events.KindBot {
			t.Fatalf("вид события %q, ожидался %q", ev.Kind, events.KindBot)
		}
	case <-time.After(time.Second):
		t.Fatal("событие об изменении команд не опубликовано")
	}
}
```

Добавить импорт `"time"` в `internal/core/core_test.go` — остальные (`encoding/json`, `events`, `wire`) там уже есть.

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/core/ -run TestEditMyCommandsNotifiesUI`
Expected: FAIL — `undefined: events.KindBot` либо таймаут ожидания события

- [ ] **Step 3: Новый вид события**

В `internal/events/bus.go`:

```go
	KindBot            = "bot"             // изменилась карточка бота (команды)
```

- [ ] **Step 4: Публиковать изменение команд**

В `internal/core/core.go`, в `EditMyCommands`, перед возвратом успешного результата:

```go
	b.Commands = raw
	// Веб-чат рисует меню команд из этого поля: без уведомления полоса
	// обновлялась бы только перезагрузкой страницы.
	c.bus.Publish(events.Event{
		Kind: events.KindBot, BotID: b.ID,
		Payload: map[string]any{"commands": emptyIfNil(patch.Commands)},
	})
	return &wire.BotCommandsInfo{Commands: emptyIfNil(patch.Commands)}, nil
```

Ветка «поля `commands` нет» события не публикует: там ничего не изменилось.

- [ ] **Step 5: Полоса чипов в разметке**

`web/static/chat.html` — перед формой `#composer`:

```html
    <div id="commands" class="commands" hidden></div>
```

- [ ] **Step 6: Стиль полосы**

В `web/static/style.css` рядом с правилами `.composer`:

```css
.commands { display: flex; gap: 6px; flex-wrap: wrap; padding: 8px 12px 0; }
.commands button {
  background: var(--accent-soft);
  color: var(--accent);
  border: none;
  border-radius: 12px;
  padding: 4px 10px;
  font-size: 13px;
}
.commands[hidden] { display: none; }
```

- [ ] **Step 7: Отрисовка чипов**

В `web/static/chat.js` дополнить `loadBot` и добавить функцию:

```js
async function loadBot() {
  const bot = await getJSON(`/mock/api/bots/${botID}`);
  $('bot-title').textContent = `${bot.name} · @${bot.username}`;
  document.title = `max-mock — ${bot.name}`;
  renderCommands(bot.commands || []);
}

// renderCommands рисует меню команд бота — то, что он опубликовал через
// PATCH /me/commands.
//
// Клик отправляет обычное текстовое сообщение «/name»: в Max выбор команды из
// меню неотличим от того, что её набрали руками. Событие bot_started к
// командам отношения не имеет — его шлёт кнопка «Начать», и путать эти два
// пути не стоит.
//
// Команд нет — полосы нет, и это диагностика: значит, бот не звал
// PATCH /me/commands либо удалил команды пустым списком.
function renderCommands(commands) {
  const box = $('commands');
  box.textContent = '';
  box.hidden = !commands.length;
  for (const c of commands) {
    const chip = el('button', {
      type: 'button',
      textContent: '/' + c.name,
      title: c.description || '',
    });
    chip.onclick = () => act({action: 'send', text: '/' + c.name});
    box.append(chip);
  }
}
```

- [ ] **Step 8: Живое обновление полосы**

В `ws.onmessage`:

```js
    if (event.kind === 'bot') await loadBot();
```

- [ ] **Step 9: Прогнать тесты и проверить руками**

Run: `go test ./internal/core/ && make run`
Expected: тест проходит. В браузере: пока бот не звал `PATCH /me/commands`, полосы нет; после вызова чипы появляются без перезагрузки; клик по `/start` отправляет сообщение с текстом `/start`, и в ленте видно `UI отправил сообщение` → `→ стенд message_created`.

- [ ] **Step 10: Коммит**

```bash
git add internal/core/core.go internal/core/core_test.go internal/events/bus.go web/static/chat.html web/static/chat.js web/static/style.css
git commit -m "feat: меню команд бота в веб-чате

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Сквозной сценарий и документация

**Files:**
- Modify: `e2e/e2e_test.go`
- Modify: `README.md`, `docs/ЗАПУСК.md`

**Interfaces:**
- Consumes: всё, что сделано в задачах 1–9.

- [ ] **Step 1: Написать сквозной тест**

В `e2e/e2e_test.go`:

```go
// TestDialogFeedTellsTheWholeStory проходит путь, на котором разбор
// интеграции раньше упирался в тупик: старт, команда из меню, нажатие кнопки,
// остановка. Лента диалога обязана содержать все звенья — включая событие,
// которое никуда не ушло, потому что стенд на него не подписан.
func TestDialogFeedTellsTheWholeStory(t *testing.T) {
	f := newE2EFixture(t)
	// Стенд подписан на три типа — ровно как демобот. message_edited в этот
	// список не входит, и ответ на нажатие кнопки его породит.
	f.subscribe(t, []string{"message_created", "message_callback", "bot_started"})

	chatID := f.chatID
	f.action(t, chatID, `{"action":"start"}`)
	f.stand.wait(t, "bot_started")

	f.botSend(t, chatID, `{"text":"Здравствуйте","attachments":[{"type":"inline_keyboard","payload":{"buttons":[[{"type":"callback","text":"Привет","payload":"hello"}]]}}],"link":null}`)

	f.action(t, chatID, `{"action":"send","text":"/start"}`)
	f.stand.wait(t, "message_created")

	mid := f.lastBotMessageMid(t, chatID)
	f.action(t, chatID, `{"action":"press","mid":"`+mid+`","payload":"hello"}`)
	cb := f.stand.wait(t, "message_callback")
	callbackID := cb["callback"].(map[string]any)["callback_id"].(string)

	f.botAnswer(t, callbackID, `{"message":{"text":"Вы нажали: Привет","attachments":null,"link":null}}`)
	f.action(t, chatID, `{"action":"stop"}`)

	feed := f.dialogEvents(t, chatID)

	has := func(pred func(store.DialogEvent) bool, what string) {
		t.Helper()
		for _, e := range feed {
			if pred(e) {
				return
			}
		}
		t.Errorf("в ленте нет звена: %s", what)
	}
	has(func(e store.DialogEvent) bool { return e.Kind == "ui" && e.UI.Action == "start" },
		"действие «Начать»")
	has(func(e store.DialogEvent) bool {
		return e.Kind == "delivery" && e.Delivery.UpdateType == "bot_started" && e.Delivery.Status == 200
	}, "доставка bot_started")
	has(func(e store.DialogEvent) bool {
		return e.Kind == "ui" && e.UI.Action == "send" && strings.Contains(e.UI.Detail, "/start")
	}, "команда /start из меню")
	has(func(e store.DialogEvent) bool {
		return e.Kind == "request" && e.Request.Path == "/answers" &&
			e.Request.ChatID != nil && *e.Request.ChatID == chatID
	}, "вызов /answers с проставленным диалогом")
	has(func(e store.DialogEvent) bool {
		return e.Kind == "delivery" && e.Delivery.UpdateType == "message_edited" &&
			strings.Contains(e.Delivery.Error, "нет подписки")
	}, "недоставленное message_edited с причиной")
	has(func(e store.DialogEvent) bool {
		return e.Kind == "delivery" && e.Delivery.UpdateType == "bot_stopped"
	}, "доставка bot_stopped")
}
```

Хелперы `newE2EFixture`, `subscribe`, `action`, `botSend`, `botAnswer`, `lastBotMessageMid`, `dialogEvents` и `stand.wait` собрать по образцу существующих в `e2e/harness_test.go` и `e2e/e2e_test.go` — там уже есть и фейковый стенд с ожиданием события по типу (`waiter`), и вызовы Bot API с токеном. Новых механизмов не требуется: `dialogEvents` — обычный `GET /mock/api/dialogs/{chatId}/events` с разбором в `[]store.DialogEvent`, `action` — `POST /mock/api/dialogs/{chatId}/actions`.

Важно: `bot_stopped` в подписку не входит, поэтому проверка на его доставку ожидает запись **о недоставке**. Если в сценарии нужен успешный `bot_stopped`, добавить его тип в `subscribe`.

- [ ] **Step 2: Прогнать сквозной тест**

Run: `make e2e`
Expected: PASS

- [ ] **Step 3: Прогнать всё**

Run: `make test`
Expected: PASS во всех пакетах

- [ ] **Step 4: Обновить README**

В `README.md`:

- в список исходящих событий (строка 53) добавить `bot_stopped`:

```markdown
Исходящие события: `message_created`, `message_callback`, `message_edited`,
`message_removed`, `bot_started`, `bot_stopped`.
```

- в абзац про действия тестировщика (строка 60) — кнопка-переключатель и меню команд;
- новый абзац про ленту событий: что она объединяет три источника, что вызовы без диалога помечены, и что событие без подписки видно с причиной;
- оговорка о неизмеренном поведении рядом с прочими такими же:

```markdown
Остановленный диалог **не запрещает** боту слать в него сообщения. Как ведёт
себя живой Max после `bot_stopped` — не измерено; мок шлёт событие и не
выдумывает ограничение, которого не проверял.
```

- [ ] **Step 5: Обновить инструкцию запуска**

В `docs/ЗАПУСК.md`:

- в разделе про веб-чат — кнопка «Начать»/«Остановить» и полоса команд над полем ввода;
- в таблицу неисправностей — строка «событие не пришло на стенд»: смотреть вкладку «События», строка с прочерком вместо кода и текстом «нет подписки на этот тип события» означает, что стенд не подписан на этот тип, а не что мок его не отправил.

- [ ] **Step 6: Коммит**

```bash
git add e2e/e2e_test.go README.md docs/ЗАПУСК.md
git commit -m "test: сквозной сценарий ленты событий; docs: старт/стоп, лента и команды

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Самопроверка плана

**Покрытие спеки:**

| Требование спеки | Задача |
|---|---|
| `bot_stopped` как исходящее событие | 1 |
| Идемпотентность старта и остановки, возобновление после остановки | 1 |
| Действие `stop` в control-API | 2 |
| Кнопка-переключатель в веб-чате | 2 |
| Остановленный диалог не запрещает боту писать | 1 (решение в комментарии), 10 (README) |
| `chat_id` в обоих журналах + миграция + индексы | 3 |
| `chat_id` проставляет обработчик операции | 4 |
| Таблица `ui_actions`, `detail` как есть, неудачные действия | 6 |
| Действия пишет control-API, а не ядро | 6 |
| Запись о событии без подписки | 5 |
| Эндпоинт ленты, вызовы без диалога подмешаны | 7 |
| Вкладка «События», старые вкладки на месте | 8 |
| Цепочки-трассы не строятся | — (в плане нет по замыслу спеки) |
| Чипы команд, описание в подсказке, пустой список прячет полосу | 9 |
| Событие шины при `EditMyCommands` | 9 |
| Тесты всех уровней | 1, 3, 4, 5, 6, 7, 10 |
| README и ЗАПУСК | 10 |

**Согласованность имён между задачами:** `SetDialogStopped` (1) → `ClientStop` (1) → действие `stop` (2, 6); `RequestLogEntry.ChatID` / `DeliveryEntry.ChatID` (3) → `noteChat` (4), `logSkipped` (5), `listDialogRequests` (7); `UIActionEntry` / `LogUIAction` / `ListUIActions` (6) → `ListDialogEvents` (7) → `renderEvents` (8); `events.KindUIAction` (6) и `events.KindBot` (9) → обработчик WebSocket (8, 9).
