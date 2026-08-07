namespace MaxBotApi;

/** Общая схема, представляющая вложение сообщения */
@discriminator("type")
model Attachment {
  /** Тип вложения */
  // 64 — в оригинале лимита нет; по аналогии с официальным лимитом коротких имён (BotCommand.name)
  @maxLength(64)
  @pattern("^[a-z_]+$")
  type: string;
}

/** Базовая полезная нагрузка медиа-вложения */
model AttachmentPayload {
  /**
   * URL медиа-вложения. Этот URL будет получен в объекте Update после
   * отправки сообщения в чат.
   *
   * Прямую ссылку на видео также можно получить с помощью метода
   * `GET /videos/{-videoToken-}`
   */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @maxLength(2048)
  url: string;
}

/** Полезная нагрузка фото-вложения */
model PhotoAttachmentPayload {
  /** Уникальный ID этого изображения */
  @minValue(0)
  @maxValue(9007199254740991)
  photo_id: int64;

  /** Токен вложения */
  // 1024 — в оригинале лимита нет; по аналогии с официальным лимитом кнопочного payload (CallbackButton.payload)
  @maxLength(1024)
  token: string;

  /** URL изображения. Время жизни ссылки ограничено: срок истечения указан в параметре `expires`, после чего ссылку необходимо запросить повторно. Пока доступно только в веб-клиенте */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @maxLength(2048)
  url: string;
}

/** Полезная нагрузка медиа-вложения (видео/аудио) */
model MediaAttachmentPayload extends AttachmentPayload {
  /** Используйте `token`, если вы пытаетесь повторно использовать одно и то же вложение в другом сообщении */
  // 1024 — в оригинале лимита нет; по аналогии с официальным лимитом кнопочного payload (CallbackButton.payload)
  @maxLength(1024)
  token: string;
}

/** Полезная нагрузка файлового вложения */
model FileAttachmentPayload extends AttachmentPayload {
  /** Используйте `token`, если вы пытаетесь повторно использовать одно и то же вложение в другом сообщении */
  // 1024 — в оригинале лимита нет; по аналогии с официальным лимитом кнопочного payload (CallbackButton.payload)
  @maxLength(1024)
  token: string;
}

/** Полезная нагрузка вложения-контакта */
model ContactAttachmentPayload {
  /** Информация о пользователе в формате VCF */
  // 4096 — в оригинале лимита нет; запас под VCF-карточку
  @maxLength(4096)
  vcf_info?: string | null;

  /**
   * Хеш информации о пользователе в формате VCF. Используется для проверки
   * того, что пользователь поделился номером телефона, привязанным к его
   * аккаунту в МАКС. Подробнее — в разделе «Типы кнопок»
   */
  @maxLength(255)
  hash?: string | null;

  /** Информация о пользователе */
  max_info?: User | null;
}

/** Полезная нагрузка вложения-стикера */
model StickerAttachmentPayload extends AttachmentPayload {
  /** ID стикера */
  @maxLength(255)
  code: string;
}

/** Полезная нагрузка запроса ShareAttachmentRequest */
model ShareAttachmentPayload {
  /** URL, прикрепленный к сообщению в качестве предпросмотра медиа */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @minLength(1)
  @maxLength(2048)
  url?: string | null;

  /** Токен вложения */
  // 1024 — в оригинале лимита нет; по аналогии с официальным лимитом кнопочного payload (CallbackButton.payload)
  @maxLength(1024)
  token?: string | null;
}

/** Миниатюра видео */
model VideoThumbnail {
  /** URL изображения */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @maxLength(2048)
  url: string;
}

/**
 * URL-ы видео в разных разрешениях.
 *
 * Лимит 2048 у всех полей — официальный лимит URL в оригинальной схеме
 * (LinkButton.url, LinkMarkup.url).
 */
model VideoUrls {
  /** URL видео в разрешении 1080p, если доступно */
  @maxLength(2048)
  mp4_1080?: string | null;

  /** URL видео в разрешении 720p, если доступно */
  @maxLength(2048)
  mp4_720?: string | null;

  /** URL видео в разрешении 480p, если доступно */
  @maxLength(2048)
  mp4_480?: string | null;

  /** URL видео в разрешении 360p, если доступно */
  @maxLength(2048)
  mp4_360?: string | null;

  /** URL видео в разрешении 240p, если доступно */
  @maxLength(2048)
  mp4_240?: string | null;

  /** URL видео в разрешении 144p, если доступно */
  @maxLength(2048)
  mp4_144?: string | null;

  /** URL трансляции, если доступна */
  @maxLength(2048)
  hls?: string | null;
}

/** Подробная информация о видео-вложении */
model VideoAttachmentDetails {
  /** Токен видео-вложения */
  // 1024 — в оригинале лимита нет; по аналогии с официальным лимитом кнопочного payload (CallbackButton.payload)
  @maxLength(1024)
  token: string;

  /** URL-ы для скачивания или воспроизведения видео. Может быть `null`, если видео недоступно */
  urls?: VideoUrls | null;

  /** Миниатюра видео */
  thumbnail?: PhotoAttachmentPayload | null;

  /** Ширина видео */
  @minValue(0)
  @maxValue(2147483647)
  width: integer;

  /** Высота видео */
  @minValue(0)
  @maxValue(2147483647)
  height: integer;

  /** Длина видео в секундах */
  @minValue(0)
  @maxValue(2147483647)
  duration: integer;
}

/** Вложение изображения */
model PhotoAttachment extends Attachment {
  type: "image";

  /** Полезная нагрузка фото-вложения */
  payload: PhotoAttachmentPayload;
}

/** Вложение видео */
model VideoAttachment extends Attachment {
  type: "video";

  /** Полезная нагрузка видео-вложения */
  payload: MediaAttachmentPayload;

  /** Миниатюра видео */
  thumbnail?: VideoThumbnail | null;

  /** Ширина видео */
  @minValue(0)
  @maxValue(2147483647)
  width?: integer | null;

  /** Высота видео */
  @minValue(0)
  @maxValue(2147483647)
  height?: integer | null;

  /** Длина видео в секундах */
  @minValue(0)
  @maxValue(2147483647)
  duration?: integer | null;
}

/** Вложение аудио */
model AudioAttachment extends Attachment {
  /** Тип вложения: `audio` */
  type: "audio";

  /** Полезная нагрузка аудио-вложения */
  payload: MediaAttachmentPayload;

  /** Аудио транскрипция */
  // TODO: 255 — временно, пока транскрипции не используются. Реальная
  // транскрипция длинного голосового (до 60 мин) в этот лимит не влезет —
  // при включении фичи поднять лимит и длину в pattern (в оригинале лимита нет).
  @maxLength(255)
  @pattern("^[\\s\\S]{0,255}$")
  transcription?: string | null;
}

/** Вложение файла */
model FileAttachment extends Attachment {
  type: "file";

  /** Полезная нагрузка файлового вложения */
  payload: FileAttachmentPayload;

  /** Имя загруженного файла */
  @maxLength(255)
  filename: string;

  /** Размер файла в байтах */
  @minValue(0)
  @maxValue(9007199254740991)
  size: int64;
}

/** Вложение стикера */
model StickerAttachment extends Attachment {
  type: "sticker";

  /** Полезная нагрузка вложения-стикера */
  payload: StickerAttachmentPayload;

  /** Ширина стикера */
  @minValue(0)
  @maxValue(2147483647)
  width: integer;

  /** Высота стикера */
  @minValue(0)
  @maxValue(2147483647)
  height: integer;
}

/** Вложение с карточкой контакта */
model ContactAttachment extends Attachment {
  type: "contact";

  /** Полезная нагрузка вложения-контакта */
  payload: ContactAttachmentPayload;
}

/** Кнопки в сообщении */
model InlineKeyboardAttachment extends Attachment {
  type: "inline_keyboard";

  /** Полезная нагрузка клавиатуры */
  payload: Keyboard;
}

/** Вложение с предпросмотром ссылки */
model ShareAttachment extends Attachment {
  type: "share";

  /** Полезная нагрузка предпросмотра ссылки */
  payload: ShareAttachmentPayload;

  /** Заголовок предпросмотра ссылки */
  @maxLength(255)
  title?: string | null;

  /** Описание предпросмотра ссылки */
  @maxLength(255)
  description?: string | null;

  /** Изображение предпросмотра ссылки */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @maxLength(2048)
  image_url?: string | null;
}

/** Вложение географического положения */
model LocationAttachment extends Attachment {
  type: "location";

  /** Широта */
  latitude: float64;

  /** Долгота */
  longitude: float64;
}

/**
 * Вложение, содержащее полезную нагрузку, отправленную через `SendMessageButton`.
 *
 * В оригинальной схеме отсутствует в `Attachment.discriminator.mapping` —
 * см. DEVIATIONS `mapping[data]` в scripts/compare.py (тот же приём, что и
 * для `ChatButton`).
 */
model DataAttachment extends Attachment {
  type: "data";

  /** Полезная нагрузка, отправленная через SendMessageButton */
  // 1024 — официальный лимит источника значения (ReplyButton.payload, maxLength 1024 в оригинале)
  @maxLength(1024)
  data: string;
}

/**
 * Пользовательская reply-клавиатура в сообщении.
 *
 * В оригинальной схеме отсутствует в `Attachment.discriminator.mapping` —
 * см. DEVIATIONS `mapping[reply_keyboard]` в scripts/compare.py (тот же
 * приём, что и для `ChatButton`).
 */
model ReplyKeyboardAttachment extends Attachment {
  type: "reply_keyboard";

  /** Массив рядов с кнопками */
  @maxItems(100)
  buttons: ReplyButtonRow[];
}
