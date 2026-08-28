#!/usr/bin/env python3
"""Сравнение двух вариантов кодогенерации Go-структур из одного контракта.

В репозитории временно живут оба:

  gen/quicktype/   — quicktype, через промежуточную JSON Schema
              (openapi.MaxBotApi.yaml → scripts/openapi_to_jsonschema.py
               → gen/max.schema.json → quicktype → scripts/finalize_go_models.py)
  gen/oapi-codegen/ — oapi-codegen, напрямую из openapi.MaxBotApi.yaml

Скрипт печатает то, по чему их стоит выбирать: покрытие схем контракта,
выдуманные имена, зависимости и объём рукописного кода, который каждый вариант
требует держать между контрактом и Go. Решение — за человеком; здесь только
цифры, посчитанные по факту, а не по памяти.
"""
import ast
import io
import json
import re
import subprocess
import sys
import tokenize
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
API_DOC = ROOT / "openapi.MaxBotApi.yaml"

# Имена, которые генератор обязан выдумать сам и которые поэтому не считаются
# искажением: у inline-энумов (поле-дискриминатор `type`/`update_type`) своего
# имени в контракте нет, а обвязка HTTP-клиента к схемам вообще не относится.
INVENTED_OK = re.compile(
    r"(Type|Params|JSONBody|JSONRequestBody|Response|RequestEditorFn)$"
    r"|^(Client|ClientInterface|ClientOption|ClientWithResponses"
    r"|ClientWithResponsesInterface|HttpRequestDoer)$"
)

VARIANTS = (
    (
        "quicktype",
        ROOT / "gen" / "quicktype",
        (ROOT / "scripts" / "openapi_to_jsonschema.py", ROOT / "scripts" / "finalize_go_models.py"),
    ),
    ("oapi-codegen", ROOT / "gen" / "oapi-codegen", ()),
)


def real_code_lines(path):
    """Строки кода без комментариев, докстрингов и пустых.

    Считается токенизатором, а не grep-ом: grep записывает докстринги в код и
    завышает результат почти вдвое.
    """
    src = path.read_text(encoding="utf-8")
    doc_lines = set()
    for node in ast.walk(ast.parse(src)):
        if isinstance(node, (ast.Module, ast.FunctionDef, ast.ClassDef)):
            if ast.get_docstring(node, clean=False) is not None:
                body = node.body[0]
                doc_lines.update(range(body.lineno, body.end_lineno + 1))
    comment_lines = {
        tok.start[0]
        for tok in tokenize.generate_tokens(io.StringIO(src).readline)
        if tok.type == tokenize.COMMENT
    }
    return sum(
        1
        for i, line in enumerate(src.splitlines(), 1)
        if line.strip() and i not in doc_lines and i not in comment_lines
    )


def go_deps(module_dir):
    """Зависимости модуля.

    Через `go mod edit -json`, а не регуляркой по go.mod: в go.mod один и тот
    же require встречается и одной строкой, и внутри блока, и с хвостом
    `// indirect` — регулярка на этом ошибается и показывает ноль там, где
    зависимости есть.
    """
    if not (module_dir / "go.mod").is_file():
        return []
    result = subprocess.run(
        ["go", "mod", "edit", "-json"], cwd=module_dir, capture_output=True, text=True, check=True
    )
    require = json.loads(result.stdout).get("Require") or []
    return sorted(
        r["Path"] + ("" if not r.get("Indirect") else " (косвенная)") for r in require
    )


def go_test(module_dir):
    result = subprocess.run(
        ["go", "test", "-count=1", "./..."], cwd=module_dir, capture_output=True, text=True
    )
    return "OK" if result.returncode == 0 else "ПАДАЮТ"


def main():
    if not API_DOC.is_file():
        print(f"Нет {API_DOC.name} — сначала выполните: make build")
        sys.exit(2)
    schemas = set(yaml.safe_load(API_DOC.read_text(encoding="utf-8"))["components"]["schemas"])
    # Две схемы, которых нет в api-документе, но которые есть в контракте:
    # вариант quicktype их подмешивает, вариант oapi-codegen — нет. Считаем их
    # частью ожидаемого покрытия, иначе UpdateUnified у quicktype выглядит
    # выдуманным именем, хотя это настоящая схема webhook-документа.
    schemas |= {"WebhookUpdate", "UpdateUnified"}

    rows = []
    for name, module_dir, handwritten in VARIANTS:
        models = module_dir / "models.go"
        if not models.is_file():
            print(f"Нет {models.relative_to(ROOT)} — сначала выполните: make gen")
            sys.exit(2)
        src = models.read_text(encoding="utf-8")
        types = set(re.findall(r"^type (\w+) ", src, re.MULTILINE))
        invented = sorted(t for t in types - schemas if not INVENTED_OK.search(t))
        rows.append(
            {
                "вариант": name,
                "строк": len(src.splitlines()),
                "типов": len(types),
                "схем без своего типа": sorted(schemas - types),
                "выдуманные имена": invented,
                "зависимости": go_deps(module_dir),
                "рукописных строк между контрактом и Go": sum(real_code_lines(p) for p in handwritten),
                "тесты": go_test(module_dir),
            }
        )

    width = max(len(k) for k in rows[0])
    print(f"Схем в контракте: {len(schemas)} ({API_DOC.name} + WebhookUpdate/UpdateUnified)\n")
    for key in rows[0]:
        if key == "вариант":
            continue
        print(f"{key:<{width}}")
        for row in rows:
            value = row[key]
            if isinstance(value, list):
                value = f"{len(value)}" + (f" — {', '.join(value)}" if value else "")
            print(f"    {row['вариант']:<14} {value}")
        print()

    compare_examples()

    print(json.dumps(rows, ensure_ascii=False, indent=2), file=open(ROOT / "gen" / "compare.json", "w"))
    print(f"Машиночитаемая копия: {(ROOT / 'gen' / 'compare.json').relative_to(ROOT)}")


# Пары файлов, решающих одну задачу на двух вариантах кодогенерации.
EXAMPLE_PAIRS = (
    ("клиент", "example-quicktype/maxclient/client.go", "example-oapi-codegen/maxclient/client.go"),
    ("webhook-сервер", "example-quicktype/webhook/server.go", "example-oapi-codegen/webhook/server.go"),
    ("echobot", "example-quicktype/cmd/echobot/main.go", "example-oapi-codegen/cmd/echobot/main.go"),
)


def go_code_lines(path):
    """Строки Go-кода без комментариев и пустых."""
    return sum(
        1
        for line in path.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.strip().startswith("//")
    )


def compare_examples():
    """Сколько рукописного кода требует каждый вариант на одной задаче.

    Метрики по моделям (числа типов, строк) не отвечают на вопрос, ради
    которого выбирают генератор: сколько кода придётся написать самому.
    Отвечает только пара примеров, решающих одно и то же.
    """
    pairs = [
        (name, ROOT / a, ROOT / b)
        for name, a, b in EXAMPLE_PAIRS
    ]
    if not all(a.is_file() and b.is_file() for _, a, b in pairs):
        print("Примеры не найдены — пропускаю сравнение рукописного кода\n")
        return

    print("Рукописных строк на одной задаче (два примера, одинаковые тесты)")
    print(f"    {'часть':<18}{'quicktype':>12}{'oapi-codegen':>15}{'разница':>10}")
    total_a = total_b = 0
    for name, a, b in pairs:
        ca, cb = go_code_lines(a), go_code_lines(b)
        total_a += ca
        total_b += cb
        print(f"    {name:<18}{ca:>12}{cb:>15}{cb - ca:>+10}")
    print(f"    {'ИТОГО':<18}{total_a:>12}{total_b:>15}{total_b - total_a:>+10}")
    print()


if __name__ == "__main__":
    main()
