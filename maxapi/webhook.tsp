import "@typespec/http";
import "@typespec/openapi";
import "@typespec/openapi3";
import "./models/updates.tsp";

using Http;
using OpenAPI;

/**
 * Контракт webhook-эндпоинта, который реализует РАЗРАБОТЧИК БОТА (а не
 * платформа MAX). После регистрации URL через `POST /subscriptions`
 * платформа доставляет события бота HTTPS POST-запросами на этот эндпоинт.
 *
 * Требования платформы: HTTPS, порт 443, сертификат от доверенного центра
 * (самоподписные и HTTP не поддерживаются с 25 мая 2026). Отвечайте 200 OK
 * как можно быстрее; обработку события выполняйте после ответа.
 */
@service(#{ title: "Max Bot Webhook" })
// TODO: contact — плейсхолдер (требование SCHEMA-валидатора: info.contact
// обязателен); заменить на реальные контакты команды перед публикацией.
@info(#{
  contact: #{
    name: "Max Bot Webhook (placeholder)",
    url: "https://dev.max.ru/docs-api",
    email: "bot-api-placeholder@example.com",
  },
})
@server("https://bot.example.ru", "Ваш webhook-endpoint (URL из POST /subscriptions)")
namespace MaxBotWebhook;

@tag("webhook-server")
interface WebhookReceiver {
  /**
   * Приём события от платформы MAX. Тело описано двумя эквивалентными
   * способами (anyOf — любое событие соответствует обоим):
   * `UpdateUnified` — единая «плоская» схема (общие поля обязательны,
   * вариативные опциональны, у каждого указано, при каких `update_type`
   * приходит); `WebhookUpdate` — строгий oneOf из 16 конкретных типов
   * для типизированной кодогенерации.
   *
   * Если при подписке был задан `secret`, платформа передаёт его в заголовке
   * `X-Max-Bot-Api-Secret` — проверяйте его, чтобы убедиться, что запрос
   * пришёл от MAX, а не от постороннего.
   */
  @summary("Приём события от платформы MAX")
  @route("/")
  @post
  receiveUpdate(
    /** Секрет, заданный при создании подписки (см. SubscriptionRequestBody.secret) */
    // 5..256 — значение из официальной схемы MAX (SubscriptionRequestBody.secret)
    @minLength(5)
    @maxLength(256)
    @pattern("^[a-zA-Z0-9_-]{5,256}$")
    @header("X-Max-Bot-Api-Secret") secret?: string,

    /** Событие; какие поля заполнены — определяется значением update_type */
    @body update: UpdateUnified | WebhookUpdate,
  ): AcceptedResponse;
}

/**
 * Единая («плоская») схема события: объединение полей всех 16 типов
 * `Update` в один объект — в стиле Telegram Bot API. Обязательны только
 * `update_type` и `timestamp`; остальные поля приходят в зависимости от
 * типа события. Удобна, когда на сервере хочется один класс/структуру
 * на все события без union-машинерии.
 */
model UpdateUnified {
  /**
   * Тип события: message_created | message_callback | message_edited |
   * message_removed | message_chat_created | bot_added | bot_removed |
   * bot_started | bot_stopped | chat_title_changed | dialog_cleared |
   * dialog_muted | dialog_unmuted | dialog_removed | user_added |
   * user_removed
   */
  @maxLength(64)
  @pattern("^[a-z_]+$")
  update_type: string;

  /** Unix-время, когда произошло событие */
  @minValue(0)
  @maxValue(9007199254740991)
  timestamp: int64;

  /** ID чата. [bot_added, bot_started, bot_stopped, bot_removed, chat_title_changed, dialog_cleared, dialog_muted, dialog_unmuted, dialog_removed, message_removed, user_added, user_removed] */
  @minValue(0)
  @maxValue(9007199254740991)
  chat_id?: int64;

  /** Пользователь-инициатор события. [bot_added, bot_started, bot_stopped, bot_removed, chat_title_changed, dialog_cleared, dialog_muted, dialog_unmuted, dialog_removed, user_added, user_removed] */
  user?: MaxBotApi.User;

  /** Текущий язык пользователя (IETF BCP 47), доступен только в диалогах. [bot_started, bot_stopped, dialog_cleared, dialog_muted, dialog_unmuted, dialog_removed, message_created, message_callback] */
  @maxLength(255)
  user_locale?: string | null;

  /** Признак «событие в канале, а не в чате». [bot_added, bot_removed, user_added, user_removed] */
  is_channel?: boolean;

  /** Сообщение: новое, отредактированное или с нажатой кнопкой (может быть null). [message_created, message_edited, message_callback] */
  message?: MaxBotApi.Message | null;

  /** Нажатие callback-кнопки. [message_callback] */
  callback?: MaxBotApi.Callback;

  /** Новое название чата. [chat_title_changed] */
  // 200 — официальный лимит названия чата (ChatButton.chat_title, ChatPatch.title)
  @maxLength(200)
  title?: string;

  /** Данные из диплинка, переданные при запуске бота. [bot_started] */
  // 512 — значение из официальной схемы MAX (BotStartedUpdate.payload)
  @maxLength(512)
  payload?: string | null;

  /** До какого времени отключены уведомления (Unix-время). [dialog_muted] */
  @minValue(0)
  @maxValue(9007199254740991)
  muted_until?: int64;

  /** Кто пригласил пользователя; null, если вошёл по ссылке. [user_added] */
  @minValue(0)
  @maxValue(9007199254740991)
  inviter_id?: int64 | null;

  /** Администратор, удаливший пользователя; отсутствует, если пользователь вышел сам. [user_removed] */
  @minValue(0)
  @maxValue(9007199254740991)
  admin_id?: int64;

  /** ID сообщения: удалённого или того, где была нажата кнопка чата. [message_removed, message_chat_created] */
  @minLength(1)
  @maxLength(255)
  @pattern("^[a-zA-Z0-9._\\-]+$")
  message_id?: string;

  /** Автор удалённого сообщения. [message_removed] */
  @minValue(0)
  @maxValue(9007199254740991)
  user_id?: int64;

  /** Созданный чат. [message_chat_created] */
  chat?: MaxBotApi.Chat;

  /** Полезная нагрузка кнопки чата. [message_chat_created] */
  // 512 — официальный лимит источника значения (ChatButton.start_payload)
  @maxLength(512)
  start_payload?: string | null;
}

/**
 * Явный oneOf всех 16 типов событий — чтобы Swagger UI и генераторы
 * показывали полный список вариантов, а не только базовые поля Update
 * (allOf-наследование дискриминированного union'а UI не разворачивают).
 */
@oneOf
union WebhookUpdate {
  MaxBotApi.MessageCreatedUpdate,
  MaxBotApi.MessageCallbackUpdate,
  MaxBotApi.MessageEditedUpdate,
  MaxBotApi.MessageRemovedUpdate,
  MaxBotApi.MessageChatCreatedUpdate,
  MaxBotApi.BotAddedToChatUpdate,
  MaxBotApi.BotRemovedFromChatUpdate,
  MaxBotApi.BotStartedUpdate,
  MaxBotApi.BotStoppedUpdate,
  MaxBotApi.ChatTitleChangedUpdate,
  MaxBotApi.DialogClearedUpdate,
  MaxBotApi.DialogMutedUpdate,
  MaxBotApi.DialogUnmutedUpdate,
  MaxBotApi.DialogRemovedUpdate,
  MaxBotApi.UserAddedToChatUpdate,
  MaxBotApi.UserRemovedFromChatUpdate,
}

/** Подтверждение приёма: платформа ожидает 200 OK без тела */
model AcceptedResponse {
  @statusCode statusCode: 200;
}
