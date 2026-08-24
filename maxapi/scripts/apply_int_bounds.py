#!/usr/bin/env python3
"""Постобработка сгенерированного OpenAPI: явный `maximum` у целочисленных полей.

Требование КБ — у числового типа должны стоять и `minimum`, и `maximum`.
Обычно границы задаются в *.tsp через `@maxValue`, но так выразимы не любые
значения: компилятор TypeSpec держит числовые литералы в JS-числах, поэтому
всё выше 2^53-1 теряет точность и эмиттер молча выбрасывает ограничение
(проверено на 2^63-1 и 2^63-2 — `maximum` не появляется в выводе).

Единственное поле схемы, которому нужен полный диапазон int64, — MessageBody.seq
(упакованное «время + счётчик», прод отдаёт ~1.17e17; см. комментарий у поля).

Скрипт дописывает границу самого типа тем целочисленным схемам, где автор
намеренно не задал `@maxValue`: int64 -> 2^63-1, int32 -> 2^31-1. Это не
расширение контракта, а перевод неявной границы типа в явную запись, которой
требует КБ. Поля с собственным `@maxValue` не трогаются.
"""
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = ROOT / "tsp-output" / "@typespec" / "openapi3"

# format -> максимум типа
BOUNDS = {"int64": 2**63 - 1, "int32": 2**31 - 1}


def find_targets(node, path, out):
    """(строка ключа `type`, отступ, максимум) для схем без `maximum`."""
    if isinstance(node, yaml.SequenceNode):
        for i, item in enumerate(node.value):
            find_targets(item, f"{path}[{i}]", out)
        return
    if not isinstance(node, yaml.MappingNode):
        return
    keys = {k.value: (k, v) for k, v in node.value}
    if keys.get("type", (None, None))[1] is not None:
        t = keys["type"][1]
        fmt = keys.get("format", (None, None))[1]
        if (getattr(t, "value", None) == "integer" and fmt is not None
                and fmt.value in BOUNDS and "maximum" not in keys):
            k = keys["type"][0]
            out.append((k.start_mark.line, k.start_mark.column, BOUNDS[fmt.value], path))
    for k, v in node.value:
        find_targets(v, f"{path}.{k.value}", out)


def main():
    files = sorted(OUT_DIR.glob("*.yaml"))
    if not files:
        print(f"Нет *.yaml в {OUT_DIR.relative_to(ROOT)} — сначала выполните: npx tsp compile .")
        return 2
    total = 0
    for f in files:
        text = f.read_text(encoding="utf-8")
        targets = []
        find_targets(yaml.compose(text), "", targets)
        if not targets:
            print(f"{f.name}: 0 полей без maximum")
            continue
        lines = text.split("\n")
        # снизу вверх, чтобы номера строк выше точки правки не смещались
        for line, indent, bound, path in sorted(targets, reverse=True):
            lines.insert(line + 1, " " * indent + f"maximum: {bound}")
            print(f"  {f.name}: {path.lstrip('.')} -> maximum: {bound}")
        f.write_text("\n".join(lines), encoding="utf-8")
        total += len(targets)
        print(f"{f.name}: {len(targets)} границ дописано")
    print(f"Итого: {total}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
