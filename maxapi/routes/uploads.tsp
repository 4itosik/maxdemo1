import "@typespec/http";
import "@typespec/openapi";

using Http;
using OpenAPI;

namespace MaxBotApi;

// Модель живёт в routes-файле осознанно: единственный потребитель (Uploads.getUploadUrl ниже), отдельного models-файла для upload-домена нет.
/** Точка доступа, куда следует загружать ваши бинарные файлы */
model UploadEndpoint {
  /** URL для загрузки медиафайла. Срок жизни ссылки не ограничен */
  // 2048 — официальный лимит URL в оригинальной схеме (LinkButton.url, LinkMarkup.url)
  @maxLength(2048)
  @pattern("^[\\w\\W]{0,2048}$")
  url: string;

  /** Токен для отправки медиафайла во вложении к сообщению с помощью POST /messages или PUT /messages */
  // 1024 — в оригинале лимита нет; по аналогии с официальным лимитом кнопочного payload (CallbackButton.payload)
  @maxLength(1024)
  @pattern("^[\\w\\W]{0,1024}$")
  token?: string;
}

@tag("upload")
interface Uploads {
  /**
   * Метод возвращает URL для загрузки медиафайла и токен для передачи
   * загруженного файла во вложении к сообщению в чате или канале
   */
  @summary("Загрузка медиафайлов")
  @tag("bot-sending")
  @operationId("getUploadUrl")
  @route("/uploads")
  @post
  getUploadUrl(
    /** Тип загружаемого файла. Возможные значения: `"image"`, `"video"`, `"audio"`, `"file"` */
    @query type: UploadType,
  ): UploadEndpoint | Unauthorized | InternalError;
}
