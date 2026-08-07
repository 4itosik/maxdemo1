package core

import (
	"encoding/json"
	"errors"
	"fmt"

	"maxmock/internal/events"
	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// SendMessage — POST /messages: бот отправляет сообщение в диалог.
// Ровно один из userID/chatID должен быть задан.
func (c *Core) SendMessage(bot *store.Bot, userID, chatID *int64, req wire.NewMessageBody) (*wire.Message, error) {
	d, err := c.resolveDialog(bot, userID, chatID)
	if err != nil {
		return nil, err
	}
	return c.appendMessage(bot, d, store.SenderBot, req)
}

// resolveDialog находит диалог по user_id или chat_id, проверяя, что он
// принадлежит этому боту: чужой диалог для бота не существует.
func (c *Core) resolveDialog(bot *store.Bot, userID, chatID *int64) (*store.Dialog, error) {
	var (
		d   *store.Dialog
		err error
	)
	switch {
	case chatID != nil:
		d, err = c.store.DialogByChatID(*chatID)
	case userID != nil:
		d, err = c.store.DialogByUser(bot.ID, *userID)
	default:
		return nil, fmt.Errorf("%w: не указан ни user_id, ни chat_id", ErrBadRequest)
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrChatNotFound
	}
	if err != nil {
		return nil, err
	}
	if d.BotID != bot.ID {
		return nil, ErrChatNotFound
	}
	return d, nil
}

// appendMessage сохраняет сообщение и рассылает message_created.
//
// Боту его собственные сообщения по webhook не уходят: живой Max их не
// присылает, и любой отвечающий бот на них зациклился бы — эхо порождало бы
// эхо. Веб-чат получает событие всегда: он играет роль клиента и обязан
// видеть обе стороны разговора.
func (c *Core) appendMessage(bot *store.Bot, d *store.Dialog, sender string, req wire.NewMessageBody) (*wire.Message, error) {
	attachments, err := c.buildAttachments(req)
	if err != nil {
		return nil, err
	}
	rec := &store.Message{ChatID: d.ChatID, BotID: bot.ID, Sender: sender}
	err = c.store.AppendMessage(rec, func(mid string, seq int64) ([]byte, error) {
		return json.Marshal(wire.MessageBody{Mid: mid, Seq: seq, Text: textOrEmpty(req.Text), Attachments: attachments})
	})
	if err != nil {
		return nil, err
	}

	msg, err := c.buildMessage(rec)
	if err != nil {
		return nil, err
	}
	if sender == store.SenderBot {
		c.publish(events.KindMessage, bot.ID, d.ChatID, msg, "", nil)
		return &msg, nil
	}

	// Время события берётся у самого сообщения, а не у часов: живой Max
	// присылает в message_created один и тот же timestamp снаружи и внутри —
	// событие описывает только что созданное сообщение, и расходиться им не
	// на чем. Отдельное обращение к часам разводило бы их на границе
	// миллисекунды.
	c.publish(events.KindMessage, bot.ID, d.ChatID, msg,
		wire.UpdateMessageCreated, wire.MessageCreatedUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageCreated, Timestamp: msg.Timestamp},
			Message:    msg,
			UserLocale: wire.Ptr("ru"),
		})
	return &msg, nil
}

// EditMessage — PUT /messages: бот правит собственное сообщение.
func (c *Core) EditMessage(bot *store.Bot, mid string, req wire.NewMessageBody) error {
	rec, err := c.ownMessage(bot, mid)
	if err != nil {
		return err
	}
	_, err = c.applyEdit(bot, rec, req)
	return err
}

// applyEdit применяет правку к сообщению и рассылает message_edited.
//
// Семантика полей задана контрактом: text == null — текст не меняется,
// attachments == null — вложения не трогаем, attachments == [] — удаляем все.
func (c *Core) applyEdit(bot *store.Bot, rec *store.Message, req wire.NewMessageBody) (*wire.Message, error) {
	var body wire.MessageBody
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		return nil, err
	}
	if req.Text != nil {
		body.Text = req.Text
	}
	if req.AttachmentsSet() {
		body.Attachments = nil
		for _, ar := range req.Attachments {
			att, err := c.attachmentFromRequest(ar)
			if err != nil {
				return nil, err
			}
			body.Attachments = append(body.Attachments, att)
		}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if err := c.store.UpdateMessageBody(rec.Mid, raw); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrMessageNotFound
		}
		return nil, err
	}
	rec.Body = raw

	msg, err := c.buildMessage(rec)
	if err != nil {
		return nil, err
	}
	// Боту его собственная правка по webhook не уходит — по той же причине, по
	// которой не уходят его собственные сообщения (см. appendMessage).
	// Замерено на проде: после POST /answers, заменившего текст сообщения,
	// пришёл только `200 {"success":true}`, события не было. Контракт говорит
	// то же самое: «Вы получите это событие, как только ПОЛЬЗОВАТЕЛЬ
	// отредактирует сообщение». Правка через PUT /messages — тот же случай:
	// действие бота, а не пользователя.
	//
	// Веб-чат событие получает всегда: он играет роль клиента и обязан
	// увидеть, что текст сообщения изменился.
	c.publish(events.KindMessageEdited, bot.ID, rec.ChatID, msg, "", nil)
	return &msg, nil
}

// DeleteMessage — DELETE /messages. В диалоге бот может удалять только
// собственные сообщения (так описано в контракте).
func (c *Core) DeleteMessage(bot *store.Bot, mid string) error {
	rec, err := c.ownMessage(bot, mid)
	if err != nil {
		return err
	}
	if err := c.store.RemoveMessage(mid); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrMessageNotFound
		}
		return err
	}
	c.publish(events.KindMessageRemoved, bot.ID, rec.ChatID,
		map[string]any{"message_id": mid, "chat_id": rec.ChatID},
		wire.UpdateMessageRemoved, wire.MessageRemovedUpdate{
			UpdateBase: wire.UpdateBase{UpdateType: wire.UpdateMessageRemoved, Timestamp: nowMS()},
			MessageID:  mid, ChatID: rec.ChatID, UserID: bot.UserID,
		})
	return nil
}

// ownMessage находит активное сообщение бота, проверяя права на него.
func (c *Core) ownMessage(bot *store.Bot, mid string) (*store.Message, error) {
	rec, err := c.store.MessageByMid(mid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, err
	}
	if rec.BotID != bot.ID {
		return nil, ErrMessageNotFound
	}
	if rec.Status == store.StatusRemoved {
		return nil, ErrMessageNotFound
	}
	if rec.Sender != store.SenderBot {
		return nil, ErrForbidden
	}
	return rec, nil
}

// AnswerCallback — POST /answers: ответ бота на нажатие кнопки.
// Если задано message, исходное сообщение с клавиатурой редактируется.
func (c *Core) AnswerCallback(bot *store.Bot, callbackID string, ans wire.CallbackAnswer) error {
	cb, err := c.store.CallbackByID(callbackID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrCallbackNotFound
	}
	if err != nil {
		return err
	}
	if cb.BotID != bot.ID {
		return ErrCallbackNotFound
	}

	if ans.Message != nil {
		rec, err := c.ownMessage(bot, cb.Mid)
		if err != nil {
			return err
		}
		if _, err := c.applyEdit(bot, rec, *ans.Message); err != nil {
			return err
		}
	}
	if err := c.store.MarkCallbackAnswered(callbackID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

// GetMessages — GET /messages.
func (c *Core) GetMessages(bot *store.Bot, f store.MessageFilter) (*wire.MessageList, error) {
	// Бот видит только свои диалоги: без фильтра по чату ограничиваем выборку
	// его сообщениями.
	if f.ChatID != nil {
		d, err := c.store.DialogByChatID(*f.ChatID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrChatNotFound
		}
		if err != nil {
			return nil, err
		}
		if d.BotID != bot.ID {
			return nil, ErrForbidden
		}
	}

	recs, err := c.store.ListMessages(f)
	if err != nil {
		return nil, err
	}
	out := &wire.MessageList{Messages: []wire.Message{}}
	for i := range recs {
		if recs[i].BotID != bot.ID {
			continue
		}
		msg, err := c.buildMessage(&recs[i])
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, msg)
	}
	return out, nil
}

// GetMessageByID — GET /messages/{messageId}.
func (c *Core) GetMessageByID(bot *store.Bot, mid string) (*wire.Message, error) {
	rec, err := c.store.MessageByMid(mid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, err
	}
	if rec.BotID != bot.ID || rec.Status == store.StatusRemoved {
		return nil, ErrMessageNotFound
	}
	msg, err := c.buildMessage(rec)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// Subscribe — POST /subscriptions.
func (c *Core) Subscribe(bot *store.Bot, req wire.SubscriptionRequestBody) error {
	if req.URL == "" {
		return fmt.Errorf("%w: пустой url подписки", ErrBadRequest)
	}
	_, err := c.store.AddSubscription(bot.ID, req.URL, req.UpdateTypes, req.Secret)
	return err
}

// Subscriptions — GET /subscriptions.
func (c *Core) Subscriptions(bot *store.Bot) (*wire.GetSubscriptionsResult, error) {
	subs, err := c.store.ListSubscriptions(bot.ID)
	if err != nil {
		return nil, err
	}
	out := &wire.GetSubscriptionsResult{Subscriptions: []wire.Subscription{}}
	for _, s := range subs {
		out.Subscriptions = append(out.Subscriptions, wire.Subscription{
			URL: s.URL, Time: s.CreatedAt, UpdateTypes: s.UpdateTypes,
		})
	}
	return out, nil
}

// Unsubscribe — DELETE /subscriptions.
func (c *Core) Unsubscribe(bot *store.Bot, url string) error {
	err := c.store.DeleteSubscription(bot.ID, url)
	if errors.Is(err, store.ErrNotFound) {
		// Реальный Max на отписку от неизвестного URL отвечает успехом:
		// операция идемпотентна, состояние после неё то же самое.
		return nil
	}
	return err
}

// CreateUpload — POST /uploads: резервирует токен и выдаёт адрес заливки.
func (c *Core) CreateUpload(bot *store.Bot, uploadType string) (*wire.UploadEndpoint, error) {
	att := &store.Attachment{BotID: bot.ID, Type: uploadType}
	if err := c.store.SaveAttachment(att); err != nil {
		return nil, err
	}
	return &wire.UploadEndpoint{URL: c.UploadURL(att.Token), Token: att.Token}, nil
}
