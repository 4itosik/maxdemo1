#!/usr/bin/env python3
"""Проверка, что артефакты в git соответствуют исходникам.

Единый шаг `make` перегенерирует всё разом, поэтому при обычной работе
артефакты расходиться не должны. Но «не должны» — это про дисциплину, а в git
лежат ОБА конца цепочки:

    *.tsp  →  openapi.MaxBotApi.yaml     ← читает валидатор на периметре
                        ↓
                  models.go              ← читает бот

Разойтись они могут молча: достаточно закоммитить YAML, не перегенерировав
структуры (или наоборот), или поправить YAML руками. Валидатор на периметре
получит один контракт, бот будет работать по другому, и ни компилятор, ни
тесты этого не заметят — оба файла по отдельности корректны.

Скрипт запускается ПОСЛЕ полной перегенерации (цель check-sync зависит от
all) и просто спрашивает git, изменилось ли что-нибудь. Если да — значит в
коммите лежали артефакты, не соответствующие исходникам.

Место этой проверки — CI и pre-push, не ежедневная работа.
"""
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# Артефакты, которые порождаются из *.tsp и лежат в git.
TRACKED_ARTIFACTS = (
    "openapi.MaxBotApi.yaml",
    "openapi.MaxBotWebhook.yaml",
    "gen/quicktype/models.go",
    "gen/oapi-codegen/models.go",
)


def git(*args):
    return subprocess.run(
        ["git", *args], cwd=ROOT, capture_output=True, text=True, check=True
    ).stdout


def main():
    try:
        git("rev-parse", "--git-dir")
    except subprocess.CalledProcessError:
        print("не git-репозиторий — проверять нечего")
        return

    paths = [p for p in TRACKED_ARTIFACTS if (ROOT / p).is_file()]
    missing = [p for p in TRACKED_ARTIFACTS if not (ROOT / p).is_file()]
    if missing:
        print(f"нет артефактов: {', '.join(missing)} — сначала выполните: make")
        sys.exit(2)

    # --porcelain разом покрывает и изменённые, и ещё не добавленные файлы:
    # `git diff` пропустил бы новый артефакт, которого нет в индексе.
    status = git("status", "--porcelain", "--", *paths).strip()
    if not status:
        print(f"артефакты соответствуют исходникам ({len(paths)} файла)")
        return

    # `??` — файла нет в git вовсе; остальное — есть, но отличается от
    # закоммиченного. Причины разные, и путать их не стоит.
    untracked = [l[3:] for l in status.splitlines() if l.startswith("??")]
    changed = [l for l in status.splitlines() if not l.startswith("??")]

    print("АРТЕФАКТЫ НЕ СООТВЕТСТВУЮТ ИСХОДНИКАМ.")
    print()
    if changed:
        print("Перегенерация изменила закоммиченные файлы:")
        for line in changed:
            print(f"  {line}")
        print()
        print("  Значит в коммите они не соответствуют *.tsp: валидатор на периметре")
        print("  и бот получат разные версии контракта.")
        print()
    if untracked:
        print("Не добавлены в git:")
        for path in untracked:
            print(f"  {path}")
        print()
        print("  Контракт читает валидатор на периметре, структуры — бот;")
        print("  оба берут файлы из репозитория, а там их нет.")
        print()
    print("Починка: выполните `make` и закоммитьте результат целиком.")
    sys.exit(1)


if __name__ == "__main__":
    main()
