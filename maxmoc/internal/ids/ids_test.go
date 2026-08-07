package ids

import (
	"regexp"
	"testing"
)

func TestMidDeterministicAndScoped(t *testing.T) {
	if Mid(7, 3) != Mid(7, 3) {
		t.Fatal("Mid не детерминирован")
	}
	if Mid(7, 3) == Mid(8, 3) {
		t.Fatal("Mid не различает чаты")
	}
	if Mid(7, 3) == Mid(7, 4) {
		t.Fatal("Mid не различает порядковые номера")
	}
	if !regexp.MustCompile(`^mid\.[0-9a-f]{32}$`).MatchString(Mid(7, 3)) {
		t.Fatalf("формат mid: %s", Mid(7, 3))
	}
}

// Образец mid с живого Max: `mid.` и две упакованные в hex половины по 16
// символов — chat_id и seq. Проверяем разбором, а не сравнением строк:
// сломается формат — тест покажет, какая именно половина поехала.
func TestMidMatchesLiveSample(t *testing.T) {
	const (
		sample = "mid.000000001a5b94f2019fdaa593db1d43"
		chatID = 442209522
		seq    = 117052520019991875
	)
	if got := Mid(chatID, seq); got != sample {
		t.Errorf("Mid(%d, %d) = %s, образец с прода = %s", chatID, seq, got, sample)
	}
}

// Живой Max упаковывает в seq время создания сообщения: в образце с прода
// seq = 117052520019991875, а timestamp сообщения = 1786079712219, и
// seq >> 16 даёт ровно timestamp. Младшие 16 бит — счётчик внутри
// миллисекунды.
func TestSeqPacksTimestampIntoHighBits(t *testing.T) {
	const ts = 1786079712219
	if got := Seq(ts, 0) >> 16; got != ts {
		t.Errorf("Seq(%d, 0) >> 16 = %d, ожидалось %d", ts, got, ts)
	}
}

// Порядковый номер обязан строго расти внутри чата: по нему идёт сортировка
// ленты, и на него стоит UNIQUE(chat_id, seq). Внутри одной миллисекунды
// время в старших битах одинаково, растёт только счётчик.
func TestSeqStrictlyGrowsWithinOneMillisecond(t *testing.T) {
	const ts = 1786079712219
	prev := int64(0)
	for i := 0; i < 5; i++ {
		s := Seq(ts, prev)
		if s <= prev {
			t.Fatalf("шаг %d: Seq(%d, %d) = %d — номер не вырос", i, ts, prev, s)
		}
		prev = s
	}
}

// Часы могут отойти назад (перевод времени, NTP), но номер откатываться не
// должен — иначе сообщение встанет в ленте раньше уже существующих.
func TestSeqNeverGoesBackwards(t *testing.T) {
	const ts = 1786079712219
	ahead := Seq(ts+5000, 0)
	if s := Seq(ts, ahead); s <= ahead {
		t.Errorf("Seq(%d, %d) = %d — номер откатился назад", ts, ahead, s)
	}
}

// Обратная сторона TestIDsSurviveFloat64RoundTrip: chat_id и user_id мок
// намеренно держит внутри безопасной для JS зоны, а seq — намеренно выводит
// за неё, потому что живой Max выдаёт seq порядка 1.17e17. Клиент, который
// сравнивает seq или кладёт его в ключ, обязан спотыкаться на моке так же,
// как споткнётся на проде: мок, скрывающий это расхождение, бесполезен.
func TestSeqExceedsFloat64PrecisionLikeProd(t *testing.T) {
	const maxSafeInteger = int64(1)<<53 - 1
	distorted := 0
	prev := int64(0)
	for i := 0; i < 100; i++ {
		s := Seq(1786079712219, prev)
		if s <= maxSafeInteger {
			t.Fatalf("seq = %d не выходит за Number.MAX_SAFE_INTEGER — расхождение с продом скрыто", s)
		}
		if int64(float64(s)) != s {
			distorted++
		}
		prev = s
	}
	// Не каждое значение искажается: точность теряют те, у кого значащими
	// оказываются младшие биты. Важно, что счётчик вообще участвует — при
	// нулевом счётчике не искажалось бы ни одно.
	if distorted == 0 {
		t.Error("ни один seq не искажается при разборе как float64 — младшие биты незначимы")
	}
}

func TestIDsPositiveAndUnique(t *testing.T) {
	seen := make(map[int64]bool, 100)
	for i := 0; i < 100; i++ {
		id := NewChatID()
		if id <= 0 {
			t.Fatalf("неположительный chat_id: %d", id)
		}
		if seen[id] {
			t.Fatalf("повтор chat_id: %d", id)
		}
		seen[id] = true
	}
	if NewUserID() <= 0 {
		t.Fatal("неположительный user_id")
	}
}

// Идентификаторы проходят через JSON и разбираются в JavaScript как float64:
// всё, что больше 2^53−1, там теряет точность. Проверяем строго — round-trip
// через float64 обязан вернуть исходное значение.
func TestIDsSurviveFloat64RoundTrip(t *testing.T) {
	const maxSafeInteger = int64(1)<<53 - 1
	for i := 0; i < 1000; i++ {
		for name, id := range map[string]int64{"chat_id": NewChatID(), "user_id": NewUserID()} {
			if id > maxSafeInteger {
				t.Fatalf("%s = %d превышает Number.MAX_SAFE_INTEGER", name, id)
			}
			if int64(float64(id)) != id {
				t.Fatalf("%s = %d искажается при разборе как float64", name, id)
			}
		}
	}
}

func TestTokens(t *testing.T) {
	if NewToken("upload") == NewToken("upload") {
		t.Fatal("токены совпали")
	}
	for _, tok := range []string{NewToken("upload"), NewBotToken(), NewUploadToken()} {
		if !regexp.MustCompile(`^[a-z]+\.[0-9a-f]{32}$`).MatchString(tok) {
			t.Errorf("формат токена: %s", tok)
		}
	}
}

// callback_id живого Max — 116 символов base64url без префикса (образец с
// прода декодируется ровно в 87 байт). Длина здесь не косметика: она втрое
// больше прежней мокавой, и клиент, отведший под поле короткую колонку,
// обязан спотыкаться на моке так же, как споткнётся на проде.
func TestCallbackIDMatchesLiveFormat(t *testing.T) {
	if NewCallbackID() == NewCallbackID() {
		t.Fatal("идентификаторы нажатия совпали")
	}
	for i := 0; i < 100; i++ {
		id := NewCallbackID()
		if len(id) != 116 {
			t.Fatalf("длина callback_id = %d, у прода 116: %s", len(id), id)
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{116}$`).MatchString(id) {
			t.Fatalf("callback_id вне алфавита base64url: %s", id)
		}
	}
}
