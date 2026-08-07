import "@typespec/http";
import "@typespec/openapi";

using Http;
using OpenAPI;

namespace MaxBotApi;

@tag("bots")
interface Bots {
  /** Возвращает информацию о боте, который идентифицируется с помощью токена доступа `access_token` */
  @summary("Получение информации о боте")
  @tag("bot-sending")
  @operationId("getMyInfo")
  @route("/me")
  @get
  getMyInfo(): BotInfo | Unauthorized | InternalError;

  /** Добавляет, изменяет или удаляет команды бота. Для удаления команд передайте пустой массив `commands` */
  @summary("Редактирование команд бота")
  @operationId("editMyCommands")
  @route("/me/commands")
  @patch(#{ implicitOptionality: false })
  editMyCommands(@body body: BotCommandsPatch): BotCommandsInfo | Unauthorized | InternalError;
}
