namespace MaxBotApi;

/** Формат текста сообщения */
enum TextFormat {
  markdown: "markdown",
  html: "html",
}

/** Тип связанного сообщения */
enum MessageLinkType {
  forward: "forward",
  reply: "reply",
}

/**
 * Действие, отправляемое участникам чата. Возможные значения:
 * - `"typing_on"` — Бот набирает сообщение
 * - `"sending_photo"` — Бот отправляет фото
 * - `"sending_video"` — Бот отправляет видео
 * - `"sending_audio"` — Бот отправляет аудиофайл
 * - `"sending_file"` — Бот отправляет файл
 */
enum SenderAction {
  typing_on: "typing_on",
  sending_photo: "sending_photo",
  sending_video: "sending_video",
  sending_audio: "sending_audio",
  sending_file: "sending_file",
}

// Определён здесь (а не в models/chats.tsp), т.к. нужен уже для Recipient.chat_type;
// Chat.type (Task 7) переиспользует этот же MaxBotApi.ChatType, а не переобъявляет его.
/** Тип чата: диалог, чат */
enum ChatType {
  chat: "chat",
}

/** Новый получатель сообщения. Может быть пользователем или чатом */
model Recipient {
  /** ID чата. Как получить ID — в разделе «Получение chat_id» */
  @minValue(0)
  @maxValue(9007199254740991)
  chat_id: int64 | null;

  /** Тип чата */
  chat_type: ChatType;

  /** ID пользователя, если сообщение было отправлено пользователю */
  @minValue(0)
  @maxValue(9007199254740991)
  user_id: int64 | null;
}

/** Статистика сообщения. Возвращается только для постов в каналах */
model MessageStat {
  /** Количество пользователей, которые увидели пост в канале. Просмотр засчитывается, когда пост попадает в область видимости экрана */
  @minValue(0)
  @maxValue(2147483647)
  views: integer;
}

/** Схема, представляющая тело сообщения */
model MessageBody {
  /** Уникальный ID сообщения */
  mid: MessageId;

  /** ID последовательности сообщения в чате */
  @minValue(0)
  @maxValue(9007199254740991)
  seq: int64;

  /** Новый текст сообщения */
  // 4000 — официальный лимит текста сообщения (NewMessageBody.text)
  @maxLength(4000)
  text: string | null;

  /** Вложения сообщения. Могут быть одним из типов `Attachment`. Смотрите описание схемы */
  @maxItems(100)
  attachments?: Attachment[] | null;

  /** Разметка текста сообщения. Для подробной информации загляните в раздел [Форматирование](/docs-api#Форматирование%20текста%20в%20сообщениях) */
  @maxItems(500)
  markup?: MarkupElement[] | null;
}

/** Пересланное или ответное сообщение */
model LinkedMessage {
  /** Тип связанного сообщения */
  type: MessageLinkType;

  /** Пользователь, отправивший сообщение */
  sender?: User;

  /** Чат, в котором сообщение было изначально опубликовано. Только для пересланных сообщений. Как получить ID — в разделе «Получение chat_id» */
  @minValue(0)
  @maxValue(9007199254740991)
  chat_id?: int64;

  /** Содержимое связанного (пересланного или ответного) сообщения */
  message: MessageBody;
}

/** Сообщение в чате */
model Message {
  /** Пользователь, отправивший сообщение */
  sender?: User;

  /** Получатель сообщения. Может быть пользователем или чатом */
  recipient: Recipient;

  /** Время создания сообщения в формате Unix-time */
  @minValue(0)
  @maxValue(9007199254740991)
  timestamp: int64;

  /** Пересланное или ответное сообщение */
  link?: LinkedMessage | null;

  /** Содержимое сообщения. Текст + вложения. Может быть `null`, если сообщение содержит только пересланное сообщение */
  body: MessageBody;

  /** Статистика сообщения. Возвращается только для постов в каналах */
  stat?: MessageStat | null;

  /** Публичная ссылка на пост в канале. Отсутствует для диалогов и групповых чатов */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @maxLength(2048)
  url?: string | null;
}

/** Пагинированный список сообщений */
model MessageList {
  /** Массив сообщений */
  @maxItems(100)
  messages: Message[];
}

/** Ссылка на сообщение, которое нужно переслать или на которое нужно ответить */
model NewMessageLink {
  /** Тип ссылки сообщения */
  type: MessageLinkType;

  /** ID исходного сообщения */
  mid: MessageId;
}

/** Новое сообщение, отправляемое ботом */
model NewMessageBody {
  /** Новый текст сообщения */
  // 4000 — значение из официальной схемы MAX
  @maxLength(4000)
  text: string | null;

  /** Вложения сообщения. Если поле равно `null`, изменений не произойдет. Если пусто, все вложения будут удалены */
  @maxItems(100)
  attachments: AttachmentRequest[] | null;

  /** Ссылка на сообщение */
  link: NewMessageLink | null;

  /** Если false, участники чата не будут уведомлены (по умолчанию `true`) */
  notify?: boolean = true;

  /** Если установлен, текст сообщения будет форматирован данным способом. Для подробной информации загляните в раздел [Форматирование](/docs-api#Форматирование%20текста%20в%20сообщениях) */
  `format`?: TextFormat | null;
}

/** Результат отправки сообщения */
model SendMessageResult {
  /** Отправленное сообщение */
  message: Message;
}

/** Объект, отправленный боту, когда пользователь нажимает кнопку */
model Callback {
  /** Unix-время, когда пользователь нажал кнопку */
  @minValue(0)
  @maxValue(9007199254740991)
  timestamp: int64;

  /** Текущий ID клавиатуры */
  // 1024 — в оригинале лимита нет; по аналогии с официальным лимитом кнопочного payload (CallbackButton.payload)
  @maxLength(1024)
  callback_id: string;

  /** Токен кнопки */
  // 1024 — официальный лимит источника значения: токен приходит из CallbackButton.payload (maxLength 1024 в оригинале)
  @maxLength(1024)
  payload?: string;

  /** Пользователь, нажавший на кнопку */
  user: User;
}

/** Отправьте этот объект, когда ваш бот хочет отреагировать на нажатие кнопки */
model CallbackAnswer {
  /** Заполните это, если хотите изменить текущее сообщение */
  message?: NewMessageBody | null;
}

/** Тело запроса на отправку действия отправителя (индикатор набора текста и т. п.) */
model ActionRequestBody {
  /** Действие, отправляемое участникам чата */
  action: SenderAction;
}

/** Тело запроса на закрепление сообщения в чате */
model PinMessageBody {
  /** ID сообщения, которое нужно закрепить. Соответствует полю `Message.body.mid` */
  message_id: MessageId;

  /** Если `true`, участники получат уведомление с системным сообщением о закреплении */
  notify?: boolean | null = true;
}

/** Результат запроса закреплённого сообщения */
model GetPinnedMessageResult {
  /** Закреплённое сообщение. Может быть `null`, если в чате нет закреплённого сообщения */
  message?: Message | null;
}
