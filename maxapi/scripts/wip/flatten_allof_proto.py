#!/usr/bin/env python3
"""ПРОТОТИП: расплющивание одного allOf-семейства в готовом OpenAPI.

Берёт базу с discriminator и её наследников и переписывает пару
«база-объект + наследники через allOf» в каноническую форму OpenAPI:

  БЫЛО                              СТАЛО
  Base:  {type: object,             Base:  {oneOf: [...N ссылок],
          properties, required,             discriminator, description}
          discriminator, description}
  Child: {type: object, properties, Child: {type: object, properties,
          additionalProperties:false,       required: <свой + базы>,
          required: <только свой>,          additionalProperties: false,
          allOf: [$ref Base],               description}
          description}

Правки построчные — форматирование остального файла не трогается.
"""
import sys
from pathlib import Path
import yaml

OUT = Path(__file__).resolve().parent.parent.parent / "tsp-output" / "@typespec" / "openapi3"


def schema_nodes(root):
    comp = next(v for k, v in root.value if k.value == "components")
    schemas = next(v for k, v in comp.value if k.value == "schemas")
    starts = [(k.value, k.start_mark.line, v) for k, v in schemas.value]
    bounds = [ln for _, ln, _ in starts[1:]] + [schemas.end_mark.line]
    return {n: (ln, end, node) for (n, ln, node), end in zip(starts, bounds)}


def key_spans(node, schema_end):
    """{ключ: (строка ключа, строка за концом его значения)} внутри схемы."""
    ks = [(k.value, k.start_mark.line) for k, v in node.value]
    bounds = [ln for _, ln in ks[1:]] + [schema_end]
    return {k: (ln, end) for (k, ln), end in zip(ks, bounds)}


def main(base_name, fname="openapi.MaxBotApi.yaml"):
    f = OUT / fname
    text = f.read_text(encoding="utf-8")
    doc = yaml.safe_load(text)
    sch = doc["components"]["schemas"]
    root = yaml.compose(text)
    spans = schema_nodes(root)

    base = sch[base_name]
    base_required = list(base.get("required") or [])
    children = [n for n, s in sch.items()
                if any(isinstance(p, dict) and p.get("$ref", "").endswith("/" + base_name)
                       for p in (s.get("allOf") or []))]
    print(f"база {base_name}: required={base_required}, наследников {len(children)}")

    lines = text.split("\n")
    edits = []   # (начало, конец, новые строки) — применяем снизу вверх

    # --- наследники: убрать allOf, дописать required базы
    for name in children:
        s = sch[name]
        start, end, node = spans[name]
        ks = key_spans(node, end)
        a_start, a_end = ks["allOf"]
        edits.append((a_start, a_end, []))
        missing = [r for r in base_required if r not in (s.get("required") or [])]
        if not missing:
            continue
        if "required" in ks:
            r_start, r_end = ks["required"]
            edits.append((r_end, r_end, [f"        - {r}" for r in missing]))
        else:
            t_start, _ = ks["type"]
            edits.append((t_start + 1, t_start + 1,
                          ["      required:"] + [f"        - {r}" for r in missing]))

    # --- база: type/required/properties -> oneOf со ссылками на наследников
    b_start, b_end, b_node = spans[base_name]
    bks = key_spans(b_node, b_end)
    first = min(bks["type"][0], bks["required"][0], bks["properties"][0])
    last = max(bks["type"][1], bks["required"][1], bks["properties"][1])
    order = list(base.get("discriminator", {}).get("mapping", {}).values())
    refs = [r.rsplit("/", 1)[1] for r in order] or sorted(children)
    refs += [c for c in sorted(children) if c not in refs]
    edits.append((first, last, ["      oneOf:"] +
                  [f"        - $ref: '#/components/schemas/{c}'" for c in refs]))

    for start, end, new in sorted(edits, key=lambda e: e[0], reverse=True):
        lines[start:end] = new
    f.write_text("\n".join(lines), encoding="utf-8")
    print(f"правок применено: {len(edits)}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else "openapi.MaxBotApi.yaml")
