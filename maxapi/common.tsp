import "@typespec/http";

using Http;

namespace MaxBotApi;

/** Сервер возвращает это, если возникло исключение при вашем запросе */
@error
model Error {
  /** Ошибка */
  error?: string;

  /** Код ошибки */
  code: string;

  /** Читаемое описание ошибки */
  message: string;
}

/** Простой ответ на запрос */
model SimpleQueryResult {
  /** `true`, если запрос был успешным, `false` — в противном случае */
  success: boolean;

  /** Объяснительное сообщение, если результат не был успешным */
  message?: string;
}

/** Общая схема, описывающая объект изображения */
model Image {
  /** URL изображения */
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
