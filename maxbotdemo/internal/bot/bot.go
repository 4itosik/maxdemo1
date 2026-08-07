// Package bot содержит логику демонстрационного бота: как реагировать на
// события Max. Пакет не знает про HTTP — он работает через узкий интерфейс
// Sender, что позволяет тестировать поведение без сети.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"maxbotdemo/internal/maxapi"
)

// Sender — то, что боту нужно от клиента Max API.
type Sender interface {
	SendMessage(ctx context.Context, to maxapi.Target, body maxapi.NewMessageBody) (maxapi.Message, error)
	AnswerOnCallback(ctx context.Context, callbackID string, answer maxapi.CallbackAnswer) error
}

// Имена команд без ведущего слэша — в таком виде их принимает PATCH /me/commands.
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

// msgReceivedHeader открывает ответ и на контакт, и на геопозицию: смысл в
// обоих случаях один и тот же, разъезжаться ему незачем.
const msgReceivedHeader = "Спасибо! Вот что пришло:"

// Демонстрационные callback-кнопки: подпись, которую видит человек, и payload,
// который MAX присылает боту при нажатии.
//
// В событии message_callback подписи нет — только payload. Поэтому подписи
// хранятся здесь и служат одновременно для сборки клавиатуры и для ответа
// человеку: иначе на «Пока» пришлось бы отвечать «Вы нажали: bye».
var callbackButtons = []struct {
	Text    string
	Payload string
}{
	{Text: "Привет", Payload: "hello"},
	{Text: "Пока", Payload: "bye"},
}

// buttonLabel возвращает подпись кнопки по её payload. Для неизвестного
// payload — например, от кнопки в сообщении, отправленном прошлой версией
// бота — возвращает сам payload, чтобы ответ не оказался пустым.
func buttonLabel(payload string) string {
	for _, b := range callbackButtons {
		if b.Payload == payload {
			return b.Text
		}
	}
	return payload
}

const docsURL = "https://dev.max.ru/docs-api"

// Commands возвращает команды, которые бот публикует в меню клиента Max.
func Commands() []maxapi.BotCommand {
	return []maxapi.BotCommand{
		{Name: cmdStart, Description: "Начать разговор"},
		{Name: cmdHelp, Description: "Показать список команд"},
		{Name: cmdButtons, Description: "Прислать сообщение с кнопками"},
		{Name: cmdPhone, Description: "Прислать свой номер телефона"},
		{Name: cmdLocation, Description: "Прислать свою геопозицию"},
	}
}

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

// Bot обрабатывает события Max.
type Bot struct {
	api Sender
	log *slog.Logger
}

// New создаёт бота.
func New(api Sender, log *slog.Logger) *Bot {
	return &Bot{api: api, log: log}
}

// Handle разбирает событие и отвечает на него.
//
// Ошибку не возвращает: к моменту вызова webhook уже ответил Max'у 200 OK,
// и единственная осмысленная реакция на сбой — запись в лог.
func (b *Bot) Handle(ctx context.Context, u maxapi.Update) {
	switch u.UpdateType {
	case maxapi.UpdateBotStarted:
		b.sendGreeting(ctx, u)
	case maxapi.UpdateBotAdded:
		b.sendChatGreeting(ctx, u)
	case maxapi.UpdateBotStopped:
		b.handleBotStopped(u)
	case maxapi.UpdateMessageCreated:
		b.handleMessage(ctx, u)
	case maxapi.UpdateMessageEdited:
		b.handleEdit(ctx, u)
	case maxapi.UpdateMessageCallback:
		b.handleCallback(ctx, u)
	default:
		b.log.Debug("событие без обработчика", "update_type", u.UpdateType)
	}
}

func (b *Bot) handleMessage(ctx context.Context, u maxapi.Update) {
	// Вложение проверяется раньше текста: у сообщения, которым клиент делится
	// контактом или геопозицией, текста нет вовсе. Заодно это покрывает
	// контакт, отправленный из чата скрепкой, — без всякой кнопки от бота.
	if c := u.Contact(); c != nil {
		b.handleContact(ctx, u, c)
		return
	}

	if l := u.Location(); l != nil {
		b.handleLocation(ctx, u, l)
		return
	}

	if u.Text() == "" {
		// Сообщение без текста — например, только вложение. Демобот их не умеет.
		return
	}

	if name, _, ok := u.Command(); ok {
		switch name {
		case cmdStart:
			b.sendGreeting(ctx, u)
			return
		case cmdHelp:
			b.send(ctx, u, maxapi.NewMessageBody{Text: helpText()})
			return
		case cmdButtons:
			b.send(ctx, u, maxapi.NewMessageBody{
				Text:        "Нажмите любую кнопку:",
				Attachments: []maxapi.AttachmentRequest{demoKeyboard().Attachment()},
			})
			return
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
		}
		// Неизвестная команда — отвечаем эхом, как на обычный текст.
	}

	b.send(ctx, u, maxapi.NewMessageBody{
		Text: fmt.Sprintf("Вы написали: %s", u.Text()),
	})
}

// handleEdit отвечает на правку сообщения. Команду правка не выполняет: смысл
// ответа — показать само событие, а иначе оно неотличимо от нового сообщения.
func (b *Bot) handleEdit(ctx context.Context, u maxapi.Update) {
	// Собственные правки бота приходят обратно: POST /answers заменяет
	// сообщение с кнопками, и это порождает message_edited с sender = бот.
	// Без этой проверки каждое нажатие кнопки давало бы лишний ответ.
	//
	// Отсутствие отправителя тоже молчание: sender в контракте необязателен
	// (у схемы Message в required только recipient, body и timestamp), а
	// отвечать в канал на собственный пост хуже, чем пропустить чужую правку.
	if s := u.Sender(); s == nil || s.IsBot {
		// u.Message в этой ветке может быть nil: Handle вызывается на любом
		// событии, а гарантии, что message_edited всегда несёт message, нет.
		// Обращение к u.Message.Body.MID без проверки уронило бы бота именно
		// на таком событии.
		var mid string
		if u.Message != nil {
			mid = u.Message.Body.MID
		}
		b.log.Debug("правка пропущена", "mid", mid)
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

	lines := []string{msgReceivedHeader, "Телефон: " + phone}
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

// handleLocation отвечает на вложение location.
func (b *Bot) handleLocation(ctx context.Context, u maxapi.Update, l *maxapi.Location) {
	b.send(ctx, u, maxapi.NewMessageBody{Text: strings.Join([]string{
		msgReceivedHeader,
		"Широта: " + formatCoord(l.Latitude),
		"Долгота: " + formatCoord(l.Longitude),
	}, "\n")})
}

// formatCoord печатает координату без экспоненты: %v дал бы 1e-07 вместо
// 0.0000001, а это человеку уже не координата.
func formatCoord(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func (b *Bot) handleCallback(ctx context.Context, u maxapi.Update) {
	if u.Callback == nil || u.Callback.CallbackID == "" {
		b.log.Warn("событие message_callback без callback_id")
		return
	}

	text := fmt.Sprintf("Вы нажали: %s", buttonLabel(u.Callback.Payload))
	err := b.api.AnswerOnCallback(ctx, u.Callback.CallbackID, maxapi.CallbackAnswer{
		Message: &maxapi.NewMessageBody{Text: text},
	})
	if err != nil {
		b.log.Error("ответ на нажатие кнопки не отправлен",
			"callback_id", u.Callback.CallbackID, "error", err)
		return
	}
	b.log.Info("ответ на нажатие кнопки отправлен", "payload", u.Callback.Payload)
}

func (b *Bot) sendGreeting(ctx context.Context, u maxapi.Update) {
	greeting := "Здравствуйте!"
	if name := u.Sender().DisplayName(); name != "" {
		greeting = fmt.Sprintf("Здравствуйте, %s!", name)
	}

	b.send(ctx, u, maxapi.NewMessageBody{
		Text:        greeting + " Я демонстрационный бот Max.\n\n" + helpText(),
		Attachments: []maxapi.AttachmentRequest{demoKeyboard().Attachment()},
	})
}

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

// send отправляет сообщение туда, откуда пришло событие.
func (b *Bot) send(ctx context.Context, u maxapi.Update, body maxapi.NewMessageBody) {
	to := u.Target()
	if to.IsZero() {
		b.log.Warn("не удалось определить адресата", "update_type", u.UpdateType)
		return
	}

	if _, err := b.api.SendMessage(ctx, to, body); err != nil {
		b.log.Error("сообщение не отправлено",
			"update_type", u.UpdateType, "chat_id", to.ChatID, "user_id", to.UserID, "error", err)
		return
	}
	b.log.Info("ответ отправлен", "chat_id", to.ChatID, "user_id", to.UserID)
}

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

// helpText собирает справку из того же списка команд, что публикуется в Max.
func helpText() string {
	text := "Доступные команды:"
	for _, c := range Commands() {
		text += fmt.Sprintf("\n/%s — %s", c.Name, c.Description)
	}
	return text
}
