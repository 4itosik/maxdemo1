import "@typespec/http";

using Http;

namespace MaxBotApi;

/**
 * ID сообщения (`mid`). Реальные ID имеют вид `mid.000000001a5b94f2…` —
 * содержат точку, поэтому класс символов расширен относительно паттерна
 * оригинала `[a-zA-Z0-9_\-]+` у `GET /messages/{messageId}`: тот не заякорен
 * и потому пропускает вообще любую строку с одним допустимым символом
 * (см. DEVIATIONS в scripts/compare.py). Ограничения добавлены по
 * требованиям КБ (pattern + maxLength для строк).
 */
@minLength(1)
@maxLength(255)
@pattern("^[a-zA-Z0-9._\\-]+$")
scalar MessageId extends string;

/**
 * Тип события обновления, например `message_created`, `message_callback`.
 * Ограничения добавлены по требованиям КБ (pattern + maxLength для строк);
 * в оригинале — простая строка. Лимит 64 — по аналогии с официальным
 * лимитом коротких имён (`BotCommand.name`).
 */
@minLength(1)
@maxLength(64)
@pattern("^[a-z_]+$")
scalar UpdateType extends string;

/**
 * int64 в безопасном для JSON/JS диапазоне 0..2^53-1 (требование КБ: явные
 * minimum/maximum у чисел). Используется для элементов массивов и значений
 * Record, где инлайн-декораторы недоступны.
 */
@minValue(0)
@maxValue(9007199254740991)
scalar SafeInt64 extends int64;

/** Сервер возвращает это, если возникло исключение при вашем запросе */
@error
model Error {
  /** Ошибка */
  @maxLength(255)
  error?: string;

  /** Код ошибки */
  @maxLength(255)
  code: string;

  /** Читаемое описание ошибки */
  @maxLength(255)
  message: string;
}

/** Простой ответ на запрос */
model SimpleQueryResult {
  /** `true`, если запрос был успешным, `false` — в противном случае */
  success: boolean;

  /** Объяснительное сообщение, если результат не был успешным */
  @maxLength(255)
  message?: string;
}

/** Общая схема, описывающая объект изображения */
model Image {
  /** URL изображения */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @maxLength(2048)
  url: string;
}

/** Ошибка авторизации: не предоставлен или недействителен access_token */
model Unauthorized {
  @statusCode statusCode: 401;
  @body body: Error;
}

/** Ошибка доступа: нет прав на этот ресурс */
model Forbidden {
  @statusCode statusCode: 403;
  @body body: Error;
}

/** Запрашиваемый ресурс не найден */
model NotFound {
  @statusCode statusCode: 404;
  @body body: Error;
}

/** Метод не разрешён */
model NotAllowed {
  @statusCode statusCode: 405;
  @body body: Error;
}

/** Внутренняя ошибка сервера */
model InternalError {
  @statusCode statusCode: 500;
  @body body: Error;
}
