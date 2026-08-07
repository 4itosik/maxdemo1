namespace MaxBotApi;

/** Общая схема, представляющая вложение сообщения */
@discriminator("type")
model Attachment {
  /** Тип вложения */
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
  url: string;
}

/** Полезная нагрузка фото-вложения */
model PhotoAttachmentPayload {
  /** Уникальный ID этого изображения */
  photo_id: int64;

  /** Токен вложения */
  token: string;

  /** URL изображения. Время жизни ссылки ограничено: срок истечения указан в параметре `expires`, после чего ссылку необходимо запросить повторно. Пока доступно только в веб-клиенте */
  url: string;
}

/** Полезная нагрузка медиа-вложения (видео/аудио) */
model MediaAttachmentPayload extends AttachmentPayload {
  /** Используйте `token`, если вы пытаетесь повторно использовать одно и то же вложение в другом сообщении */
  token: string;
}

/** Полезная нагрузка файлового вложения */
model FileAttachmentPayload extends AttachmentPayload {
  /** Используйте `token`, если вы пытаетесь повторно использовать одно и то же вложение в другом сообщении */
  token: string;
}

/** Полезная нагрузка вложения-контакта */
model ContactAttachmentPayload {
  /** Информация о пользователе в формате VCF */
  vcf_info?: string | null;

  /**
   * Хеш информации о пользователе в формате VCF. Используется для проверки
   * того, что пользователь поделился номером телефона, привязанным к его
   * аккаунту в МАКС. Подробнее — в разделе «Типы кнопок»
   */
  hash?: string | null;

  /** Информация о пользователе */
  max_info?: User | null;
}

/** Полезная нагрузка вложения-стикера */
model StickerAttachmentPayload extends AttachmentPayload {
  /** ID стикера */
  code: string;
}

/** Полезная нагрузка запроса ShareAttachmentRequest */
model ShareAttachmentPayload {
  /** URL, прикрепленный к сообщению в качестве предпросмотра медиа */
  @minLength(1)
  url?: string | null;

  /** Токен вложения */
  token?: string | null;
}

/** Миниатюра видео */
model VideoThumbnail {
  /** URL изображения */
  url: string;
}

/** URL-ы видео в разных разрешениях */
model VideoUrls {
  /** URL видео в разрешении 1080p, если доступно */
  mp4_1080?: string | null;

  /** URL видео в разрешении 720p, если доступно */
  mp4_720?: string | null;

  /** URL видео в разрешении 480p, если доступно */
  mp4_480?: string | null;

  /** URL видео в разрешении 360p, если доступно */
  mp4_360?: string | null;

  /** URL видео в разрешении 240p, если доступно */
  mp4_240?: string | null;

  /** URL видео в разрешении 144p, если доступно */
  mp4_144?: string | null;

  /** URL трансляции, если доступна */
  hls?: string | null;
}

/** Подробная информация о видео-вложении */
model VideoAttachmentDetails {
  /** Токен видео-вложения */
  token: string;

  /** URL-ы для скачивания или воспроизведения видео. Может быть `null`, если видео недоступно */
  urls?: VideoUrls | null;

  /** Миниатюра видео */
  thumbnail?: PhotoAttachmentPayload | null;

  /** Ширина видео */
  width: integer;

  /** Высота видео */
  height: integer;

  /** Длина видео в секундах */
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
  width?: integer | null;

  /** Высота видео */
  height?: integer | null;

  /** Длина видео в секундах */
  duration?: integer | null;
}

/** Вложение аудио */
model AudioAttachment extends Attachment {
  type: "audio";

  /** Полезная нагрузка аудио-вложения */
  payload: MediaAttachmentPayload;

  /** Аудио транскрипция */
  transcription?: string | null;
}

/** Вложение файла */
model FileAttachment extends Attachment {
  type: "file";

  /** Полезная нагрузка файлового вложения */
  payload: FileAttachmentPayload;

  /** Имя загруженного файла */
  filename: string;

  /** Размер файла в байтах */
  size: int64;
}

/** Вложение стикера */
model StickerAttachment extends Attachment {
  type: "sticker";

  /** Полезная нагрузка вложения-стикера */
  payload: StickerAttachmentPayload;

  /** Ширина стикера */
  width: integer;

  /** Высота стикера */
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
  title?: string | null;

  /** Описание предпросмотра ссылки */
  description?: string | null;

  /** Изображение предпросмотра ссылки */
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
  buttons: ReplyButton[][];
}
