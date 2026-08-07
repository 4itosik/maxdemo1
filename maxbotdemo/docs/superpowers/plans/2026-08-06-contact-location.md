# Контакт и геопозиция клиента — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Научить демобота просить у клиента номер телефона и геопозицию кнопками `request_contact` / `request_geo_location` и разбирать вложения `contact` и `location`, которые приходят в ответ.

**Architecture:** Модели `internal/maxapi` разделяют схемы исходящего (`AttachmentRequest`) и входящего (`Attachment`) вложения — как их различает контракт. Разбор карточки VCARD живёт отдельным файлом `internal/maxapi/vcard.go`. Логика бота в `internal/bot` получает две команды, две кнопки и два обработчика; `handleMessage` начинает смотреть вложения раньше текста, потому что у сообщения с контактом текста нет вовсе.

**Tech Stack:** Go 1.24, только стандартная библиотека. Модуль `maxbotdemo`. Тесты — `testing`, без сети.

Спека: [`docs/superpowers/specs/2026-08-06-contact-location-design.md`](../specs/2026-08-06-contact-location-design.md).

## Global Constraints

- **Никаких внешних зависимостей.** Только стандартная библиотека Go — это правило проекта, `go.mod` без `require`.
- **Комментарии и тексты для пользователя — на русском**, как весь существующий код.
- **Тесты не ходят в сеть.** Логика бота проверяется на подставном `Sender`, клиент — на `httptest`.
- **Имена типов следуют контракту** `max-openapi-official.json`: если в контракте схема зовётся `AttachmentRequest`, тип в Go зовётся так же.
- **После каждой задачи зелёные** `go test ./...` и `go test -race ./...`.
- **Комментарий объясняет «почему», а не «что»** — так написан весь остальной код проекта.

## Состав файлов

| Файл | Что делает | Задачи |
|---|---|---|
| `internal/maxapi/models.go` | схемы API; сюда добавляются `AttachmentRequest`, входящий `Attachment`, `ContactPayload`, `Location`, аксессоры `Update.Contact` / `Update.Location`, константы вложений и кнопок | 1, 3 |
| `internal/maxapi/vcard.go` (новый) | разбор карточки VCARD: `ContactPayload.Phone`, `ContactPayload.Name` | 2 |
| `internal/maxapi/models_test.go` | разбор событий с вложениями | 1 |
| `internal/maxapi/vcard_test.go` (новый) | разбор VCARD | 2 |
| `internal/maxapi/client_test.go` | правка типа после переименования | 1 |
| `internal/bot/keyboard.go` | билдеры кнопок `RequestContactButton`, `RequestGeoLocationButton` | 1, 3 |
| `internal/bot/bot.go` | команды `/phone`, `/location`, демо-клавиатура, `handleContact`, `handleLocation` | 1, 3, 4, 5 |
| `internal/bot/bot_test.go` | сценарии бота | 3, 4, 5 |
| `README.md` | документация | 6 |

---

### Task 1: Разделить схемы входящего и исходящего вложения

Контракт различает `AttachmentRequest` (в `NewMessageBody.attachments`, то есть в нашем `POST /messages`) и `Attachment` (в `MessageBody.attachments`, то есть в полученном сообщении). В коде сегодня один тип на оба случая, и из-за этого payload контакта разбирается в структуру клавиатуры вхолостую: `Buttons == nil`, номер недостижим, ошибки при этом никто не увидит.

**Files:**
- Modify: `internal/maxapi/models.go:180-215` (константы вложений, тип `Attachment`), `internal/maxapi/models.go:154-160` (`NewMessageBody.Attachments`), `internal/maxapi/models.go:59-95` (рядом с `Sender`/`Text`/`Command` — новые аксессоры)
- Modify: `internal/bot/keyboard.go:28-33`, `internal/bot/bot.go:109`, `internal/bot/bot.go:147`
- Test: `internal/maxapi/models_test.go`, `internal/maxapi/client_test.go:149`

**Interfaces:**
- Produces: `maxapi.AttachmentRequest{Type string; Payload *KeyboardPayload}`; `maxapi.Attachment{Type string; Payload *ContactPayload; Latitude, Longitude *float64}`; `maxapi.ContactPayload{VcfInfo, Hash string; MaxInfo *User}`; `maxapi.Location{Latitude, Longitude float64}`; `func (u Update) Contact() *ContactPayload`; `func (u Update) Location() *Location`; константы `AttachmentContact`, `AttachmentLocation`
- Consumes: ничего

- [x] **Step 1: Написать падающие тесты на разбор событий с вложениями**

Добавить в `internal/maxapi/models_test.go`. Импорт `strings` придётся дописать — сейчас файл импортирует только `encoding/json` и `testing`.

```go
const contactMessageJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000003,
  "message": {
    "sender": {"user_id": 42, "first_name": "Иван", "is_bot": false},
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {
      "mid": "mid.2", "seq": 2, "text": null,
      "attachments": [{
        "type": "contact",
        "payload": {
          "vcf_info": "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Иван Петров\r\nTEL;TYPE=CELL:+79991234567\r\nEND:VCARD\r\n",
          "hash": "9f2c",
          "max_info": {"user_id": 42, "first_name": "Иван", "last_name": "Петров", "is_bot": false}
        }
      }]
    }
  }
}`

const locationMessageJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000004,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.3", "seq": 3, "text": null,
      "attachments": [{"type": "location", "latitude": 55.751244, "longitude": 37.618423}]}
  }
}`

// Нулевые координаты — валидная точка в Гвинейском заливе. Именно ради этого
// случая Latitude и Longitude объявлены указателями.
const locationZeroJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000005,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.4", "seq": 4, "text": null,
      "attachments": [{"type": "location", "latitude": 0, "longitude": 0}]}
  }
}`

const locationWithoutCoordsJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000006,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.5", "seq": 5, "text": null,
      "attachments": [{"type": "location"}]}
  }
}`

const keyboardMessageJSON = `{
  "update_type": "message_created",
  "timestamp": 1754400000007,
  "message": {
    "recipient": {"chat_id": 777, "chat_type": "dialog", "user_id": 42},
    "body": {"mid": "mid.6", "seq": 6, "text": "нажми",
      "attachments": [{"type": "inline_keyboard",
        "payload": {"buttons": [[{"type": "callback", "text": "Привет", "payload": "hello"}]]}}]}
  }
}`

func TestUpdateContact(t *testing.T) {
	u := decodeUpdate(t, contactMessageJSON)

	c := u.Contact()
	if c == nil {
		t.Fatal("Contact() = nil, want вложение контакта")
	}
	if !strings.Contains(c.VcfInfo, "+79991234567") {
		t.Errorf("VcfInfo = %q, want карточку с номером", c.VcfInfo)
	}
	if c.Hash != "9f2c" {
		t.Errorf("Hash = %q, want %q", c.Hash, "9f2c")
	}
	if c.MaxInfo == nil || c.MaxInfo.UserID != 42 {
		t.Errorf("MaxInfo = %+v, want user_id=42", c.MaxInfo)
	}
}

func TestUpdateContactIsNilWithoutContactAttachment(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"обычный текст без вложений", messageCreatedJSON},
		{"клавиатура", keyboardMessageJSON},
		{"геопозиция", locationMessageJSON},
		{"событие без сообщения", botStartedJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeUpdate(t, tt.raw).Contact(); got != nil {
				t.Errorf("Contact() = %+v, want nil", got)
			}
		})
	}
}

func TestUpdateLocation(t *testing.T) {
	l := decodeUpdate(t, locationMessageJSON).Location()

	if l == nil {
		t.Fatal("Location() = nil, want координаты")
	}
	if l.Latitude != 55.751244 || l.Longitude != 37.618423 {
		t.Errorf("Location() = %+v, want 55.751244/37.618423", l)
	}
}

func TestUpdateLocationAcceptsZeroCoordinates(t *testing.T) {
	l := decodeUpdate(t, locationZeroJSON).Location()

	if l == nil {
		t.Fatal("Location() = nil, want точку 0,0 — она валидна")
	}
	if l.Latitude != 0 || l.Longitude != 0 {
		t.Errorf("Location() = %+v, want 0/0", l)
	}
}

func TestUpdateLocationWithoutCoordinatesIsNil(t *testing.T) {
	if got := decodeUpdate(t, locationWithoutCoordsJSON).Location(); got != nil {
		t.Errorf("Location() = %+v, want nil: координат во вложении нет", got)
	}
}
```

Дополнить существующий `TestZeroUpdateIsSafe` (`internal/maxapi/models_test.go:166`) двумя проверками:

```go
	if got := u.Contact(); got != nil {
		t.Errorf("Contact() = %+v, want nil", got)
	}
	if got := u.Location(); got != nil {
		t.Errorf("Location() = %+v, want nil", got)
	}
```

- [x] **Step 2: Убедиться, что тесты не компилируются**

Run: `go test ./internal/maxapi/ -run TestUpdate -v`
Expected: FAIL — `u.Contact undefined`, `u.Location undefined`.

- [x] **Step 3: Переименовать исходящее вложение и добавить входящее**

В `internal/maxapi/models.go` заменить блок констант вложения и тип `Attachment` (строки 180-193) на:

```go
// Типы вложений, которые бот отправляет и читает.
const (
	AttachmentInlineKeyboard = "inline_keyboard"
	AttachmentContact        = "contact"
	AttachmentLocation       = "location"
)

// AttachmentRequest — вложение исходящего сообщения. Бот отправляет только
// inline_keyboard, поэтому payload не типизирован жёстче, чем нужно.
type AttachmentRequest struct {
	Type    string           `json:"type"`
	Payload *KeyboardPayload `json:"payload,omitempty"`
}

// Attachment — вложение полученного сообщения. Контракт различает эту схему и
// AttachmentRequest, и не зря: у исходящей клавиатуры payload — массив кнопок,
// у входящего контакта — vcf_info, hash и max_info.
//
// Типизирован payload только контакта: прочие типы бот не читает, и их payload
// разберётся сюда вхолостую, оставшись пустым. Координаты вложения location
// лежат не в payload, а на верхнем уровне — так в контракте.
//
// Указатели у координат не для красоты: 0,0 — валидная точка в Гвинейском
// заливе, и отличить её от «поля в JSON не было» больше нечем.
type Attachment struct {
	Type      string          `json:"type"`
	Payload   *ContactPayload `json:"payload,omitempty"`
	Latitude  *float64        `json:"latitude,omitempty"`
	Longitude *float64        `json:"longitude,omitempty"`
}

// ContactPayload — содержимое вложения contact.
//
// Телефона в схеме User нет вовсе: до бота он доезжает только строкой TEL
// внутри vcf_info — разбор в vcard.go.
type ContactPayload struct {
	VcfInfo string `json:"vcf_info,omitempty"`
	Hash    string `json:"hash,omitempty"`
	MaxInfo *User  `json:"max_info,omitempty"`
}

// Location — координаты из вложения location.
type Location struct {
	Latitude  float64
	Longitude float64
}
```

В `NewMessageBody` (строка 156) поменять тип поля:

```go
	Attachments []AttachmentRequest `json:"attachments"`
```

`MessageBody.Attachments` (строка 137) остаётся `[]Attachment` — теперь это уже входящий тип, править нечего.

- [x] **Step 4: Добавить аксессоры**

В `internal/maxapi/models.go` после метода `Command` (строка 95):

```go
// Contact возвращает вложение-контакт из сообщения или nil, если его нет.
//
// Контракт требует, чтобы contact был единственным вложением сообщения, но
// бот на это не полагается: чужое требование — не гарантия.
func (u Update) Contact() *ContactPayload {
	for _, a := range u.attachments() {
		if a.Type == AttachmentContact && a.Payload != nil {
			return a.Payload
		}
	}
	return nil
}

// Location возвращает координаты из вложения location или nil, если вложения
// нет либо координаты в нём не заполнены.
func (u Update) Location() *Location {
	for _, a := range u.attachments() {
		if a.Type == AttachmentLocation && a.Latitude != nil && a.Longitude != nil {
			return &Location{Latitude: *a.Latitude, Longitude: *a.Longitude}
		}
	}
	return nil
}

// attachments возвращает вложения сообщения или nil, если сообщения нет.
func (u Update) attachments() []Attachment {
	if u.Message == nil {
		return nil
	}
	return u.Message.Body.Attachments
}
```

- [x] **Step 5: Провести переименование по местам вызова**

`internal/bot/keyboard.go:28-33`:

```go
// Attachment превращает клавиатуру во вложение исходящего сообщения. Имя
// метода описывает действие, а схему контракта называет тип в сигнатуре.
func (k *Keyboard) Attachment() maxapi.AttachmentRequest {
	return maxapi.AttachmentRequest{
		Type:    maxapi.AttachmentInlineKeyboard,
		Payload: &maxapi.KeyboardPayload{Buttons: k.rows},
	}
}
```

`internal/bot/bot.go:109` и `internal/bot/bot.go:147` — в обоих местах:

```go
			Attachments: []maxapi.AttachmentRequest{demoKeyboard().Attachment()},
```

`internal/maxapi/client_test.go:149`:

```go
		Attachments: []AttachmentRequest{{
```

`internal/bot/bot_test.go` править не нужно: он обращается к полям вложения, а тип по имени не называет.

- [x] **Step 6: Прогнать тесты**

Run: `go test ./... && go test -race ./...`
Expected: PASS, включая новые `TestUpdateContact`, `TestUpdateLocation`, `TestUpdateLocationAcceptsZeroCoordinates`, `TestUpdateLocationWithoutCoordinatesIsNil`.

- [x] **Step 7: Коммит**

```bash
git add internal/maxapi/models.go internal/maxapi/models_test.go \
        internal/maxapi/client_test.go internal/bot/keyboard.go internal/bot/bot.go
git commit -m "Разделить схемы входящего и исходящего вложения"
```

---

### Task 2: Разбор карточки VCARD

Номер телефона доезжает до бота одной строкой `TEL` внутри `vcf_info`. Мок отдаёт стерильную карточку в четыре строки, живой Max — ту, что отдал телефон пользователя, поэтому разбор должен терпеть параметры, групповые префиксы, свёрнутые строки и произвольный регистр.

**Files:**
- Create: `internal/maxapi/vcard.go`
- Test: `internal/maxapi/vcard_test.go` (новый)

**Interfaces:**
- Consumes: `maxapi.ContactPayload` из задачи 1
- Produces: `func (p *ContactPayload) Phone() string`, `func (p *ContactPayload) Name() string`

- [x] **Step 1: Написать падающие тесты**

Создать `internal/maxapi/vcard_test.go`:

```go
package maxapi

import "testing"

func TestContactPayloadPhone(t *testing.T) {
	tests := []struct {
		name string
		vcf  string
		want string
	}{
		{
			"простая карточка",
			"BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Иван\r\nTEL:+79991234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"параметр TYPE",
			"BEGIN:VCARD\r\nTEL;TYPE=CELL:+79991234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"групповой префикс в стиле Apple",
			"BEGIN:VCARD\r\nitem1.TEL;type=CELL:+79991234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"имя свойства в нижнем регистре",
			"BEGIN:VCARD\r\ntel:+79991234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"перевод строки без возврата каретки",
			"BEGIN:VCARD\nTEL:+79991234567\nEND:VCARD\n",
			"+79991234567",
		},
		{
			"первый из нескольких номеров",
			"BEGIN:VCARD\r\nTEL;TYPE=CELL:+79991234567\r\nTEL;TYPE=HOME:+74951234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"карточка без TEL",
			"BEGIN:VCARD\r\nFN:Иван\r\nEND:VCARD\r\n",
			"",
		},
		{"пустая карточка", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &ContactPayload{VcfInfo: tt.vcf}
			if got := p.Phone(); got != tt.want {
				t.Errorf("Phone() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContactPayloadName(t *testing.T) {
	tests := []struct {
		name    string
		payload ContactPayload
		want    string
	}{
		{
			"FN из карточки",
			ContactPayload{VcfInfo: "BEGIN:VCARD\r\nFN:Иван Петров\r\nEND:VCARD\r\n"},
			"Иван Петров",
		},
		{
			// По RFC 6350 продолжение начинается с пробела, и сам пробел в
			// значение не входит: строка склеивается без него.
			"свёрнутая строка",
			ContactPayload{VcfInfo: "BEGIN:VCARD\r\nFN:Иван Петр\r\n ович\r\nEND:VCARD\r\n"},
			"Иван Петрович",
		},
		{
			"без FN — имя из max_info",
			ContactPayload{
				VcfInfo: "BEGIN:VCARD\r\nTEL:+79991234567\r\nEND:VCARD\r\n",
				MaxInfo: &User{UserID: 42, FirstName: "Иван", LastName: "Петров"},
			},
			"Иван Петров",
		},
		{"ни карточки, ни max_info", ContactPayload{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.payload.Name(); got != tt.want {
				t.Errorf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Аксессоры Update возвращают nil, когда вложения нет, — вызов на таком
// указателе не должен ронять бота.
func TestNilContactPayloadIsSafe(t *testing.T) {
	var p *ContactPayload

	if got := p.Phone(); got != "" {
		t.Errorf("Phone() = %q, want пустую строку", got)
	}
	if got := p.Name(); got != "" {
		t.Errorf("Name() = %q, want пустую строку", got)
	}
}
```

- [x] **Step 2: Убедиться, что тесты падают**

Run: `go test ./internal/maxapi/ -run 'TestContactPayload|TestNilContactPayload' -v`
Expected: FAIL — `p.Phone undefined`, `p.Name undefined`.

- [x] **Step 3: Написать разбор**

Создать `internal/maxapi/vcard.go`:

```go
package maxapi

import "strings"

// Разбор карточки VCARD из вложения contact.
//
// Мок отдаёт стерильную карточку в четыре строки, живой Max — ту, что отдал
// телефон пользователя. Поэтому разбор терпит параметры свойства
// (TEL;TYPE=CELL:), групповые префиксы в стиле Apple (item1.TEL:), свёрнутые
// строки и любой регистр имени свойства.
//
// Чего он намеренно не делает: не разворачивает экранирование значений
// (\, \; \\) — в телефоне и имени оно не встречается; и ломается на параметре
// в кавычках с двоеточием внутри (TEL;TYPE="a:b":+7…). Контракт таких карточек
// не порождает, а гадать по кавычкам ради несуществующего случая незачем.

// Phone возвращает первый номер телефона из vcf_info или пустую строку.
func (p *ContactPayload) Phone() string {
	if p == nil {
		return ""
	}
	return vcardValue(p.VcfInfo, "TEL")
}

// Name возвращает имя контакта: FN из карточки, а если её нет — имя из
// max_info.
func (p *ContactPayload) Name() string {
	if p == nil {
		return ""
	}
	if fn := vcardValue(p.VcfInfo, "FN"); fn != "" {
		return fn
	}
	return p.MaxInfo.DisplayName()
}

// vcardValue возвращает значение первого свойства с заданным именем.
func vcardValue(vcf, prop string) string {
	for _, line := range vcardLines(vcf) {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		// Параметры отделены точкой с запятой: TEL;TYPE=CELL.
		name, _, _ = strings.Cut(name, ";")
		// Групповой префикс — всё до последней точки: item1.TEL.
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if strings.EqualFold(strings.TrimSpace(name), prop) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// vcardLines разбивает карточку на логические строки, разворачивая переносы:
// по RFC 6350 продолжение начинается с пробела или табуляции, и сам этот
// символ в значение не входит.
func vcardLines(vcf string) []string {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(vcf, "\r\n", "\n"), "\n") {
		if raw == "" {
			continue
		}
		if (raw[0] == ' ' || raw[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += raw[1:]
			continue
		}
		lines = append(lines, raw)
	}
	return lines
}
```

`p.MaxInfo.DisplayName()` на `nil`-указателе безопасен: у `DisplayName` (`internal/maxapi/models.go:107`) уже есть проверка получателя.

- [x] **Step 4: Прогнать тесты**

Run: `go test ./internal/maxapi/ -run 'TestContactPayload|TestNilContactPayload' -v`
Expected: PASS, все подтесты.

- [x] **Step 5: Прогнать всё**

Run: `go test ./... && go test -race ./...`
Expected: PASS

- [x] **Step 6: Коммит**

```bash
git add internal/maxapi/vcard.go internal/maxapi/vcard_test.go
git commit -m "Доставать телефон и имя из карточки VCARD"
```

---

### Task 3: Кнопки-запросы, команды `/phone` и `/location`

Это половина «бот просит». Кнопки `request_contact` и `request_geo_location` заставляют клиент прислать новое `message_created` с вложением — отдельного события у них нет, `message_callback` при нажатии не приходит.

**Files:**
- Modify: `internal/maxapi/models.go:196-199` (константы типов кнопок)
- Modify: `internal/bot/keyboard.go` (два билдера в конец файла)
- Modify: `internal/bot/bot.go:21-25` (константы команд), `internal/bot/bot.go:56-62` (`Commands`), `internal/bot/bot.go:98-114` (разбор команд), `internal/bot/bot.go:167-177` (`demoKeyboard`)
- Test: `internal/bot/bot_test.go`

**Interfaces:**
- Consumes: `maxapi.AttachmentRequest`, `Keyboard.Attachment()` из задачи 1
- Produces: константы `maxapi.ButtonRequestContact`, `maxapi.ButtonRequestGeoLocation`; `bot.RequestContactButton(text string) maxapi.Button`, `bot.RequestGeoLocationButton(text string) maxapi.Button`; команды `phone` и `location` в `bot.Commands()`

- [x] **Step 1: Написать падающие тесты**

Добавить в `internal/bot/bot_test.go`:

```go
// buttonsByType собирает подписи кнопок клавиатуры по типам.
func buttonsByType(rows [][]maxapi.Button) map[string]string {
	byType := make(map[string]string)
	for _, row := range rows {
		for _, btn := range row {
			byType[btn.Type] = btn.Text
		}
	}
	return byType
}

func TestPhoneCommandAsksForContact(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("/phone"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	got := buttonsByType(keyboardOf(t, sender.messages[0].Body))
	if _, ok := got[maxapi.ButtonRequestContact]; !ok {
		t.Errorf("кнопки клавиатуры: %v, want кнопку %q",
			got, maxapi.ButtonRequestContact)
	}
}

func TestLocationCommandAsksForGeoLocation(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), textMessage("/location"))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	got := buttonsByType(keyboardOf(t, sender.messages[0].Body))
	if _, ok := got[maxapi.ButtonRequestGeoLocation]; !ok {
		t.Errorf("кнопки клавиатуры: %v, want кнопку %q",
			got, maxapi.ButtonRequestGeoLocation)
	}
}

func TestDemoKeyboardHasEveryButtonType(t *testing.T) {
	got := buttonsByType(demoKeyboard().Attachment().Payload.Buttons)

	for _, want := range []string{
		maxapi.ButtonCallback,
		maxapi.ButtonLink,
		maxapi.ButtonRequestContact,
		maxapi.ButtonRequestGeoLocation,
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("в демо-клавиатуре нет кнопки типа %q; есть: %v", want, got)
		}
	}
}

// Подпись кнопки не должна разъезжаться между отдельной клавиатурой команды и
// демонстрационной — иначе человек увидит две разные кнопки для одного и того
// же действия.
func TestRequestButtonLabelsMatchAcrossKeyboards(t *testing.T) {
	demo := buttonsByType(demoKeyboard().Attachment().Payload.Buttons)

	b, sender := newTestBot()
	b.Handle(context.Background(), textMessage("/phone"))
	b.Handle(context.Background(), textMessage("/location"))
	if len(sender.messages) != 2 {
		t.Fatalf("отправлено сообщений: %d, want 2", len(sender.messages))
	}

	phone := buttonsByType(keyboardOf(t, sender.messages[0].Body))
	location := buttonsByType(keyboardOf(t, sender.messages[1].Body))

	if phone[maxapi.ButtonRequestContact] != demo[maxapi.ButtonRequestContact] {
		t.Errorf("подпись кнопки контакта: %q в /phone и %q в демо-клавиатуре",
			phone[maxapi.ButtonRequestContact], demo[maxapi.ButtonRequestContact])
	}
	if location[maxapi.ButtonRequestGeoLocation] != demo[maxapi.ButtonRequestGeoLocation] {
		t.Errorf("подпись кнопки геопозиции: %q в /location и %q в демо-клавиатуре",
			location[maxapi.ButtonRequestGeoLocation], demo[maxapi.ButtonRequestGeoLocation])
	}
}
```

`TestHelpCommandListsAllCommands` и `TestCommandsAreValidForAPI` уже есть в файле и начнут проверять новые команды сами: обе ходят по `Commands()`.

- [x] **Step 2: Убедиться, что тесты падают**

Run: `go test ./internal/bot/ -run 'TestPhoneCommand|TestLocationCommand|TestDemoKeyboard|TestRequestButtonLabels' -v`
Expected: FAIL — `maxapi.ButtonRequestContact undefined`.

- [x] **Step 3: Добавить типы кнопок**

`internal/maxapi/models.go`, блок констант типов кнопок (строка 196):

```go
// Типы кнопок, используемые ботом.
//
// request_contact и request_geo_location отдельного события не порождают: по
// нажатию клиент присылает обычное message_created с вложением contact или
// location.
const (
	ButtonCallback           = "callback"
	ButtonLink               = "link"
	ButtonRequestContact     = "request_contact"
	ButtonRequestGeoLocation = "request_geo_location"
)
```

- [x] **Step 4: Добавить билдеры кнопок**

В конец `internal/bot/keyboard.go`:

```go
// RequestContactButton — кнопка, по нажатию которой клиент присылает боту
// новое сообщение с вложением contact: там телефон и, если человек есть в
// базе Max, его user_id.
func RequestContactButton(text string) maxapi.Button {
	return maxapi.Button{Type: maxapi.ButtonRequestContact, Text: text}
}

// RequestGeoLocationButton — то же для геопозиции: вложение location в новом
// сообщении.
//
// У кнопки есть ещё поле quick — отправка без запроса подтверждения. Бот им не
// пользуется, поэтому и в модели кнопки его нет: подтверждение ближе к тому,
// как это выглядит у человека в клиенте.
func RequestGeoLocationButton(text string) maxapi.Button {
	return maxapi.Button{Type: maxapi.ButtonRequestGeoLocation, Text: text}
}
```

- [x] **Step 5: Добавить команды и клавиатуры**

`internal/bot/bot.go`, блок констант команд (строка 21):

```go
const (
	cmdStart    = "start"
	cmdHelp     = "help"
	cmdButtons  = "buttons"
	cmdPhone    = "phone"
	cmdLocation = "location"
)

// Подписи кнопок, которыми бот просит данные у клиента. Каждая нужна дважды —
// в клавиатуре своей команды и в демонстрационной, — и разъезжаться им незачем.
const (
	btnShareContact  = "Поделиться номером"
	btnShareLocation = "Отправить геопозицию"
)
```

В `Commands()` (строка 56) — две строки к списку:

```go
		{Name: cmdPhone, Description: "Прислать свой номер телефона"},
		{Name: cmdLocation, Description: "Прислать свою геопозицию"},
```

В `handleMessage`, в `switch name` — два новых случая после `cmdButtons`:

```go
		case cmdPhone:
			b.send(ctx, u, maxapi.NewMessageBody{
				Text: "Нажмите кнопку, чтобы поделиться номером телефона.",
				Attachments: []maxapi.AttachmentRequest{
					NewKeyboard().Row(RequestContactButton(btnShareContact)).Attachment(),
				},
			})
			return
		case cmdLocation:
			b.send(ctx, u, maxapi.NewMessageBody{
				Text: "Нажмите кнопку, чтобы отправить свою геопозицию.",
				Attachments: []maxapi.AttachmentRequest{
					NewKeyboard().Row(RequestGeoLocationButton(btnShareLocation)).Attachment(),
				},
			})
			return
```

`demoKeyboard()` (строка 168):

```go
// demoKeyboard — клавиатура из callback-кнопок, кнопок-запросов и ссылки.
func demoKeyboard() *Keyboard {
	row := make([]maxapi.Button, 0, len(callbackButtons))
	for _, b := range callbackButtons {
		row = append(row, CallbackButton(b.Text, b.Payload))
	}

	// Каждая кнопка-запрос занимает свой ряд: Max рекомендует не длиннее
	// 10 символов на подпись при двух кнопках в ряду, а обе заметно длиннее.
	return NewKeyboard().
		Row(row...).
		Row(RequestContactButton(btnShareContact)).
		Row(RequestGeoLocationButton(btnShareLocation)).
		Row(LinkButton("Документация", docsURL))
}
```

- [x] **Step 6: Прогнать тесты**

Run: `go test ./... && go test -race ./...`
Expected: PASS. Проверить, что прошли и старые `TestButtonsCommandSendsKeyboard`, `TestCallbackIsAnsweredWithButtonLabel`: оба ходят по всей демо-клавиатуре и новые типы кнопок пропускают.

- [x] **Step 7: Коммит**

```bash
git add internal/maxapi/models.go internal/bot/keyboard.go internal/bot/bot.go internal/bot/bot_test.go
git commit -m "Просить у клиента номер телефона и геопозицию"
```

---

### Task 4: Приём контакта

Вторая половина: разобрать то, что клиент прислал. Здесь же лечится главный блокер — `handleMessage` выходит первой же строкой при пустом тексте (`internal/bot/bot.go:93`), а у сообщения с контактом текста нет вовсе.

**Files:**
- Modify: `internal/bot/bot.go:92-96` (порядок проверок), новый `handleContact` после `handleMessage`
- Test: `internal/bot/bot_test.go`

**Interfaces:**
- Consumes: `maxapi.Update.Contact()`, `maxapi.ContactPayload.Phone()`, `.Name()` из задач 1-2
- Produces: обработку `message_created` с вложением `contact`

- [x] **Step 1: Написать падающие тесты**

Добавить в `internal/bot/bot_test.go`:

```go
const vcardIvan = "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Иван Петров\r\nTEL;TYPE=CELL:+79991234567\r\nEND:VCARD\r\n"

// contactMessage строит message_created с вложением contact и без текста —
// именно так выглядит ответ клиента на кнопку request_contact.
func contactMessage(payload maxapi.ContactPayload) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageCreated,
		Message: &maxapi.Message{
			Sender:    &maxapi.User{UserID: 42, FirstName: "Иван"},
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body: maxapi.MessageBody{
				Attachments: []maxapi.Attachment{{
					Type:    maxapi.AttachmentContact,
					Payload: &payload,
				}},
			},
		},
	}
}

func TestContactIsAnsweredWithPhoneNameAndUserID(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), contactMessage(maxapi.ContactPayload{
		VcfInfo: vcardIvan,
		Hash:    "9f2c",
		MaxInfo: &maxapi.User{UserID: 42, FirstName: "Иван", LastName: "Петров"},
	}))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	for _, want := range []string{
		"+79991234567",
		"Иван Петров",
		"user_id: 42",
		"Номер привязан к аккаунту Max: да",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("текст = %q, want подстроку %q", text, want)
		}
	}
}

func TestContactWithoutPhoneIsRefused(t *testing.T) {
	b, sender := newTestBot()

	// Имя и user_id на месте, номера нет — а он здесь и есть предмет.
	b.Handle(context.Background(), contactMessage(maxapi.ContactPayload{
		VcfInfo: "BEGIN:VCARD\r\nFN:Иван Петров\r\nEND:VCARD\r\n",
		MaxInfo: &maxapi.User{UserID: 42, FirstName: "Иван"},
	}))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if !strings.Contains(text, "номера телефона в нём нет") {
		t.Errorf("текст = %q, want отбойник про отсутствие номера", text)
	}
}

func TestContactWithoutMaxInfoOmitsUserID(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), contactMessage(maxapi.ContactPayload{VcfInfo: vcardIvan}))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if !strings.Contains(text, "+79991234567") {
		t.Errorf("текст = %q, want номер телефона", text)
	}
	if strings.Contains(text, "user_id") {
		t.Errorf("текст = %q, want без строки user_id: max_info не пришёл", text)
	}
	if !strings.Contains(text, "Номер привязан к аккаунту Max: нет") {
		t.Errorf("текст = %q, want отметку «нет»: hash пуст", text)
	}
}
```

- [x] **Step 2: Убедиться, что тесты падают**

Run: `go test ./internal/bot/ -run TestContact -v`
Expected: FAIL — «отправлено сообщений: 0, want 1»: сообщение без текста сейчас отбрасывается.

- [x] **Step 3: Переставить проверки и добавить обработчик**

`internal/bot/bot.go`, начало `handleMessage` (строки 92-96):

```go
func (b *Bot) handleMessage(ctx context.Context, u maxapi.Update) {
	// Вложение проверяется раньше текста: у сообщения, которым клиент делится
	// контактом, текста нет вовсе. Заодно это покрывает контакт, отправленный
	// из чата скрепкой, — без всякой кнопки от бота.
	if c := u.Contact(); c != nil {
		b.handleContact(ctx, u, c)
		return
	}

	if u.Text() == "" {
		// Сообщение без текста — например, только вложение. Демобот их не умеет.
		return
	}
```

После `handleMessage` — новый обработчик:

```go
// handleContact отвечает на вложение contact: им клиент делится своим номером
// в ответ на кнопку request_contact.
func (b *Bot) handleContact(ctx context.Context, u maxapi.Update, c *maxapi.ContactPayload) {
	phone := c.Phone()
	if phone == "" {
		b.send(ctx, u, maxapi.NewMessageBody{
			Text: "Контакт пришёл, но номера телефона в нём нет.",
		})
		return
	}

	lines := []string{"Спасибо! Вот что пришло:", "Телефон: " + phone}
	if name := c.Name(); name != "" {
		lines = append(lines, "Имя: "+name)
	}
	if c.MaxInfo != nil {
		lines = append(lines, fmt.Sprintf("user_id: %d", c.MaxInfo.UserID))
	}
	// Контракт описывает hash как признак того, что номер привязан к аккаунту
	// Max, но алгоритм не публикует — сверить значение не с чем. Поэтому строка
	// сообщает только о наличии поля и проверкой не является.
	lines = append(lines, "Номер привязан к аккаунту Max: "+yesNo(c.Hash != ""))

	b.send(ctx, u, maxapi.NewMessageBody{Text: strings.Join(lines, "\n")})
}

func yesNo(v bool) string {
	if v {
		return "да"
	}
	return "нет"
}
```

Дописать `"strings"` в импорты `internal/bot/bot.go` — сейчас файл импортирует `context`, `fmt`, `log/slog` и `maxbotdemo/internal/maxapi`.

- [x] **Step 4: Прогнать тесты**

Run: `go test ./internal/bot/ -run TestContact -v`
Expected: PASS

- [x] **Step 5: Прогнать всё**

Run: `go test ./... && go test -race ./...`
Expected: PASS. Отдельно убедиться, что `TestMessageWithoutTextIsIgnored` по-прежнему проходит: сообщение без текста **и без вложений** так и должно игнорироваться.

- [x] **Step 6: Коммит**

```bash
git add internal/bot/bot.go internal/bot/bot_test.go
git commit -m "Отвечать на присланный контакт"
```

---

### Task 5: Приём геопозиции

**Files:**
- Modify: `internal/bot/bot.go` (ветка в `handleMessage`, новый `handleLocation`)
- Test: `internal/bot/bot_test.go`

**Interfaces:**
- Consumes: `maxapi.Update.Location()`, `maxapi.Location` из задачи 1
- Produces: обработку `message_created` с вложением `location`

- [x] **Step 1: Написать падающие тесты**

Добавить в `internal/bot/bot_test.go`:

```go
// locationMessage строит message_created с вложением location и без текста.
func locationMessage(lat, lon float64) maxapi.Update {
	return maxapi.Update{
		UpdateType: maxapi.UpdateMessageCreated,
		Message: &maxapi.Message{
			Sender:    &maxapi.User{UserID: 42, FirstName: "Иван"},
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body: maxapi.MessageBody{
				Attachments: []maxapi.Attachment{{
					Type:      maxapi.AttachmentLocation,
					Latitude:  &lat,
					Longitude: &lon,
				}},
			},
		},
	}
}

func TestLocationIsAnsweredWithCoordinates(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), locationMessage(55.751244, 37.618423))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	for _, want := range []string{"55.751244", "37.618423"} {
		if !strings.Contains(text, want) {
			t.Errorf("текст = %q, want подстроку %q", text, want)
		}
	}
}

// %v напечатал бы мелкую координату как 1e-07 — человеку это не координата.
func TestLocationCoordinatesArePrintedWithoutExponent(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), locationMessage(0.0000001, -0.0000002))

	if len(sender.messages) != 1 {
		t.Fatalf("отправлено сообщений: %d, want 1", len(sender.messages))
	}
	text := sender.messages[0].Body.Text
	if strings.Contains(text, "e-") || strings.Contains(text, "e+") {
		t.Errorf("текст = %q, want координаты без экспоненты", text)
	}
}

func TestLocationWithoutCoordinatesIsIgnored(t *testing.T) {
	b, sender := newTestBot()

	b.Handle(context.Background(), maxapi.Update{
		UpdateType: maxapi.UpdateMessageCreated,
		Message: &maxapi.Message{
			Recipient: maxapi.Recipient{ChatID: 777, UserID: 42},
			Body: maxapi.MessageBody{
				Attachments: []maxapi.Attachment{{Type: maxapi.AttachmentLocation}},
			},
		},
	})

	if len(sender.messages) != 0 {
		t.Errorf("отправлено сообщений: %d, want 0", len(sender.messages))
	}
}
```

- [x] **Step 2: Убедиться, что тесты падают**

Run: `go test ./internal/bot/ -run TestLocation -v`
Expected: FAIL — `TestLocationIsAnsweredWithCoordinates` и `TestLocationCoordinatesArePrintedWithoutExponent` дают «отправлено сообщений: 0, want 1». `TestLocationWithoutCoordinatesIsIgnored` пройдёт сразу — так и должно быть, он сторожит, что дальше это поведение не сломается.

- [x] **Step 3: Добавить ветку и обработчик**

`internal/bot/bot.go`, в `handleMessage` — сразу после ветки контакта:

```go
	if l := u.Location(); l != nil {
		b.handleLocation(ctx, u, l)
		return
	}
```

После `handleContact`:

```go
// handleLocation отвечает на вложение location.
func (b *Bot) handleLocation(ctx context.Context, u maxapi.Update, l *maxapi.Location) {
	b.send(ctx, u, maxapi.NewMessageBody{Text: strings.Join([]string{
		"Спасибо! Вот что пришло:",
		"Широта: " + formatCoord(l.Latitude),
		"Долгота: " + formatCoord(l.Longitude),
	}, "\n")})
}

// formatCoord печатает координату без экспоненты: %v дал бы 1e-07 вместо
// 0.0000001, а это человеку уже не координата.
func formatCoord(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
```

Дописать `"strconv"` в импорты `internal/bot/bot.go`.

- [x] **Step 4: Прогнать тесты**

Run: `go test ./internal/bot/ -run TestLocation -v`
Expected: PASS

- [x] **Step 5: Прогнать всё**

Run: `go test ./... && go test -race ./...`
Expected: PASS

- [x] **Step 6: Коммит**

```bash
git add internal/bot/bot.go internal/bot/bot_test.go
git commit -m "Отвечать на присланную геопозицию"
```

---

### Task 6: Документация

**Files:**
- Modify: `README.md` — таблица «Что умеет», новый абзац после неё, раздел «Известные ограничения»

**Interfaces:**
- Consumes: поведение из задач 3-5
- Produces: ничего для кода

- [x] **Step 1: Дополнить таблицу «Что умеет»**

В README.md в таблице после строки про `/buttons` добавить:

```markdown
| `/phone` | сообщение с кнопкой «Поделиться номером» |
| `/location` | сообщение с кнопкой «Отправить геопозицию» |
```

И после строки про нажатие кнопки:

```markdown
| пользователь поделился контактом или геопозицией | разбор вложения: телефон, имя, `user_id` — или координаты |
```

- [x] **Step 2: Добавить абзац про то, где живёт телефон**

Сразу после таблицы «Что умеет»:

```markdown
Телефона и координат в схеме `User` нет вовсе — до бота они доезжают только
вложениями `contact` и `location`. Кнопки `request_contact` и
`request_geo_location` отдельного события не порождают: по нажатию клиент
присылает обычное `message_created` с таким вложением и **без текста**. Отсюда
две вещи в коде: `handleMessage` смотрит вложения раньше текста, а телефон
достаётся разбором строки `TEL` из карточки VCARD (`internal/maxapi/vcard.go`).

Тем же обработчиком покрыт контакт, отправленный из чата скрепкой, без всякой
кнопки от бота.
```

- [x] **Step 3: Дополнить «Известные ограничения»**

В конец списка:

```markdown
- **Привязка номера к аккаунту не проверяется.** Контракт описывает поле `hash`
  вложения `contact` как признак того, что человек поделился номером своего
  аккаунта Max, но алгоритм не публикует — сверить значение не с чем. Бот
  сообщает только о наличии поля.
- **Разбор VCARD упрощён.** Экранирование значений (`\,`, `\;`) не
  разворачивается, параметр в кавычках с двоеточием внутри сломает разбор.
  Контракт таких карточек не порождает.
- **`quick` у кнопки геопозиции не используется** — клиент всегда спрашивает
  подтверждение.
```

- [x] **Step 4: Проверить, что README не разошёлся с кодом**

Run: `go test ./internal/bot/ -run TestHelpCommandListsAllCommands -v`
Expected: PASS — справка собирается из `Commands()`, так что список команд в боте и в README сверяется глазами по одному месту.

Прочитать таблицу «Что умеет» рядом с `bot.Commands()` и убедиться, что имена и описания совпадают.

- [x] **Step 5: Коммит**

```bash
git add README.md
git commit -m "Описать приём контакта и геопозиции"
```

---

### Task 7: Живая проверка против max-mock

Тесты закрывают логику, но не то, что живой клиент действительно пришлёт по нажатию. Мок это воспроизводит: `internal/core/contact.go` собирает вложение так же, как его собрал бы бот, и гонит через общий перевод в форму сообщения.

**Files:** ничего не меняется — это проверка. Если она что-то вскроет, правка идёт отдельным коммитом.

**Interfaces:**
- Consumes: всё, что сделано в задачах 1-5

- [x] **Step 1: Поднять мок и бота**

```bash
cd ../maxmoc && ./max-mock          # слушает :8080
```

В соседнем терминале, из корня `maxbotdemo`:

```bash
set -a && . ./.env && set +a
go run ./cmd/bot
```

Ожидается: в логе бота проверка токена, публикация команд, синхронизация подписки — без ошибок.

- [x] **Step 2: Заполнить карточку клиента**

В админке мока `http://localhost:8080/mock` открыть карточку клиента и задать телефон и координаты. Без телефона `ClientSendContact` отвечает `400`: контакт без номера мок считает бессмысленным.

- [x] **Step 3: Проверить телефон**

В веб-чате `http://localhost:8080/mock/chat/{botId}` отправить `/phone`, нажать «Поделиться номером».

Ожидается ответ с телефоном, именем, `user_id` и строкой «Номер привязан к аккаунту Max: да».

- [x] **Step 4: Проверить геопозицию**

Отправить `/location`, нажать «Отправить геопозицию», подтвердить.

Ожидается ответ с широтой и долготой из карточки клиента.

- [x] **Step 5: Проверить отправку без кнопки**

Отправить контакт и геопозицию кнопками 👤 и 📍 рядом со скрепкой — без команды бота.

Ожидаются те же два ответа: обработчик один и тот же.

- [x] **Step 6: Проверить демо-клавиатуру**

Отправить `/buttons`. Ожидается клавиатура из четырёх рядов: «Привет»/«Пока», «Поделиться номером», «Отправить геопозицию», «Документация». Обе новые кнопки работают так же, как из своих команд.

- [x] **Step 7: Проверить журнал**

Во вкладках «Журнал» и «Доставки» админки мока: все доставки `200`, в логе бота ни одной ошибки.

- [x] **Step 8: Записать результат в спеку**

В `docs/superpowers/specs/2026-08-06-contact-location-design.md`, в раздел «Тесты», дописать абзац с итогом живой проверки — как это сделано в
`2026-08-06-mock-profile-design.md`: что именно проверено, что при этом
вскрылось, и что осталось непроверенным (вырожденный случай «контакт без
номера» через мок не воспроизводится).

```bash
git add docs/superpowers/specs/2026-08-06-contact-location-design.md
git commit -m "Зафиксировать результаты живой проверки контакта и геопозиции"
```
