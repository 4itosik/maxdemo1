#!/usr/bin/env python3
"""Постобработка сгенерированного OpenAPI: комментарии к композитным схемам.

Замечание [125] SCHEMA-валидатора — «не рекомендуется использовать oneOf,
anyOf, allOf совместно с другими ключами, т.к. композитная часть будет
проигнорирована при генерации» — самое массовое в отчёте (см. review.md).
Расплющивание дискриминированных union'ов
(`scripts/flatten_discriminated_unions.py`) уменьшило число композитных
позиций со 125 до 71 и 59, но обнулить его нельзя: остаток держится
ограничениями самого OpenAPI 3.0. Разбор — в README, раздел «Что остаётся
композитным — и почему это потолок».

Читающий YAML этого README перед глазами не имеет. Скрипт ставит рядом с
каждой оставшейся позицией однострочный комментарий с буквой случая и
причиной, а в начало файла — блок с полным разбором и подсчётом позиций по
случаям. Пять случаев:

  [A] oneOf + discriminator + description — базы полиморфных полей;
  [B] allOf вокруг одиночного $ref + description — ссылка с описанием;
  [C] то же + nullable — ссылка, принимающая null;
  [D] allOf-наследование без дискриминатора;
  [E] anyOf без соседних ключей — тело вебхука.

Главный аргумент по каждому случаю — что так же сделано в самом контракте
MAX, поэтому у каждого случая печатается пометка «В ОРИГИНАЛЕ» с конкретными
схемами. Цифры и перечень схем для неё берутся из
`reference/max-openapi-official.json`, а не вписаны в текст: если снимок
оригинала обновят, комментарии не разойдутся с ним молча. Без референса
пометки просто не печатаются — лучше их отсутствие, чем непроверенное
утверждение.

Комментарии YAML-парсерами игнорируются, поэтому на потребителей документа
(`kin-openapi` в maxmoc, сверка в `scripts/compare.py`, `validate_kb.py`)
скрипт не влияет никак — только на человека, который откроет файл.

Правки построчные: комментарий вставляется перед строкой ключа с её же
отступом, остальной файл не переформатируется. Запуск идемпотентен —
собственные строки скрипт сначала удаляет, потом расставляет заново, так
что повторный прогон даёт тот же файл.

Место в конвейере — последнее: расплющивание меняет состав композитных
позиций, и комментарии должны описывать уже итоговый документ.
"""
import json
import re
import sys
from collections import Counter
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = ROOT / "tsp-output" / "@typespec" / "openapi3"
# Снимок официальной схемы MAX: из него берутся цифры и перечень схем для
# пометок «В ОРИГИНАЛЕ», чтобы они не разъезжались с реальным оригиналом.
REFERENCE = ROOT / "reference" / "max-openapi-official.json"

# Схемы webhook-документа приходят из общего namespace с этим префиксом;
# в официальной схеме его нет.
NS_PREFIX = "MaxBotApi."

COMPOSITE_KEYS = ("oneOf", "anyOf", "allOf")

# Маркеры собственных строк — по ним же они удаляются при повторном прогоне.
SITE_PREFIX = "# КБ[125]"
HEADER_OPEN = "# --- КБ[125]: почему в документе остались oneOf/allOf/anyOf ---"
HEADER_CLOSE = "# --- конец КБ[125] ---"

# Однострочные пояснения у самих позиций.
SITE_NOTE = {
    "A": "полиморфное поле: тот же discriminator, что в оригинале MAX; без oneOf проверялись бы только свойства базы",
    "B": "обёртка одиночного $ref, как в оригинале MAX: в OpenAPI 3.0 соседи $ref игнорируются, описание бы потерялось",
    "C": "обёртка одиночного $ref, как в оригинале MAX: иначе nullable рядом с $ref игнорируется и поле не примет null",
    "E": "две равноправные формы тела на выбор разработчика бота; аналога в оригинале нет — вебхук MAX не публикует",
    "?": "композитная схема неизвестного вида — проверьте вручную",
}

# У случая [D] причина зависит от семейства: у одного расплющивание снесло бы
# саму базу, у другого — молча потеряло бы её свойства (см. README). Записаны
# эти позиции ровно так же, как в оригинальной схеме MAX.
INHERITANCE_NOTE = {
    "AttachmentPayload": (
        "наследование от AttachmentPayload, как в оригинале MAX: эти allOf — единственные "
        "ссылки на базу, после расплющивания её удалил бы prune_unused_schemas"
    ),
    "User": (
        "наследование от User, как в оригинале MAX: UserWithPhoto сама база, стабов "
        "унаследованных свойств у неё нет — расплющивание потеряло бы их"
    ),
    "UserWithPhoto": (
        "наследование от UserWithPhoto, как в оригинале MAX: у базы нет стабов "
        "унаследованных свойств, расплющивание потеряло бы их"
    ),
}

CASE_TEXT = {
    "A": [
        "[A] oneOf + discriminator + description — {n} поз.",
        "    Базы полиморфных полей: updates[], attachments[], markup[], ряды кнопок,",
        "    тело вебхука. Это каноническая для OpenAPI запись дискриминированного",
        "    union'а; соседние ключи — часть той же конструкции (discriminator) и",
        "    требование КБ (description). Прежняя форма — база-объект со свойствами",
        "    плюс наследники через allOf — валидаторами тела не проверяется: у",
        "    полиморфного поля контролируются только свойства базы, у",
        "    AttachmentRequest это один type. Замер на kin-openapi: из 13 заведомо",
        "    некорректных тел (широта 991, вложение несуществующего типа, событие с",
        "    лишним полем, кнопка без обязательного text) прежняя форма принимала 11,",
        "    нынешняя — ни одного. Каждый вариант объявлен целиком, поэтому",
        "    «игнорирование композитной части» при генерации ничего не теряет.",
        "    В ОРИГИНАЛЕ: те же шесть дискриминаторов с теми же значениями mapping —",
        "    Attachment, AttachmentRequest, Button, ReplyButton, MarkupElement, Update.",
        "    Полиморфизм здесь не наш выбор, он в предметной области; отличается только",
        "    запись. У MAX база — объект со свойством type и discriminator, а варианты",
        "    ссылаются на неё через allOf:",
        "        Attachment:       {{discriminator: {{propertyName: type, mapping: {{...9}}}},",
        "                           properties: {{type: {{type: string}}}}, required: [type]}}",
        "        PhotoAttachment:  {{allOf: [{{$ref: Attachment}}, {{properties: {{payload}},",
        "                           required: [payload]}}]}}",
        "    Сайт при этом показывает варианты уже развёрнутыми: на странице",
        "    dev.max.ru/docs-api/objects/Update перечислены все типы событий, а",
        "    update_type прямо назван полем, которое их различает.",
    ],
    "B": [
        "[B] allOf вокруг одиночного $ref + description — {n} поз.",
        "    В OpenAPI 3.0 ключи рядом с $ref игнорируются, и описание поля потерялось",
        "    бы; описание требует КБ. Обойти нельзя: на форму {{$ref, description}}",
        "    kin-openapi отвечает ошибкой загрузки «extra sibling fields», тот же",
        "    запрет у Spectral (no-$ref-siblings). Переход на OpenAPI 3.1 проверен и",
        "    делает хуже — 206 композитных позиций вместо 130.",
        "    В ОРИГИНАЛЕ: {orig_b} таких позиций, форма посимвольно та же —",
        "        Chat.type:      {{allOf: [{{$ref: ChatType}}], description: ...}}",
        "        Chat.status:    {{allOf: [{{$ref: ChatStatus}}], description: ...}}",
        "        Message.sender: {{allOf: [{{$ref: User}}], description: ..., readOnly: false}}",
        "    (dev.max.ru/docs-api/objects/Chat, /objects/Message).",
    ],
    "C": [
        "[C] allOf вокруг одиночного $ref + nullable — {n} поз.",
        "    То же ограничение 3.0, но здесь на кону семантика, а не оформление:",
        "    nullable рядом с $ref игнорируется, и поле перестаёт принимать null.",
        "    Проверено на kin-openapi.",
        "    В ОРИГИНАЛЕ: {orig_c} таких позиций —",
        "        Chat.icon:           {{allOf: [{{$ref: Image}}], description: ..., nullable: true}}",
        "        Chat.pinned_message: {{allOf: [{{$ref: Message}}], description: ..., nullable: true,",
        "                              readOnly: false}}",
        "        Message.link:        {{allOf: [{{$ref: LinkedMessage}}], description: ..., nullable: true}}",
        "    (dev.max.ru/docs-api/objects/Chat, /objects/Message).",
    ],
    "D": [
        "[D] allOf-наследование без дискриминатора — {n} поз.",
        "    Union'ами эти семейства не являются, в oneOf не сводятся. Расплющить их",
        "    в обычные объекты технически можно, но у AttachmentPayload это сделало бы",
        "    базу недостижимой (её удалил бы prune_unused_schemas), а у UserWithPhoto",
        "    — молча потеряло бы 7 свойств, которым эмиттер не выписывает стабы, так",
        "    как она сама база.",
        "    В ОРИГИНАЛЕ: те же схемы записаны точно так же, одна в одну —",
        "@D_EXAMPLES@",
        "    Наследование зафиксировано и в текстах: страница",
        "    dev.max.ru/docs-api/objects/UserWithPhoto называет объект «наследником",
        "    схемы User» и показывает все 10 полей, включая унаследованные.",
        "    Всего таких позиций в оригинале {orig_d} — у нас осталось {n}.",
    ],
    "E": [
        "[E] anyOf без соседних ключей — {n} поз.",
        "    Тело вебхука описано двумя равноправными формами: плоской UpdateUnified и",
        "    строгим oneOf WebhookUpdate. Посторонних ключей рядом нет, требование",
        "    [125] эта позиция не нарушает. Цена известна: anyOf проходит при",
        "    совпадении хотя бы одной ветви, поэтому свои исходящие события следует",
        "    сверять напрямую со схемой WebhookUpdate, а не с телом операции.",
        "    В ОРИГИНАЛЕ: аналога нет и быть не может — контракт webhook-эндпоинта MAX",
        "    не публикует, его реализует разработчик бота. Ни одного anyOf в",
        "    официальной схеме нет.",
    ],
    "?": [
        "[?] композитные схемы неизвестного вида — {n} поз.",
        "    Скрипт их не классифицировал: проверьте вручную.",
    ],
}

HEADER_INTRO = [
    "Замечание SCHEMA-валидатора: «Не рекомендуется использовать oneOf, anyOf,",
    "allOf совместно с другими ключами, т.к. композитная часть будет",
    "проигнорирована при генерации».",
    "",
    "Композитных позиций в документе — {total}, и все они относятся к случаям ниже;",
    "у каждой стоит однострочный комментарий с буквой случая. Развёрнутый разбор —",
    "в maxapi/README.md, раздел «Что остаётся композитным — и почему это потолок».",
    "",
    "Главное про все пять случаев сразу: КОМПОЗИЦИЯ ЗДЕСЬ НЕ НАША ВЫДУМКА. У каждого",
    "случая (кроме [E], которого в оригинале и не может быть) есть буквальный аналог",
    "в официальном контракте MAX — ниже, под пометкой «В ОРИГИНАЛЕ», приведены",
    "конкретные схемы. Источник сравнения: max-messenger-bot/max-bot-api-schemas,",
    "файл schema_2026_08_11.json (версия API 0.0.33), копия без изменений лежит в",
    "maxapi/reference/max-openapi-official.json; человекочитаемая версия того же —",
    "на dev.max.ru/docs-api, страницы объектов по адресу /docs-api/objects/<Имя>.",
    "",
]

HEADER_OUTRO = [
    "",
    "Итог сравнения: в официальной схеме MAX {orig_total} композитных позиций",
    "({orig_b} случая [B] + {orig_c} случая [C] + {orig_d} случая [D]), в этом",
    "документе — {total}. Настоящего allOf-наследования почти не осталось: шесть",
    "семейств переписаны в oneOf + discriminator. То есть по требованию [125]",
    "документ уже чище источника, транскрипцией которого является, а всё",
    "оставшееся — это либо форма, скопированная у MAX, либо ограничение самого",
    "OpenAPI 3.0.",
]


def classify(node):
    """(буква случая, имя базы) для mapping-узла с композитным ключом."""
    keys = {k.value for k, _ in node.value}
    if "anyOf" in keys:
        return "E", None
    if "oneOf" in keys:
        return ("A", None) if "discriminator" in keys else ("?", None)
    allof = next(v for k, v in node.value if k.value == "allOf")
    base = None
    if isinstance(allof, yaml.SequenceNode) and len(allof.value) == 1:
        item = allof.value[0]
        item_keys = {k.value for k, _ in item.value} if isinstance(item, yaml.MappingNode) else set()
        if item_keys == {"$ref"}:
            ref = next(v.value for k, v in item.value if k.value == "$ref")
            base = ref.rsplit("/", 1)[-1]
            # Обёртка ссылки: своих properties у схемы нет.
            if "properties" not in keys:
                return ("C" if "nullable" in keys else "B"), base
    return "D", base


def collect(node, sites):
    """Все композитные позиции файла: (строка ключа, отступ, случай, база)."""
    if isinstance(node, yaml.SequenceNode):
        for item in node.value:
            collect(item, sites)
        return
    if not isinstance(node, yaml.MappingNode):
        return
    for key, value in node.value:
        if key.value in COMPOSITE_KEYS:
            case, base = classify(node)
            sites.append((key.start_mark.line, key.start_mark.column, case, base))
            break
    for _, value in node.value:
        collect(value, sites)


def strip_own(lines):
    """Удаление строк предыдущего прогона: блок шапки и комментарии позиций."""
    out, inside = [], False
    for line in lines:
        stripped = line.strip()
        if stripped == HEADER_OPEN:
            inside = True
            continue
        if inside:
            if stripped == HEADER_CLOSE:
                inside = False
            continue
        if stripped.startswith(SITE_PREFIX):
            continue
        out.append(line)
    if inside:
        print("Незакрытый блок КБ[125] в шапке — файл правили вручную, прерываю")
        sys.exit(2)
    return out


def official():
    """Оригинал MAX: счётчики по случаям и карта «наследник -> база».

    Цифры и перечень схем в пометках «В ОРИГИНАЛЕ» считаются по снимку, а не
    вписаны руками: если референс обновят, комментарии не разойдутся с ним
    молча. Без референса пометки не печатаются вовсе — лучше их отсутствие,
    чем непроверенное утверждение.
    """
    if not REFERENCE.exists():
        return None
    doc = json.loads(REFERENCE.read_text(encoding="utf-8"))
    counts, inherit = Counter(), {}

    def walk(node, name):
        if isinstance(node, dict):
            keys = set(node)
            if keys & set(COMPOSITE_KEYS):
                seq = node.get("allOf")
                single_ref = (
                    isinstance(seq, list)
                    and len(seq) == 1
                    and isinstance(seq[0], dict)
                    and set(seq[0]) == {"$ref"}
                )
                if "allOf" not in keys:
                    counts["A" if "discriminator" in keys else "?"] += 1
                elif single_ref and "properties" not in keys:
                    counts["C" if "nullable" in keys else "B"] += 1
                else:
                    counts["D"] += 1
                    if name and isinstance(seq, list) and isinstance(seq[0], dict):
                        inherit[name] = seq[0].get("$ref", "").rsplit("/", 1)[-1]
            for k, v in node.items():
                walk(v, name)
        elif isinstance(node, list):
            for v in node:
                walk(v, name)

    for schema_name, schema in doc.get("components", {}).get("schemas", {}).items():
        walk(schema, schema_name)
    walk({k: v for k, v in doc.items() if k != "components"}, None)
    return {"counts": counts, "inherit": inherit, "total": sum(counts.values())}


def d_examples(children, off):
    """Строки «В ОРИГИНАЛЕ» для случая [D] — только реально сверенные схемы."""
    pairs = []
    for name, base in children:
        # В webhook-документе схемы приходят с префиксом namespace-а.
        plain, plain_base = name.removeprefix(NS_PREFIX), (base or "").removeprefix(NS_PREFIX)
        if off["inherit"].get(plain) == plain_base:
            pairs.append((plain, plain_base))
    if not pairs:
        return ["        (в снимке оригинала таких схем не нашлось — проверьте вручную)"]
    width = max(len(n) for n, _ in pairs) + 1
    return [
        f"        {n + ':':<{width}} {{allOf: [{{$ref: {b}}}, {{properties: ...}}]}}"
        for n, b in sorted(pairs)
    ]


def header(counts, total, children, off):
    ctx = {"total": total}
    if off:
        ctx.update(
            orig_total=off["total"],
            orig_b=off["counts"]["B"],
            orig_c=off["counts"]["C"],
            orig_d=off["counts"]["D"],
        )
    body = [t.format(**ctx) for t in HEADER_INTRO]
    for case in ("A", "B", "C", "D", "E", "?"):
        if not counts.get(case):
            continue
        for text in CASE_TEXT[case]:
            if text == "@D_EXAMPLES@":
                body += d_examples(children, off)
                continue
            if not off and "{orig_" in text:
                continue  # без референса цифру подставить не из чего
            body.append(text.format(n=counts[case], **ctx))
        body.append("")
    body = body[:-1] + [t.format(**ctx) for t in HEADER_OUTRO]
    lines = [HEADER_OPEN] + [("# " + t).rstrip() for t in body] + [HEADER_CLOSE]
    return [l + "\n" for l in lines]


def inheritance_children(doc):
    """[(имя схемы, база)] для схем документа, наследующих через allOf."""
    out = []
    for name, schema in doc.get("components", {}).get("schemas", {}).items():
        if not isinstance(schema, dict) or "allOf" not in schema or "properties" not in schema:
            continue
        seq = schema["allOf"]
        if isinstance(seq, list) and seq and isinstance(seq[0], dict):
            out.append((name, seq[0].get("$ref", "").rsplit("/", 1)[-1]))
    return out


def annotate(path, off):
    text = path.read_text(encoding="utf-8")
    lines = strip_own(text.splitlines(keepends=True))

    sites = []
    collect(yaml.compose("".join(lines)), sites)
    counts = Counter(case for _, _, case, _ in sites)
    children = inheritance_children(yaml.safe_load("".join(lines))) if off else []

    # Вставки идут снизу вверх, иначе съезжают номера строк.
    for line_no, column, case, base in sorted(sites, reverse=True):
        note = INHERITANCE_NOTE.get(base, f"наследование от {base}") if case == "D" else SITE_NOTE[case]
        target = lines[line_no].strip()
        if not re.match(r"^(- )?(oneOf|anyOf|allOf):$", target):
            # Ключ не открывает собственную строку (flow-стиль) — комментарий
            # встал бы не туда, поэтому позиция пропускается.
            print(f"  {path.name}:{line_no + 1}: пропуск, ключ не в начале строки: {target[:60]}")
            continue
        lines.insert(line_no, f"{' ' * column}{SITE_PREFIX} [{case}] {note}\n")

    lines = header(counts, len(sites), children, off) + lines
    path.write_text("".join(lines), encoding="utf-8")
    return counts, len(sites)


def main():
    files = sorted(OUT_DIR.glob("*.yaml"))
    if not files:
        print(f"Нет *.yaml в {OUT_DIR.relative_to(ROOT)} — сначала выполните: npx tsp compile .")
        sys.exit(2)
    off = official()
    if off is None:
        print(f"Нет {REFERENCE.relative_to(ROOT)} — пометки «В ОРИГИНАЛЕ» пропущены")
    total = 0
    for f in files:
        counts, n = annotate(f, off)
        by_case = " ".join(f"{c}:{counts[c]}" for c in sorted(counts))
        print(f"{f.name}: {n} композитных позиций прокомментировано ({by_case})")
        total += n
        if counts.get("?"):
            print(f"  ВНИМАНИЕ: {counts['?']} позиций не классифицированы")
    print(f"Итого: {total}")


if __name__ == "__main__":
    main()
