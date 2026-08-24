// Команда validate-capture сверяет с контрактом тела, снятые с живого Max.
//
// Регресс-тест на живом Max (см. maxbotdemo/README.md) собирает из лога бота
// настоящие события и тела запросов к API, а эта команда отвечает на вопрос,
// ради которого их собирали: описывает ли наш контракт то, что платформа
// присылает и принимает на самом деле. Схемы берутся встроенные — те же, по
// которым мок валидирует трафик, поэтому расхождение здесь означает
// расхождение контракта с реальностью, а не с моком.
//
// Контракт грузится СЫРЫМ, без нормализаций из internal/specs: мок правит
// загруженный документ под дефекты схемы, и с такими правками регресс
// перестал бы замечать ровно те расхождения, ради которых затевался. Цена
// такой правки уже известна: пока мок дописывал "dialog" в enum ChatType,
// расхождение контракта с платформой не видел никто — нашёл его первый же
// прогон этой команды на сыром документе.
//
// Вход — NDJSON: по одному JSON-документу на строку. Пустые строки и строки,
// начинающиеся с #, пропускаются, так что файл можно комментировать.
//
//	# события webhook (по умолчанию)
//	jq -c 'select(.msg == "получено событие") | .payload' bot.log |
//	    validate-capture
//
//	# тела запросов к API — против именованной схемы контракта
//	jq -c 'select(.msg == "запрос к API" and .path == "/messages") | .payload' bot.log |
//	    validate-capture -schema NewMessageBody
//
// Код возврата 1, если хотя бы одно тело не прошло проверку.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"maxmock/api"

	"github.com/dlclark/regexp2"
	"github.com/getkin/kin-openapi/openapi3"
)

// ecmaMatcher — тот же ECMAScript-движок регулярок, что использует мок:
// стандартный regexp Go построен на RE2 и не понимает конструкции, которые
// встречаются в паттернах контракта.
type ecmaMatcher struct{ re *regexp2.Regexp }

func (m ecmaMatcher) MatchString(s string) bool {
	ok, err := m.re.MatchString(s)
	return err == nil && ok
}

func compileRegex(expr string) (openapi3.RegexMatcher, error) {
	re, err := regexp2.Compile(expr, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return ecmaMatcher{re: re}, nil
}

func load(name string) (*openapi3.T, error) {
	b, err := api.FS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	l := openapi3.NewLoader()
	doc, err := l.LoadFromData(b)
	if err != nil {
		return nil, err
	}
	// Единственная правка документа: имя http-схемы авторизации к нижнему
	// регистру. Без неё документ не проходит собственную валидацию, а на
	// проверку тел она не влияет никак.
	for _, ref := range doc.Components.SecuritySchemes {
		if ref != nil && ref.Value != nil && ref.Value.Type == "http" {
			ref.Value.Scheme = strings.ToLower(ref.Value.Scheme)
		}
	}
	if err := doc.Validate(l.Context,
		openapi3.DisableExamplesValidation(),
		openapi3.SetRegexCompiler(compileRegex),
	); err != nil {
		return nil, err
	}
	return doc, nil
}

// webhookSchema — схема тела единственной операции webhook-контракта.
func webhookSchema(doc *openapi3.T) (*openapi3.SchemaRef, error) {
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op.RequestBody == nil || op.RequestBody.Value == nil {
				continue
			}
			if mt := op.RequestBody.Value.Content.Get("application/json"); mt != nil {
				return mt.Schema, nil
			}
		}
	}
	return nil, fmt.Errorf("в webhook-контракте нет операции с телом application/json")
}

func main() {
	schema := flag.String("schema", "", "имя схемы из components.schemas Bot API; без него тело проверяется как событие webhook")
	quiet := flag.Bool("q", false, "печатать только итог и расхождения")
	flag.Parse()

	var ref *openapi3.SchemaRef
	var webhookDoc *openapi3.T
	var version, what string

	if *schema != "" {
		doc, err := load(api.BotAPIFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "контракт Bot API не загружен:", err)
			os.Exit(2)
		}
		if ref = doc.Components.Schemas[*schema]; ref == nil {
			fmt.Fprintf(os.Stderr, "в контракте нет схемы %q\n", *schema)
			os.Exit(2)
		}
		version, what = doc.Info.Version, "тел по схеме "+*schema
	} else {
		doc, err := load(api.WebhookFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "webhook-контракт не загружен:", err)
			os.Exit(2)
		}
		if ref, err = webhookSchema(doc); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		version, what, webhookDoc = doc.Info.Version, "событий", doc
		if version == "" || version == "0.0.0" {
			if bot, err := load(api.BotAPIFile); err == nil {
				version = bot.Info.Version
			}
		}
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var total, bad int
	seen := map[string]int{}
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		total++
		kind := describe(line)
		if kind != "" {
			seen[kind]++
		}
		if err := validate(ref, []byte(line)); err != nil {
			if webhookDoc != nil {
				err = pinpoint(webhookDoc, []byte(line), err)
			}
			bad++
			fmt.Printf("строка %d: НЕ СООТВЕТСТВУЕТ КОНТРАКТУ\n  %s\n  тело: %s\n", n, firstLine(err.Error()), clip(line, 300))
			continue
		}
		if !*quiet {
			fmt.Printf("строка %d: ok  %s\n", n, kind)
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "чтение входа:", err)
		os.Exit(2)
	}

	if len(seen) > 0 {
		fmt.Printf("\nпокрыто типов событий: %d\n", len(seen))
		for _, k := range sortedKeys(seen) {
			fmt.Printf("  %-24s %d\n", k, seen[k])
		}
	}
	fmt.Printf("\nконтракт %s: проверено %s — %d, расхождений — %d\n", version, what, total, bad)
	if bad > 0 {
		os.Exit(1)
	}
}

func validate(ref *openapi3.SchemaRef, body []byte) error {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return fmt.Errorf("не разобрано как JSON: %w", err)
	}
	return ref.Value.VisitJSON(v, openapi3.MultiErrors())
}

// pinpoint переводит бесполезное «doesn't match any schema from anyOf» в
// конкретную причину: тело события проверяется против той схемы, которую
// выбирает дискриминатор по его update_type. Без этого расхождение видно, но
// непонятно, в каком поле.
func pinpoint(doc *openapi3.T, body []byte, fallback error) error {
	kind := describe(string(body))
	if kind == "" {
		return fallback
	}
	for _, base := range doc.Components.Schemas {
		if base.Value == nil || base.Value.Discriminator == nil {
			continue
		}
		target, ok := base.Value.Discriminator.Mapping[kind]
		if !ok {
			continue
		}
		name := target.Ref[strings.LastIndexByte(target.Ref, '/')+1:]
		concrete := doc.Components.Schemas[name]
		if concrete == nil || concrete.Value == nil {
			continue
		}
		var v any
		if json.Unmarshal(body, &v) != nil {
			return fallback
		}
		if err := concrete.Value.VisitJSON(v, openapi3.MultiErrors()); err != nil {
			return fmt.Errorf("как %s: %s", name, firstLine(err.Error()))
		}
	}
	return fallback
}

// describe вытаскивает из события его тип — так в выводе видно, какие ветки
// контракта регресс действительно покрыл, а какие остались непроверенными.
func describe(line string) string {
	var probe struct {
		UpdateType string `json:"update_type"`
	}
	if json.Unmarshal([]byte(line), &probe) == nil && probe.UpdateType != "" {
		return probe.UpdateType
	}
	return ""
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
