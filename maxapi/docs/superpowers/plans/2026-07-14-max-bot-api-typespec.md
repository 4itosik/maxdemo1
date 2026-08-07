# Max Bot API TypeSpec Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Рукописное TypeSpec-описание всего Max Bot API (28 операций, ~128 моделей, все 114 ограничений), компилирующееся в OpenAPI 3, семантически эквивалентный официальной схеме.

**Architecture:** Один сервисный неймспейс `MaxBotApi`, разбитый по файлам: `common.tsp` (ошибки, обёртки ответов), `models/*.tsp` (модели по доменам, дискриминированные union через `@discriminator` + `extends`), `routes/*.tsp` (интерфейсы по тегам). Скрипт `scripts/compare.py` — «тест»: семантическая сверка нашего OpenAPI с официальным; пишется ПЕРВЫМ и падает, пока спека не полна.

**Tech Stack:** TypeSpec (@typespec/compiler, @typespec/http, @typespec/openapi, @typespec/openapi3), Python 3 (stdlib) для сверки.

## Global Constraints

- Источник истины: `reference/max-openapi-official.json` (снимок официальной схемы от 2026-07-10, API v0.0.32, из репозитория `max-messenger-bot/max-bot-api-schemas`). НЕ редактировать.
- Имена моделей TypeSpec = имена схем оригинала, точно (кроме допущений ниже).
- Имена операций = официальные `operationId`; на каждой операции обязателен `@operationId("...")` (иначе эмиттер выдаст `Interface_name`).
- Все `@doc` — русские докстроки `/** ... */`; формулировки переносим из `description` оригинала (можно сокращать, описания сверкой не проверяются).
- `namespace MaxBotApi;` в каждом .tsp-файле (вложенные неймспейсы запрещены — испортят имена схем).
- Каждый task заканчивается: `npx tsp compile .` без ошибок → `python3 scripts/compare.py` (число расхождений уменьшилось) → `git commit`.

**Таблица переноса ограничений OpenAPI → TypeSpec** (обязательна к применению при транскрипции КАЖДОЙ модели):

| В оригинале | В TypeSpec |
|---|---|
| `maxLength`/`minLength` (строка) | `@maxLength(N)` / `@minLength(N)` |
| `maximum`/`minimum` (число) | `@maxValue(N)` / `@minValue(N)` |
| `maxItems`/`minItems` (массив) | `@maxItems(N)` / `@minItems(N)` |
| `pattern` | `@pattern("...")` (regex дословно) |
| `format: int64` / `int32` / `double` | тип `int64` / `int32` / `float64` |
| `type: string` без формата | `string` |
| `enum: [...]` (схема-enum) | `enum Имя { значение, ... }` |
| `nullable: true` | `| null` в типе свойства |
| не в `required` | `?` у свойства |
| `default: X` | `= X` после типа (`notify?: boolean = true`) |
| `additionalProperties: {schema}` | `Record<Тип>` |
| `allOf: [$ref Base, {...}]` + discriminator | `model Child extends Base { disc: "литерал"; ... }` |
| `discriminator.propertyName` на базе | `@discriminator("имя_поля")` на базовой модели |
| `readOnly: true` (1 шт., `User.name`) | НЕ переносим (см. известные отличия) |
| `uniqueItems: true` (7 шт.) | НЕ переносим — в TypeSpec нет аналога (см. известные отличия) |

**Известные осознанные отличия** (зашиты в allowlist `DEVIATIONS` compare.py, документируются в README в Task 10):

1. `securitySchemes`: у нас `ApiKeyAuth`/`BearerAuth` вместо официального единственного `access_token` (query, устаревший) — мы дополнительно описываем актуальную Bearer-авторизацию.
2. `schemas.UserIdsList` — в оригинале свойство `user_ids` имеет параметро-подобную структуру (вложенный `schema`, `style`, строковый `maxItems: "100"` на items). У нас: обычный массив `int64[]` с `@maxItems(100)`.
3. `uniqueItems` и `readOnly` не переносятся.
4. Схема `bigint` оригинала не воспроизводится (служебный алиас int64; ссылки на неё разворачиваются при сверке).
5. `MessageChatCreatedUpdate` у нас попадает в `discriminator.mapping` схемы `Update` (в оригинале схема есть, а в mapping не включена — недосмотр оригинала).

**Артефакты оригинала, которые compare.py нормализует сам** (в TypeSpec пишем «как правильно», диффа не будет): `pattern` на integer-параметрах — игнорируется; `minLength` на массивах кнопок — трактуется как `minItems`; строковые числа (`maxItems: "100"`, `default: "20"`) — приводятся к int; `format: "string"` на массивах — игнорируется; `enum`-схемы без `type: string` — type игнорируется при наличии enum.

**Как читать оригинал.** Дамп любой схемы:
```bash
python3 -c "import json;print(json.dumps(json.load(open('reference/max-openapi-official.json'))['components']['schemas']['ИМЯ'],ensure_ascii=False,indent=1))"
```
Дамп операции:
```bash
python3 -c "import json;print(json.dumps(json.load(open('reference/max-openapi-official.json'))['paths']['/me']['get'],ensure_ascii=False,indent=1))"
```

---

### Task 1: Каркас проекта и референс-схема

**Files:**
- Create: `package.json`, `tspconfig.yaml`, `main.tsp`, `.gitignore`, `reference/max-openapi-official.json`

**Interfaces:**
- Produces: компилирующийся пустой сервис `MaxBotApi` (title "Max Bot API", version 0.0.32, сервер, две auth-схемы); референс-файл для всех последующих задач.

- [ ] **Step 1: Создать файлы каркаса**

`.gitignore`:
```
node_modules/
tsp-output/
```

`package.json`:
```json
{
  "name": "max-bot-api-typespec",
  "version": "0.0.32",
  "private": true,
  "description": "TypeSpec-описание Max Bot API (https://dev.max.ru/docs-api)"
}
```

`tspconfig.yaml`:
```yaml
emit:
  - "@typespec/openapi3"
options:
  "@typespec/openapi3":
    file-type: json
```

`main.tsp`:
```typespec
import "@typespec/http";
import "@typespec/openapi";

using Http;
using OpenAPI;

/** Бот-платформа мессенджера MAX. https://dev.max.ru/docs-api */
@service(#{ title: "Max Bot API" })
@info(#{ version: "0.0.32" })
@server("https://platform-api2.max.ru")
@useAuth(ApiKeyAuth<ApiKeyLocation.query, "access_token"> | BearerAuth)
namespace MaxBotApi;
```

- [ ] **Step 2: Установить зависимости**

Run: `npm install --save-dev @typespec/compiler @typespec/http @typespec/openapi @typespec/openapi3`
Expected: без ошибок, появился `node_modules/`.

- [ ] **Step 3: Скачать референс-схему**

Run:
```bash
mkdir -p reference
curl -sf "https://raw.githubusercontent.com/max-messenger-bot/max-bot-api-schemas/main/schema_2026_07_10.json" -o reference/max-openapi-official.json
python3 -c "import json;s=json.load(open('reference/max-openapi-official.json'));print(s['info']['version'], len(s['paths']), len(s['components']['schemas']))"
```
Expected: `0.0.32 16 128`.

- [ ] **Step 4: Скомпилировать**

Run: `npx tsp compile .`
Expected: успех; `tsp-output/@typespec/openapi3/openapi.json` существует, внутри `"title": "Max Bot API"`.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: каркас TypeSpec-проекта Max Bot API + референс-схема"
```

---

### Task 2: Скрипт семантической сверки (наш «падающий тест»)

**Files:**
- Create: `scripts/compare.py`

**Interfaces:**
- Consumes: `reference/max-openapi-official.json`, `tsp-output/@typespec/openapi3/openapi.json` (Task 1).
- Produces: `python3 scripts/compare.py` — печатает расхождения по разделам (paths, schemas), exit 0 только при отсутствии неизвестных расхождений. Все последующие задачи меряют прогресс этим скриптом.

- [ ] **Step 1: Написать scripts/compare.py**

```python
#!/usr/bin/env python3
"""Семантическая сверка сгенерированного OpenAPI с официальной схемой Max Bot API.

Сравнивает по сути (пути, методы, operationId, параметры, тела, ответы,
свойства, типы, ограничения), разворачивая $ref и allOf. Имена схем
сравниваются по покрытию components.schemas (имена у нас совпадают с
оригиналом по договорённости). Описания/summary не сравниваются.
"""
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OFFICIAL = ROOT / "reference" / "max-openapi-official.json"
OURS = ROOT / "tsp-output" / "@typespec" / "openapi3" / "openapi.json"

# Осознанные отличия (подстрока строки диффа) — см. README «Отличия от официальной схемы»
DEVIATIONS = [
    "schemas.UserIdsList",     # параметро-подобная структура user_ids в оригинале
    "message_chat_created",    # отсутствует в mapping оригинала (недосмотр)
]
# Схемы оригинала, которые мы намеренно не воспроизводим
NAME_ALLOW_MISSING = {"bigint"}

CONSTRAINTS = [
    "type", "format", "enum", "required", "nullable", "default",
    "maxLength", "minLength", "maximum", "minimum",
    "maxItems", "minItems", "pattern",
]

diffs = []


def report(path, msg):
    diffs.append(f"{path}: {msg}")


def load(p):
    with open(p) as f:
        return json.load(f)


def deref(s, root):
    while isinstance(s, dict) and "$ref" in s:
        node = root
        for part in s["$ref"].lstrip("#/").split("/"):
            node = node[part]
        s = node
    return s


def flatten(s, root):
    """deref + слияние allOf в один уровень + чистка параметро-подобных обёрток."""
    s = deref(s, root)
    if not isinstance(s, dict):
        return {}
    if "schema" in s and not ({"type", "properties", "allOf", "enum", "items"} & set(s)):
        s = deref(s["schema"], root)  # артефакт оригинала (UserIdsList)
    if "allOf" not in s:
        return s
    merged, props, req = {}, {}, set()
    parts = [flatten(p, root) for p in s["allOf"]]
    parts.append({k: v for k, v in s.items() if k != "allOf"})
    for p in parts:
        props.update(p.get("properties", {}))
        req |= set(p.get("required", []))
        for k, v in p.items():
            if k not in ("properties", "required"):
                merged[k] = v
    if props:
        merged["properties"] = props
    if req:
        merged["required"] = sorted(req)
    return merged


def norm_int(v):
    if isinstance(v, str) and v.lstrip("-").isdigit():
        return int(v)
    return v


def constraints_of(s):
    c = {}
    for k in CONSTRAINTS:
        if k in s:
            v = norm_int(s[k])
            c[k] = sorted(v) if isinstance(v, list) else v
    # нормализация артефактов оригинала:
    if c.get("type") == "integer":
        c.pop("pattern", None)          # pattern на числах не имеет смысла
    if c.get("type") == "array":
        if "minLength" in c:
            c["minItems"] = c.pop("minLength")   # кнопки клавиатур
        if c.get("format") == "string":
            c.pop("format")             # GET /messages message_ids
    if "enum" in c:
        c.pop("type", None)             # enum-схемы оригинала без type: string
    if c.get("nullable") is False:
        c.pop("nullable")
    if "required" in c:
        c["required"] = sorted(set(c["required"]))
    return c


def cmp_schema(a, b, ra, rb, path, seen):
    na = a.get("$ref") if isinstance(a, dict) else None
    nb = b.get("$ref") if isinstance(b, dict) else None
    if na or nb:
        key = (na, nb)
        if key in seen:
            return
        seen.add(key)
    a, b = flatten(a, ra), flatten(b, rb)
    ca, cb = constraints_of(a), constraints_of(b)
    for k in sorted(set(ca) | set(cb)):
        if ca.get(k) != cb.get(k):
            report(path, f"{k}: официально {ca.get(k)!r}, у нас {cb.get(k)!r}")
    pa, pb = a.get("properties", {}), b.get("properties", {})
    for k in sorted(set(pa) | set(pb)):
        if k not in pb:
            report(f"{path}.{k}", "свойства нет у нас")
        elif k not in pa:
            report(f"{path}.{k}", "лишнее свойство у нас")
        else:
            cmp_schema(pa[k], pb[k], ra, rb, f"{path}.{k}", seen)
    if ("items" in a) != ("items" in b):
        report(path, "items только с одной стороны")
    elif "items" in a:
        cmp_schema(a["items"], b["items"], ra, rb, f"{path}[]", seen)
    ap, bp = a.get("additionalProperties"), b.get("additionalProperties")
    if isinstance(ap, dict) or isinstance(bp, dict):
        if isinstance(ap, dict) and isinstance(bp, dict):
            cmp_schema(ap, bp, ra, rb, f"{path}{{}}", seen)
        else:
            report(path, f"additionalProperties: официально {ap!r}, у нас {bp!r}")
    da = a.get("discriminator", {})
    db = b.get("discriminator", {})
    if da.get("propertyName") != db.get("propertyName"):
        report(path, f"discriminator: {da.get('propertyName')} vs {db.get('propertyName')}")
    ma, mb = da.get("mapping", {}), db.get("mapping", {})
    for k in sorted(set(ma) ^ set(mb)):
        report(path, f"discriminator.mapping[{k}] только с одной стороны")
    for k in sorted(set(ma) & set(mb)):
        cmp_schema({"$ref": ma[k]}, {"$ref": mb[k]}, ra, rb, f"{path}<{k}>", seen)


def cmp_content(a, b, ra, rb, path, seen):
    sa = (a or {}).get("content", {}).get("application/json", {}).get("schema")
    sb = (b or {}).get("content", {}).get("application/json", {}).get("schema")
    if (sa is None) != (sb is None):
        report(path, f"тело только с одной стороны (официально: {sa is not None})")
    elif sa is not None:
        cmp_schema(sa, sb, ra, rb, path, seen)


def main():
    off, ours = load(OFFICIAL), load(OURS)
    seen = set()

    # 1. Пути и операции
    po, pu = off.get("paths", {}), ours.get("paths", {})
    for p in sorted(set(po) | set(pu)):
        if p not in pu:
            report(f"paths.{p}", "путь отсутствует у нас")
            continue
        if p not in po:
            report(f"paths.{p}", "лишний путь у нас")
            continue
        mo = {m: v for m, v in po[p].items() if m in ("get", "post", "put", "patch", "delete")}
        mu = {m: v for m, v in pu[p].items() if m in ("get", "post", "put", "patch", "delete")}
        for m in sorted(set(mo) | set(mu)):
            path = f"paths.{p}.{m}"
            if m not in mu:
                report(path, "операция отсутствует у нас")
                continue
            if m not in mo:
                report(path, "лишняя операция у нас")
                continue
            a, b = mo[m], mu[m]
            if a.get("operationId") != b.get("operationId"):
                report(path, f"operationId: {a.get('operationId')} vs {b.get('operationId')}")
            if sorted(a.get("tags", [])) != sorted(b.get("tags", [])):
                report(path, f"tags: {a.get('tags')} vs {b.get('tags')}")
            qa = {(x["name"], x["in"]): x for x in (deref(x, off) for x in a.get("parameters", []))}
            qb = {(x["name"], x["in"]): x for x in (deref(x, ours) for x in b.get("parameters", []))}
            for key in sorted(set(qa) | set(qb)):
                ppath = f"{path}.param({key[1]}:{key[0]})"
                if key not in qb:
                    report(ppath, "параметра нет у нас")
                elif key not in qa:
                    report(ppath, "лишний параметр у нас")
                else:
                    if bool(qa[key].get("required")) != bool(qb[key].get("required")):
                        report(ppath, "required не совпадает")
                    cmp_schema(qa[key].get("schema", {}), qb[key].get("schema", {}),
                               off, ours, ppath, seen)
            ba, bb = a.get("requestBody"), b.get("requestBody")
            if (ba is None) != (bb is None):
                report(f"{path}.requestBody", f"тело только с одной стороны (официально: {ba is not None})")
            elif ba is not None:
                cmp_content(deref(ba, off), deref(bb, ours), off, ours, f"{path}.requestBody", seen)
            for code in sorted(set(a.get("responses", {})) | set(b.get("responses", {}))):
                rpath = f"{path}.responses[{code}]"
                if code not in b.get("responses", {}):
                    report(rpath, "ответа нет у нас")
                elif code not in a.get("responses", {}):
                    report(rpath, "лишний ответ у нас")
                else:
                    cmp_content(deref(a["responses"][code], off), deref(b["responses"][code], ours),
                                off, ours, rpath, seen)

    # 2. Покрытие components.schemas по именам + структурная сверка одноимённых
    so = set(off["components"]["schemas"]) - NAME_ALLOW_MISSING
    su = set(ours.get("components", {}).get("schemas", {}))
    for n in sorted(so - su):
        report(f"schemas.{n}", "схема отсутствует у нас")
    for n in sorted(so & su):
        cmp_schema({"$ref": f"#/components/schemas/{n}"}, {"$ref": f"#/components/schemas/{n}"},
                   off, ours, f"schemas.{n}", seen)

    known = [d for d in diffs if any(t in d for t in DEVIATIONS)]
    real = [d for d in diffs if d not in known]
    for d in real:
        print(f"DIFF  {d}")
    for d in known:
        print(f"KNOWN {d}")
    print(f"\nИтого: {len(real)} расхождений, {len(known)} известных отличий")
    sys.exit(1 if real else 0)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Запустить — убедиться, что «тест» падает осмысленно**

Run: `python3 scripts/compare.py | tail -5`
Expected: exit 1; в конце `Итого: N расхождений` где N > 150 (16 отсутствующих путей + ~127 отсутствующих схем). Никаких Python-трейсбеков.

- [ ] **Step 3: Commit**

```bash
git add scripts/compare.py && git commit -m "feat: скрипт семантической сверки с официальной схемой"
```

---

### Task 3: common.tsp и models/users.tsp

**Files:**
- Create: `common.tsp`, `models/users.tsp`
- Modify: `main.tsp` (добавить импорты)

**Interfaces:**
- Produces: модели `Error`, `SimpleQueryResult`, `Image`, обёртки `Unauthorized`(401), `Forbidden`(403), `NotFound`(404), `NotAllowed`(405), `InternalError`(500) — используются во ВСЕХ routes-задачах; `User`, `UserWithPhoto`, `BotInfo`, `BotCommand`, `BotPatch`.

- [ ] **Step 1: Написать common.tsp**

```typespec
import "@typespec/http";

using Http;

namespace MaxBotApi;

/** Сервер возвращает это, если возникло исключение при вашем запросе */
@error
model Error {
  /** Ошибка */
  error?: string;

  /** Код ошибки */
  code: string;

  /** Читаемое описание ошибки */
  message: string;
}

/** Простой ответ на запрос */
model SimpleQueryResult {
  /** `true`, если запрос был успешным, `false` — в противном случае */
  success: boolean;

  /** Объяснительное сообщение, если результат не был успешным */
  message?: string;
}

/** Общая схема, описывающая объект изображения */
model Image {
  /** URL изображения */
  url: string;
}

/** Ошибка авторизации: не предоставлен или недействителен access_token */
model Unauthorized {
  @statusCode statusCode: 401;
  @body body: Error;
}

/** Ошибка доступа: нет прав на этот ресурс */
model Forbidden {
  @statusCode statusCode: 403;
  @body body: Error;
}

/** Запрашиваемый ресурс не найден */
model NotFound {
  @statusCode statusCode: 404;
  @body body: Error;
}

/** Метод не разрешён */
model NotAllowed {
  @statusCode statusCode: 405;
  @body body: Error;
}

/** Внутренняя ошибка сервера */
model InternalError {
  @statusCode statusCode: 500;
  @body body: Error;
}
```

Примечание: обёртки ответов — вспомогательные конструкции TypeSpec; в emitted OpenAPI они НЕ создают схем с этими именами (тело — `$ref` на `Error`), поэтому на покрытие имён не влияют. Если после компиляции они всё же появились в `components.schemas` — убрать у них модельность через inline-описание в операциях (проверить в Task 9 по выводу compare.py как «лишние схемы» — таких репортов быть не должно, compare проверяет только отсутствующие).

- [ ] **Step 2: Написать models/users.tsp**

Транскрибировать из референса схемы: `User`, `UserWithPhoto`, `BotInfo`, `BotPatch`, `BotCommand`. Дамп каждой — командой из Global Constraints. Образец (сверить поля с реальным дампом, здесь показан паттерн):

```typespec
namespace MaxBotApi;

/** Объект с общей информацией о пользователе или боте */
model User {
  /** Идентификатор пользователя или бота */
  user_id: int64;

  /** Отображаемое имя пользователя или бота */
  first_name: string;

  /** Отображаемая фамилия пользователя */
  last_name?: string | null;

  /** Отображаемое имя пользователя или бота */
  name: string;

  /** Уникальное публичное имя пользователя или бота. Может быть null, если недоступно */
  username?: string | null;

  /** `true`, если это бот */
  is_bot: boolean;

  /** Время последней активности пользователя в MAX (Unix-время в миллисекундах) */
  last_activity_time?: int64;
}

/** Команда, поддерживаемая ботом */
model BotCommand {
  /** Название команды */
  @minLength(1)
  @maxLength(64)
  name: string;

  /** Описание команды */
  @minLength(1)
  @maxLength(128)
  description?: string | null;
}
```

`UserWithPhoto` — это `allOf: [User, {...}]` без дискриминатора → `model UserWithPhoto extends User { ... }` (у `description` там `maxLength: 16000`). `BotInfo` — `allOf: [UserWithPhoto, {...}]` → `extends UserWithPhoto`, поле `commands` с `@maxItems(32)`. `BotPatch` — самостоятельная модель (все ограничения из дампа: имена 1–64, description 1–16000, commands ≤ 32).

ВАЖНО: сверять каждое поле с дампом референса — состав полей выше мог устареть; истина в reference-файле.

- [ ] **Step 3: Подключить в main.tsp**

Добавить после существующих import:
```typespec
import "./common.tsp";
import "./models/users.tsp";
```

- [ ] **Step 4: Скомпилировать и свериться**

Run: `npx tsp compile . && python3 scripts/compare.py | grep -E "schemas.(User|Bot|Image|Error|SimpleQueryResult)" | head -20`
Expected: компиляция ок; строк `schemas.User... схема отсутствует` больше нет; допустимы диффы формата — исправить до чистоты по этим схемам (кроме `KNOWN`). Итоговое число расхождений уменьшилось (~на 8).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: базовые модели (Error, User, BotInfo) и обёртки ответов"
```

---

### Task 4: models/keyboard.tsp и models/markup.tsp

**Files:**
- Create: `models/keyboard.tsp`, `models/markup.tsp`
- Modify: `main.tsp` (2 импорта, по образцу Task 3 Step 3)

**Interfaces:**
- Consumes: ничего из предыдущих.
- Produces: `Keyboard`, база `Button` + 7 наследников, `Intent` (enum), база `ReplyButton` + 3 наследника (`SendMessageButton`, `SendGeoLocationButton`, `SendContactButton`); база `MarkupElement` + 10 наследников `*Markup`. Нужны Task 5 (вложения-клавиатуры) и Task 6 (Message.markup).

- [ ] **Step 1: Написать models/keyboard.tsp**

Дискриминированный union — базовый паттерн для ВСЕХ union-задач:

```typespec
namespace MaxBotApi;

/** Кнопка клавиатуры */
@discriminator("type")
model Button {
  /** Видимый текст кнопки */
  @minLength(1)
  @maxLength(128)
  text: string;
}

/** После нажатия клиент отправляет на сервер полезную нагрузку */
model CallbackButton extends Button {
  type: "callback";

  /** Токен кнопки */
  @maxLength(1024)
  payload: string;

  /** Намерение кнопки */
  intent?: Intent = Intent.default;
}
```

(поле `intent` — сверить с дампом `CallbackButton`; если его нет или default отличается — следовать дампу. Если у enum `Intent` есть член `default` — в TypeSpec это ключевое слово, экранировать обратными кавычками: `` `default` ``).

Транскрибировать по дампам: `Keyboard`, `Button` (база), `CallbackButton`, `LinkButton` (url ≤ 2048), `RequestGeoLocationButton`, `RequestContactButton`, `ChatButton` (chat_title ≤ 200, chat_description ≤ 400, start_payload ≤ 512), `OpenAppButton`, `ClipboardButton` (payload ≤ 1024), `MessageButton` (text ≤ 128), `Intent`, `ReplyButton` (база: text 1–128, payload ≤ 1024) + `SendMessageButton`, `SendGeoLocationButton`, `SendContactButton`.

Значения дискриминатора брать из `discriminator.mapping` баз: Button → callback, link, request_geo_location, request_contact, open_app, message, clipboard; ReplyButton → message, user_geo_location, user_contact.

Осторожно: у `ChatButton` в mapping ключ отсутствует — проверить дамп `Button.discriminator.mapping`: `ChatButton` маплен на ключ `chat`? Если `ChatButton` не в mapping — он просто `extends Button` не подходит (появится лишний mapping-ключ). Решение как с `MessageChatCreatedUpdate`: если схема есть, а в mapping её нет — всё равно `extends` с литералом типа по имени схемы, а строку-ключ добавить в DEVIATIONS compare.py с комментарием. Сверить фактический mapping перед написанием.

- [ ] **Step 2: Написать models/markup.tsp**

База: `MarkupElement` (`@discriminator("type")`, поля `from`: int32, `length`: int32 — сверить required по дампу). Наследники (значения type из mapping): `StrongMarkup`("strong"), `EmphasizedMarkup`("emphasized"), `MonospacedMarkup`("monospaced"), `LinkMarkup`("link", url: 1–2048), `StrikethroughMarkup`("strikethrough"), `UnderlineMarkup`("underline"), `UserMentionMarkup`("user_mention", user_id int64), `HeadingMarkup`("heading"), `HighlightedMarkup`("highlighted"), `QuoteMarkup`("quote").

- [ ] **Step 3: Импорты в main.tsp, компиляция, сверка**

Run: `npx tsp compile . && python3 scripts/compare.py | grep -cE "^DIFF"`
Expected: компиляция ок; число DIFF уменьшилось ещё на ~25 (21 схема + отсутствие диффов по ним). Диффы по `Button`/`MarkupElement` устранить (обычно это пропущенное ограничение или несовпадение required).

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: клавиатуры (Button, ReplyButton) и разметка текста (MarkupElement)"
```

---

### Task 5: models/attachments.tsp и models/attachment-requests.tsp

**Files:**
- Create: `models/attachments.tsp`, `models/attachment-requests.tsp`
- Modify: `main.tsp` (2 импорта)

**Interfaces:**
- Consumes: `Keyboard`, `ReplyButton` из Task 4; `Image` из Task 3.
- Produces: база `Attachment` + 9 наследников + payload-модели (`PhotoAttachmentPayload`, `MediaAttachmentPayload`, `FileAttachmentPayload`, `AttachmentPayload`, `ContactAttachmentPayload`, `StickerAttachmentPayload`, `ShareAttachmentPayload`, `VideoThumbnail`, `VideoUrls`, `VideoAttachmentDetails`, `DataAttachment`, `ReplyKeyboardAttachment`); база `AttachmentRequest` + 9 наследников + request-payload-модели (`PhotoAttachmentRequestPayload`, `PhotoToken`, `PhotoTokens`, `UploadedInfo`, `ContactAttachmentRequestPayload`, `StickerAttachmentRequestPayload`, `InlineKeyboardAttachmentRequestPayload`), `UploadType` (enum). Нужны Task 6 (Message, NewMessageBody).

- [ ] **Step 1: Написать models/attachments.tsp**

Значения `type` из mapping базы `Attachment`: image, video, audio, file, sticker, contact, inline_keyboard, share, location. Пример паттерна «наследник + payload»:

```typespec
/** Фото-вложение */
model PhotoAttachment extends Attachment {
  type: "image";
  payload: PhotoAttachmentPayload;
}

/** Полезная нагрузка фото-вложения */
model PhotoAttachmentPayload {
  /** Уникальный идентификатор фото */
  photo_id: int64;
  token: string;

  /** URL изображения */
  url: string;
}
```

Особые случаи (свериться с дампами): `LocationAttachment` — latitude/longitude `float64`; `FileAttachment` — size `int64`; `ShareAttachmentPayload` — url `@minLength(1)`; `ReplyKeyboardAttachment` — в mapping базы его нет (проверить) — правило то же, что для ChatButton в Task 4; `DataAttachment` — самостоятельная или наследник (по дампу).

- [ ] **Step 2: Написать models/attachment-requests.tsp**

Mapping базы `AttachmentRequest`: те же 9 ключей. Ограничения: `PhotoAttachmentRequestPayload.url` `@minLength(1)`; `InlineKeyboardAttachmentRequestPayload.buttons` — `@minItems(1)` (в оригинале артефакт `minLength: 1`, compare нормализует); `ReplyKeyboardAttachmentRequest` — `direct_user_id` int64, `direct` default false, `buttons` `@minItems(1)`; `ContactAttachmentRequestPayload.contact_id` int64.

- [ ] **Step 3: Импорты, компиляция, сверка**

Run: `npx tsp compile . && python3 scripts/compare.py | grep -cE "^DIFF"`
Expected: DIFF уменьшилось ещё на ~35. Диффы по свежим схемам устранить.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: вложения (Attachment) и запросы вложений (AttachmentRequest)"
```

---

### Task 6: models/messages.tsp

**Files:**
- Create: `models/messages.tsp`
- Modify: `main.tsp` (импорт)

**Interfaces:**
- Consumes: `User` (Task 3), `Attachment`, `AttachmentRequest` (Task 5), `MarkupElement` (Task 4).
- Produces: `Message`, `MessageBody`, `MessageStat`, `MessageList`, `Recipient`, `LinkedMessage`, `MessageLinkType` (enum), `NewMessageBody`, `NewMessageLink`, `SendMessageResult`, `TextFormat` (enum), `Callback`, `CallbackAnswer`, `SenderAction` (enum), `ActionRequestBody`, `PinMessageBody`, `GetPinnedMessageResult`. Нужны Task 7 (Chat.pinned_message), Task 8 (updates), Task 9 (routes).

- [ ] **Step 1: Транскрибировать модели по дампам**

Ключевые ограничения: `NewMessageBody.text` `@maxLength(4000)`, nullable → `text?: string | null`; `NewMessageBody.notify` `= true`; `Recipient.chat_id`/`user_id` int64 nullable; `Message.timestamp` int64; `MessageBody.seq` int64; `LinkedMessage.chat_id` int64. Enum'ы: `TextFormat` (markdown/html — по дампу), `MessageLinkType` (reply/forward), `SenderAction` (typing_on, sending_photo, sending_video, sending_audio, sending_file).

Пример enum:
```typespec
/** Действие, отправляемое участникам чата */
enum SenderAction {
  typing_on,
  sending_photo,
  sending_video,
  sending_audio,
  sending_file,
}
```

- [ ] **Step 2: Импорт, компиляция, сверка**

Run: `npx tsp compile . && python3 scripts/compare.py | grep -cE "^DIFF"`
Expected: DIFF уменьшилось ещё на ~17; диффы по новым схемам устранены.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: модели сообщений (Message, NewMessageBody, Callback)"
```

---

### Task 7: models/chats.tsp

**Files:**
- Create: `models/chats.tsp`
- Modify: `main.tsp` (импорт)

**Interfaces:**
- Consumes: `Message` (Task 6), `Image` (Task 3), `UserWithPhoto` (Task 3).
- Produces: `Chat`, `ChatType` (enum), `ChatStatus` (enum), `ChatList`, `ChatPatch`, `ChatMember`, `ChatAdmin`, `ChatAdminPermission` (enum), `ChatMembersList`, `ChatAdminsList`, `UserIdsList`, `ModifyMembersResult`, `FailedUserDetails`. Нужны Task 8 и Task 9.

- [ ] **Step 1: Транскрибировать модели по дампам**

Ключевое: `Chat.participants` — `Record<int64> | null` (additionalProperties int64, nullable); `Chat.chat_id`/`owner_id`/`last_event_time` int64, `participants_count` int32; `ChatPatch.title` 1–200; `ChatMember extends UserWithPhoto` (проверить по дампу — там allOf) с `last_access_time`/`join_time` int64; маркеры пагинации во всех `*List` — int64 nullable; `UserIdsList.user_ids` — ОСОЗНАННОЕ ОТЛИЧИЕ (см. Global Constraints): у нас чистый `@maxItems(100) user_ids: int64[]` (дифф по этой схеме попадёт в KNOWN автоматически).

- [ ] **Step 2: Импорт, компиляция, сверка**

Run: `npx tsp compile . && python3 scripts/compare.py | grep -cE "^DIFF"`
Expected: DIFF уменьшилось ещё на ~13.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: модели чатов (Chat, ChatMember, списки)"
```

---

### Task 8: models/updates.tsp

**Files:**
- Create: `models/updates.tsp`
- Modify: `main.tsp` (импорт)

**Interfaces:**
- Consumes: `Message`, `Callback` (Task 6), `User` (Task 3).
- Produces: база `Update` (`@discriminator("update_type")`) + 16 наследников: `MessageCreatedUpdate`, `MessageCallbackUpdate`, `MessageEditedUpdate`, `MessageRemovedUpdate`, `BotAddedToChatUpdate`, `BotRemovedFromChatUpdate`, `BotStartedUpdate`, `BotStoppedUpdate`, `ChatTitleChangedUpdate`, `DialogClearedUpdate`, `DialogMutedUpdate`, `DialogUnmutedUpdate`, `DialogRemovedUpdate`, `UserAddedToChatUpdate`, `UserRemovedFromChatUpdate`, `MessageChatCreatedUpdate`; `UpdateList`, `Subscription`, `SubscriptionRequestBody`, `GetSubscriptionsResult`. Нужны Task 9 (GET /updates, subscriptions).

- [ ] **Step 1: Транскрибировать по дампам**

Значения `update_type` — из mapping базы `Update` (15 ключей) плюс `MessageChatCreatedUpdate` с `update_type: "message_chat_created"` (его нет в mapping оригинала — KNOWN-отличие, уже в DEVIATIONS). Ключевые ограничения: все `chat_id`/`user_id`/`inviter_id`/`admin_id`/`muted_until`/`timestamp` — int64; `BotStartedUpdate.payload` ≤ 512; `SubscriptionRequestBody.secret` — `@minLength(5) @maxLength(256) @pattern("^[a-zA-Z0-9_-]{5,256}$")` (в оригинале паттерн с экранированием — перенести дословно из дампа); `Subscription.update_types` — элементы `@minLength(1)`? (в оригинале minLength на items строк — перенести как есть на элемент: `update_types?: string[]` — ограничение на элементе массива в TypeSpec выражается через именованный тип; если сложно — оставить string[] и проверить, всплывёт ли дифф; при диффе использовать паттерн:

```typespec
model Subscription {
  ...
  update_types?: minLength1String[] | null;
}

@minLength(1)
scalar minLength1String extends string;
```

Осторожно: scalar может эмититься отдельной схемой — если появится дифф «лишняя схема»/несовпадение, откатиться к `string[]` и добавить точечную запись в DEVIATIONS с комментарием).

- [ ] **Step 2: Импорт, компиляция, сверка**

Run: `npx tsp compile . && python3 scripts/compare.py | grep -cE "^DIFF"`
Expected: остались только диффы по путям (paths.*): все 127 схем покрыты.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: события (Update) и подписки (Subscription)"
```

---

### Task 9: Все маршруты (routes/*.tsp)

**Files:**
- Create: `routes/bots.tsp`, `routes/chats.tsp`, `routes/messages.tsp`, `routes/subscriptions.tsp`, `routes/uploads.tsp`
- Modify: `main.tsp` (5 импортов)

**Interfaces:**
- Consumes: все модели Task 3–8, обёртки ответов из common.tsp.
- Produces: 28 операций, полностью совпадающих с официальными путями/методами/operationId/параметрами/ответами.

- [ ] **Step 1: routes/bots.tsp — образец для остальных**

```typespec
import "@typespec/http";
import "@typespec/openapi";

using Http;
using OpenAPI;

namespace MaxBotApi;

@tag("bots")
interface Bots {
  /** Возвращает информацию о текущем боте, которого идентифицирует access_token */
  @summary("Получение информации о текущем боте")
  @operationId("getMyInfo")
  @route("/me")
  @get
  getMyInfo(): BotInfo | Unauthorized | InternalError;
}
```

Состав ответов КАЖДОЙ операции сверять с дампом её `responses` (набор кодов у операций разный: 200 всегда; 401 и 500 почти всегда; 403 у 13, 404 у 10, 405 у 2).

- [ ] **Step 2: Остальные четыре файла**

Паттерн параметров и тела (пример — `PATCH /chats/{chatId}`):

```typespec
@tag("chats")
interface Chats {
  /** Изменение информации о чате: заголовок, значок и т.д. */
  @summary("Изменение информации о чате")
  @operationId("editChat")
  @route("/chats/{chatId}")
  @patch(#{ implicitOptionality: false })
  editChat(
    /** ID чата */
    @path chatId: int64,
    @body body: ChatPatch,
  ): Chat | Unauthorized | NotFound | InternalError;
  // ...
}
```

Паттерн query-параметров с ограничениями и default (пример — `GET /chats/{chatId}/members`):

```typespec
getMembers(
  @path chatId: int64,
  @query user_ids?: int64[],
  @query marker?: int64,

  /** Количество участников в ответе */
  @minValue(1)
  @maxValue(100)
  @query count?: int32 = 20,
): ChatMembersList | Unauthorized | Forbidden | NotFound | InternalError;
```

Полный список операций по файлам (метод, путь, operationId — из референса, там же параметры/тела/ответы):

- `routes/chats.tsp` (интерфейс `Chats`, @tag("chats")): getChats, getChat, editChat, sendAction, getPinnedMessage, pinMessage, unpinMessage, getMembership, leaveChat, getAdmins, postAdmins, deleteAdmin, getMembers, addMembers, removeMember — 15 операций (GET /chats; GET+PATCH /chats/{chatId}; POST .../actions; GET+PUT+DELETE .../pin; GET+DELETE .../members/me; GET+POST .../members/admins; DELETE .../members/admins/{userId}; GET+POST+DELETE .../members).
- `routes/messages.tsp` (интерфейс `Messages`, @tag("messages")): getMessages, sendMessage, editMessage, deleteMessage, getMessageById, getVideoAttachmentDetails (GET /videos/{videoToken}), answerOnCallback (POST /answers) — 7 операций.
- `routes/subscriptions.tsp` (интерфейс `Subscriptions`, @tag("subscriptions")): getSubscriptions, subscribe, unsubscribe, getUpdates (GET /updates: limit 1–1000 = 100, timeout 0–90 = 30, marker int64, types) — 4 операции.
- `routes/uploads.tsp` (интерфейс `Uploads`, @tag("upload")): getUploadUrl (POST /uploads, query-параметр type: UploadType) — 1 операция.

Технические замечания:
- `@patch(#{ implicitOptionality: false })` — обязательно, иначе TypeSpec сделает все поля тела PATCH опциональными и появятся диффы по `required`.
- Путь `GET /updates` в оригинале — параметр `types`: сверить схему по дампу (массив строк?).
- `DELETE /messages` и `PUT /messages` — `message_ids`/`message_id` в query: брать имена/типы из дампов.
- Ответ 200 некоторых операций — `$ref` на `SuccessResponse` (тело `SimpleQueryResult`): возвращать просто `SimpleQueryResult`.

- [ ] **Step 3: Импорты, компиляция, полная сверка**

Run: `npx tsp compile . && python3 scripts/compare.py`
Expected: единицы DIFF (огрехи транскрипции). Устранить все по одному, перезапуская сверку.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: все 28 операций Max Bot API"
```

---

### Task 10: Ноль расхождений + README

**Files:**
- Create: `README.md`
- Modify: `scripts/compare.py` (только если всплыли новые оправданные DEVIATIONS — каждую комментировать)

**Interfaces:**
- Consumes: всё.
- Produces: `python3 scripts/compare.py` → exit 0; README с описанием проекта и таблицей осознанных отличий.

- [ ] **Step 1: Добить сверку до нуля**

Run: `python3 scripts/compare.py`
Expected: `Итого: 0 расхождений, N известных отличий`, exit 0. Каждое KNOWN-отличие должно соответствовать списку из Global Constraints; новых DEVIATIONS без письменного обоснования в README не добавлять.

- [ ] **Step 2: Написать README.md**

Содержание: что это (рукописный TypeSpec Max Bot API v0.0.32); как собрать (`npm install`, `npx tsp compile .`); как сверить (`python3 scripts/compare.py`); откуда референс (репозиторий max-messenger-bot/max-bot-api-schemas, снимок 2026-07-10); таблица «Осознанные отличия от официальной схемы» — все 5 пунктов из Global Constraints + всплывшие в ходе работы, каждый с обоснованием.

- [ ] **Step 3: Финальная проверка и commit**

Run: `npx tsp compile . && python3 scripts/compare.py && git status --short`
Expected: компиляция ок, сверка exit 0.

```bash
git add -A && git commit -m "feat: полная сверка с официальной схемой + README"
```
