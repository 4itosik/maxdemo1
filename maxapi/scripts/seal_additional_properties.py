#!/usr/bin/env python3
"""Постобработка сгенерированного OpenAPI: additionalProperties: {not: {}} → false.

@typespec/openapi3 (seal-object-schemas: true) запечатывает object-схемы
семантически эквивалентным `additionalProperties: {not: {}}` вместо
литерального `additionalProperties: false` — это жёстко зашито в эмиттере
и не настраивается через tspconfig.yaml (see @typespec/openapi3/dist/src/
schema-emitter.js, applyModelIndexer). Требование КБ — явный `false`, поэтому
переписываем результат после `tsp compile`.

Схемы с настоящим индексатором (Record<T>, additionalProperties: {$ref: ...})
не трогаем — паттерн привязан к точному телу `not: {}`.
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = ROOT / "tsp-output" / "@typespec" / "openapi3"

PATTERN = re.compile(r"^( *)additionalProperties:\n\1  not: \{\}$", re.MULTILINE)


def main():
    files = sorted(OUT_DIR.glob("*.yaml"))
    if not files:
        print(f"Нет *.yaml в {OUT_DIR.relative_to(ROOT)} — сначала выполните: npx tsp compile .")
        sys.exit(2)
    total = 0
    for f in files:
        text = f.read_text(encoding="utf-8")
        patched, n = PATTERN.subn(r"\1additionalProperties: false", text)
        if n:
            f.write_text(patched, encoding="utf-8")
        total += n
        print(f"{f.name}: {n} схем запечатано (additionalProperties: false)")
    print(f"Итого: {total}")


if __name__ == "__main__":
    main()
