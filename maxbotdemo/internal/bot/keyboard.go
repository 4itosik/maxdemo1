package bot

import "maxbotdemo/internal/maxapi"

// Keyboard собирает inline-клавиатуру. В API это вложение типа inline_keyboard
// с двумерным массивом кнопок: внешний уровень — ряды.
//
// Max рекомендует ограничивать длину подписи в зависимости от числа кнопок в
// ряду: 20 символов при одной кнопке, 10 при двух, 5 при трёх, 3 при четырёх.
type Keyboard struct {
	rows [][]maxapi.Button
}

// NewKeyboard создаёт пустую клавиатуру.
func NewKeyboard() *Keyboard {
	return &Keyboard{}
}

// Row добавляет ряд кнопок.
func (k *Keyboard) Row(buttons ...maxapi.Button) *Keyboard {
	if len(buttons) > 0 {
		k.rows = append(k.rows, buttons)
	}
	return k
}

// Attachment превращает клавиатуру во вложение исходящего сообщения. Имя
// метода описывает действие, а схему контракта называет тип в сигнатуре.
func (k *Keyboard) Attachment() maxapi.AttachmentRequest {
	return maxapi.AttachmentRequest{
		Type:    maxapi.AttachmentInlineKeyboard,
		Payload: &maxapi.KeyboardPayload{Buttons: k.rows},
	}
}

// CallbackButton — кнопка, при нажатии которой бот получает событие
// message_callback с указанным payload.
func CallbackButton(text, payload string) maxapi.Button {
	return maxapi.Button{Type: maxapi.ButtonCallback, Text: text, Payload: payload}
}

// LinkButton — кнопка, открывающая ссылку.
func LinkButton(text, url string) maxapi.Button {
	return maxapi.Button{Type: maxapi.ButtonLink, Text: text, URL: url}
}

// RequestContactButton — кнопка, по нажатию которой клиент присылает боту
// новое сообщение с вложением contact: там телефон и, если человек есть в
// базе Max, его user_id.
func RequestContactButton(text string) maxapi.Button {
	return maxapi.Button{Type: maxapi.ButtonRequestContact, Text: text}
}

// RequestGeoLocationButton — то же для геопозиции: вложение location в новом
// сообщении.
//
// У кнопки есть ещё поле quick — отправка без запроса подтверждения. Бот им не
// пользуется, поэтому и в модели кнопки его нет: подтверждение ближе к тому,
// как это выглядит у человека в клиенте.
func RequestGeoLocationButton(text string) maxapi.Button {
	return maxapi.Button{Type: maxapi.ButtonRequestGeoLocation, Text: text}
}
