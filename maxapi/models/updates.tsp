namespace MaxBotApi;

/**
 * Объект `Update` описывает возможные события в чате или канале. Может
 * возвращаться в следующих случаях:
 * - вы подписались на обновления через Webhook — при наступлении события
 *   МАКС пришлёт POST-запрос `/subscriptions`, который содержит объект `Update`;
 * - вы отправили GET-запрос `/updates` для получения обновлений через
 *   Long Polling — в ответ вернётся объект `Update`.
 */
@discriminator("update_type")
model Update {
  /** Тип события */
  update_type: string;

  /** Unix-время, когда произошло событие */
  timestamp: int64;
}

/** Вы получите это событие, как только сообщение будет создано */
model MessageCreatedUpdate extends Update {
  update_type: "message_created";

  /** Новое созданное сообщение */
  message: Message;

  /** Текущий язык пользователя в формате IETF BCP 47. Доступно только в диалогах */
  user_locale?: string | null;
}

/** Вы получите это событие, как только пользователь нажмёт кнопку */
model MessageCallbackUpdate extends Update {
  update_type: "message_callback";

  /** Нажатая пользователем кнопка */
  callback: Callback;

  /**
   * Изначальное сообщение, содержащее встроенную клавиатуру. Может быть
   * `null`, если оно было удалено к моменту, когда бот получил это событие
   */
  message: Message | null;

  /** Текущий язык пользователя в формате IETF BCP 47 */
  user_locale?: string | null;
}

/** Вы получите это событие, как только пользователь отредактирует сообщение */
model MessageEditedUpdate extends Update {
  update_type: "message_edited";

  /** Отредактированное сообщение */
  message: Message;
}

/** Вы получите это событие, как только сообщение будет удалено */
model MessageRemovedUpdate extends Update {
  update_type: "message_removed";

  /** ID удалённого сообщения */
  message_id: string;

  /** ID чата, где сообщение было удалено. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, удаливший сообщение */
  user_id: int64;
}

/** Вы получите это событие, как только бот будет добавлен в чат или канал */
model BotAddedToChatUpdate extends Update {
  update_type: "bot_added";

  /** ID чата, куда был добавлен бот. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, добавивший бота в чат */
  user: User;

  /** Указывает, что бот добавлен в канал, а не в чат */
  is_channel: boolean;
}

/** Вы получите это событие, как только бот будет удалён из чата */
model BotRemovedFromChatUpdate extends Update {
  update_type: "bot_removed";

  /** ID чата, откуда был удалён бот. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, удаливший бота из чата */
  user: User;

  /** Указывает, что бот удалён из канала, а не из чата */
  is_channel: boolean;
}

/**
 * Вы получите это событие, как только пользователь начнёт или возобновит
 * общение с ботом: нажмёт соответствующую кнопку в настройках бота в МАКС
 */
model BotStartedUpdate extends Update {
  update_type: "bot_started";

  /** ID диалога, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, который нажал кнопку `Start` */
  user: User;

  /** Дополнительные данные из диплинков, переданные при запуске бота */
  @maxLength(512)
  payload?: string | null;

  /** Текущий язык пользователя в формате IETF BCP 47 */
  user_locale?: string;
}

/** Вы получите это событие, как только пользователь остановит бота в его настройках в МАКС */
model BotStoppedUpdate extends Update {
  update_type: "bot_stopped";

  /** ID диалога, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, который остановил бота */
  user: User;

  /** Текущий язык пользователя в формате IETF BCP 47 */
  user_locale?: string;
}

/** Вы получите это событие, как только будет изменено название чата */
model ChatTitleChangedUpdate extends Update {
  update_type: "chat_title_changed";

  /** ID чата, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, который изменил название */
  user: User;

  /** Новое название */
  title: string;
}

/** Вы получите это событие, как только пользователь очистит историю сообщений */
model DialogClearedUpdate extends Update {
  update_type: "dialog_cleared";

  /** ID чата, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, который очистил историю сообщений */
  user: User;

  /** Текущий язык пользователя в формате IETF BCP 47 */
  user_locale?: string;
}

/**
 * Вы получите это событие, как только пользователь отключит уведомления о
 * новых сообщениях в диалоге с ботом
 */
model DialogMutedUpdate extends Update {
  update_type: "dialog_muted";

  /** ID чата, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, который отключил уведомления */
  user: User;

  /** Время в формате Unix, до наступления которого диалог был отключён */
  muted_until: int64;

  /** Текущий язык пользователя в формате IETF BCP 47 */
  user_locale?: string;
}

/**
 * Вы получите это событие, как только пользователь включит уведомления о
 * новых сообщениях в диалоге с ботом
 */
model DialogUnmutedUpdate extends Update {
  update_type: "dialog_unmuted";

  /** ID чата, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, который включил уведомления */
  user: User;

  /** Текущий язык пользователя в формате IETF BCP 47 */
  user_locale?: string;
}

/** Вы получите это событие, как только пользователь удалит чат */
model DialogRemovedUpdate extends Update {
  update_type: "dialog_removed";

  /** ID чата, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, который удалил чат */
  user: User;

  /** Текущий язык пользователя в формате IETF BCP 47 */
  user_locale?: string;
}

/**
 * Вы получите это событие, как только пользователь будет добавлен в чат,
 * где бот является администратором
 */
model UserAddedToChatUpdate extends Update {
  update_type: "user_added";

  /** ID чата, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, добавленный в чат */
  user: User;

  /**
   * Пользователь, который добавил нового пользователя в чат. Может быть
   * `null`, если пользователь присоединился к чату по ссылке
   */
  inviter_id?: int64 | null;

  /** Указывает, что пользователь добавлен в канал, а не в чат */
  is_channel: boolean;
}

/**
 * Вы получите это событие, как только пользователь будет удалён из чата,
 * где бот является администратором
 */
model UserRemovedFromChatUpdate extends Update {
  update_type: "user_removed";

  /** ID чата, где произошло событие. Как получить ID — в разделе «Получение chat_id» */
  chat_id: int64;

  /** Пользователь, удалённый из чата */
  user: User;

  /**
   * Администратор, который удалил пользователя из чата. Может быть `null`,
   * если пользователь покинул чат сам
   */
  admin_id?: int64;

  /** Указывает, что пользователь удалён из канала, а не из чата */
  is_channel: boolean;
}

/**
 * Вы получите это событие, как только пользователь нажмёт в сообщении на
 * кнопку с типом `chat` и будет создан новый чат с ботом, связанный с этим
 * сообщением с кнопкой.
 *
 * KNOWN: в оригинальной схеме этот дискриминатор отсутствует в
 * `Update.discriminator.mapping` (недосмотр оригинала) — см. DEVIATIONS
 * `message_chat_created` в scripts/compare.py.
 */
model MessageChatCreatedUpdate extends Update {
  update_type: "message_chat_created";

  /** Созданный чат */
  chat: Chat;

  /** ID сообщения, где была нажата кнопка */
  message_id: string;

  /** Полезная нагрузка от кнопки чата */
  start_payload?: string | null;
}

/**
 * Список обновлений о событиях в чатах и каналах, в которые добавлен бот.
 * Обратите внимание, чтобы получать события из групповых чатов и каналов,
 * бот должен быть администратором
 */
model UpdateList {
  /** Список обновлений о событиях в чатах и каналах, в которые добавлен бот */
  updates: Update[];

  /** Указатель на следующую страницу данных */
  marker: int64 | null;
}

/** Схема для описания подписки на WebHook */
model Subscription {
  /** URL вебхука */
  url: string;

  /** Unix-время, когда была создана подписка */
  time: int64;

  /**
   * Типы событий, на которые подписан бот.
   *
   * В оригинале элементы массива несут `minLength: 1` (см. DEVIATIONS
   * `Subscription.update_types\[\]` в scripts/compare.py) — перенос этого
   * ограничения через именованный scalar-тип создаёт в нашей схеме лишнюю
   * (не присутствующую в оригинале) схему `components.schemas`, поэтому мы
   * его не переносим и документируем как осознанное отличие.
   */
  update_types: string[] | null;
}

/** Запрос на настройку подписки WebHook */
model SubscriptionRequestBody {
  /** URL HTTPS-endpoint вашего бота. Должен начинаться с `https://` */
  url: string;

  /** Список типов событий, которые хочет получать ваш бот */
  update_types?: string[];

  /**
   * Секрет, который должен быть отправлен в заголовке `X-Max-Bot-Api-Secret`
   * в каждом запросе Webhook. Разрешены только символы `A-Z`, `a-z`, `0-9`,
   * и дефис. Заголовок рекомендован, чтобы запрос поступал из установленного
   * веб-узла
   */
  @minLength(5)
  @maxLength(256)
  @pattern("^[a-zA-Z0-9_-]{5,256}$")
  secret?: string;
}

/** Список всех WebHook подписок */
model GetSubscriptionsResult {
  /** Список текущих подписок */
  subscriptions: Subscription[];
}
