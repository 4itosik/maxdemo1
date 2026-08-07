#!/usr/bin/env bash
# Ручная проверка живого мока: полный круг интеграции через curl.
#
# Запуск:  ./scripts/smoke.sh [адрес мока]
# По умолчанию адрес — http://localhost:8080 (мок должен быть уже запущен).
#
# Скрипт поднимает на localhost простейший стенд-приёмник webhook-ов на
# Python и печатает всё, что тот получил. Требуется curl, python3 и jq.

set -euo pipefail

MOCK="${1:-http://localhost:8080}"
STAND_PORT="${STAND_PORT:-9099}"
STAND_URL="http://127.0.0.1:${STAND_PORT}/hook"
SECRET="smoke-secret-1234"
EVENTS_FILE="$(mktemp -t maxmock-events)"

for tool in curl python3 jq; do
  command -v "$tool" >/dev/null || { echo "нужен $tool" >&2; exit 1; }
done

curl -sf "${MOCK}/healthz" >/dev/null || {
  echo "мок недоступен по адресу ${MOCK} — запустите ./max-mock" >&2
  exit 1
}

say() { printf '\n\033[1m== %s\033[0m\n' "$1"; }

# --- стенд-приёмник webhook-ов -------------------------------------------
python3 - "$STAND_PORT" "$EVENTS_FILE" <<'PY' &
import http.server, json, sys, threading

port, path = int(sys.argv[1]), sys.argv[2]

class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        with open(path, 'a') as f:
            f.write(json.dumps({
                'secret': self.headers.get('X-Max-Bot-Api-Secret'),
                'update': json.loads(body or b'{}'),
            }, ensure_ascii=False) + '\n')
        self.send_response(200)
        self.end_headers()

    def log_message(self, *args):
        pass

http.server.HTTPServer(('127.0.0.1', port), Handler).serve_forever()
PY
STAND_PID=$!
trap 'kill "$STAND_PID" 2>/dev/null || true; rm -f "$EVENTS_FILE"' EXIT
sleep 0.5

# --- сценарий -------------------------------------------------------------
say "Регистрация бота"
BOT=$(curl -sf -X POST "${MOCK}/mock/api/bots" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Бот поддержки","username":"support_bot"}')
BOT_ID=$(jq -r .id <<<"$BOT")
TOKEN=$(jq -r .token <<<"$BOT")
echo "bot_id=${BOT_ID} token=${TOKEN}"

auth=(-H "Authorization: ${TOKEN}" -H 'Content-Type: application/json')

say "GET /me"
curl -sf "${MOCK}/me" "${auth[@]}" | jq .

say "Подписка стенда на события"
curl -sf -X POST "${MOCK}/subscriptions" "${auth[@]}" \
  -d "{\"url\":\"${STAND_URL}\",\"secret\":\"${SECRET}\"}" | jq .

say "Создание клиента"
CLIENT=$(curl -sf -X POST "${MOCK}/mock/api/bots/${BOT_ID}/clients" \
  -H 'Content-Type: application/json' -d '{"first_name":"Иван"}')
CHAT_ID=$(jq -r .dialog.chat_id <<<"$CLIENT")
USER_ID=$(jq -r .client.user_id <<<"$CLIENT")
echo "chat_id=${CHAT_ID} user_id=${USER_ID}"

action() {
  curl -sf -X POST "${MOCK}/mock/api/dialogs/${CHAT_ID}/actions" \
    -H 'Content-Type: application/json' -d "$1" | jq -c .
}

say "Клиент нажимает «Начать» и пишет боту"
action '{"action":"start","payload":"smoke"}'
action '{"action":"send","text":"здравствуйте, нужна помощь"}'

say "КЦ отвечает сообщением с кнопками"
SENT=$(curl -sf -X POST "${MOCK}/messages?user_id=${USER_ID}" "${auth[@]}" -d '{
  "text": "Выберите тему обращения",
  "link": null,
  "attachments": [{"type":"inline_keyboard","payload":{"buttons":[[
    {"type":"callback","text":"Оплата","payload":"topic:payment"},
    {"type":"callback","text":"Доставка","payload":"topic:delivery"}
  ]]}}]
}')
MID=$(jq -r .message.body.mid <<<"$SENT")
echo "mid=${MID}"

say "Клиент нажимает кнопку"
action "{\"action\":\"press\",\"mid\":\"${MID}\",\"payload\":\"topic:payment\"}"
sleep 0.4

CALLBACK_ID=$(jq -r 'select(.update.update_type=="message_callback") | .update.callback.callback_id' \
  "$EVENTS_FILE" | tail -1)
echo "callback_id=${CALLBACK_ID}"

say "КЦ отвечает на нажатие, заменяя сообщение"
curl -sf -X POST "${MOCK}/answers?callback_id=${CALLBACK_ID}" "${auth[@]}" \
  -d '{"message":{"text":"Тема «Оплата» принята в работу","attachments":[],"link":null}}' | jq .

say "Загрузка файла и отправка вложения"
UPLOAD=$(curl -sf -X POST "${MOCK}/uploads?type=file" "${auth[@]}")
UPLOAD_URL=$(jq -r .url <<<"$UPLOAD")
TMP_FILE="$(mktemp -t maxmock-file)"
echo "содержимое вложения" > "$TMP_FILE"
ATT_TOKEN=$(curl -sf -X POST "$UPLOAD_URL" -F "data=@${TMP_FILE};filename=отчёт.txt" | jq -r .token)
rm -f "$TMP_FILE"
curl -sf -X POST "${MOCK}/messages?chat_id=${CHAT_ID}" "${auth[@]}" \
  -d "{\"text\":\"Отчёт во вложении\",\"link\":null,\"attachments\":[{\"type\":\"file\",\"payload\":{\"token\":\"${ATT_TOKEN}\"}}]}" \
  | jq '.message.body.attachments'

say "Правка и удаление сообщения"
curl -sf -X PUT "${MOCK}/messages?message_id=${MID}" "${auth[@]}" \
  -d '{"text":"Заявка №123 зарегистрирована","attachments":null,"link":null}' | jq -c .
curl -sf -X DELETE "${MOCK}/messages?message_id=${MID}" "${auth[@]}" | jq -c .

say "Переписка диалога"
curl -sf "${MOCK}/messages?chat_id=${CHAT_ID}" "${auth[@]}" | jq '.messages | length'

sleep 0.5
say "События, полученные стендом"
jq -r '"\(.update.update_type)\tсекрет: \(.secret)"' "$EVENTS_FILE"

say "Итог"
echo "Всего событий: $(wc -l < "$EVENTS_FILE" | tr -d ' ')"
echo "Веб-чат тестировщика: ${MOCK}/mock/chat/${BOT_ID}"
echo "Админка: ${MOCK}/mock"
