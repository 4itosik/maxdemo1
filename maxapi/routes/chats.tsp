import "@typespec/http";
import "@typespec/openapi";

using Http;
using OpenAPI;

namespace MaxBotApi;

@tag("chats")
interface Chats {
  /**
   * Возвращает список всех групповых чатов и каналов для бота.
   *
   * ОТЛИЧИЕ ОТ ОРИГИНАЛА (осознанное, см. DEVIATIONS в scripts/compare.py):
   * начиная с июня 2026 метод в оригинальной схеме больше не документирован —
   * его `responses` полностью отсутствуют (равно как и раздел `parameters`),
   * хотя `tags`/`operationId`/`summary`/`description` остались. Схема
   * `ChatList` при этом по-прежнему присутствует в `components.schemas`,
   * но не используется ни одним путём — явный осиротевший рудимент этого
   * же метода. Мы транскрибируем содержательный (до депрекации) контракт
   * метода — 200 ChatList, 401, 500 — вместо того, чтобы оставить операцию
   * буквально без единого ответа (invalid по OpenAPI и невыразимо в TypeSpec).
   */
  #deprecated "С июня 2026 метод GET /chats не поддерживается сервером — API не предоставляет готового способа получить список чатов бота"
  @summary("Получение списка всех групповых чатов и каналов для бота")
  @operationId("getChats")
  @route("/chats")
  @get
  getChats(): ChatList | Unauthorized | InternalError;

  /** Возвращает информацию о групповом чате или канале по его ID */
  @summary("Получение информации о групповом чате или канале")
  @tag("bot-sending")
  @operationId("getChat")
  @route("/chats/{chatId}")
  @get
  getChat(
    /** ID запрашиваемого группового чата или канала */
    @path chatId: int64,
  ): Chat | Unauthorized | InternalError;

  /** Изменение информации о групповом чате или канале: заголовок, значок и т.д. */
  @summary("Изменение информации о групповом чате или канале")
  @operationId("editChat")
  @route("/chats/{chatId}")
  @patch(#{ implicitOptionality: false })
  editChat(
    /** ID чата */
    @path chatId: int64,
    @body body: ChatPatch,
  ): Chat | Unauthorized | Forbidden | InternalError;

  /** Отправка действия бота в групповой чат (например «набор текста» или «отправка фото») */
  @summary("Отправка действия бота в групповой чат")
  @tag("bot-sending")
  @operationId("sendAction")
  @route("/chats/{chatId}/actions")
  @post
  sendAction(
    /** ID чата */
    @path chatId: int64,
    @body body: ActionRequestBody,
  ): SimpleQueryResult | Unauthorized | InternalError;

  /** Возвращает закреплённое сообщение в групповом чате или канале */
  @summary("Получение закреплённого сообщения в групповом чате или канале")
  @tag("bot-sending")
  @operationId("getPinnedMessage")
  @route("/chats/{chatId}/pin")
  @get
  getPinnedMessage(
    /** ID чата */
    @path chatId: int64,
  ): GetPinnedMessageResult | Unauthorized | Forbidden | NotFound | InternalError;

  /** Закрепляет сообщение в групповом чате или канале */
  @summary("Закрепление сообщения в групповом чате или канале")
  @tag("bot-sending")
  @operationId("pinMessage")
  @route("/chats/{chatId}/pin")
  @put
  pinMessage(
    /** ID группового чата или канала, где нужно закрепить сообщение */
    @path chatId: int64,
    @body body: PinMessageBody,
  ): SimpleQueryResult | Unauthorized | Forbidden | NotFound | InternalError;

  /** Открепляет сообщение в групповом чате или канале */
  @summary("Открепление сообщения в групповом чате или канале")
  @operationId("unpinMessage")
  @route("/chats/{chatId}/pin")
  @delete
  unpinMessage(
    /** ID группового чата или канала, в котором нужно открепить сообщение или пост */
    @path chatId: int64,
  ): SimpleQueryResult | Unauthorized | Forbidden | NotFound | InternalError;

  /** Возвращает информацию о членстве бота в групповом чате или канале */
  @summary("Получение информации о членстве бота в групповом чате или канале")
  @tag("bot-sending")
  @operationId("getMembership")
  @route("/chats/{chatId}/members/me")
  @get
  getMembership(
    /** ID группового чата или канала */
    @path chatId: int64,
  ): ChatMember | Unauthorized | Forbidden | NotFound | InternalError;

  /** Удаляет бота из участников группового чата или канала */
  @summary("Удаление бота из группового чата или канала")
  @operationId("leaveChat")
  @route("/chats/{chatId}/members/me")
  @delete
  leaveChat(
    /** ID группового чата или канала */
    @path chatId: int64,
  ): SimpleQueryResult | Unauthorized | Forbidden | NotFound | InternalError;

  /** Возвращает список всех администраторов группового чата или канала */
  @summary("Получение списка администраторов группового чата или канала")
  @operationId("getAdmins")
  @route("/chats/{chatId}/members/admins")
  @get
  getAdmins(
    /** ID группового чата или канала */
    @path chatId: int64,
  ): ChatMembersList | Unauthorized | Forbidden | NotFound | InternalError;

  /** Выдаёт пользователям и ботам, которые являются участниками чата или подписчиками канала, права администратора */
  @summary("Назначить администратора группового чата или канала")
  @operationId("postAdmins")
  @route("/chats/{chatId}/members/admins")
  @post
  postAdmins(
    /** ID группового чата или канала */
    @path chatId: int64,
    @body body: ChatAdminsList,
  ): SimpleQueryResult | Unauthorized | InternalError;

  /** Лишает пользователя или бота прав администратора в групповом чате или канале. При этом из чата и канала они не исключаются */
  @summary("Отменить права администратора в групповом чате или канале")
  @operationId("deleteAdmin")
  @route("/chats/{chatId}/members/admins/{userId}")
  @delete
  deleteAdmin(
    /** ID группового чата или канала */
    @path chatId: int64,

    /** Идентификатор пользователя или бота, которого надо лишить прав администратора */
    @path userId: int64,
  ): SimpleQueryResult | Unauthorized | InternalError;

  /** Возвращает список участников группового чата или канала */
  @summary("Получение участников группового чата или канала")
  @operationId("getMembers")
  @route("/chats/{chatId}/members")
  @get
  getMembers(
    /** ID группового чата или канала */
    @path chatId: int64,

    /** Список ID пользователей, чьё членство нужно получить. Когда этот параметр передан, параметры `count` и `marker` игнорируются */
    @query user_ids?: int64[] | null,

    /** Указатель на следующую страницу данных */
    @query marker?: int64,

    /** Количество участников, которых нужно вернуть в ответе */
    @minValue(1)
    @maxValue(100)
    @query count?: integer = 20,
  ): ChatMembersList | Unauthorized | Forbidden | NotFound | InternalError;

  /** Добавляет участников в групповой чат */
  @summary("Добавление участников в групповой чат")
  @operationId("addMembers")
  @route("/chats/{chatId}/members")
  @post
  addMembers(
    /** ID группового чата */
    @path chatId: int64,
    @body body: UserIdsList,
  ): ModifyMembersResult | Unauthorized | Forbidden | NotFound | InternalError;

  /** Удаляет участника из группового чата или канала */
  @summary("Удаление участников из группового чата или канала")
  @operationId("removeMember")
  @route("/chats/{chatId}/members")
  @delete
  removeMember(
    /** ID группового чата или канала */
    @path chatId: int64,

    /** ID пользователя, которого нужно удалить из группового чата или канала */
    @query user_id: int64,

    /** Если передать `true`, пользователь будет заблокирован в чате. Применяется только для чатов с публичной или приватной ссылкой. Игнорируется в остальных случаях */
    @query block?: boolean = false,
  ): SimpleQueryResult | Unauthorized | Forbidden | InternalError;
}
