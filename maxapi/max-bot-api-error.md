# Результаты валидации схемы

Источник отчёта: `f.html`  
Источник схемы: `tsp-output/@typespec/openapi3/openapi.MaxBotApi.yaml`  
Всего элементов: **99** (50 ошибок, 49 предупреждений)  
Парсер: `scripts/parse_errors.py`.

---

## Ошибки, сгруппированные по типам

### 1. Необходимо добавить описание (description) — 19

| Строка | Схема | Свойство |
| -----: | ----- | -------- |
| 151 | `post` | `is_channel` |
| 186 | `post` | `commands` |
| 187 | `post` | `description` |
| 188 | `post` | `avatar_url` |
| 189 | `post` | `full_avatar_url` |
| 190 | `post` | `user_id` |
| 191 | `post` | `first_name` |
| 192 | `post` | `last_name` |
| 193 | `post` | `username` |
| 194 | `post` | `is_bot` |
| 195 | `post` | `last_activity_time` |
| 226 | `post` | `is_channel` |
| 262 | `post` | `user_locale` |
| 277 | `post` | `update_type` |
| 294 | `post` | `user_locale` |
| 367 | `post` | `type` |
| 374 | `post` | `payload` |
| 503 | `post` | `type` |
| 530 | `post` | `text` |

### 2. Необходимо явно указать тип схемы (type) — 16

| Строка | Схема | Свойство |
| -----: | ----- | -------- |
| 151 | `post` | `is_channel` |
| 186 | `post` | `commands` |
| 187 | `post` | `description` |
| 188 | `post` | `avatar_url` |
| 189 | `post` | `full_avatar_url` |
| 190 | `post` | `user_id` |
| 191 | `post` | `first_name` |
| 192 | `post` | `last_name` |
| 193 | `post` | `username` |
| 194 | `post` | `is_bot` |
| 195 | `post` | `last_activity_time` |
| 226 | `post` | `is_channel` |
| 262 | `post` | `user_locale` |
| 294 | `post` | `user_locale` |
| 374 | `post` | `payload` |
| 530 | `post` | `text` |

### 3. Для строковых (string) типов данных должен указываться pattern — 15

| Строка | Схема | Свойство |
| -----: | ----- | -------- |
| 91 | `post` | `url` |
| 254 | `post` | `payload` |
| 259 | `post` | `user_locale` |
| 291 | `post` | `user_locale` |
| 311 | `post` | `text` |
| 347 | `post` | `callback_id` |
| 351 | `post` | `payload` |
| 371 | `post` | `payload` |
| 407 | `post` | `title` |
| 446 | `post` | `link` |
| 451 | `post` | `description` |
| 507 | `post` | `chat_title` |
| 511 | `post` | `chat_description` |
| 516 | `post` | `start_payload` |
| 580 | `post` | `alias` |

---

## Сводная таблица

| # | Тип ошибки | Кол-во |
| - | ---------- | -----: |
| 1 | Необходимо добавить описание (description) | 19 |
| 2 | Необходимо явно указать тип схемы (type) | 16 |
| 3 | Для строковых (string) типов данных должен указываться pattern | 15 |
|   | **Итого** | **50** |

## Закономерности

Топ-5 схем по количеству ошибок:
- `post`: 50 (строки [91, 151, 186, 187, 188]…)

## Воспроизведение

```bash
python3 scripts/parse_errors.py
```
