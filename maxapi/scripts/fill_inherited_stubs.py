#!/usr/bin/env python3
"""Постобработка сгенерированного OpenAPI: раскрытие пустых стабов наследников.

При `seal-object-schemas: true` эмиттер @typespec/openapi3 обязан перечислить
у схемы-наследника все унаследованные свойства — иначе `additionalProperties:
false` отсекал бы их как посторонние. Перечисляет он их пустыми схемами:

    BotAddedToChatUpdate:
      properties:
        ...
        timestamp: {}          # <- унаследовано от Update через allOf
      allOf:
        - $ref: '#/components/schemas/Update'

Семантически это корректно (тип и ограничения приходят из базовой схемы через
allOf), но требования КБ читают каждое свойство изолированно: у `{}` нет ни
`type`, ни `description`. Скрипт заменяет каждый такой стаб на полное
объявление свойства, найденное в базовой схеме по цепочке allOf. Ограничения
при этом дублируются, а не меняются: allOf-пересечение с идентичной копией
эквивалентно исходной схеме.

Форматирование остальной части файла не трогается — правка построчная.
"""
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = ROOT / "tsp-output" / "@typespec" / "openapi3"


def base_names(schema):
    """Имена базовых схем, подмешанных через allOf."""
    out = []
    for part in schema.get("allOf") or []:
        ref = part.get("$ref") if isinstance(part, dict) else None
        if ref and ref.startswith("#/components/schemas/"):
            out.append(ref.rsplit("/", 1)[1])
    return out


def resolve(schemas, name, prop, seen=None):
    """Полное объявление `prop`, унаследованное схемой `name` через allOf."""
    seen = seen or set()
    if name in seen:
        return None
    seen.add(name)
    for base in base_names(schemas.get(name) or {}):
        b = schemas.get(base) or {}
        decl = (b.get("properties") or {}).get(prop)
        if decl:                      # непустое объявление найдено
            return decl
        if prop in (b.get("properties") or {}):   # стаб — идём глубже
            found = resolve(schemas, base, prop, seen)
            if found:
                return found
        else:
            found = resolve(schemas, base, prop, seen)
            if found:
                return found
    return None


def find_stubs(node, schemas_line_scope):
    """(line, indent, prop, schema_name) для каждого `prop: {}` в components.schemas."""
    out = []
    for sname, snode in schemas_line_scope:
        props = None
        for k, v in snode.value:
            if k.value == "properties":
                props = v
        if props is None:
            continue
        for pk, pv in props.value:
            if isinstance(pv, yaml.MappingNode) and not pv.value:
                out.append((pk.start_mark.line, pk.start_mark.column, pk.value, sname))
    return out


def schema_nodes(root):
    for k, v in root.value:
        if k.value != "components":
            continue
        for ck, cv in v.value:
            if ck.value != "schemas":
                continue
            return [(sk.value, sv) for sk, sv in cv.value]
    return []


def render(prop, decl, indent):
    text = yaml.dump({prop: decl}, default_flow_style=False, allow_unicode=True,
                     sort_keys=False, width=10 ** 6)
    pad = " " * indent
    return [pad + line if line else line for line in text.rstrip("\n").split("\n")]


def process(path):
    text = path.read_text(encoding="utf-8")
    doc = yaml.safe_load(text)
    root = yaml.compose(text)
    schemas = (doc.get("components") or {}).get("schemas") or {}
    stubs = find_stubs(root, schema_nodes(root))

    lines = text.split("\n")
    unresolved = []
    # снизу вверх, чтобы номера строк выше точки правки не смещались
    for line_no, indent, prop, sname in sorted(stubs, reverse=True):
        decl = resolve(schemas, sname, prop)
        if not decl:
            unresolved.append(f"{sname}.{prop}")
            continue
        lines[line_no:line_no + 1] = render(prop, decl, indent)
    path.write_text("\n".join(lines), encoding="utf-8")
    return len(stubs) - len(unresolved), unresolved


def main():
    files = sorted(OUT_DIR.glob("*.yaml"))
    if not files:
        print(f"Нет *.yaml в {OUT_DIR.relative_to(ROOT)} — сначала выполните: npx tsp compile .")
        return 2
    total, bad = 0, []
    for f in files:
        n, unresolved = process(f)
        total += n
        bad += [f"{f.name}: {u}" for u in unresolved]
        print(f"{f.name}: {n} стабов раскрыто" + (f", {len(unresolved)} не разрешено" if unresolved else ""))
    for b in bad:
        print(f"  НЕ РАЗРЕШЕНО: {b}")
    print(f"Итого: {total}")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
