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
причиной, а в начало файла — краткую сводку по случаям с подсчётом позиций.
Пять случаев:

  [A] oneOf + discriminator + description — базы полиморфных полей;
  [B] allOf вокруг одиночного $ref + description — ссылка с описанием;
  [C] то же + nullable — ссылка, принимающая null;
  [D] allOf-наследование без дискриминатора;
  [E] anyOf без соседних ключей — тело вебхука.

Главный аргумент по каждому случаю — что так же сделано в самом контракте
MAX, поэтому у каждого случая печатается пометка «в оригинале» с конкретными
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
import textwrap
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
# LEGACY_* — маркеры прежней редакции комментариев; нужны только затем, чтобы
# файл, размеченный старым скриптом, тоже вычищался начисто.
SITE_PREFIX = "# композит"
HEADER_OPEN = "# --- oneOf/allOf/anyOf в этом документе: что осталось и почему ---"
HEADER_CLOSE = "# --- конец ---"
LEGACY_SITE_PREFIX = "# КБ[125]"
LEGACY_HEADER_OPEN = "# --- КБ[125]: почему в документе остались oneOf/allOf/anyOf ---"
LEGACY_HEADER_CLOSE = "# --- конец КБ[125] ---"

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
        "[A] oneOf + discriminator + description — {n} поз. Базы полиморфных полей: updates[],",
        "    attachments[], markup[], ряды кнопок, тело вебхука; discriminator и description —",
        "    часть той же конструкции. В оригинале те же шесть дискриминаторов, но варианты",
        "    подключены к базе через allOf: при такой записи у полиморфного поля проверяются",
        "    только свойства базы — из 13 некорректных тел kin-openapi пропускал 11, теперь ноль.",
    ],
    "B": [
        "[B] allOf вокруг одиночного $ref + description — {n} поз. В OpenAPI 3.0 соседи $ref",
        "    игнорируются: без обёртки описание поля пропало бы, а форму {{$ref, description}}",
        "    kin-openapi и Spectral не принимают вовсе. Переход на 3.1 проверен — хуже,",
        "    206 композитных позиций вместо 130.",
        "    В оригинале {orig_b} таких же позиций: Chat.type, Chat.status, Message.sender.",
    ],
    "C": [
        "[C] allOf вокруг одиночного $ref + nullable — {n} поз. То же ограничение 3.0, но на",
        "    кону семантика: без обёртки nullable игнорируется и поле перестаёт принимать null.",
        "    В оригинале {orig_c} таких позиций: Chat.icon, Chat.pinned_message, Message.link.",
    ],
    "D": [
        "[D] allOf-наследование без дискриминатора — {n} поз. Union'ами эти семейства не",
        "    являются, в oneOf не сводятся; расплющивание сделало бы базу AttachmentPayload",
        "    недостижимой, а у UserWithPhoto молча потеряло бы 7 свойств.",
        "@D_EXAMPLES@",
    ],
    "E": [
        "[E] anyOf без соседних ключей — {n} поз. Тело вебхука: на выбор плоская UpdateUnified",
        "    или строгий oneOf WebhookUpdate, посторонних ключей рядом нет. anyOf проходит при",
        "    совпадении любой ветви, поэтому свои исходящие события сверяйте прямо со схемой",
        "    WebhookUpdate. В оригинале аналога нет — webhook-контракт MAX не публикует.",
    ],
    "?": [
        "[?] композитные схемы неизвестного вида — {n} поз. Скрипт их не классифицировал:",
        "    проверьте вручную.",
    ],
}

HEADER_INTRO = [
    "Композитных позиций — {total}; у каждой ниже стоит однострочный комментарий с буквой",
    "случая. Всё оставшееся — либо форма, скопированная из официального контракта MAX",
    "(reference/max-openapi-official.json — снимок schema_2026_08_11.json, API 0.0.33; он же",
    "на dev.max.ru/docs-api/objects/<Имя>), либо ограничение самого OpenAPI 3.0.",
    "Подробный разбор — maxapi/README.md, «Что остаётся композитным — и почему это потолок».",
    "",
]

HEADER_OUTRO = [
    "",
    "Итого: в официальной схеме MAX {orig_total} композитных позиций ({orig_b} [B], {orig_c} [C],",
    "{orig_d} [D]), в этом документе — {total}. Настоящего allOf-наследования почти не осталось:",
    "шесть семейств переписаны в oneOf + discriminator.",
]

# Тот же итог без цифр оригинала — на случай, когда снимка референса нет.
HEADER_OUTRO_PLAIN = [
    "",
    "Настоящего allOf-наследования почти не осталось: шесть семейств переписаны",
    "в oneOf + discriminator.",
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
    """Удаление строк предыдущего прогона: блок шапки и комментарии позиций.

    Маркеры прежней редакции убираются наравне с нынешними — иначе файл,
    размеченный старым скриптом, получил бы вторую шапку поверх первой.
    """
    out, inside, close = [], False, None
    for line in lines:
        stripped = line.strip()
        if not inside and stripped in (HEADER_OPEN, LEGACY_HEADER_OPEN):
            inside = True
            close = HEADER_CLOSE if stripped == HEADER_OPEN else LEGACY_HEADER_CLOSE
            continue
        if inside:
            if stripped == close:
                inside = False
            continue
        if stripped.startswith((SITE_PREFIX, LEGACY_SITE_PREFIX)):
            continue
        out.append(line)
    if inside:
        print("Незакрытый блок шапки — файл правили вручную, прерываю")
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
    """Строка «в оригинале» для случая [D] — только реально сверенные схемы."""
    names = []
    for name, base in children:
        # В webhook-документе схемы приходят с префиксом namespace-а.
        plain, plain_base = name.removeprefix(NS_PREFIX), (base or "").removeprefix(NS_PREFIX)
        if off["inherit"].get(plain) == plain_base:
            names.append(plain)
    if not names:
        return ["    (в снимке оригинала таких схем не нашлось — проверьте вручную)"]
    text = (
        f"В оригинале ({off['counts']['D']} поз.) те же схемы записаны один в один: "
        + ", ".join(sorted(names))
        + "."
    )
    return textwrap.wrap(text, width=86, initial_indent="    ", subsequent_indent="    ")


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
                if off:
                    body += d_examples(children, off)
                continue
            if not off and "{orig_" in text:
                continue  # без референса цифру подставить не из чего
            body.append(text.format(n=counts[case], **ctx))
        body.append("")
    body = body[:-1] + [t.format(**ctx) for t in (HEADER_OUTRO if off else HEADER_OUTRO_PLAIN)]
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
