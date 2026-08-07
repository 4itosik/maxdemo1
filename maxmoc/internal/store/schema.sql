-- Схема max-mock. Все таймстемпы — unix-миллисекунды, как в wire-объектах Max.

CREATE TABLE IF NOT EXISTS bots (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL UNIQUE,   -- user_id бота в терминах Max
    name        TEXT    NOT NULL,
    username    TEXT    NOT NULL,
    token       TEXT    NOT NULL UNIQUE,   -- access_token, выдаётся при регистрации
    description TEXT    NOT NULL DEFAULT '',
    commands    TEXT    NOT NULL DEFAULT '[]',  -- JSON-массив BotCommand
    created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS clients (
    user_id    INTEGER PRIMARY KEY,        -- виртуальный пользователь Max
    bot_id     INTEGER NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    first_name TEXT    NOT NULL,
    last_name  TEXT,
    username   TEXT,
    phone      TEXT,                       -- уезжает к боту только вложением contact
    latitude   REAL,                       -- координаты по умолчанию для вложения location
    longitude  REAL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_clients_bot ON clients(bot_id);

CREATE TABLE IF NOT EXISTS dialogs (
    chat_id    INTEGER PRIMARY KEY,
    bot_id     INTEGER NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES clients(user_id) ON DELETE CASCADE,
    started    INTEGER NOT NULL DEFAULT 0, -- нажата ли кнопка «Начать»
    created_at INTEGER NOT NULL,
    UNIQUE (bot_id, user_id)
);

CREATE TABLE IF NOT EXISTS messages (
    mid        TEXT    PRIMARY KEY,
    chat_id    INTEGER NOT NULL REFERENCES dialogs(chat_id) ON DELETE CASCADE,
    bot_id     INTEGER NOT NULL,
    seq        INTEGER NOT NULL,
    sender     TEXT    NOT NULL,           -- 'bot' | 'client'
    body       TEXT    NOT NULL,           -- JSON wire.MessageBody
    status     TEXT    NOT NULL,           -- 'active' | 'removed'
    created_at INTEGER NOT NULL,
    edited_at  INTEGER,
    UNIQUE (chat_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_messages_chat ON messages(chat_id, seq);

CREATE TABLE IF NOT EXISTS callbacks (
    callback_id TEXT    PRIMARY KEY,
    bot_id      INTEGER NOT NULL,
    chat_id     INTEGER NOT NULL,
    mid         TEXT    NOT NULL,          -- сообщение с кнопкой
    user_id     INTEGER NOT NULL,          -- кто нажал
    payload     TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    answered_at INTEGER
);

CREATE TABLE IF NOT EXISTS attachments (
    token      TEXT    PRIMARY KEY,
    bot_id     INTEGER,
    type       TEXT    NOT NULL,           -- image | video | audio | file
    filename   TEXT    NOT NULL DEFAULT '',
    mime       TEXT    NOT NULL DEFAULT '',
    size       INTEGER NOT NULL DEFAULT 0,
    path       TEXT    NOT NULL DEFAULT '',
    uploaded   INTEGER NOT NULL DEFAULT 0, -- файл уже залит на upload-эндпоинт
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    bot_id       INTEGER NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    url          TEXT    NOT NULL,
    update_types TEXT,                     -- JSON-массив; NULL — все типы
    secret       TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    UNIQUE (bot_id, url)
);

CREATE TABLE IF NOT EXISTS request_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            INTEGER NOT NULL,
    bot_id        INTEGER,
    chat_id       INTEGER,
    method        TEXT    NOT NULL,
    path          TEXT    NOT NULL,
    query         TEXT    NOT NULL DEFAULT '',
    status        INTEGER NOT NULL,
    request_body  TEXT    NOT NULL DEFAULT '',
    response_body TEXT    NOT NULL DEFAULT '',
    latency_ms    INTEGER NOT NULL,
    error         TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_request_log_bot_ts ON request_log(bot_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_request_log_ts ON request_log(ts);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              INTEGER NOT NULL,
    bot_id          INTEGER NOT NULL,
    chat_id         INTEGER,
    subscription_id INTEGER,
    url             TEXT    NOT NULL,
    update_type     TEXT    NOT NULL,
    body            TEXT    NOT NULL,
    attempt         INTEGER NOT NULL,
    status          INTEGER NOT NULL,      -- HTTP-статус; 0 — ответа не было
    error           TEXT    NOT NULL DEFAULT '',
    response_body   TEXT    NOT NULL DEFAULT '',  -- ответ стенда, обрезан по потолку
    duration_ms     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_deliveries_bot_ts ON webhook_deliveries(bot_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_deliveries_ts ON webhook_deliveries(ts);

-- Действия тестировщика в веб-чате. Отдельная таблица, а не общая с журналом
-- вызовов: у действия нет ни метода с путём, ни url с попыткой, и половина
-- чужих столбцов стояла бы пустой.
CREATE TABLE IF NOT EXISTS ui_actions (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    ts      INTEGER NOT NULL,
    bot_id  INTEGER NOT NULL,
    chat_id INTEGER NOT NULL,
    action  TEXT    NOT NULL,
    detail  TEXT    NOT NULL DEFAULT '',
    error   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ui_actions_chat_ts ON ui_actions(chat_id, ts DESC);
