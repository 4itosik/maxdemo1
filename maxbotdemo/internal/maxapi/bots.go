package maxapi

import (
	"context"
	"net/http"
)

// GetMyInfo возвращает информацию о боте (GET /me). Заодно проверяет токен.
func (c *Client) GetMyInfo(ctx context.Context) (BotInfo, error) {
	var info BotInfo
	if err := c.do(ctx, http.MethodGet, "/me", nil, nil, &info); err != nil {
		return BotInfo{}, err
	}
	return info, nil
}

// SetCommands публикует список команд бота (PATCH /me/commands).
// Пустой список удаляет все команды.
func (c *Client) SetCommands(ctx context.Context, commands []BotCommand) error {
	if commands == nil {
		commands = []BotCommand{}
	}
	return c.do(ctx, http.MethodPatch, "/me/commands", nil, BotCommandsPatch{Commands: commands}, nil)
}
