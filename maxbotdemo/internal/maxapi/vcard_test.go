package maxapi

import "testing"

func TestContactPayloadPhone(t *testing.T) {
	tests := []struct {
		name string
		vcf  string
		want string
	}{
		{
			"простая карточка",
			"BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Иван\r\nTEL:+79991234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"параметр TYPE",
			"BEGIN:VCARD\r\nTEL;TYPE=CELL:+79991234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"групповой префикс в стиле Apple",
			"BEGIN:VCARD\r\nitem1.TEL;type=CELL:+79991234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"имя свойства в нижнем регистре",
			"BEGIN:VCARD\r\ntel:+79991234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"перевод строки без возврата каретки",
			"BEGIN:VCARD\nTEL:+79991234567\nEND:VCARD\n",
			"+79991234567",
		},
		{
			"первый из нескольких номеров",
			"BEGIN:VCARD\r\nTEL;TYPE=CELL:+79991234567\r\nTEL;TYPE=HOME:+74951234567\r\nEND:VCARD\r\n",
			"+79991234567",
		},
		{
			"карточка без TEL",
			"BEGIN:VCARD\r\nFN:Иван\r\nEND:VCARD\r\n",
			"",
		},
		{"пустая карточка", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &ContactPayload{VcfInfo: tt.vcf}
			if got := p.Phone(); got != tt.want {
				t.Errorf("Phone() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContactPayloadName(t *testing.T) {
	tests := []struct {
		name    string
		payload ContactPayload
		want    string
	}{
		{
			"FN из карточки",
			ContactPayload{VcfInfo: "BEGIN:VCARD\r\nFN:Иван Петров\r\nEND:VCARD\r\n"},
			"Иван Петров",
		},
		{
			// По RFC 6350 продолжение начинается с пробела, и сам пробел в
			// значение не входит: строка склеивается без него.
			"свёрнутая строка",
			ContactPayload{VcfInfo: "BEGIN:VCARD\r\nFN:Иван Петр\r\n ович\r\nEND:VCARD\r\n"},
			"Иван Петрович",
		},
		{
			"без FN — имя из max_info",
			ContactPayload{
				VcfInfo: "BEGIN:VCARD\r\nTEL:+79991234567\r\nEND:VCARD\r\n",
				MaxInfo: &User{UserID: 42, FirstName: "Иван", LastName: "Петров"},
			},
			"Иван Петров",
		},
		{"ни карточки, ни max_info", ContactPayload{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.payload.Name(); got != tt.want {
				t.Errorf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Аксессоры Update возвращают nil, когда вложения нет, — вызов на таком
// указателе не должен ронять бота.
func TestNilContactPayloadIsSafe(t *testing.T) {
	var p *ContactPayload

	if got := p.Phone(); got != "" {
		t.Errorf("Phone() = %q, want пустую строку", got)
	}
	if got := p.Name(); got != "" {
		t.Errorf("Name() = %q, want пустую строку", got)
	}
}
