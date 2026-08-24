#!/usr/bin/env python3
"""Семантическая сверка сгенерированного OpenAPI с официальной схемой Max Bot API.

Сравнивает по сути (пути, методы, operationId, параметры, тела, ответы,
свойства, типы, ограничения), разворачивая $ref и allOf. Имена схем
сравниваются по покрытию components.schemas (имена у нас совпадают с
оригиналом по договорённости). Описания/summary не сравниваются.
"""
import json
import re
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
OFFICIAL = ROOT / "reference" / "max-openapi-official.json"
# Сверяется только основной сервис MaxBotApi; второй файл
# (openapi.MaxBotWebhook.yaml — контракт webhook-сервера разработчика)
# официального аналога не имеет и не сверяется.
OURS = ROOT / "tsp-output" / "@typespec" / "openapi3" / "openapi.MaxBotApi.yaml"

# Осознанные отличия (regex по строке диффа "path: msg", re.search) — см.
# README «Отличия от официальной схемы». Каждый паттерн должен быть максимально
# точечным, чтобы не глушить непредвиденные расхождения в том же поддереве.
DEVIATIONS = [
    r"message_chat_created",  # отсутствует в mapping оригинала (недосмотр)
    r"mapping\[chat\]",  # ChatButton есть в схемах, но отсутствует в Button.discriminator.mapping оригинала (недосмотр)
    # Оригинал не объявляет свойство `type` в properties/required у ReplyButton
    # (и его наследников по <mapping>-цепочке), хотя дискриминатор задан — в
    # отличие от Button. У нас `type` эмиттится легитимно. Ограничено двумя
    # конкретными формами диффа внутри поддерева schemas.ReplyButton*, чтобы
    # любые другие расхождения (например неверный maxLength у SendContactButton)
    # по-прежнему всплывали как DIFF.
    r"^schemas\.ReplyButton(<[^>]*>)*\.type: лишнее свойство у нас$",
    r"^schemas\.ReplyButton(<[^>]*>)*: required: официально \[[^\]]*\], у нас \[[^\]]*'type'[^\]]*\]$",
    # DataAttachment есть в схемах и расширяет Attachment, но отсутствует в
    # Attachment.discriminator.mapping оригинала (недосмотр, тот же приём,
    # что и для mapping[chat])
    r"mapping\[data\]",
    # ReplyKeyboardAttachment/ReplyKeyboardAttachmentRequest есть в схемах и
    # расширяют Attachment/AttachmentRequest соответственно, но отсутствуют в
    # соответствующих discriminator.mapping оригинала (недосмотр)
    r"mapping\[reply_keyboard\]",
    # Оригинал непоследователен: VideoAttachment.thumbnail одновременно
    # объявлен как type: "string" И как allOf-ссылка на объектную схему
    # VideoThumbnail ({url: string}) — явно рудимент более ранней версии
    # схемы, где миниатюра была просто URL-строкой. Мы транскрибируем
    # семантически осмысленный вариант (VideoThumbnail, по аналогии с
    # VideoAttachmentDetails.thumbnail: PhotoAttachmentPayload), а не
    # унаследованный обрывок type: string. Тот же приём, что и для
    # ReplyButton/ChatButton — точечный regex по конкретному сообщению диффа.
    # Тот же артефакт достижим и через paths.* (Task 9: любая операция,
    # возвращающая Message/Chat с вложениями, заново раскрывает Attachment,
    # т.к. поле с doc-комментарием эмиттится как allOf-обёртка, а не голый
    # $ref — см. подробное объяснение ниже, у MessageBody). Матчим по
    # суффиксу диффа (без привязки к префиксу schemas./paths.*), т.к. само
    # сообщение диффа уникально для этого артефакта.
    r"attachments\[\](<[^>]*>)*\.thumbnail: type: официально 'string'",
    # MessageBody в оригинале непоследователен в двух местах (недосмотр):
    # 1) required перечисляет 'link', но такого свойства у MessageBody нет
    #    в properties вообще (mid/seq/text/attachments/markup) — явный
    #    рудимент копипасты из Message.required. Мы не эмитируем несуществующее
    #    свойство как required.
    # 2) свойство attachments содержит паразитный вложенный ключ
    #    "required": false прямо внутри схемы самого свойства — это не валидная
    #    конструкция OpenAPI (required — атрибут уровня схемы-объекта, а не
    #    отдельного свойства). У нас такого артефакта нет и быть не может.
    # MessageBody встречается по ссылке в огромном количестве контекстов —
    # и в components.schemas (Message.body, LinkedMessage.message,
    # SendMessageResult.message.*, GetPinnedMessageResult.message.*,
    # Chat.pinned_message.*, все варианты Update.message/.chat.pinned_message
    # — Tasks 7-8), и (Task 9) в paths.* — КАЖДАЯ операция, которая
    # возвращает Chat/Message/MessageList/UpdateList и т.п. своим кодом
    # ответа, заново раскрывает MessageBody с нуля (см. объяснение про
    # allOf-обёртку doc-комментированных свойств: $ref-дедупликация по (na, nb)
    # не срабатывает, т.к. у обёрнутого свойства нет голого "$ref" на верхнем
    # уровне). Перечислять все места-источники (со всеми префиксами schemas.*
    # и paths.*) практически невозможно и хрупко — вместо этого матчим по
    # самому телу diff-сообщения: конкретный required-список ['attachments',
    # 'link', 'mid', 'seq', 'text'] vs ['mid', 'seq', 'text'] и артефакт
    # "attachments: required: официально False" уникальны для MessageBody во
    # всей схеме (никакая другая модель не имеет такого required-набора и не
    # хранит required=false внутри схемы отдельного свойства), поэтому
    # unanchored-паттерн по значению безопасен.
    r"required: официально \['attachments', 'link', 'mid', 'seq', 'text'\], у нас \['mid', 'seq', 'text'\]$",
    r"\.attachments: required: официально False, у нас None$",
    # Subscription.update_types: элементы массива в оригинале несут
    # minLength: 1. В TypeSpec ограничение на элементе (а не на самом
    # массиве) строк выражается только через именованный scalar-тип
    # (`@minLength(1) scalar X extends string`), а такой scalar эмиттится
    # openapi3-генератором как отдельная схема components.schemas — то есть
    # появляется схема, которой нет в оригинале. Осознанно жертвуем этим
    # точечным ограничением (непустая строка типа события), чтобы не
    # засорять схему артефактом, отсутствующим в first-party спецификации.
    # Путь заякорен ровно на этом свойстве (независимо от префикса schemas.*
    # или paths.* — Task 9 достигает того же свойства через
    # paths./subscriptions.get.responses[200], т.к. GetSubscriptionsResult
    # раньше раскрывается там, чем в цикле components.schemas), чтобы не
    # глушить minLength в любом другом месте.
    r"\.subscriptions\[\]\.update_types\[\]: minLength: официально 1, у нас None$",
    # GET /chats больше не документирован в оригинале (см. подробное
    # обоснование в routes/chats.tsp у операции getChats) — оригинал вообще
    # не содержит ключа "responses" у этой операции. Мы транскрибируем
    # содержательный (исторический) контракт: 200 ChatList, 401, 500 — все
    # три кода закономерно оказываются "лишними" при сравнении с пустым
    # responses оригинала.
    r"^paths\./chats\.get\.responses\[(200|401|500)\]: лишний ответ у нас$",
    # GET /messages: параметр message_ids в оригинале — {type: array, format:
    # string} без объявленного items вообще (тот же приём копипасты, что и у
    # UserIdsList в теле POST /chats/{chatId}/members). Мы транскрибируем
    # содержательный смысл — массив строк с items.
    r"^paths\./messages\.get\.param\(query:message_ids\): items только с одной стороны$",
    # UserIdsList.user_ids: параметро-подобная структура (см. выше про
    # параметро-подобные обёртки в flatten). Раньше посещалась двумя путями:
    # (1) components.schemas.UserIdsList (теперь удалена, т.к. не посещается),
    # (2) paths.* (POST /chats/{chatId}/members requestBody) — это основной путь.
    # Якорен конкретно на этом пути, чтобы не подхватить аналогичные артефакты
    # в других местах (если вообще появятся).
    # Второй путь (schemas.UserIdsList) актуален, пока routes/chats.tsp
    # отключён в main.tsp и схема сравнивается напрямую, а не через paths.*.
    r"^(paths\./chats/\{chatId\}/members\.post\.requestBody|schemas\.UserIdsList)\.user_ids(\[\])?: maxItems: официально (None|100), у нас (100|None)$",
    # ------------------------------------------------------------------
    # Ужесточения по требованиям Кибербезопасности (КБ) — осознанные
    # отличия от официальной схемы, внесённые в TypeSpec-исходники.
    # Обоснования — в doc-комментариях у соответствующих полей *.tsp.
    # Регексы пришпилены к точным значениям наших ограничений, чтобы
    # любое другое расхождение тех же ключей всплывало как DIFF.
    # ------------------------------------------------------------------
    # seal-object-schemas: все object-схемы (кроме расширяемых через allOf
    # базовых) запечатаны через additionalProperties: false. Эмиттер сам
    # пишет эквивалентное `{not: {}}` (не настраивается через tspconfig.yaml —
    # зашито в @typespec/openapi3/schema-emitter.js), поэтому
    # scripts/seal_additional_properties.py переписывает результат
    # `tsp compile` на литеральный `false` перед сверкой.
    r"additionalProperties: официально None, у нас False$",
    # Числа: явные границы (требование КБ). int64-идентификаторы и
    # unix-время — 0..2^53-1 (JSON-safe диапазон: ID крупнее 2^53 теряли бы
    # точность в JS-клиентах, включая веб-клиент MAX); int32 и счётчики/
    # размеры (width, height, duration, views и т.п.) — 0..2^31-1.
    r"minimum: официально None, у нас 0$",
    r"maximum: официально None, у нас 9007199254740991$",
    r"maximum: официально None, у нас 2147483647$",
    # Строковые поля: maxLength. Значения по классам полей: 255 — дефолт
    # для строк без явного размера (включая message_id/mid и — временно,
    # пока не используется, — транскрипцию аудио); 64 — типы
    # вложений/событий/кнопок, BotPatch.name; 200/400 — название/описание
    # чата (зеркала официальных chat_title/chat_description); 512 —
    # start_payload (зеркало ChatButton.start_payload); 1024 — токены и
    # payload (зеркало кнопочного payload); 2048 — URL; 4000 — текст
    # сообщения (зеркало NewMessageBody.text); 4096 — VCF-карточки.
    r"maxLength: официально None, у нас (64|200|255|400|512|1024|2048|4000|4096)$",
    # Массивы: maxItems (требование КБ). Значения: 20 — списки прав
    # (в enum 13 значений); 50 — типы событий, админы (документированный
    # максимум); 100 — вложения, кнопки в ряду/рядов, страницы пагинации,
    # списки пользователей; 500 — элементы разметки; 1000 — updates
    # (зеркало максимума параметра limit).
    r"maxItems: официально None, у нас (20|50|100|500|1000)$",
    # Паттерны: тип вложения/события ([a-z_]), https-URL вебхука,
    # message_id (mid — реальные ID содержат точку), videoToken (паттерн
    # оригинала не заякорен и пропускал любую строку; заякорен и расширен
    # до типовых алфавитов токенов base64/base64url/hex).
    r"pattern: официально None, у нас '\^\[a-z_\]\+\$'$",
    r"pattern: официально None, у нас '\^https://\.\+\$'$",
    r"pattern: официально None, у нас '\^\[a-zA-Z0-9\._\\\\-\]\+\$'$",
    r"pattern: официально '\[a-zA-Z0-9_\\\\-\]\+', у нас '\^\[a-zA-Z0-9\+/=\._\\\\-\]\+\$'$",
    # minLength 1 приносят scalar-типы MessageId/UpdateType.
    r"minLength: официально None, у нас 1$",
    # КБ-идиома «любой текст заданной длины» для полей свободного текста
    # (имена/описания бота и команд, транскрипция).
    r"pattern: официально None, у нас '\^\[\\\\s\\\\S\]\{[01],\d+\}\$'$",
    # callback_id: паттерн оригинала использует lookahead (не поддерживается
    # RE2/СОВА) и пропускает любое непустое значение — заменён на «любые
    # непробельные символы».
    r"^paths\./answers\.post\.param\(query:callback_id\): pattern: официально '\^\(\?!\\\\s\*\$\)\.\+', у нас '\^\\\\S\{1,1024\}\$'$",
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
        if p.suffix in (".yaml", ".yml"):
            return yaml.safe_load(f)
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
        for pk, pv in p.get("properties", {}).items():
            # seal-object-schemas добавляет наследникам пустые стабы ({})
            # унаследованных свойств, чтобы additionalProperties их не
            # отсекал; при слиянии allOf стаб не должен затирать
            # типизированное объявление из базовой схемы.
            if pv == {} and pk in props:
                continue
            props[pk] = pv
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
    if c.get("type") == "object":
        c.pop("type")               # эмиттер всегда пишет type: object, оригинал — почти никогда
    if "required" in c and isinstance(c["required"], list):
        c["required"] = sorted(set(c["required"]))
    return c


def cmp_schema(a, b, ra, rb, path, seen, is_disc_prop=False):
    na = a.get("$ref") if isinstance(a, dict) else None
    nb = b.get("$ref") if isinstance(b, dict) else None
    if na or nb:
        key = (na, nb)
        if key in seen:
            return
        seen.add(key)
    a, b = flatten(a, ra), flatten(b, rb)
    ca, cb = constraints_of(a), constraints_of(b)
    # Дискриминатор-литералы: TypeSpec `type: "callback"` компилируется в
    # {type: string, enum: [callback]} на наследнике; оригинал почти всегда
    # просто наследует нетипизированное строковое поле базовой схемы, не
    # переобъявляя его с enum. Семантически это одно и то же (значение
    # дискриминатора фиксировано через discriminator.mapping), поэтому не
    # считаем расхождением. Применяем нормализацию ТОЛЬКО к самому полю
    # дискриминатора (is_disc_prop), а не к любому string-полю с одиночным
    # enum — иначе случайно захардкоженный enum на обычном свойстве (напр.
    # "status") будет молча проглочен вместо DIFF.
    if (is_disc_prop
            and a.get("type") == "string" and "enum" not in a
            and b.get("type") == "string"
            and isinstance(b.get("enum"), list) and len(b["enum"]) == 1):
        ca.pop("type", None)
        cb.pop("type", None)
        cb.pop("enum", None)
    for k in sorted(set(ca) | set(cb)):
        if ca.get(k) != cb.get(k):
            report(path, f"{k}: официально {ca.get(k)!r}, у нас {cb.get(k)!r}")
    pa, pb = a.get("properties", {}), b.get("properties", {})
    # propertyName дискриминатора текущей (родительской) схемы; flatten()
    # протаскивает discriminator базовой схемы в наследников через allOf,
    # так что это доступно и на развёрнутых дочерних схемах.
    disc_prop = (a.get("discriminator") or {}).get("propertyName") or (b.get("discriminator") or {}).get("propertyName")
    for k in sorted(set(pa) | set(pb)):
        if k not in pb:
            report(f"{path}.{k}", "свойства нет у нас")
        elif k not in pa:
            report(f"{path}.{k}", "лишнее свойство у нас")
        else:
            cmp_schema(pa[k], pb[k], ra, rb, f"{path}.{k}", seen, is_disc_prop=(k == disc_prop))
    if ("items" in a) != ("items" in b):
        report(path, "items только с одной стороны")
    elif "items" in a:
        cmp_schema(a["items"], b["items"], ra, rb, f"{path}[]", seen)
    ap, bp = a.get("additionalProperties"), b.get("additionalProperties")
    if isinstance(ap, dict) and isinstance(bp, dict):
        cmp_schema(ap, bp, ra, rb, f"{path}{{}}", seen)
    elif ap != bp:
        report(path, f"additionalProperties: официально {ap!r}, у нас {bp!r}")
    da = a.get("discriminator", {})
    db = b.get("discriminator", {})
    if da.get("propertyName") != db.get("propertyName"):
        report(path, f"discriminator: {da.get('propertyName')} vs {db.get('propertyName')}")
    ma, mb = da.get("mapping", {}), db.get("mapping", {})
    for k in sorted(set(ma) ^ set(mb)):
        report(path, f"discriminator.mapping[{k}] только с одной стороны")
    for k in sorted(set(ma) & set(mb)):
        # Оригинал непоследователен: mapping-значения обычно строки-$ref
        # ("#/components/schemas/Foo"), но у Update это вложенные объекты
        # {"$ref": "..."} (см. Update.discriminator.mapping). Нормализуем
        # обе формы к строке-ref, чтобы сравнение не падало и работало
        # одинаково независимо от того, какую форму выбрал оригинал.
        va = ma[k]["$ref"] if isinstance(ma[k], dict) else ma[k]
        vb = mb[k]["$ref"] if isinstance(mb[k], dict) else mb[k]
        cmp_schema({"$ref": va}, {"$ref": vb}, ra, rb, f"{path}<{k}>", seen)


def cmp_content(a, b, ra, rb, path, seen):
    sa = (a or {}).get("content", {}).get("application/json", {}).get("schema")
    sb = (b or {}).get("content", {}).get("application/json", {}).get("schema")
    if (sa is None) != (sb is None):
        report(path, f"тело только с одной стороны (официально: {sa is not None})")
    elif sa is not None:
        cmp_schema(sa, sb, ra, rb, path, seen)


def main():
    if not OURS.exists():
        print(f"Нет {OURS.relative_to(ROOT)} — сначала выполните: npx tsp compile .")
        sys.exit(2)
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
            # Официальные теги обязаны присутствовать у нас; дополнительные
            # наши теги (сквозные группировки вроде bot-sending) допустимы.
            if not set(a.get("tags", [])) <= set(b.get("tags", [])):
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

    known = [d for d in diffs if any(re.search(p, d) for p in DEVIATIONS)]
    real = [d for d in diffs if d not in known]
    for d in real:
        print(f"DIFF  {d}")
    for d in known:
        print(f"KNOWN {d}")
    print(f"\nИтого: {len(real)} расхождений, {len(known)} известных отличий")
    sys.exit(1 if real else 0)


if __name__ == "__main__":
    main()
