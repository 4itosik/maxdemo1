package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"maxmock/internal/store"
	"maxmock/internal/wire"
)

// Контакт и геопозиция клиента — то, чем в Max отвечают на кнопки
// request_contact и request_geo_location.
//
// Ни телефона, ни координат в схеме `User` нет: до бота они доезжают
// исключительно вложениями `contact` и `location`. Поэтому карточка клиента
// хранит их сама, а эти функции переводят её в формы контракта.

// ClientSendContact — клиент поделился своим контактом.
//
// Собирается тот же запрос на прикрепление, который прислал бы бот, и
// проходит через общий перевод в форму сообщения: одна дорога вместо двух
// означает, что стенд видит одинаковое вложение независимо от того, кто его
// отправил.
func (c *Core) ClientSendContact(chatID int64) (*wire.Message, error) {
	d, bot, cl, err := c.dialogContext(chatID)
	if err != nil {
		return nil, err
	}
	if cl.Phone == "" {
		return nil, fmt.Errorf(
			"%w: у клиента не задан телефон, контакт без номера бессмыслен — заполните телефон в карточке",
			ErrBadRequest)
	}
	payload, err := json.Marshal(wire.ContactRequestPayload{
		Name:      wire.Ptr(clientFullName(cl)),
		ContactID: wire.Ptr(cl.UserID),
		VcfPhone:  wire.Ptr(cl.Phone),
	})
	if err != nil {
		return nil, err
	}
	return c.appendMessage(bot, d, store.SenderClient, wire.NewMessageBody{
		Attachments: []wire.AttachmentRequest{{Type: wire.AttachmentContact, Payload: payload}},
	})
}

// ClientSendLocation — клиент поделился геопозицией. Нулевые координаты
// берутся из карточки клиента.
func (c *Core) ClientSendLocation(chatID int64, lat, lon *float64) (*wire.Message, error) {
	d, bot, cl, err := c.dialogContext(chatID)
	if err != nil {
		return nil, err
	}
	if lat == nil {
		lat = cl.Latitude
	}
	if lon == nil {
		lon = cl.Longitude
	}
	if lat == nil || lon == nil {
		return nil, fmt.Errorf(
			"%w: координаты не заданы ни в запросе, ни в карточке клиента", ErrBadRequest)
	}
	return c.appendMessage(bot, d, store.SenderClient, wire.NewMessageBody{
		Attachments: []wire.AttachmentRequest{{
			Type: wire.AttachmentLocation, Latitude: lat, Longitude: lon,
		}},
	})
}

// contactPayloadFromRequest переводит запрос на прикрепление контакта в
// полезную нагрузку вложения сообщения.
//
// Схемы у них разные, и это легко пропустить: в запросе `name`, `contact_id`,
// `vcf_phone`, в сообщении — `vcf_info`, `hash`, `max_info`.
func (c *Core) contactPayloadFromRequest(p wire.ContactRequestPayload) wire.ContactPayload {
	out := wire.ContactPayload{VcfInfo: p.VcfInfo}

	name := ""
	if p.Name != nil {
		name = *p.Name
	}
	phone := ""
	if p.VcfPhone != nil {
		phone = *p.VcfPhone
	}
	phone = normalizePhone(phone)
	if out.VcfInfo == nil && (name != "" || phone != "") {
		out.VcfInfo = wire.Ptr(vcard(name, phone))
	}
	if phone != "" {
		out.Hash = wire.Ptr(contactHash(phone))
	}
	// `contact_id` контракт описывает как «ID контакта, если он зарегистрирован
	// в MAX». Для мока «зарегистрирован» — значит есть в базе; тогда в
	// max_info уезжает тот же User, что и в sender, и КЦ получает телефон и
	// user_id одним вложением.
	if p.ContactID != nil {
		if cl, err := c.store.ClientByUserID(*p.ContactID); err == nil {
			out.MaxInfo = wire.Ptr(clientUser(cl))
		}
	}
	return out
}

// normalizePhone приводит телефон к тому виду, в каком его отдаёт живой Max:
// одни цифры, российский номер — с семёрки (в образце с прода
// `TEL;TYPE=cell:79639986193`).
//
// Карточку клиента заполняет человек через веб-интерфейс мока, и записать он
// может что угодно — «+7 (963) 998-61-93», «8963…». В самой карточке значение
// остаётся как введено, нормализация касается только того, что уходит боту:
// иначе КЦ, разбирающий номер из vcf_info, на моке видел бы формы, которых
// прод не присылает.
func normalizePhone(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	out := digits.String()
	// Единственная замена, которую можно сделать не гадая: одиннадцатизначный
	// номер с восьмёрки — российский, записанный по-междугородному.
	if len(out) == 11 && out[0] == '8' {
		out = "7" + out[1:]
	}
	return out
}

// vcard собирает карточку VCARD 3.0 — формат, в котором контракт ждёт
// vcf_info.
//
// Порядок строк, регистр параметра TYPE и строка PRODID повторяют карточку с
// прода: Max собирает её библиотекой ez-vcard и представляется в PRODID.
func vcard(name, phone string) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\nPRODID:ez-vcard 0.10.3\r\n")
	if phone != "" {
		b.WriteString("TEL;TYPE=cell:" + phone + "\r\n")
	}
	if name != "" {
		b.WriteString("FN:" + name + "\r\n")
	}
	b.WriteString("END:VCARD\r\n")
	return b.String()
}

// contactHash считает значение поля hash вложения-контакта.
//
// Контракт описывает его как признак того, что пользователь поделился
// номером, привязанным к аккаунту Max. Алгоритм живого Max не опубликован,
// поэтому мок берёт SHA-256 от нормализованного телефона: пустое поле
// означало бы «номер не привязан» и вводило бы КЦ в заблуждение, случайное —
// ломало бы повторяемость тестов. Сверять это значение со стендом нельзя, но
// длина обязана совпадать: на проде это 64 hex-символа, и обрезанный вдвое
// хэш молча не влез бы в поле, рассчитанное на полный.
func contactHash(phone string) string {
	sum := sha256.Sum256([]byte(phone))
	return hex.EncodeToString(sum[:])
}

// clientFullName собирает отображаемое имя клиента для карточки контакта.
func clientFullName(cl *store.Client) string {
	if cl.LastName == "" {
		return cl.FirstName
	}
	return cl.FirstName + " " + cl.LastName
}
