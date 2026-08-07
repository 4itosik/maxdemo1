import "@typespec/http";
import "@typespec/openapi";
import "./common.tsp";
import "./models/users.tsp";
import "./models/keyboard.tsp";
import "./models/markup.tsp";
import "./models/attachments.tsp";
import "./models/attachment-requests.tsp";
import "./models/messages.tsp";
import "./models/chats.tsp";
import "./models/updates.tsp";
import "./routes/bots.tsp";
// import "./routes/chats.tsp";
import "./routes/messages.tsp";
import "./routes/subscriptions.tsp";
import "./routes/uploads.tsp";
import "./webhook.tsp";

using Http;
using OpenAPI;

/** Бот-платформа мессенджера MAX. https://dev.max.ru/docs-api */
@service(#{ title: "Max Bot API" })
@info(#{ version: "0.0.32" })
@server("https://platform-api2.max.ru")
@useAuth(ApiKeyAuth<ApiKeyLocation.query, "access_token"> | BearerAuth)
namespace MaxBotApi;
