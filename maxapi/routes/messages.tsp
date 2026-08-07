import "@typespec/http";
import "@typespec/openapi";

using Http;
using OpenAPI;

namespace MaxBotApi;

@tag("messages")
interface Messages {
  /**
   * Возвращает информацию о сообщении или массив сообщений из чата. Нужно
   * указать один из параметров — `chat_id` или `message_ids`
   */
  @summary("Получение сообщений")
  @operationId("getMessages")
  @route("/messages")
  @get
  getMessages(
    /** ID чата, чтобы получить сообщения из определённого чата. Обязательный параметр, если не указан `message_ids` */
    @query chat_id?: int64,

    /**
     * Список ID сообщений, которые нужно получить (через запятую).
     * Обязательный параметр, если не указан `chat_id`.
     *
     * ОТЛИЧИЕ ОТ ОРИГИНАЛА (осознанное, см. DEVIATIONS в scripts/compare.py):
     * в оригинальной схеме этот параметр описан как массив без объявленного
     * items вообще — недосмотр (тот же приём копипасты, что и у UserIdsList).
     * Мы транскрибируем содержательный смысл: массив строк.
     */
    @query message_ids?: string[],

    /** Время, до которого будут запрошены все сообщения с начала чата (в формате Unix timestamp) */
    @query from?: int64,

    /** Время, начиная с которого будут запрошены все сообщения до конца чата (в формате Unix timestamp) */
    @query to?: int64,

    /** Максимальное количество сообщений в ответе */
    @minValue(1)
    @maxValue(100)
    @query count?: int32 = 50,
  ): MessageList | Unauthorized | Forbidden | InternalError;

  /**
   * Отправляет сообщение в диалог, групповой чат или канал. Можно отправлять
   * не более двух сообщений в секунду в один диалог, групповой чат или канал —
   * при превышении лимита ставьте сообщения в очередь или делайте задержку
   */
  @summary("Отправить сообщение")
  @tag("bot-sending")
  @operationId("sendMessage")
  @route("/messages")
  @post
  sendMessage(
    /** Если вы хотите отправить сообщение пользователю, укажите его ID */
    @query user_id?: int64,

    /** Если сообщение отправляется в чат, укажите его ID */
    @query chat_id?: int64,

    /** Если `true`, сервер не будет генерировать превью для ссылок в тексте сообщения */
    @query disable_link_preview?: boolean = false,

    @body body: NewMessageBody,
  ): SendMessageResult | Unauthorized | InternalError;

  /**
   * Редактирует сообщения, отправленные ботом. В диалогах с ботом сообщения
   * с кнопками `inline_keyboard` редактируются независимо от срока давности,
   * остальные — если отправлены менее 7 суток назад; в групповых чатах и
   * каналах — независимо от срока давности. Не более двух редактирований в
   * секунду в одном диалоге, групповом чате или канале
   */
  @summary("Редактировать сообщение")
  @tag("bot-sending")
  @operationId("editMessage")
  @route("/messages")
  @put
  editMessage(
    /** ID редактируемого сообщения */
    @minLength(1)
    @query message_id: string,

    @body body: NewMessageBody,
  ): SimpleQueryResult | Unauthorized | InternalError;

  /**
   * Удаляет сообщения в диалоге, групповом чате или канале. Бот должен быть
   * администратором и иметь право на удаление сообщений. В диалоге бот может
   * удалять только собственные сообщения, в групповом чате и канале — любые.
   * Не более двух удалений в секунду в одном диалоге, групповом чате или канале
   */
  @summary("Удалить сообщение")
  @tag("bot-sending")
  @operationId("deleteMessage")
  @route("/messages")
  @delete
  deleteMessage(
    /** ID удаляемого сообщения */
    @minLength(1)
    @query message_id: string,
  ): SimpleQueryResult | Unauthorized | Forbidden | InternalError;

  /** Возвращает сообщение по его ID */
  @summary("Получить сообщение")
  @operationId("getMessageById")
  @route("/messages/{messageId}")
  @get
  getMessageById(
    /** ID сообщения (`mid`), чтобы получить одно сообщение в чате */
    @pattern("[a-zA-Z0-9_\\-]+")
    @path messageId: string,
  ): Message | Unauthorized | NotFound | InternalError;

  /** Возвращает подробную информацию о прикреплённом видео, URL-адреса воспроизведения и дополнительные метаданные */
  @summary("Получить информацию о видео")
  @operationId("getVideoAttachmentDetails")
  @route("/videos/{videoToken}")
  @get
  getVideoAttachmentDetails(
    /** Токен видео-вложения */
    @pattern("[a-zA-Z0-9_\\-]+")
    @path videoToken: string,
  ): VideoAttachmentDetails | Unauthorized | Forbidden | NotFound | InternalError;

  /**
   * Отправка ответа после того, как пользователь нажал на кнопку. Ответом
   * может быть обновлённое сообщение. Не более двух ответов в секунду в один
   * диалог, групповой чат или канал
   */
  @summary("Ответ на callback")
  @tag("bot-sending")
  @operationId("answerOnCallback")
  @route("/answers")
  @post
  answerOnCallback(
    /** Идентификатор кнопки, на которую нажал пользователь */
    @minLength(1)
    @pattern("^(?!\\s*$).+")
    @query callback_id: string,

    @body body: CallbackAnswer,
  ): SimpleQueryResult | Unauthorized | NotAllowed | InternalError;
}
