# События жизненного цикла бота — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Бот обрабатывает три новых события — `bot_added` (приветствие в чат), `bot_stopped` (запись в лог) и `message_edited` («Вы исправили: …»), — а подписка перестраивается сама, когда набор событий меняется.

**Architecture:** Список подписки переезжает из `cmd/bot/main.go` в `internal/bot` к `Commands()` — туда, где живут ветки `Handle`, которым он обязан соответствовать. Три новых обработчика ложатся в тот же `switch`. `syncSubscription` начинает сверять не только адрес подписки, но и набор типов, а обновляет её одним `POST` без предварительного `DELETE`: дедупликация в Max идёт по URL. Отдельная тонкость — `message_edited` приходит и на собственные правки бота (`POST /answers` заменяет сообщение с кнопками), поэтому обработчик отсеивает отправителя с `is_bot`.

**Tech Stack:** Go 1.24, только стандартная библиотека (`log/slog`, `encoding/json`, `net/http`). Модуль `maxbotdemo`. Тесты — `testing` и `net/http/httptest`, без сети.

Спека: [`docs/superpowers/specs/2026-08-06-lifecycle-events-design.md`](../specs/2026-08-06-lifecycle-events-design.md).

## Global Constraints

- **Никаких внешних зависимостей.** Только стандартная библиотека Go — правило проекта, `go.mod` без `require`.
- **Комментарии, сообщения лога и тексты бота — на русском**, как весь существующий код.
- **Комментарий объясняет «почему», а не «что»** — так написан весь остальной код проекта.
- **Тесты не ходят в сеть.** Бот проверяется через подставной `Sender`, `register` — через `httptest`.
- **После каждой задачи зелёные** `go test ./...` и `go test -race ./...`.
- **Коммит после каждой задачи**, сообщение на русском в повелительном наклонении — как в `git log` проекта («Отвечать на присланную геопозицию», «Маскировать секрет подписки в логе»).
- **Список `bot.UpdateTypes()` и ветки `bot.Handle` обязаны совпадать.** Подписка на необработанное событие даёт шум в логе, обработчик без подписки не сработает ни разу. За этим следит тест из задачи 4.

## Состав файлов

| Файл | Что делает | Задачи |
|---|---|---|
| `internal/maxapi/models.go` | три новые константы типов событий | 1, 2, 3 |
| `internal/bot/bot.go` | `sendChatGreeting`, `handleBotStopped`, `handleEdit`, `UpdateTypes()`, ветки `Handle` | 1, 2, 3, 4 |
| `internal/bot/bot_test.go` | тесты трёх обработчиков и соответствия списка веткам | 1, 2, 3, 4 |
| `cmd/bot/main.go` | `bot.UpdateTypes()` вместо литерала, `sameSet`, сверка набора в `syncSubscription` | 4, 5 |
| `cmd/bot/register_test.go` | `fakeAPI` обновляет подписку по URL, тесты сверки набора и `sameSet` | 5 |
| `README.md` | «Что умеет», описание подписки, «Известные ограничения» | 6 |
| `docs/superpowers/specs/2026-08-06-lifecycle-events-design.md` | результаты живого прогона | 7 |

---

### Task 1: `bot_added` — приветствие в чат

Бота добавили в групповой чат или канал. Адресат лежит не в `message.recipient` (сообщения тут нет вовсе), а в `chat_id` верхнего уровня; `user` — это тот, кто добавил. Существующий `Update.Target()` этот случай уже разбирает: при `Message == nil` он возвращает `Target{ChatID: u.ChatID, UserID: u.User.UserID}`, а `SendMessage` предпочитает `chat_id` — значит ответ уйдёт в чат, а не в личку добавившему. Правок в `maxapi` сверх константы не нужно.

**Files:**
- Modify: `internal/maxapi/models.go:7-13` (блок констант)
- Modify: `internal/bot/bot.go:96-107` (`Handle`), новый метод после `sendGreeting`
- Test: `internal/bot/bot_test.go`

**Interfaces:**
- Consumes: `maxapi.Update.Target()`, `maxapi.Update.Sender()`, `(*maxapi.User).DisplayName()`, `demoKeyboard()`, `helpText()`, `(*Bot).send` — всё существующее
- Produces: константа `maxapi.UpdateBotAdded = "bot_added"`; метод `func (b *Bot) sendChatGreeting(ctx context.Context, u maxapi.Update)`

- [x] **Step 1: Написать падающие тесты**

Добавить в конец `internal/bot/bot_test.go`:

```go
// botAddedUpdate строит событие bot_added: сообщения в нём нет, адресат лежит
// в chat_id верхнего уровня, а user — это тот, кто добавил бота.
func botAddedUpdate(user *maxapi.User) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateBotAdded,
		ChatID:     555,
		User:       user,
	}
}

func TestBotAddedGreetsChatAndNamesWhoAdded(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), botAddedUpdate(
		&maxapi.User{UserID: 42, FirstName: "Иван", LastName: "Петров"}))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	got := sender.messages[0]
	if got.To.ChatID != 555 {
		t.Errorf("chat_id адресата = %d, want 555: приветствие идёт в чат", got.To.ChatID)
	}
	if !strings.Contains(got.Body.Text, "Иван Петров") {
		t.Errorf("текст = %q, want имя добавившего", got.Body.Text)
	}
	if !strings.Contains(got.Body.Text, "/help") {
		t.Errorf("текст = %q, want справку по командам", got.Body.Text)
	}
	if keyboardOf(t, got.Body) == nil {
		t.Error("к приветствию в чат не приложена клавиатура")
	}
}

// Поле user в контракте обязательное, но бот на чужие гарантии не полагается:
// без имени приветствие должно остаться осмысленным.
func TestBotAddedWithoutUserStillGreets(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), botAddedUpdate(nil))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Body.Text, "демонстрационный бот") {
		t.Errorf("текст = %q, want приветствие без имени", sender.messages[0].Body.Text)
	}
}
```

- [x] **Step 2: Прогнать тесты и убедиться, что они падают**

Run: `go test ./internal/bot/ -run 'TestBotAdded' -v`
Expected: FAIL — `undefined: maxapi.UpdateBotAdded`

- [x] **Step 3: Добавить константу**

В `internal/maxapi/models.go` заменить блок констант на:

```go
// Типы событий. Полный список — в описании объекта Update в документации;
// здесь только те, на которые подписывается бот.
const (
	UpdateMessageCreated  = "message_created"
	UpdateMessageCallback = "message_callback"
	UpdateBotStarted      = "bot_started"
	UpdateBotAdded        = "bot_added"
)
```

- [x] **Step 4: Добавить обработчик**

В `internal/bot/bot.go` в `Handle` добавить ветку сразу после `case maxapi.UpdateBotStarted`:

```go
	case maxapi.UpdateBotAdded:
		b.sendChatGreeting(ctx, u)
```

И метод сразу после `sendGreeting`:

```go
// sendChatGreeting здоровается в чате или канале, куда бота только что
// добавили. От личного приветствия отличается адресатом и формулировкой:
// Sender() здесь — не собеседник, а тот, кто добавил бота.
//
// В канале приветствие такое же: его увидят все подписчики, но демобот на то и
// демонстрационный — он показывает событие, а не молчит о нём.
func (b *Bot) sendChatGreeting(ctx context.Context, u maxapi.Update) {
	greeting := "Здравствуйте! Я демонстрационный бот Max."
	if name := u.Sender().DisplayName(); name != "" {
		greeting = fmt.Sprintf("Здравствуйте! Меня добавил %s. Я демонстрационный бот Max.", name)
	}

	b.send(ctx, u, maxapi.NewMessageBody{
		Text:        greeting + "\n\n" + helpText(),
		Attachments: []maxapi.AttachmentRequest{demoKeyboard().Attachment()},
	})
}
```

- [x] **Step 5: Прогнать тесты и убедиться, что они проходят**

Run: `go test ./... && go test -race ./...`
Expected: PASS

- [x] **Step 6: Коммит**

```bash
git add internal/maxapi/models.go internal/bot/bot.go internal/bot/bot_test.go
git commit -m "Здороваться в чате, куда добавили бота"
```

---

### Task 2: `bot_stopped` — запись в лог

Пользователь остановил бота в его настройках. Отправить нечего: он бота и остановил, прощальное сообщение либо не дойдёт, либо будет отклонено API. Состояния бот не хранит, чистить тоже нечего — остаётся строка в логе.

Ловушка: `Update.Sender()` возвращает указатель, и он бывает пустым. `DisplayName()` от этого защищён проверкой на `nil`-получателя, а обращение к полю `UserID` — нет, и уронило бы обработчик.

**Files:**
- Modify: `internal/maxapi/models.go:7-14` (блок констант)
- Modify: `internal/bot/bot.go` (`Handle`), новый метод после `sendChatGreeting`
- Test: `internal/bot/bot_test.go`

**Interfaces:**
- Consumes: `maxapi.Update.Sender()`, `b.log` — существующее
- Produces: константа `maxapi.UpdateBotStopped = "bot_stopped"`; метод `func (b *Bot) handleBotStopped(u maxapi.Update)` — без `ctx`, потому что никуда не ходит

- [x] **Step 1: Написать падающие тесты**

Добавить в конец `internal/bot/bot_test.go`:

```go
func TestBotStoppedIsOnlyLogged(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{
		UpdateType: maxapi.UpdateBotStopped,
		ChatID:     777,
		User:       &maxapi.User{UserID: 42, FirstName: "Иван"},
	})

	if len(sender.messages) != 0 || len(sender.answers) != 0 {
		t.Errorf("на bot_stopped отправлено %d сообщений и %d ответов, want 0 и 0: пользователь бота остановил",
			len(sender.messages), len(sender.answers))
	}
}

// Sender() возвращает указатель, и он бывает пустым: обращение к полю без
// проверки уронило бы обработчик. DisplayName() от этого защищён, доступ к
// UserID — нет.
func TestBotStoppedWithoutUserDoesNotPanic(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{
		UpdateType: maxapi.UpdateBotStopped,
		ChatID:     777,
	})

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0", len(sender.messages))
	}
}
```

- [x] **Step 2: Прогнать тесты и убедиться, что они падают**

Run: `go test ./internal/bot/ -run 'TestBotStopped' -v`
Expected: FAIL — `undefined: maxapi.UpdateBotStopped`

- [x] **Step 3: Добавить константу**

В `internal/maxapi/models.go` дописать в блок констант:

```go
	UpdateBotStopped      = "bot_stopped"
```

- [x] **Step 4: Добавить обработчик**

В `internal/bot/bot.go` в `Handle` добавить ветку после `case maxapi.UpdateBotAdded`:

```go
	case maxapi.UpdateBotStopped:
		b.handleBotStopped(u)
```

И метод после `sendChatGreeting`:

```go
// handleBotStopped фиксирует остановку в логе. Отправлять нечего: пользователь
// бота и остановил, а состояния, которое стоило бы почистить, у бота нет.
// Контекст не нужен — обработчик никуда не ходит.
func (b *Bot) handleBotStopped(u maxapi.Update) {
	// Sender() возвращает указатель, и он бывает пустым: обращаться к полю
	// напрямую нельзя. DisplayName() от этого защищён, доступ к UserID — нет.
	var userID int64
	if s := u.Sender(); s != nil {
		userID = s.UserID
	}
	b.log.Info("пользователь остановил бота", "chat_id", u.ChatID, "user_id", userID)
}
```

- [x] **Step 5: Прогнать тесты и убедиться, что они проходят**

Run: `go test ./... && go test -race ./...`
Expected: PASS

- [x] **Step 6: Коммит**

```bash
git add internal/maxapi/models.go internal/bot/bot.go internal/bot/bot_test.go
git commit -m "Записывать в лог остановку бота пользователем"
```

---

### Task 3: `message_edited` — эхо правки с защитой от собственных правок

Пользователь отредактировал своё сообщение — бот отвечает «Вы исправили: …» отдельным сообщением. Команда при этом **не выполняется**: правка `/hepl` → `/help` даёт эхо, а не справку. Так событие видно отдельно от обычного эхо, иначе правка была бы неотличима от нового сообщения.

Главная ловушка задачи. `AnswerOnCallback` с полем `Message` заменяет исходное сообщение — то самое, с кнопками, которое отправил сам бот. В `max-mock` это буквально `AnswerCallback → applyEdit → publish(message_edited)` (`internal/core/bot.go:206` и `:136`), и, в отличие от `appendMessage`, фильтра «своё боту не присылать» там нет. Без проверки `is_bot` каждое нажатие кнопки давало бы лишний ответ вида «Вы исправили: Вы нажали: Привет».

`Target()`, `Sender()` и `Text()` работают без изменений: `message_edited` приносит полный объект `Message` с `recipient` и `sender`.

**Files:**
- Modify: `internal/maxapi/models.go:7-15` (блок констант)
- Modify: `internal/bot/bot.go` (`Handle`), новый метод после `handleMessage`
- Test: `internal/bot/bot_test.go`

**Interfaces:**
- Consumes: `maxapi.Update.Sender()`, `maxapi.Update.Text()`, `(*Bot).send` — существующее
- Produces: константа `maxapi.UpdateMessageEdited = "message_edited"`; метод `func (b *Bot) handleEdit(ctx context.Context, u maxapi.Update)`

- [x] **Step 1: Написать падающие тесты**

Добавить в конец `internal/bot/bot_test.go`:

```go
// humanUser и botUser — отправители события message_edited. Разделены не для
// красоты: правки самого бота приходят обратно тем же типом события.
func humanUser() *maxapi.User { return &maxapi.User{UserID: 42, FirstName: "Иван"} }
func botUser() *maxapi.User {
	return &maxapi.User{UserID: 1, FirstName: "Демобот", IsBot: true}
}

// editedMessage строит событие message_edited: в нём приезжает полный объект
// сообщения — уже с новым телом.
func editedMessage(text string, sender *maxapi.User) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageEdited,
		Message: &maxapi.Message{
			Sender:    sender,
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body:      maxapi.MessageBody{MID: "mid-1", Text: text},
		},
	}
}

func TestEditedMessageIsAnsweredWithCorrection(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("Привет, бот", humanUser()))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if !strings.Contains(text, "исправили") {
		t.Errorf("текст = %q, want отметку о правке", text)
	}
	if !strings.Contains(text, "Привет, бот") {
		t.Errorf("текст = %q, want новый текст сообщения", text)
	}
}

// POST /answers заменяет сообщение с кнопками — то, которое отправил сам бот, —
// и это возвращается боту как message_edited. Без проверки is_bot каждое
// нажатие кнопки давало бы лишний ответ «Вы исправили: Вы нажали: …».
func TestEditedOwnMessageIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("Вы нажали: Привет", botUser()))

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0: это правка собственного сообщения бота",
			len(sender.messages))
	}
}

func TestEditedMessageWithoutTextIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("", humanUser()))

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0", len(sender.messages))
	}
}

// Правка не перезапускает команду: показать нужно саму правку, иначе она
// неотличима от нового сообщения.
func TestEditedCommandIsEchoedNotExecuted(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), editedMessage("/help", humanUser()))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if !strings.Contains(text, "исправили") || !strings.Contains(text, "/help") {
		t.Errorf("текст = %q, want эхо правки", text)
	}
	if strings.Contains(text, "Доступные команды") {
		t.Errorf("текст = %q, want без справки: правка команду не выполняет", text)
	}
}
```

- [x] **Step 2: Прогнать тесты и убедиться, что они падают**

Run: `go test ./internal/bot/ -run 'TestEdited' -v`
Expected: FAIL — `undefined: maxapi.UpdateMessageEdited`

- [x] **Step 3: Добавить константу**

В `internal/maxapi/models.go` дописать в блок констант после `UpdateMessageCallback`:

```go
	UpdateMessageEdited   = "message_edited"
```

- [x] **Step 4: Добавить обработчик**

В `internal/bot/bot.go` в `Handle` добавить ветку после `case maxapi.UpdateMessageCreated`:

```go
	case maxapi.UpdateMessageEdited:
		b.handleEdit(ctx, u)
```

И метод сразу после `handleMessage`:

```go
// handleEdit отвечает на правку сообщения. Команду правка не выполняет: смысл
// ответа — показать само событие, а иначе оно неотличимо от нового сообщения.
func (b *Bot) handleEdit(ctx context.Context, u maxapi.Update) {
	// Собственные правки бота приходят обратно: POST /answers заменяет
	// сообщение с кнопками, и это порождает message_edited с sender = бот.
	// Без этой проверки каждое нажатие кнопки давало бы лишний ответ.
	if s := u.Sender(); s != nil && s.IsBot {
		return
	}

	if u.Text() == "" {
		// Правка вложения или снятие текста — отвечать нечем. Разбирать
		// вложения повторно незачем: contact и location осмысленны один раз.
		return
	}

	b.send(ctx, u, maxapi.NewMessageBody{
		Text: fmt.Sprintf("Вы исправили: %s", u.Text()),
	})
}
```

- [x] **Step 5: Прогнать тесты и убедиться, что они проходят**

Run: `go test ./... && go test -race ./...`
Expected: PASS

- [x] **Step 6: Коммит**

```bash
git add internal/maxapi/models.go internal/bot/bot.go internal/bot/bot_test.go
git commit -m "Отвечать на правку сообщения, пропуская собственные правки"
```

---

### Task 4: `bot.UpdateTypes()` — список подписки рядом с обработчиками

Сейчас список типов собирается литералом в `cmd/bot/main.go:145`, а `switch` по типам — в `internal/bot/bot.go`. Два места, которые обязаны совпадать, лежат в разных пакетах. Список переезжает к `Commands()`: и то и другое — свойство поведения бота, а `main.go` их только применяет.

Тест соответствия устроен так: событие каждого типа из списка прогоняется через `Handle`, и в логе не должно оказаться записи ветки `default`. Плюс контрольный случай на заведомо чужой тип — без него проверка прошла бы для чего угодно, если бы `default` замолчал или строка изменилась.

**Files:**
- Modify: `internal/bot/bot.go` (новая функция после `Commands()`)
- Modify: `cmd/bot/main.go:145-152`
- Test: `internal/bot/bot_test.go`

**Interfaces:**
- Consumes: константы `maxapi.Update*` из задач 1–3
- Produces: `func UpdateTypes() []string` в пакете `bot` — шесть типов; используется в `cmd/bot/main.go` и в задаче 5

- [x] **Step 1: Написать падающие тесты**

Добавить в конец `internal/bot/bot_test.go` (в блок импортов добавить `"bytes"`):

```go
// handleWithLog прогоняет событие через бота и возвращает лог обработки.
// Уровень Debug обязателен: ветка default пишет именно на нём.
func handleWithLog(t *testing.T, u maxapi.Update) string {
	t.Helper()

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	New(&fakeSender{}, log).Handle(context.Background(), u)
	return buf.String()
}

// Список подписки и ветки Handle обязаны совпадать: подписка на необработанное
// событие даёт шум, обработчик без подписки не сработает ни разу.
func TestEveryUpdateTypeIsHandled(t *testing.T) {
	types := UpdateTypes()
	if len(types) == 0 {
		t.Fatal("UpdateTypes() пуст")
	}

	for _, ut := range types {
		t.Run(ut, func(t *testing.T) {
			// Событие намеренно пустое: проверяется маршрутизация, а не ответ.
			// Ни один обработчик не должен на этом падать.
			got := handleWithLog(t, maxapi.Update{UpdateType: ut})
			if strings.Contains(got, "событие без обработчика") {
				t.Errorf("тип %q ушёл в default; лог: %s", ut, got)
			}
		})
	}
}

// Контрольный случай: без него проверка выше проходила бы для любого типа,
// замолчи ветка default или изменись её текст.
func TestUnhandledUpdateTypeReachesDefault(t *testing.T) {
	got := handleWithLog(t, maxapi.Update{UpdateType: "chat_title_changed"})

	if !strings.Contains(got, "событие без обработчика") {
		t.Errorf("лог = %s, want запись о необработанном событии", got)
	}
}
```

- [x] **Step 2: Прогнать тесты и убедиться, что они падают**

Run: `go test ./internal/bot/ -run 'UpdateType' -v`
Expected: FAIL — `undefined: UpdateTypes`

- [x] **Step 3: Добавить `UpdateTypes()`**

В `internal/bot/bot.go` сразу после `Commands()`:

```go
// UpdateTypes возвращает события, на которые бот подписывается. Список обязан
// совпадать с ветками Handle: подписка на необработанное событие даёт шум,
// обработчик без подписки не сработает ни разу. За соответствием следит
// TestEveryUpdateTypeIsHandled.
func UpdateTypes() []string {
	return []string{
		maxapi.UpdateMessageCreated,
		maxapi.UpdateMessageCallback,
		maxapi.UpdateMessageEdited,
		maxapi.UpdateBotStarted,
		maxapi.UpdateBotStopped,
		maxapi.UpdateBotAdded,
	}
}
```

- [x] **Step 4: Использовать список в `main.go`**

В `cmd/bot/main.go` заменить хвост `syncSubscription` (строки 145–153) на:

```go
	if err := api.Subscribe(ctx, cfg.WebhookURL, cfg.Secret, bot.UpdateTypes()); err != nil {
		return fmt.Errorf("подписка на webhook %s: %w", cfg.WebhookURL, err)
	}
	return nil
}
```

Локальная переменная `updateTypes` удаляется целиком. Импорт `maxbotdemo/internal/bot` в файле уже есть — его использует `register` для `bot.Commands()`.

- [x] **Step 5: Прогнать тесты и убедиться, что они проходят**

Run: `go test ./... && go test -race ./...`
Expected: PASS

- [x] **Step 6: Коммит**

```bash
git add internal/bot/bot.go internal/bot/bot_test.go cmd/bot/main.go
git commit -m "Держать список событий подписки рядом с обработчиками"
```

---

### Task 5: Подписка сверяет набор событий, а не только адрес

`syncSubscription` считает подписку актуальной, если совпал URL. Значит расширенный список из задачи 4 до Max не доедет: запись с нужным адресом уже есть, и `POST` не отправляется — новые события не придут ни разу, причём молча.

Обновление делается **одним `POST`, без предварительного `DELETE`**. Документация метода говорит прямо: «Чтобы обновить подписку на события, используйте `POST /subscriptions`» — дедупликация идёт по URL. Наблюдение из README о том, что `POST` «добавляет ещё одну подписку», относится к *другому* адресу: новый туннель — новый URL, отсюда и несколько записей. `DELETE` перед `POST` оставил бы окно, в котором события падают в никуда.

Отсюда же правка `fakeAPI`: сейчас его `POST` всегда добавляет запись, и это расходится с поведением, которое мы от Max ожидаем. Заглушка должна моделировать тот API, на который рассчитан код; окончательно это подтверждает живой прогон (задача 7).

**Files:**
- Modify: `cmd/bot/main.go:124-154` (`syncSubscription`), новая функция `sameSet` следом
- Modify: `cmd/bot/register_test.go:15-60` (`fakeAPI`), `:127-141` (тест «уже подписан»)
- Test: `cmd/bot/register_test.go`

**Interfaces:**
- Consumes: `bot.UpdateTypes()` из задачи 4, `maxapi.Client.GetSubscriptions/Subscribe/Unsubscribe`, `maxapi.Subscription.UpdateTypes`
- Produces: `func sameSet(a, b []string) bool` в пакете `main`

- [x] **Step 1: Научить `fakeAPI` обновлять подписку по URL**

В `cmd/bot/register_test.go` заменить комментарий к типу и ветку `POST`:

```go
// fakeAPI — заглушка Max API, которая ведёт список подписок так же, как
// настоящий сервер: подписка опознаётся по URL, повторный POST на тот же адрес
// обновляет набор событий, а на другой — заводит вторую запись.
type fakeAPI struct {
	calls    []string
	subs     []maxapi.Subscription
	failPath string
}

// upsert повторяет дедупликацию Max: ключ подписки — её URL.
func (f *fakeAPI) upsert(sub maxapi.Subscription) {
	for i, s := range f.subs {
		if s.URL == sub.URL {
			f.subs[i] = sub
			return
		}
	}
	f.subs = append(f.subs, sub)
}
```

И в `handler` ветку `POST /subscriptions`:

```go
	case r.URL.Path == "/subscriptions" && r.Method == http.MethodPost:
		var body maxapi.SubscriptionRequestBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.upsert(maxapi.Subscription{URL: body.URL, UpdateTypes: body.UpdateTypes})
		_, _ = io.WriteString(w, `{"success":true}`)
```

- [x] **Step 2: Написать падающие тесты**

В `cmd/bot/register_test.go` в блок импортов добавить `"maxbotdemo/internal/bot"`. Заменить `TestRegisterDoesNotResubscribeWhenAlreadySubscribed` (строки 127–141) на пару тестов ниже и дописать тест `sameSet`:

```go
// reversed возвращает копию списка в обратном порядке: порядок update_types в
// ответе API контрактом не оговорён, и сверка не должна на него полагаться.
func reversed(in []string) []string {
	out := make([]string, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		out = append(out, in[i])
	}
	return out
}

func TestRegisterDoesNotResubscribeWhenUpdateTypesMatch(t *testing.T) {
	existing := []maxapi.Subscription{{
		URL:         "https://example.test/webhook",
		UpdateTypes: reversed(bot.UpdateTypes()),
	}}
	client, api := newFakeAPI(t, existing, "")

	if _, err := register(context.Background(), client, testConfig()); err != nil {
		t.Fatalf("register: %v", err)
	}

	if strings.Contains(strings.Join(api.calls, ", "), "POST /subscriptions") {
		t.Errorf("вызовы = %v, want без повторной подписки: набор совпадает", api.calls)
	}
	if got := subURLs(api.subs); len(got) != 1 {
		t.Errorf("подписки = %v, want одну", got)
	}
}

// Подписка с нашим адресом, но старым набором событий: без обновления новые
// события не пришли бы ни разу, причём молча.
func TestRegisterResubscribesWhenUpdateTypesDiffer(t *testing.T) {
	existing := []maxapi.Subscription{{
		URL:         "https://example.test/webhook",
		UpdateTypes: []string{maxapi.UpdateMessageCreated},
	}}
	client, api := newFakeAPI(t, existing, "")

	if _, err := register(context.Background(), client, testConfig()); err != nil {
		t.Fatalf("register: %v", err)
	}

	calls := strings.Join(api.calls, ", ")
	if !strings.Contains(calls, "POST /subscriptions") {
		t.Errorf("вызовы = %v, want POST с новым набором событий", api.calls)
	}
	// DELETE оставил бы окно, в котором события падают в никуда: POST на тот
	// же URL обновляет подписку сам.
	if strings.Contains(calls, "DELETE /subscriptions") {
		t.Errorf("вызовы = %v, want без DELETE своего же адреса", api.calls)
	}
	if len(api.subs) != 1 {
		t.Fatalf("подписки = %v, want одну", subURLs(api.subs))
	}
	if !sameSet(api.subs[0].UpdateTypes, bot.UpdateTypes()) {
		t.Errorf("update_types = %v, want %v", api.subs[0].UpdateTypes, bot.UpdateTypes())
	}
}

func TestSameSet(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"одинаковые", []string{"a", "b"}, []string{"a", "b"}, true},
		{"другой порядок", []string{"b", "a"}, []string{"a", "b"}, true},
		{"разная длина", []string{"a"}, []string{"a", "b"}, false},
		{"разный состав", []string{"a", "c"}, []string{"a", "b"}, false},
		{"дубликат вместо элемента", []string{"a", "a"}, []string{"a", "b"}, false},
		{"обе пустые", nil, nil, true},
		{"одна пустая", nil, []string{"a"}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sameSet(c.a, c.b); got != c.want {
				t.Errorf("sameSet(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
```

- [x] **Step 3: Прогнать тесты и убедиться, что они падают**

Run: `go test ./cmd/bot/ -v`
Expected: FAIL — `undefined: sameSet`

- [x] **Step 4: Переписать `syncSubscription` и добавить `sameSet`**

В `cmd/bot/main.go` заменить `syncSubscription` целиком (вместе с комментарием, строки 118–154):

```go
// syncSubscription приводит подписки бота к одной — на cfg.WebhookURL с
// набором событий из bot.UpdateTypes().
//
// Подписка опознаётся по URL: чужие адреса снимаются, а свой обновляется одним
// POST. Так описано в документации метода — «чтобы обновить подписку на
// события, используйте POST /subscriptions», — и DELETE перед ним только
// открыл бы окно, в котором события падают в никуда. Несколько подписок сразу
// возникают от разных адресов: новый туннель — новый URL, и MAX начинает слать
// каждое событие на все живые адреса.
func syncSubscription(ctx context.Context, api *maxapi.Client, cfg config) error {
	existing, err := api.GetSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("чтение подписок: %w", err)
	}

	want := bot.UpdateTypes()
	var current []string
	found := false
	for _, s := range existing {
		if s.URL == cfg.WebhookURL {
			current, found = s.UpdateTypes, true
			continue
		}
		if err := api.Unsubscribe(ctx, s.URL); err != nil {
			return fmt.Errorf("снятие устаревшей подписки %s: %w", s.URL, err)
		}
	}

	// Сверять нужно и набор событий: иначе расширенный список не доедет до
	// MAX — запись с нужным адресом уже есть, — и новые события не придут
	// ни разу, причём молча.
	if found && sameSet(current, want) {
		return nil
	}

	if err := api.Subscribe(ctx, cfg.WebhookURL, cfg.Secret, want); err != nil {
		return fmt.Errorf("подписка на webhook %s: %w", cfg.WebhookURL, err)
	}
	return nil
}

// sameSet сообщает, что списки состоят из одних и тех же элементов. Порядок
// update_types в ответе API контрактом не оговорён, полагаться на него нельзя.
// Повторы считаются по количеству — контракт объявляет update_types
// uniqueItems, так что до этого дойти не должно, но и молча схлопывать дубли
// незачем.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	count := make(map[string]int, len(a))
	for _, v := range a {
		count[v]++
	}
	for _, v := range b {
		count[v]--
		if count[v] < 0 {
			return false
		}
	}
	return true
}
```

- [x] **Step 5: Прогнать тесты и убедиться, что они проходят**

Run: `go test ./... && go test -race ./...`
Expected: PASS

- [x] **Step 6: Коммит**

```bash
git add cmd/bot/main.go cmd/bot/register_test.go
git commit -m "Сверять при старте не только адрес подписки, но и набор событий"
```

---

### Task 6: README

Документация — часть задачи, а не хвост после неё. Три места.

**Files:**
- Modify: `README.md:9-31` (таблица «Что умеет»), `:102-106` (описание подписки), `:223-252` («Известные ограничения»)

**Interfaces:**
- Consumes: поведение из задач 1–5
- Produces: ничего (документация)

- [x] **Step 1: Дополнить таблицу «Что умеет»**

В `README.md` в таблицу после строки про `bot_started` добавить:

```markdown
| Бота добавили в чат или канал (`bot_added`) | приветствие в этот чат с именем добавившего |
| Пользователь остановил бота (`bot_stopped`) | только запись в лог — отправлять уже некуда |
```

И после строки про контакт и геопозицию:

```markdown
| пользователь отредактировал своё сообщение (`message_edited`) | «Вы исправили: …»; команда при этом не выполняется |
```

- [x] **Step 2: Переписать абзац про подписку**

Заменить абзац на строках 102–106 (начинается с «Настройка подписки — не просто…») на:

```markdown
Настройка подписки — не просто `POST /subscriptions`. Подписка опознаётся по
URL: повторный `POST` на тот же адрес обновляет её, а на другой — заводит
вторую. После пары запусков с разными адресами (новый туннель — новый адрес)
MAX начинает слать каждое событие на все подписки сразу. Поэтому бот сначала
читает `GET /subscriptions` и снимает чужие адреса.

Свой адрес он сверяет и по набору событий: список живёт в `bot.UpdateTypes()`
рядом с обработчиками, и стоит ему измениться — подписка обновляется одним
`POST`. Без этой сверки новые события не пришли бы ни разу, причём молча:
запись с нужным адресом уже есть. `DELETE` перед `POST` не делается — он
открыл бы окно, в котором события падают в никуда. Посмотреть текущее
состояние:
```

- [x] **Step 3: Дополнить «Известные ограничения»**

Добавить в список три пункта:

```markdown
- **Остановка бота только логируется.** На `bot_stopped` отправить нечего:
  пользователь бота и остановил. Состояния, которое стоило бы почистить, у
  демобота нет.
- **Правка сообщения не перезапускает команду.** На `message_edited` бот
  отвечает эхом правки: показать нужно само событие, иначе оно неотличимо от
  нового сообщения. Правки, сделанные самим ботом, пропускаются — `POST
  /answers` заменяет сообщение с кнопками, и это возвращается как
  `message_edited` с `sender.is_bot`.
- **`bot_added` не воспроизводится на max-mock.** Групповых чатов у эмулятора
  нет, поэтому событие проверяется только на живом Max.
```

- [x] **Step 4: Прогнать тесты (README на них не влияет, но правило есть правило)**

Run: `go test ./... && go test -race ./...`
Expected: PASS

- [x] **Step 5: Коммит**

```bash
git add README.md
git commit -m "Описать в README новые события и сверку набора подписки"
```

---

### Task 7: Живая проверка против max-mock

Юнит-тесты кормят бота структурами, которые сами и придумали. Допущения — что `POST` обновляет подписку по URL, что правка ответа на кнопку возвращается боту событием `message_edited` с `is_bot` — проверить может только мок или живой Max.

Мок покрывает всё, кроме `bot_added`: групповых чатов в нём нет. Проверка идёт control-API (`POST /mock/api/dialogs/{chatId}/actions`) — тем же путём, которым ходит веб-чат, так что события собираются настоящие, а прогон воспроизводим. Действия: `start`, `stop`, `send`, `send_contact`, `send_location`, `press`, `edit`, `delete`.

**Files:**
- Modify: `docs/superpowers/specs/2026-08-06-lifecycle-events-design.md` (раздел «Живая проверка»)

**Interfaces:**
- Consumes: собранный бот из задач 1–6
- Produces: ничего в коде — запись результатов в спеку

- [x] **Step 1: Поднять мок и бота**

```bash
cd ../maxmoc && ./max-mock            # мок слушает :8080
```

В соседнем терминале, из корня `maxbotdemo`:

```bash
set -a && . ./.env && set +a
go run ./cmd/bot 2>&1 | tee /tmp/bot-lifecycle.log
```

Бот в `.env` уже настроен на мок. Если бот в моке ещё не заведён — завести в админке `http://localhost:8080/mock` и положить выданный токен в `.env`.

- [x] **Step 2: Проверить, что подписка обновилась одним POST**

```bash
curl -s -H "Authorization: $MAX_BOT_TOKEN" http://localhost:8080/subscriptions
```

Ожидается **одна** запись с `http://localhost:8081/webhook` и всеми шестью типами в `update_types`. Две записи с одним URL означали бы, что `POST` не обновляет подписку и нужен `DELETE` — тогда правится `syncSubscription` и спека.

- [x] **Step 3: Завести клиента и получить chat_id**

```bash
BOT_ID=$(curl -s http://localhost:8080/mock/api/bots \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["id"])')

# Ответ — {"client": …, "dialog": {"chat_id": …}}: chat_id лежит в dialog.
CHAT=$(curl -s -X POST http://localhost:8080/mock/api/bots/$BOT_ID/clients \
  -H 'Content-Type: application/json' -d '{"first_name":"Иван","last_name":"Петров"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["dialog"]["chat_id"])')

curl -s -X POST http://localhost:8080/mock/api/dialogs/$CHAT/actions \
  -H 'Content-Type: application/json' -d '{"action":"start"}'
```

Ожидается приветствие бота в диалоге — `bot_started` работал и раньше, это проверка стенда.

- [x] **Step 4: Проверить `message_edited`**

Отправить сообщение, забрать его `mid`, отредактировать. Действие `send`
возвращает объект `Message` контракта — `mid` там внутри `body`:

```bash
MID=$(curl -s -X POST http://localhost:8080/mock/api/dialogs/$CHAT/actions \
  -H 'Content-Type: application/json' -d '{"action":"send","text":"Привет"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["body"]["mid"])')

curl -s -X POST http://localhost:8080/mock/api/dialogs/$CHAT/actions \
  -H 'Content-Type: application/json' \
  -d "{\"action\":\"edit\",\"mid\":\"$MID\",\"text\":\"Привет, бот\"}"

# Лента диалога — плоский список: mid на верхнем уровне, sender — "bot"/"client".
curl -s http://localhost:8080/mock/api/dialogs/$CHAT/messages \
  | python3 -c 'import json,sys; [print(m["sender"], "|", m["body"].get("text")) for m in json.load(sys.stdin)]'
```

Ожидается: на первое сообщение — «Вы написали: Привет», на правку — «Вы исправили: Привет, бот».

- [x] **Step 5: Проверить защиту от собственных правок**

Нажать кнопку в приветствии (payload `hello` — из `callbackButtons`):

```bash
# Сообщение бота с клавиатурой — приветствие. mid берётся с верхнего уровня.
GREET=$(curl -s http://localhost:8080/mock/api/dialogs/$CHAT/messages \
  | python3 -c 'import json,sys; ms=[m for m in json.load(sys.stdin) if m["sender"]=="bot" and m["body"].get("attachments")]; print(ms[0]["mid"])')

curl -s -X POST http://localhost:8080/mock/api/dialogs/$CHAT/actions \
  -H 'Content-Type: application/json' \
  -d "{\"action\":\"press\",\"mid\":\"$GREET\",\"payload\":\"hello\"}"

curl -s http://localhost:8080/mock/api/dialogs/$CHAT/messages \
  | python3 -c 'import json,sys; [print(m["sender"], "|", m["body"].get("text")) for m in json.load(sys.stdin)]'
```

Ожидается: сообщение с кнопками заменено на «Вы нажали: Привет» — и **никакого** «Вы исправили: Вы нажали: Привет». В логе бота при этом должно быть событие `message_edited`: оно доехало, но обработчик его пропустил. Это ключевая проверка задачи 3 — без неё защита стоит только на юнит-тесте, придуманном по чтению чужого кода.

- [x] **Step 6: Проверить `bot_stopped`**

```bash
curl -s -X POST http://localhost:8080/mock/api/dialogs/$CHAT/actions \
  -H 'Content-Type: application/json' -d '{"action":"stop"}'

grep 'остановил бота' /tmp/bot-lifecycle.log
```

Ожидается строка лога с `chat_id` и `user_id` — и ни одного нового сообщения в диалоге.

- [x] **Step 7: Свериться с журналом доставок**

```bash
curl -s http://localhost:8080/mock/api/bots/$BOT_ID/deliveries
```

Ожидается: все доставки со статусом `200`, в логе бота ни `ERROR`, ни `WARN`.

- [x] **Step 8: Записать результаты в спеку**

В `docs/superpowers/specs/2026-08-06-lifecycle-events-design.md` в раздел «Живая проверка» дописать таблицу с фактическими результатами по образцу спеки контакта и геопозиции: что проверялось, что получилось, что разошлось с ожиданием. Отдельной строкой — что `bot_added` моком не проверялся и ждёт живого Max.

Если что-то разошлось с ожиданием — это правка кода и спеки, а не пометка «проверено».

- [x] **Step 9: Коммит**

```bash
git add docs/superpowers/specs/2026-08-06-lifecycle-events-design.md
git commit -m "Зафиксировать результаты живой проверки событий жизненного цикла"
```

---

## Что остаётся непроверенным

`bot_added` не воспроизводится ни на моке, ни автоматически: нужен живой Max (`.env.live`), туннель и групповой чат, куда бота добавят руками. До этого прогона событие стоит на юнит-тестах задачи 1. Это ограничение записано и в спеку, и в README — умолчать о нём нельзя.
