package core

import (
	"encoding/json"
	"errors"
	"fmt"

	"maxmock/internal/events"
	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// Действия клиента доступны через control-API: их выполняет тестировщик,
// играющий роль пользователя мессенджера.

// dialogContext собирает всё, что нужно для действия клиента в диалоге.
func (c *Core) dialogContext(chatID int64) (*store.Dialog, *store.Bot, *store.Client, error) {
	d, err := c.store.DialogByChatID(chatID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, nil, ErrChatNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	bot, cl, err := c.dialogParties(d)
	if err != nil {
		return nil, nil, nil, err
	}
	return d, bot, cl, nil
}

// ClientStart — клиент нажал «Начать». Повторное нажатие ничего не делает:
// в реальном мессенджере кнопка исчезает после первого раза.
func (c *Core) ClientStart(chatID int64, payload string) error {
	d, bot, cl, err := c.dialogContext(chatID)
	if err != nil {
		return err
	}
	changed, err := c.store.SetDialogStarted(d.ChatID)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	upd := wire.BotStartedUpdate{
		UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateBotStarted, Timestamp: nowMS()},
		ChatID:     d.ChatID,
		User:       clientUser(cl),
		UserLocale: wire.Ptr("ru"),
	}
	if payload != "" {
		upd.Payload = wire.Ptr(payload)
	}
	c.publish(events.KindDialog, bot.ID, d.ChatID,
		map[string]any{"chat_id": d.ChatID, "started": true},
		wire.UpdateBotStarted, upd)
	return nil
}

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

// ClientSendMessage — клиент отправил сообщение: текст и/или вложения по
// токенам ранее загруженных файлов.
func (c *Core) ClientSendMessage(chatID int64, text string, attachmentTokens []string) (*wire.Message, error) {
	d, bot, _, err := c.dialogContext(chatID)
	if err != nil {
		return nil, err
	}
	req := wire.NewMessageBody{}
	if text != "" {
		req.Text = wire.Ptr(text)
	}
	for _, token := range attachmentTokens {
		ar, err := c.attachmentRequestForToken(token)
		if err != nil {
			return nil, err
		}
		req.Attachments = append(req.Attachments, ar)
	}
	if req.Text == nil && len(req.Attachments) == 0 {
		return nil, fmt.Errorf("%w: пустое сообщение", ErrBadRequest)
	}
	return c.appendMessage(bot, d, store.SenderClient, req)
}

// attachmentRequestForToken строит запрос на прикрепление уже загруженного
// в мок файла.
func (c *Core) attachmentRequestForToken(token string) (wire.AttachmentRequest, error) {
	att, err := c.store.AttachmentByToken(token)
	if errors.Is(err, store.ErrNotFound) {
		return wire.AttachmentRequest{}, ErrAttachmentNotFound
	}
	if err != nil {
		return wire.AttachmentRequest{}, err
	}
	if att.Type == wire.AttachmentImage {
		payload, err := json.Marshal(wire.PhotoRequestPayload{Token: wire.Ptr(att.Token)})
		if err != nil {
			return wire.AttachmentRequest{}, err
		}
		return wire.AttachmentRequest{Type: wire.AttachmentImage, Payload: payload}, nil
	}
	payload, err := json.Marshal(wire.UploadedInfo{Token: att.Token})
	if err != nil {
		return wire.AttachmentRequest{}, err
	}
	return wire.AttachmentRequest{Type: att.Type, Payload: payload}, nil
}

// ClientPressButton — клиент нажал инлайн-кнопку сообщения бота.
func (c *Core) ClientPressButton(chatID int64, mid, payload string) error {
	d, bot, cl, err := c.dialogContext(chatID)
	if err != nil {
		return err
	}
	rec, err := c.store.MessageByMid(mid)
	if errors.Is(err, store.ErrNotFound) {
		return ErrMessageNotFound
	}
	if err != nil {
		return err
	}
	if rec.ChatID != d.ChatID || rec.Status == store.StatusRemoved {
		return ErrMessageNotFound
	}
	if !hasCallbackButton(rec.Body, payload) {
		return fmt.Errorf("%w: в сообщении нет кнопки с такой полезной нагрузкой", ErrBadRequest)
	}

	cb := &store.Callback{BotID: bot.ID, ChatID: d.ChatID, Mid: mid, UserID: cl.UserID, Payload: payload}
	if err := c.store.SaveCallback(cb); err != nil {
		return err
	}
	msg, err := c.buildMessage(rec)
	if err != nil {
		return err
	}
	c.publish(events.KindMessage, bot.ID, d.ChatID,
		map[string]any{"callback_id": cb.CallbackID, "mid": mid, "payload": payload},
		// Время события — время самого нажатия, а не отдельное показание
		// часов: у прода эти два поля совпадают (см. live_sample_test.go),
		// потому что событие описывает только что случившееся действие.
		wire.UpdateMessageCallback, wire.MessageCallbackUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageCallback, Timestamp: cb.CreatedAt},
			Callback: wire.Callback{
				Timestamp:  cb.CreatedAt,
				CallbackID: cb.CallbackID,
				Payload:    payload,
				User:       clientUser(cl),
			},
			Message:    &msg,
			UserLocale: wire.Ptr("ru"),
		})
	return nil
}

// hasCallbackButton проверяет, что в теле сообщения есть callback-кнопка с
// такой полезной нагрузкой: нажать можно только то, что бот прислал.
func hasCallbackButton(body []byte, payload string) bool {
	var b wire.MessageBody
	if err := json.Unmarshal(body, &b); err != nil {
		return false
	}
	for _, att := range b.Attachments {
		var rows [][]wire.Button
		switch att.Type {
		case wire.AttachmentInlineKeyboard:
			var kb wire.Keyboard
			if err := json.Unmarshal(att.Payload, &kb); err != nil {
				continue
			}
			rows = kb.Buttons
		case wire.AttachmentReplyKeyboard:
			rows = att.Buttons
		default:
			continue
		}
		for _, row := range rows {
			for _, btn := range row {
				if btn.Payload == payload {
					return true
				}
			}
		}
	}
	return false
}

// ClientEditMessage — клиент отредактировал своё сообщение.
func (c *Core) ClientEditMessage(chatID int64, mid, text string) error {
	rec, bot, err := c.clientMessage(chatID, mid)
	if err != nil {
		return err
	}
	var body wire.MessageBody
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		return err
	}
	body.Text = wire.Ptr(text)
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if err := c.store.UpdateMessageBody(mid, raw); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrMessageNotFound
		}
		return err
	}
	rec.Body = raw

	msg, err := c.buildMessage(rec)
	if err != nil {
		return err
	}
	c.publish(events.KindMessageEdited, bot.ID, chatID, msg,
		wire.UpdateMessageEdited, wire.MessageEditedUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageEdited, Timestamp: nowMS()},
			Message:    msg,
		})
	return nil
}

// ClientDeleteMessage — клиент удалил своё сообщение.
func (c *Core) ClientDeleteMessage(chatID int64, mid string) error {
	rec, bot, err := c.clientMessage(chatID, mid)
	if err != nil {
		return err
	}
	if err := c.store.RemoveMessage(mid); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrMessageNotFound
		}
		return err
	}
	_, _, cl, err := c.dialogContext(chatID)
	if err != nil {
		return err
	}
	c.publish(events.KindMessageRemoved, bot.ID, rec.ChatID,
		map[string]any{"message_id": mid, "chat_id": rec.ChatID},
		wire.UpdateMessageRemoved, wire.MessageRemovedUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageRemoved, Timestamp: nowMS()},
			MessageID:  mid, ChatID: rec.ChatID, UserID: cl.UserID,
		})
	return nil
}

// clientMessage находит активное сообщение клиента в его диалоге.
func (c *Core) clientMessage(chatID int64, mid string) (*store.Message, *store.Bot, error) {
	d, bot, _, err := c.dialogContext(chatID)
	if err != nil {
		return nil, nil, err
	}
	rec, err := c.store.MessageByMid(mid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if rec.ChatID != d.ChatID || rec.Status == store.StatusRemoved {
		return nil, nil, ErrMessageNotFound
	}
	if rec.Sender != store.SenderClient {
		return nil, nil, ErrForbidden
	}
	return rec, bot, nil
}

// CreateClient заводит клиента с диалогом и сообщает об этом UI.
func (c *Core) CreateClient(botID int64, in store.ClientInput) (*store.Client, *store.Dialog, error) {
	cl, d, err := c.store.CreateClient(botID, in)
	if err != nil {
		return nil, nil, err
	}
	c.bus.Publish(events.Event{
		Kind: events.KindClient, BotID: botID, ChatID: d.ChatID,
		Payload: map[string]any{"client": cl, "dialog": d},
	})
	return cl, d, nil
}

// UIMessage — сообщение в виде, удобном веб-интерфейсу: тело в разобранном
// виде плюс служебные поля, которых нет в контракте Max (кто отправил, статус,
// время правки).
type UIMessage struct {
	Mid       string           `json:"mid"`
	Seq       int64            `json:"seq"`
	Sender    string           `json:"sender"`
	Status    string           `json:"status"`
	CreatedAt int64            `json:"created_at"`
	EditedAt  *int64           `json:"edited_at,omitempty"`
	Body      wire.MessageBody `json:"body"`
}

// DialogMessages возвращает переписку диалога в хронологическом порядке —
// так её и рисует чат.
func (c *Core) DialogMessages(chatID int64) ([]UIMessage, error) {
	if _, err := c.store.DialogByChatID(chatID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrChatNotFound
		}
		return nil, err
	}
	recs, err := c.store.ListMessages(store.MessageFilter{ChatID: &chatID, Count: 500})
	if err != nil {
		return nil, err
	}
	out := make([]UIMessage, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		rec := recs[i]
		var body wire.MessageBody
		if err := json.Unmarshal(rec.Body, &body); err != nil {
			return nil, err
		}
		out = append(out, UIMessage{
			Mid: rec.Mid, Seq: rec.Seq, Sender: rec.Sender, Status: rec.Status,
			CreatedAt: rec.CreatedAt, EditedAt: rec.EditedAt, Body: body,
		})
	}
	return out, nil
}

// SaveUploadedFile регистрирует файл, загруженный тестировщиком через UI.
func (c *Core) SaveUploadedFile(botID int64, uploadType, filename, mime, path string, size int64) (*store.Attachment, error) {
	att := &store.Attachment{
		BotID: botID, Type: uploadType, Filename: filename, Mime: mime,
		Size: size, Path: path, Uploaded: true,
	}
	if err := c.store.SaveAttachment(att); err != nil {
		return nil, err
	}
	return att, nil
}
