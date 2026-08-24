package maxapi

import (
	"strings"
)

// Типы событий. Полный список — в описании объекта Update в документации;
// здесь только те, на которые подписывается бот.
const (
	UpdateMessageCreated  = "message_created"
	UpdateMessageCallback = "message_callback"
	UpdateMessageEdited   = "message_edited"
	UpdateBotStarted      = "bot_started"
	UpdateBotAdded        = "bot_added"
	UpdateBotStopped      = "bot_stopped"
)

// Update — событие от Max. В API это oneOf с дискриминатором update_type,
// поэтому поля, специфичные для конкретного типа, объявлены указателями:
// пустой указатель означает «в этом типе события такого поля нет».
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

// Target — адресат исходящего сообщения.
type Target struct {
	ChatID int64
	UserID int64
}

// IsZero сообщает, что адресат не заполнен и отправлять сообщение некуда.
func (t Target) IsZero() bool { return t.ChatID == 0 && t.UserID == 0 }

// Target нормализует адресата ответа. У message_created, message_callback и
// message_edited он лежит в message.recipient, у bot_started и bot_added —
// в полях верхнего уровня.
//
// Осторожно: в диалоге message.recipient.user_id — это сам бот (получатель
// входящего сообщения), а не собеседник. Отвечать нужно по chat_id, поэтому
// SendMessage предпочитает его; user_id остаётся запасным вариантом для
// bot_started, где он указывает на человека.
func (u Update) Target() Target {
	if u.Message != nil {
		return Target{
			ChatID: u.Message.Recipient.ChatID,
			UserID: u.Message.Recipient.UserID,
		}
	}
	t := Target{ChatID: u.ChatID}
	if u.User != nil {
		t.UserID = u.User.UserID
	}
	return t
}

// Sender возвращает автора события или nil, если его определить нельзя.
func (u Update) Sender() *User {
	switch {
	case u.Message != nil && u.Message.Sender != nil:
		return u.Message.Sender
	case u.Callback != nil && u.Callback.User != nil:
		return u.Callback.User
	default:
		return u.User
	}
}

// Text возвращает текст сообщения или пустую строку, если текста нет.
func (u Update) Text() string {
	if u.Message == nil {
		return ""
	}
	return u.Message.Body.Text
}

// Command разбирает текст сообщения как команду: "/help меня" → ("help", "меня", true).
// В групповых чатах Max дописывает к команде идентификатор бота ("/help:id-773") —
// суффикс отбрасывается. Имя команды приводится к нижнему регистру.
// Для текста, не начинающегося со слэша, ok == false.
func (u Update) Command() (name, args string, ok bool) {
	text := strings.TrimSpace(u.Text())
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}

	head, rest, _ := strings.Cut(text[1:], " ")
	head, _, _ = strings.Cut(head, ":")
	if head == "" {
		return "", "", false
	}
	return strings.ToLower(head), strings.TrimSpace(rest), true
}

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

// User — пользователь или бот.
type User struct {
	UserID    int64  `json:"user_id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
	IsBot     bool   `json:"is_bot"`
}

// DisplayName возвращает имя для обращения к пользователю.
func (u *User) DisplayName() string {
	if u == nil {
		return ""
	}
	if name := strings.TrimSpace(u.FirstName + " " + u.LastName); name != "" {
		return name
	}
	return u.Username
}

// Recipient — получатель сообщения: диалог с пользователем либо чат.
type Recipient struct {
	ChatID   int64  `json:"chat_id,omitempty"`
	ChatType string `json:"chat_type,omitempty"`
	UserID   int64  `json:"user_id,omitempty"`
}

// Message — сообщение в чате.
type Message struct {
	Sender    *User       `json:"sender,omitempty"`
	Recipient Recipient   `json:"recipient"`
	Timestamp int64       `json:"timestamp"`
	Body      MessageBody `json:"body"`
}

// MessageBody — содержимое сообщения.
type MessageBody struct {
	MID         string       `json:"mid,omitempty"`
	Seq         int64        `json:"seq,omitempty"`
	Text        string       `json:"text,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Callback приходит, когда пользователь нажал inline-кнопку.
type Callback struct {
	Timestamp  int64  `json:"timestamp"`
	CallbackID string `json:"callback_id"`
	Payload    string `json:"payload"`
	User       *User  `json:"user,omitempty"`
}

// NewMessageBody — тело запроса на отправку или изменение сообщения.
//
// text, attachments и link объявлены в контракте обязательными и при этом
// nullable: незаполненные уходят как null, а не опускаются. Живой Max
// принимает тело и без них, но эмулятор max-mock проверяет запросы по
// контракту — и он прав по букве спецификации.
type NewMessageBody struct {
	Text        string              `json:"text"`
	Attachments []AttachmentRequest `json:"attachments"`
	Link        *NewMessageLink     `json:"link"`
	Notify      *bool               `json:"notify,omitempty"`
	Format      string              `json:"format,omitempty"` // FormatMarkdown | FormatHTML
}

// NewMessageLink — ссылка на исходное сообщение (пересылка или ответ).
// Демонстрационный бот её не использует, но поле обязано присутствовать.
type NewMessageLink struct {
	Type string `json:"type"` // forward | reply
	Mid  string `json:"mid"`
}

// Форматы разметки текста сообщения.
const (
	FormatMarkdown = "markdown"
	FormatHTML     = "html"
)

// CallbackAnswer — ответ на нажатие кнопки. Message заменяет исходное сообщение.
type CallbackAnswer struct {
	Message *NewMessageBody `json:"message,omitempty"`
}

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

// KeyboardPayload — двумерный массив кнопок: внешний уровень задаёт ряды.
type KeyboardPayload struct {
	Buttons [][]Button `json:"buttons"`
}

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

// Button — кнопка клавиатуры. Набор полей зависит от Type.
type Button struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
}

// BotInfo — ответ GET /me.
type BotInfo struct {
	User
	Description string       `json:"description,omitempty"`
	Commands    []BotCommand `json:"commands,omitempty"`
}

// BotCommand — команда бота, показываемая в меню клиента Max.
type BotCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// BotCommandsPatch — тело запроса PATCH /me/commands.
type BotCommandsPatch struct {
	Commands []BotCommand `json:"commands"`
}

// SendMessageResult — ответ POST /messages.
type SendMessageResult struct {
	Message Message `json:"message"`
}

// SimpleQueryResult — типовой ответ вида {"success": true}.
type SimpleQueryResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// Subscription — действующая webhook-подписка бота.
type Subscription struct {
	URL         string   `json:"url"`
	Time        int64    `json:"time"`
	UpdateTypes []string `json:"update_types"`
}

// GetSubscriptionsResult — ответ GET /subscriptions.
type GetSubscriptionsResult struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

// SubscriptionRequestBody — тело запроса POST /subscriptions.
type SubscriptionRequestBody struct {
	URL         string   `json:"url"`
	UpdateTypes []string `json:"update_types,omitempty"`
	Secret      string   `json:"secret,omitempty"`
}
