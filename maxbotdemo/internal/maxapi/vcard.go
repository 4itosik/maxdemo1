package maxapi

import "strings"

// Разбор карточки VCARD из вложения contact.
//
// Мок отдаёт стерильную карточку в четыре строки, живой Max — ту, что отдал
// телефон пользователя. Поэтому разбор терпит параметры свойства
// (TEL;TYPE=CELL:), групповые префиксы в стиле Apple (item1.TEL:), свёрнутые
// строки и любой регистр имени свойства.
//
// Чего он намеренно не делает: не разворачивает экранирование значений
// (\, \; \\) — в телефоне и имени оно не встречается; и ломается на параметре
// в кавычках с двоеточием внутри (TEL;TYPE="a:b":+7…). Контракт таких карточек
// не порождает, а гадать по кавычкам ради несуществующего случая незачем.

// Phone возвращает первый номер телефона из vcf_info или пустую строку.
func (p *ContactPayload) Phone() string {
	if p == nil {
		return ""
	}
	return vcardValue(p.VcfInfo, "TEL")
}

// Name возвращает имя контакта: FN из карточки, а если её нет — имя из
// max_info.
func (p *ContactPayload) Name() string {
	if p == nil {
		return ""
	}
	if fn := vcardValue(p.VcfInfo, "FN"); fn != "" {
		return fn
	}
	return p.MaxInfo.DisplayName()
}

// vcardValue возвращает значение первого свойства с заданным именем.
func vcardValue(vcf, prop string) string {
	for _, line := range vcardLines(vcf) {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		// Параметры отделены точкой с запятой: TEL;TYPE=CELL.
		name, _, _ = strings.Cut(name, ";")
		// Групповой префикс — всё до последней точки: item1.TEL.
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if strings.EqualFold(strings.TrimSpace(name), prop) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// vcardLines разбивает карточку на логические строки, разворачивая переносы:
// по RFC 6350 продолжение начинается с пробела или табуляции, и сам этот
// символ в значение не входит.
func vcardLines(vcf string) []string {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(vcf, "\r\n", "\n"), "\n") {
		if raw == "" {
			continue
		}
		if (raw[0] == ' ' || raw[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += raw[1:]
			continue
		}
		lines = append(lines, raw)
	}
	return lines
}
