package maxfacade

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/getkin/kin-openapi/routers"

	"maxmock/internal/core"
	"maxmock/internal/wire"
)

// Коды ошибок мока.
//
// Официальный контракт Max не описывает ни одного ответа 4xx, кроме 401, 403,
// 404 и 405, и не документирует коды в теле Error. Всё, что мок различает
// сверх контракта, помечается префиксом `mock.` — так видно, что это
// конвенция эмулятора, а не наблюдение за реальной платформой. При появлении
// доступа к проду коды заменяются на настоящие без изменения структуры.
const (
	CodeValidation         = "mock.validation"
	CodeNotImplemented     = "mock.not_implemented"
	CodeChatNotFound       = "mock.chat.not_found"
	CodeMessageNotFound    = "mock.message.not_found"
	CodeCallbackNotFound   = "mock.callback.not_found"
	CodeAttachmentNotFound = "mock.attachment.not_found"
	CodeForbidden          = "mock.forbidden"
	CodeBadRequest         = "mock.bad_request"
	CodeInternal           = "mock.internal"
)

// CodeVerifyToken — код ошибки авторизации. Единственный код мока без
// префикса `mock.`: и он, и англоязычные тексты его сообщений дословно
// скопированы с живого Max (замер 2026-08-06,
// docs/specs/2026-08-06-auth-parity-tz.md). Клиент вправе завязаться на
// code, и мок обязан отдать тот же самый.
const CodeVerifyToken = "verify.token"

// statusFor выбирает код ответа: желаемый, если он объявлен в контракте для
// этой операции, иначе 400.
//
// Так мок не выдумывает статусов сверх контракта. Пример: «сообщение не
// найдено» для GET /messages/{messageId} даёт 404 (он объявлен), а для
// DELETE /messages — 400 с кодом mock.message.not_found, потому что 404 для
// удаления контракт не описывает.
func statusFor(route *routers.Route, preferred int) int {
	if route == nil || route.Operation == nil || route.Operation.Responses == nil {
		return preferred
	}
	if route.Operation.Responses.Value(strconv.Itoa(preferred)) != nil {
		return preferred
	}
	return http.StatusBadRequest
}

// errorResponse переводит доменную ошибку в статус и код мока.
func errorResponse(route *routers.Route, err error) (int, string, string) {
	switch {
	case errors.Is(err, core.ErrChatNotFound):
		return statusFor(route, http.StatusNotFound), CodeChatNotFound, "диалог не найден"
	case errors.Is(err, core.ErrMessageNotFound):
		return statusFor(route, http.StatusNotFound), CodeMessageNotFound, "сообщение не найдено"
	case errors.Is(err, core.ErrCallbackNotFound):
		return statusFor(route, http.StatusNotFound), CodeCallbackNotFound, "нажатие кнопки не найдено или устарело"
	case errors.Is(err, core.ErrAttachmentNotFound):
		return statusFor(route, http.StatusNotFound), CodeAttachmentNotFound, "вложение не найдено: сначала загрузите файл"
	case errors.Is(err, core.ErrForbidden):
		return statusFor(route, http.StatusForbidden), CodeForbidden, "операция запрещена"
	case errors.Is(err, core.ErrBadRequest):
		return http.StatusBadRequest, CodeBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, CodeInternal, err.Error()
	}
}

// writeJSON пишет тело ответа.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"code":"`+CodeInternal+`","message":"сериализация ответа"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeError пишет тело ошибки в схеме Error.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, wire.Error{Code: code, Message: message})
}
