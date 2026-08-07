'use strict';

// Админка мока: регистрация ботов, их токены и подписки, общий журнал
// вызовов Bot API. Живое обновление — через тот же WebSocket, что и чат.

const api = {
  async get(path) {
    const r = await fetch(path);
    if (!r.ok) throw new Error(await describe(r));
    return r.json();
  },
  async post(path, body) {
    const r = await fetch(path, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(body),
    });
    if (!r.ok) throw new Error(await describe(r));
    return r.json();
  },
};

async function describe(response) {
  try {
    const body = await response.json();
    return body.message || body.code || response.statusText;
  } catch {
    return response.statusText;
  }
}

const el = (tag, props = {}, children = []) => {
  const node = Object.assign(document.createElement(tag), props);
  for (const child of [].concat(children)) {
    // null означает «этого элемента здесь нет» — иначе в разметку попало бы
    // слово «null».
    if (child === null || child === undefined) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
};

const timeOf = (ms) => new Date(ms).toLocaleTimeString('ru-RU');

// Фильтр журнала: пустая строка — все боты. Хранится здесь же, чтобы живое
// обновление по WebSocket перерисовывало журнал в том же разрезе, а не
// сбрасывало выбор пользователя.
const state = {
  logBotID: '',
  logLimit: '50',
  botNames: new Map(),
};

async function loadBots() {
  const bots = await api.get('/mock/api/bots');
  const tbody = document.querySelector('#bots tbody');
  tbody.textContent = '';
  document.getElementById('bots-empty').style.display = bots.length ? 'none' : '';

  state.botNames = new Map(bots.map((b) => [String(b.id), b.name]));
  syncBotFilter(bots);

  for (const bot of bots) {
    const token = el('code', {textContent: bot.token, title: 'нажмите, чтобы скопировать',
                              style: 'cursor:pointer'});
    token.onclick = async () => {
      try {
        await navigator.clipboard.writeText(bot.token);
        token.textContent = 'скопировано';
        setTimeout(() => { token.textContent = bot.token; }, 1200);
      } catch {
        // Буфер обмена недоступен без защищённого контекста — не беда,
        // токен виден целиком и его можно выделить мышью.
      }
    };

    const subs = el('span', {className: 'muted', textContent: '…'});
    api.get(`/mock/api/bots/${bot.id}/subscriptions`).then((list) => {
      subs.textContent = '';
      if (!list.length) {
        subs.append(el('span', {className: 'muted', textContent: 'нет'}));
        return;
      }
      for (const s of list) {
        const types = s.update_types ? s.update_types.join(', ') : 'все события';
        subs.append(el('div', {}, [el('code', {textContent: s.url}), ' ',
                                   el('span', {className: 'badge', textContent: types})]));
      }
    }).catch(() => { subs.textContent = 'ошибка'; });

    const open = el('button', {className: 'secondary', textContent: 'Открыть чат'});
    open.onclick = () => { location.href = `/mock/chat/${bot.id}`; };

    tbody.append(el('tr', {}, [
      el('td', {textContent: bot.name}),
      el('td', {}, el('code', {textContent: '@' + bot.username})),
      el('td', {}, el('code', {textContent: String(bot.user_id)})),
      el('td', {}, token),
      el('td', {}, subs),
      el('td', {}, open),
    ]));
  }
}

// syncBotFilter обновляет список ботов в выпадающем списке, сохраняя выбор.
function syncBotFilter(bots) {
  const select = document.getElementById('log-bot');
  const chosen = state.logBotID;
  select.textContent = '';
  select.append(el('option', {value: '', textContent: 'все боты'}));
  for (const bot of bots) {
    select.append(el('option', {value: String(bot.id), textContent: `${bot.name} (@${bot.username})`}));
  }
  // Выбранный бот мог исчезнуть — тогда честно возвращаемся ко «всем».
  select.value = [...select.options].some((o) => o.value === chosen) ? chosen : '';
  state.logBotID = select.value;
}

// logURL строит адрес журнала: по боту или общий.
function logURL() {
  const limit = `limit=${encodeURIComponent(state.logLimit)}`;
  return state.logBotID
    ? `/mock/api/bots/${encodeURIComponent(state.logBotID)}/log?${limit}`
    : `/mock/api/log?${limit}`;
}

async function loadLog() {
  const entries = await api.get(logURL());
  const box = document.getElementById('log');
  box.textContent = '';
  if (!entries.length) {
    box.append(el('div', {
      className: 'empty',
      textContent: state.logBotID ? 'У этого бота вызовов пока не было.' : 'Вызовов пока не было.',
    }));
    return;
  }
  for (const e of entries) {
    box.append(renderLogEntry(e));
  }
}

function renderLogEntry(e) {
  const head = el('div', {className: 'row'}, [
    el('span', {
      className: 'badge ' + (e.status < 400 ? 'ok' : 'err'),
      textContent: String(e.status),
    }),
    el('code', {textContent: `${e.method} ${e.path}${e.query ? '?' + e.query : ''}`}),
    botBadge(e),
    el('span', {className: 'muted', textContent: `${e.latency_ms} мс · ${timeOf(e.ts)}`}),
  ]);

  const entry = el('div', {className: 'log-entry'}, head);
  // Журнал существует ради отладки обмена «стенд ↔ мок», поэтому видны обе
  // стороны: что прислал КЦ и что ответил мок.
  if (e.request_body) {
    entry.append(el('div', {className: 'muted', textContent: 'запрос'}));
    entry.append(el('pre', {textContent: pretty(e.request_body)}));
  }
  if (e.response_body) {
    entry.append(el('div', {className: 'muted', textContent: 'ответ'}));
    entry.append(el('pre', {textContent: pretty(e.response_body)}));
  }
  return entry;
}

// botBadge подписывает, чей это вызов. Показывается только в общем режиме:
// при выборе конкретного бота подпись была бы шумом.
function botBadge(e) {
  if (state.logBotID) return null;
  if (e.bot_id === null || e.bot_id === undefined) {
    return el('span', {className: 'badge err', textContent: 'без авторизации'});
  }
  const name = state.botNames.get(String(e.bot_id)) || `бот ${e.bot_id}`;
  return el('span', {className: 'badge', textContent: name});
}

function pretty(raw) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

document.getElementById('bot-form').onsubmit = async (ev) => {
  ev.preventDefault();
  try {
    await api.post('/mock/api/bots', {
      name: document.getElementById('bot-name').value.trim(),
      username: document.getElementById('bot-username').value.trim(),
      description: document.getElementById('bot-description').value.trim(),
    });
    ev.target.reset();
    await loadBots();
  } catch (err) {
    alert('Не удалось зарегистрировать бота: ' + err.message);
  }
};

function connect() {
  const state = document.getElementById('ws-state');
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}/mock/ws`);

  ws.onopen = () => { state.textContent = 'на связи'; state.className = 'badge ok'; };
  ws.onclose = () => {
    state.textContent = 'соединение потеряно';
    state.className = 'badge err';
    setTimeout(connect, 2000);
  };
  ws.onmessage = (ev) => {
    const event = JSON.parse(ev.data);
    if (event.kind === 'request') loadLog();
    if (event.kind === 'client') loadBots();
  };
}

document.getElementById('log-bot').onchange = (ev) => {
  state.logBotID = ev.target.value;
  loadLog();
};

document.getElementById('log-limit').onchange = (ev) => {
  state.logLimit = ev.target.value;
  loadLog();
};

loadBots().then(loadLog).catch((err) => alert('Не удалось загрузить данные: ' + err.message));
connect();
