namespace MaxBotApi;

/** Ряд кнопок клавиатуры */
@maxItems(100)
model ButtonRow is Array<Button>;

/** Ряд кнопок reply-клавиатуры */
@maxItems(100)
model ReplyButtonRow is Array<ReplyButton>;

/** Клавиатура — это двумерный массив кнопок. Подробнее о типах кнопок и клавиатуре в ботах — в разделе «Клавиатура для чат-бота» */
model Keyboard {
  /** Массив рядов с кнопками */
  @maxItems(100)
  buttons: ButtonRow[];
}

/** Кнопка клавиатуры */
@discriminator("type")
model Button {
  /** Тип кнопки */
  // 64 — в оригинале лимита нет; по аналогии с официальным лимитом коротких имён (BotCommand.name)
  @maxLength(64)
  @pattern("^[a-z_]+$")
  type: string;

  /** Видимый текст кнопки. Чтобы он отображался полностью, рекомендуем не превышать заданное количество символов в зависимости от размещения текста: `20` символов — при 1 кнопке в ряду, `10` — при 2, `5` — при 3, `3` — при 4 */
  // 128 — значение из официальной схемы MAX
  @minLength(1)
  @maxLength(128)
  text: string;
}

/** После нажатия на такую кнопку клиент отправляет на сервер полезную нагрузку */
model CallbackButton extends Button {
  type: "callback";

  /** Токен кнопки */
  // 1024 — значение из официальной схемы MAX
  @maxLength(1024)
  payload: string;
}

/** После нажатия на такую кнопку пользователь переходит по ссылке, которую она содержит */
model LinkButton extends Button {
  type: "link";

  /** Ссылка */
  // 2048 — значение из официальной схемы MAX
  @maxLength(2048)
  url: string;
}

/** После нажатия на такую кнопку клиент отправляет новое сообщение с вложением текущего географического положения пользователя */
model RequestGeoLocationButton extends Button {
  type: "request_geo_location";

  /** Если `true`, отправляет местоположение без запроса подтверждения пользователя */
  quick?: boolean = false;
}

/** После нажатия на такую кнопку клиент отправляет новое сообщение с вложением текущего контакта пользователя */
model RequestContactButton extends Button {
  type: "request_contact";
}

/**
 * Кнопка, которая создает новый чат, как только первый пользователь на нее нажмёт.
 * Бот будет добавлен в участники чата как администратор.
 * Автор сообщения станет владельцем чата.
 *
 * В оригинальной схеме отсутствует в `Button.discriminator.mapping` — см.
 * DEVIATIONS `mapping[chat]` в scripts/compare.py: литерал "chat" выведен по
 * конвенции соседних ключей (тот же приём, что и для `DataAttachment`).
 */
model ChatButton extends Button {
  type: "chat";

  /** Название чата, который будет создан */
  // 200 — значение из официальной схемы MAX
  @maxLength(200)
  chat_title: string;

  /** Описание чата */
  // 400 — значение из официальной схемы MAX
  @maxLength(400)
  chat_description?: string | null;

  /** Стартовая полезная нагрузка будет отправлена боту, как только чат будет создан */
  // 512 — значение из официальной схемы MAX
  @maxLength(512)
  start_payload?: string | null;

  /**
   * Уникальный ID кнопки среди всех кнопок чата на клавиатуре.
   * Если `uuid` изменён, новый чат будет создан при следующем нажатии.
   * Сервер сгенерирует его в момент, когда кнопка будет впервые размещена.
   * Используйте его при редактировании сообщения.
   */
  @minValue(0)
  @maxValue(2147483647)
  uuid?: integer | null;
}

/** Кнопка для запуска мини-приложения */
model OpenAppButton extends Button {
  type: "open_app";

  /** Публичное имя (username) бота или ссылка на него, чьё мини-приложение надо запустить */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @maxLength(2048)
  web_app?: string;

  /** Идентификатор бота, чьё мини-приложение надо запустить */
  @minValue(0)
  @maxValue(9007199254740991)
  contact_id?: int64;

  /** Параметр запуска, который будет передан в initData мини-приложения */
  // 1024 — в оригинале лимита нет; по аналогии с официальным лимитом кнопочного payload (CallbackButton.payload)
  @maxLength(1024)
  payload?: string;
}

/** При нажатии на кнопку с типом `clipboard` текст, указанный в свойстве `payload`, копируется в буфер обмена */
model ClipboardButton extends Button {
  type: "clipboard";

  /** Текст, который будет скопирован */
  // 1024 — значение из официальной схемы MAX
  @maxLength(1024)
  payload: string;
}

/** Кнопка, при нажатии на которую от лица пользователя в чат отправляется указанный текст */
model MessageButton extends Button {
  type: "message";
}

/** Намерение кнопки */
enum Intent {
  positive: "positive",
  negative: "negative",
  `default`: "default",
}

/** После нажатия на такую кнопку клиент отправляет сообщение от лица пользователя с заданной полезной нагрузкой */
@discriminator("type")
model ReplyButton {
  /** Тип кнопки */
  // 64 — в оригинале лимита нет; по аналогии с официальным лимитом коротких имён (BotCommand.name)
  @maxLength(64)
  @pattern("^[a-z_]+$")
  type: string;

  /** Видимый текст кнопки */
  // 128 — значение из официальной схемы MAX
  @minLength(1)
  @maxLength(128)
  text: string;

  /** Токен кнопки */
  // 1024 — значение из официальной схемы MAX
  @maxLength(1024)
  payload?: string | null;
}

/** После нажатия на такую кнопку клиент отправляет сообщение от лица пользователя с заданной полезной нагрузкой */
model SendMessageButton extends ReplyButton {
  type: "message";
}

/** После нажатия на такую кнопку клиент отправляет новое сообщение с вложением текущего географического положения пользователя */
model SendGeoLocationButton extends ReplyButton {
  type: "user_geo_location";

  /** Если `true`, отправляет местоположение без запроса подтверждения пользователя */
  quick?: boolean = false;
}

/** После нажатия на такую кнопку клиент отправляет новое сообщение с вложением текущего контакта пользователя */
model SendContactButton extends ReplyButton {
  type: "user_contact";
}
