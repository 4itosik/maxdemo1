#!/usr/bin/env python3
"""Проверка сгенерированного OpenAPI на требования КБ-валидатора.

Три правила (по отчёту валидатора, см. review.md):
  1. У каждой схемы/свойства должен быть `description`.
  2. У каждой схемы/свойства должен быть явно указан `type`.
  3. У каждой схемы с `type: string` должен быть `pattern`.

Схемы, состоящие только из `$ref` (или `allOf` из одного `$ref`),
проверке не подлежат: тип и описание берутся из целевой схемы.

Печатает нарушения с номерами строк исходного YAML и код возврата 1,
если найдено хотя бы одно.
"""
import sys
from collections import Counter
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = ROOT / "tsp-output" / "@typespec" / "openapi3"

# Ключи, по которым значение является схемой (или контейнером схем).
SCHEMA_CHILD = ("items", "additionalProperties", "not")
SCHEMA_LIST = ("allOf", "anyOf", "oneOf")


def node_get(node, key):
    for k, v in node.value:
        if k.value == key:
            return v
    return None


def keys(node):
    return {k.value for k, v in node.value}


def is_mapping(node):
    return isinstance(node, yaml.MappingNode)


def is_ref_only(node):
    """Схема-ссылка: сам $ref либо обёртка allOf вокруг единственного $ref."""
    ks = keys(node)
    if "$ref" in ks:
        return True
    if "allOf" in ks:
        allof = node_get(node, "allOf")
        if isinstance(allof, yaml.SequenceNode) and len(allof.value) == 1:
            # {allOf: [$ref], description: ...} — тип наследуется от цели
            return True
    return False


def walk(node, path, findings, in_schema=False, inline=False):
    if isinstance(node, yaml.SequenceNode):
        for i, item in enumerate(node.value):
            walk(item, f"{path}[{i}]", findings, in_schema=in_schema, inline=inline)
        return
    if not is_mapping(node):
        return

    if in_schema:
        check_schema(node, path, findings, inline=inline)

    ks = keys(node)
    for k, v in node.value:
        name = k.value
        child_path = f"{path}.{name}" if path else name
        if in_schema:
            if name == "properties" and is_mapping(v):
                for pk, pv in v.value:
                    walk(pv, f"{path}.{pk.value}", findings, in_schema=True)
                continue
            if name in SCHEMA_CHILD or name in SCHEMA_LIST:
                walk(v, child_path, findings, in_schema=True, inline=inline)
                continue
            # прочие ключи схемы (description, enum, ...) — не схемы
            continue
        # вне схемы: ищем точки входа
        if name == "schema":
            # Инлайн-схема параметра / тела запроса: `description` живёт на
            # родительском объекте (parameter / requestBody / response),
            # валидатор её на самой схеме не требует.
            walk(v, child_path, findings, in_schema=True, inline=True)
        elif name == "schemas" and path == "components":
            for sk, sv in v.value:
                walk(sv, f"components.schemas.{sk.value}", findings, in_schema=True)
        else:
            walk(v, child_path, findings, in_schema=False)
    _ = ks


def check_schema(node, path, findings, inline=False):
    ks = keys(node)
    line = node.start_mark.line + 1
    if is_ref_only(node):
        return
    if "description" not in ks and not inline:
        findings.append((line, path, "нет description"))
    if "type" not in ks and not (ks & {"allOf", "anyOf", "oneOf", "enum"}):
        findings.append((line, path, "нет type"))
    tnode = node_get(node, "type")
    if tnode is not None and tnode.value == "string" and "pattern" not in ks and "enum" not in ks:
        findings.append((line, path, "нет pattern у string"))


def check_file(path):
    with open(path, encoding="utf-8") as f:
        root = yaml.compose(f)
    findings = []
    walk(root, "", findings, in_schema=False)
    findings.sort(key=lambda x: (x[0], x[1]))
    return findings


def main():
    files = sorted(OUT_DIR.glob("*.yaml"))
    if not files:
        print(f"Нет *.yaml в {OUT_DIR.relative_to(ROOT)} — сначала выполните: npx tsp compile .")
        return 2
    total = 0
    verbose = "-q" not in sys.argv
    for f in files:
        findings = check_file(f)
        total += len(findings)
        by_kind = Counter(kind for _, _, kind in findings)
        print(f"=== {f.name}: {len(findings)} нарушений ===")
        for kind, n in sorted(by_kind.items()):
            print(f"  {kind}: {n}")
        if verbose:
            for line, p, kind in findings:
                print(f"  {f.name}:{line}  {p}  — {kind}")
    print(f"Итого: {total} нарушений требований КБ")
    return 1 if total else 0


if __name__ == "__main__":
    sys.exit(main())
