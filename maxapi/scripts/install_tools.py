#!/usr/bin/env python3
"""Установка кодогенераторов с закреплёнными версиями через `go install`.

Зачем отдельный этап, а не `go run пакет@версия` прямо в цели сборки:

  * `go run` при каждом вызове сверяется с модулем — это сеть в шаге, который
    обязан работать в закрытом контуре и в оффлайне;
  * версия генератора оказывается вписанной в команду, а команд две (Makefile
    и package.json), и они расходятся молча;
  * подменить версию для проверки нечем, кроме правки обеих команд.

Здесь версия закреплена ОДИН раз, ниже. Оба вызывающих — `make install-tools`
и `npm run install:tools` — идут сюда, так что разойтись им негде.

Закрепление версии важно не из педантизма: генератор обязан давать один и тот
же models.go на одном контракте. Иначе файл, лежащий в git, начнёт «плавать»
между машинами, и `git diff` перестанет означать «контракт изменился».

Бинарь ставится туда, куда его кладёт сам Go, — в $GOBIN, а если тот пуст, в
$GOPATH/bin, — и вызывается по имени. Своего каталога проект не заводит:
инструмент один на все проекты, а лишний ./bin пришлось бы держать в
.gitignore и чистить.

Скрипт идемпотентен: если бинарь уже нужной версии на месте, он ничего не
делает. Проверяется именно версия, а не наличие файла, — иначе обновление
закреплённой версии не доехало бы до машин, где бинарь уже стоит.
"""
import os
import shutil
import subprocess
import sys
from pathlib import Path

# --- закреплённые версии -------------------------------------------------
# quicktype здесь нет намеренно: он ставится через npm, и его версию держит
# package-lock.json.
TOOLS = (
    {
        "name": "oapi-codegen",
        "package": "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen",
        "version": "v2.8.0",
    },
)


def go_env(name):
    result = subprocess.run(["go", "env", name], capture_output=True, text=True, check=True)
    return result.stdout.strip()


def install_dir():
    """Каталог, куда `go install` кладёт бинарники — по правилам самого Go."""
    gobin = go_env("GOBIN")
    if gobin:
        return Path(gobin)
    return Path(go_env("GOPATH")) / "bin"


def installed_version(binary):
    """Версия установленного бинаря или None, если его нет.

    `oapi-codegen --version` печатает две строки: путь пакета и версию,
    — берём последнюю.
    """
    if not binary.is_file():
        return None
    try:
        result = subprocess.run(
            [str(binary), "--version"], capture_output=True, text=True, timeout=30
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if result.returncode != 0:
        return None
    lines = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    return lines[-1] if lines else None


def main():
    if shutil.which("go") is None:
        print("go не найден в PATH — установите Go и повторите")
        sys.exit(2)

    target_dir = install_dir()
    problems = []

    for tool in TOOLS:
        binary = target_dir / tool["name"]
        current = installed_version(binary)

        if current == tool["version"]:
            print(f"{tool['name']}: {current} — уже установлен ({binary})")
        else:
            if current is not None:
                print(
                    f"{tool['name']}: установлен {current}, закреплён {tool['version']}"
                    " — переустанавливаю"
                )
            print(f"{tool['name']}: устанавливаю {tool['version']}")
            try:
                subprocess.run(
                    ["go", "install", f"{tool['package']}@{tool['version']}"],
                    check=True,
                    env=os.environ,
                )
            except subprocess.CalledProcessError as err:
                print(f"{tool['name']}: установка не удалась (код {err.returncode})")
                sys.exit(1)

            confirmed = installed_version(binary)
            if confirmed != tool["version"]:
                print(
                    f"{tool['name']}: после установки версия {confirmed},"
                    f" ожидалась {tool['version']}"
                )
                sys.exit(1)
            print(f"{tool['name']}: {confirmed} — установлен ({binary})")

        # Сборка вызывает бинарь по имени, поэтому каталог обязан быть в PATH.
        # Без этой проверки `make gen-oapi-codegen` падал бы невнятным
        # «command not found» уже после успешной установки.
        found = shutil.which(tool["name"])
        if found is None:
            problems.append(
                f"{tool['name']} установлен в {target_dir}, но этого каталога нет в PATH.\n"
                f'  Добавьте его: export PATH="{target_dir}:$PATH"'
            )
        elif Path(found).resolve() != binary.resolve():
            problems.append(
                f"по имени {tool['name']} в PATH находится {found},\n"
                f"  а не только что установленный {binary} — уберите лишний или поправьте PATH"
            )

    if problems:
        print()
        for problem in problems:
            print(f"ВНИМАНИЕ: {problem}")
        sys.exit(1)


if __name__ == "__main__":
    main()
