package maxapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// SendMessage отправляет сообщение адресату (POST /messages).
//
// API принимает ровно один из параметров chat_id и user_id. Если известен
// chat_id — используем его: он работает и для диалога, и для группового чата.
func (c *Client) SendMessage(ctx context.Context, to Target, body NewMessageBody) (Message, error) {
	if to.IsZero() {
		return Message{}, errors.New("SendMessage: не задан адресат (chat_id или user_id)")
	}

	q := url.Values{}
	if to.ChatID != 0 {
		q.Set("chat_id", strconv.FormatInt(to.ChatID, 10))
	} else {
		q.Set("user_id", strconv.FormatInt(to.UserID, 10))
	}

	var res SendMessageResult
	if err := c.do(ctx, http.MethodPost, "/messages", q, body, &res); err != nil {
		return Message{}, err
	}
	return res.Message, nil
}

// AnswerOnCallback отвечает на нажатие inline-кнопки (POST /answers).
func (c *Client) AnswerOnCallback(ctx context.Context, callbackID string, answer CallbackAnswer) error {
	if callbackID == "" {
		return errors.New("AnswerOnCallback: пустой callback_id")
	}

	q := url.Values{"callback_id": {callbackID}}
	return c.do(ctx, http.MethodPost, "/answers", q, answer, nil)
}
