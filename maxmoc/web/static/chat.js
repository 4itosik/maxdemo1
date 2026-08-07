'use strict';

// Веб-чат тестировщика: он играет роль клиента мессенджера, а сообщения бота
// приходят от КЦ через Bot API. Всё живое — состояние обновляется по
// WebSocket, перезагружать страницу не нужно.

const botID = Number(location.pathname.split('/').pop());

const state = {
  clients: [],
  chatID: null,
  started: false,
  pendingAttachment: null,
};

const $ = (id) => document.getElementById(id);

const el = (tag, props = {}, children = []) => {
  const node = Object.assign(document.createElement(tag), props);
  for (const child of [].concat(children)) {
    if (child === null || child === undefined) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
};

async function request(path, options) {
  const r = await fetch(path, options);
  if (!r.ok) {
    let message = r.statusText;
    try {
      const body = await r.json();
      message = body.message || body.code || message;
    } catch { /* тело не JSON — оставляем статус */ }
    throw new Error(message);
  }
  return r.status === 204 ? null : r.json();
}

const getJSON = (path) => request(path);
const postJSON = (path, body) => request(path, {
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify(body),
});

const timeOf = (ms) => new Date(ms).toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'});

// Выбранный чат живёт в адресе (?chat=…): страницу можно перезагрузить или
// прислать ссылку коллеге — откроется тот же диалог. replaceState, а не
// pushState: «назад» должно возвращать в админку, а не листать чаты.
function chatFromURL() {
  const raw = new URLSearchParams(location.search).get('chat');
  const id = Number(raw);
  return raw && Number.isInteger(id) ? id : null;
}

function chatToURL(chatID) {
  const url = new URL(location.href);
  url.searchParams.set('chat', String(chatID));
  history.replaceState(null, '', url);
}

async function loadBot() {
  const bot = await getJSON(`/mock/api/bots/${botID}`);
  $('bot-title').textContent = `${bot.name} · @${bot.username}`;
  document.title = `max-mock — ${bot.name}`;
  renderCommands(bot.commands || []);
}

// renderCommands рисует меню команд бота — то, что он опубликовал через
// PATCH /me/commands.
//
// Клик отправляет обычное текстовое сообщение «/name»: в Max выбор команды из
// меню неотличим от того, что её набрали руками. Событие bot_started к
// командам отношения не имеет — его шлёт кнопка «Начать», и путать эти два
// пути не стоит.
//
// Команд нет — полосы нет, и это диагностика: значит, бот не звал
// PATCH /me/commands либо удалил команды пустым списком.
function renderCommands(commands) {
  const box = $('commands');
  box.textContent = '';
  box.hidden = !commands.length;
  for (const c of commands) {
    const chip = el('button', {
      type: 'button',
      textContent: '/' + c.name,
      title: c.description || '',
    });
    chip.onclick = () => act({action: 'send', text: '/' + c.name});
    box.append(chip);
  }
}

async function loadClients() {
  state.clients = await getJSON(`/mock/api/bots/${botID}/clients`);
  // Новые сверху: только что созданный клиент — тот, с которым сейчас
  // работают, и искать его в хвосте длинного списка незачем.
  state.clients.sort((a, b) => b.created_at - a.created_at || b.user_id - a.user_id);
  const box = $('clients');
  box.textContent = '';

  if (!state.clients.length) {
    box.append(el('div', {className: 'empty', style: 'padding:12px',
                          textContent: 'Клиентов нет. Создайте первого кнопкой «+».'}));
  }
  for (const c of state.clients) {
    const item = el('div', {className: 'client' + (c.chat_id === state.chatID ? ' active' : '')}, [
      el('div', {className: 'name', textContent: [c.first_name, c.last_name].filter(Boolean).join(' ')}),
      el('div', {className: 'muted mono', textContent: `user_id ${c.user_id}`}),
      // Телефон видно сразу: без него кнопка «поделиться контактом» откажет.
      c.phone ? el('div', {className: 'muted', textContent: c.phone}) : null,
      c.started ? null : el('span', {className: 'badge', textContent: 'не начат'}),
    ]);
    item.onclick = () => selectClient(c.chat_id);
    box.append(item);
  }

  if (state.chatID === null && state.clients.length) {
    // Чат из адреса мог быть удалён или принадлежать другому боту — тогда
    // честно открываем первый в списке, а не пустой экран.
    const wanted = chatFromURL();
    const target = state.clients.find((c) => c.chat_id === wanted) || state.clients[0];
    selectClient(target.chat_id);
  }
}

function currentClient() {
  return state.clients.find((c) => c.chat_id === state.chatID) || null;
}

async function selectClient(chatID) {
  state.chatID = chatID;
  chatToURL(chatID);
  const client = currentClient();
  state.started = client ? client.started : false;
  $('dialog-title').textContent = client ? `Диалог с ${client.first_name} · chat_id ${chatID}` : '';
  renderStartButton();
  await loadClients();
  await loadMessages();
  // Вкладки кроме «Чата» больше не завязаны на бота целиком — «События»
  // читают ленту конкретного диалога, и без перерисовки здесь она осталась
  // бы от прежнего клиента, а заголовок уже говорил бы про нового.
  await panels[activeTab]();
}

// renderStartButton держит подпись кнопки в согласии с состоянием диалога.
// Кнопка не прячется после первого нажатия: bot_started приходит и при
// возобновлении общения, а проверять это нужно, не заводя нового клиента.
function renderStartButton() {
  const btn = $('start-btn');
  btn.textContent = state.started ? 'Остановить' : 'Начать';
  btn.title = state.started
    ? 'Остановить бота в настройках — уйдёт bot_stopped'
    : 'Начать разговор — уйдёт bot_started';
}

async function loadMessages() {
  if (state.chatID === null) return;
  const messages = await getJSON(`/mock/api/dialogs/${state.chatID}/messages`);
  const box = $('messages');
  box.textContent = '';
  for (const m of messages) {
    box.append(renderMessage(m));
  }
  box.scrollTop = box.scrollHeight;
}

function renderMessage(m) {
  const mine = m.sender === 'client';
  const wrap = el('div', {className: `msg from-${m.sender}` + (m.status === 'removed' ? ' removed' : '')});
  const bubble = el('div', {className: 'bubble'});

  bubble.append(el('div', {textContent: m.body.text || (m.status === 'removed' ? 'сообщение удалено' : '')}));
  for (const att of m.body.attachments || []) {
    bubble.append(renderAttachment(att, m.mid));
  }
  wrap.append(bubble);

  const meta = el('div', {className: 'msg-meta'}, [
    timeOf(m.created_at),
    m.edited_at ? 'изменено' : null,
    el('span', {className: 'mono', textContent: m.mid.slice(0, 12) + '…', title: m.mid}),
  ]);
  if (mine && m.status !== 'removed') {
    const edit = el('button', {className: 'link', textContent: 'править'});
    edit.onclick = async () => {
      const text = prompt('Новый текст сообщения', m.body.text || '');
      if (text === null) return;
      await act({action: 'edit', mid: m.mid, text});
    };
    const del = el('button', {className: 'link danger', textContent: 'удалить'});
    del.onclick = () => act({action: 'delete', mid: m.mid});
    meta.append(edit, del);
  }
  wrap.append(meta);
  return wrap;
}

// renderButton оживляет кнопку клавиатуры.
//
// Типы у inline- и reply-клавиатуры называются по-разному: у reply это
// user_contact, user_geo_location и message. Разбор живёт здесь, а не в ядре:
// ядру достаточно трёх действий клиента, а какое из них вызвать — знает тот,
// кто видит нажатую кнопку.
function renderButton(btn, mid) {
  const b = el('button', {type: 'button', textContent: btn.text});
  switch (btn.type) {
    case 'callback':
      b.onclick = () => act({action: 'press', mid, payload: btn.payload});
      break;
    case 'link':
      b.onclick = () => window.open(btn.url, '_blank', 'noopener');
      break;
    case 'request_contact':
    case 'user_contact':
      b.onclick = () => act({action: 'send_contact'});
      break;
    case 'request_geo_location':
    case 'user_geo_location':
      // quick: true — «без запроса подтверждения», как описано в контракте.
      b.onclick = () => btn.quick ? act({action: 'send_location'}) : askLocation();
      break;
    case 'message':
      // Контракт не предусматривает поля, в котором до бота доехал бы токен
      // кнопки: уходит обычное сообщение с её текстом.
      b.onclick = () => act({action: 'send', text: btn.text});
      break;
    case 'clipboard':
      b.onclick = async () => {
        try {
          await navigator.clipboard.writeText(btn.payload || '');
          b.textContent = 'скопировано';
          setTimeout(() => { b.textContent = btn.text; }, 1500);
        } catch {
          alert('Текст кнопки: ' + (btn.payload || ''));
        }
      };
      break;
    default:
      // chat требует группы /chats, open_app — мини-приложения; обе вне
      // объёма мока, и делать вид, что они работают, вреднее, чем честно
      // это показать.
      b.disabled = true;
      b.title = `кнопка типа ${btn.type} моком не обрабатывается`;
  }
  return b;
}

// askLocation спрашивает координаты, подставляя значения из карточки клиента.
async function askLocation() {
  const client = state.clients.find((c) => c.chat_id === state.chatID);
  const preset = client && client.latitude != null ? `${client.latitude}, ${client.longitude}` : '';
  const answer = prompt('Координаты через запятую (широта, долгота)', preset);
  if (answer === null) return;
  const parts = answer.split(',').map((p) => parseFloat(p.trim()));
  if (answer.trim() && (parts.length !== 2 || parts.some(Number.isNaN))) {
    alert('Нужны два числа через запятую, например: 55.751244, 37.618423');
    return;
  }
  await act(answer.trim()
    ? {action: 'send_location', latitude: parts[0], longitude: parts[1]}
    : {action: 'send_location'});
}

function renderAttachment(att, mid) {
  if (att.type === 'inline_keyboard' || att.type === 'reply_keyboard') {
    const rows = att.type === 'inline_keyboard' ? (att.payload?.buttons || []) : (att.buttons || []);
    const kb = el('div', {className: 'keyboard'});
    for (const row of rows) {
      const line = el('div', {className: 'kb-row'});
      for (const btn of row) {
        line.append(renderButton(btn, mid));
      }
      kb.append(line);
    }
    return kb;
  }

  if (att.type === 'location') {
    return el('div', {className: 'attachment'},
      el('span', {textContent: `📍 ${att.latitude}, ${att.longitude}`}));
  }

  if (att.type === 'contact') {
    // vcf_info — VCARD; для карточки в чате хватает имени и телефона.
    const vcf = att.payload?.vcf_info || '';
    const name = (vcf.match(/^FN:(.*)$/m) || [])[1] || '';
    const phone = (vcf.match(/^TEL[^:]*:(.*)$/m) || [])[1] || '';
    return el('div', {className: 'attachment'},
      el('span', {textContent: `👤 ${[name, phone].filter(Boolean).join(' · ') || 'контакт'}`}));
  }

  const url = att.payload?.url;
  if (att.type === 'image' && url) {
    return el('div', {className: 'attachment'}, el('img', {src: url, alt: 'изображение'}));
  }
  const label = att.filename || att.type;
  return el('div', {className: 'attachment'},
    url ? el('a', {href: url, target: '_blank', rel: 'noopener', textContent: '📎 ' + label})
        : el('span', {className: 'muted', textContent: '📎 ' + label}));
}

async function act(payload) {
  try {
    await postJSON(`/mock/api/dialogs/${state.chatID}/actions`, payload);
  } catch (err) {
    alert('Действие не выполнено: ' + err.message);
  }
}

$('add-client').onclick = () => {
  const form = $('client-form');
  form.reset();
  form.first_name.value = 'Клиент ' + (state.clients.length + 1);
  $('client-dialog').showModal();
};

$('client-cancel').onclick = () => $('client-dialog').close();

$('client-form').onsubmit = async (ev) => {
  ev.preventDefault();
  const form = ev.target;
  const body = {
    first_name: form.first_name.value.trim(),
    last_name: form.last_name.value.trim(),
    username: form.username.value.trim(),
    phone: form.phone.value.trim(),
  };

  const coords = form.coords.value.trim();
  if (coords) {
    const parts = coords.split(',').map((p) => parseFloat(p.trim()));
    if (parts.length !== 2 || parts.some(Number.isNaN)) {
      alert('Координаты — два числа через запятую, например: 55.751244, 37.618423');
      return;
    }
    [body.latitude, body.longitude] = parts;
  }

  const userID = form.user_id.value.trim();
  if (userID) {
    if (!/^\d+$/.test(userID)) {
      alert('user_id — целое число');
      return;
    }
    body.user_id = Number(userID);
  }

  try {
    const created = await postJSON(`/mock/api/bots/${botID}/clients`, body);
    $('client-dialog').close();
    await loadClients();
    await selectClient(created.dialog.chat_id);
  } catch (err) {
    alert('Не удалось создать клиента: ' + err.message);
  }
};

$('start-btn').onclick = async () => {
  await act({action: state.started ? 'stop' : 'start'});
  // Состояние берётся из ответа сервера, а не инвертируется на месте:
  // act() гасит ошибку алертом и наружу её не отдаёт, так что по факту
  // вызова судить об успехе нельзя, а подпись кнопки — единственный
  // постоянный признак того, начат диалог или нет.
  //
  // Обновление списка прикрыто отдельно: подпись обязана быть перерисована
  // даже когда список не доехал, иначе она замрёт на значении до клика —
  // ровно то расхождение, ради которого состояние и берётся с сервера.
  try {
    await loadClients();
  } catch (err) {
    alert('Список клиентов не обновлён: ' + err.message);
  }
  const client = currentClient();
  state.started = client ? client.started : false;
  renderStartButton();
};

$('attach').onclick = () => $('file').click();

// Контакт и геопозицию можно отправить и без кнопки от бота: в Max так
// можно, и обработку на стороне КЦ надо проверять в обоих случаях.
$('send-contact').onclick = () => act({action: 'send_contact'});
$('send-location').onclick = () => askLocation();

$('file').onchange = async () => {
  const file = $('file').files[0];
  if (!file) return;
  const form = new FormData();
  form.append('file', file);
  try {
    const att = await request(`/mock/api/bots/${botID}/files`, {method: 'POST', body: form});
    state.pendingAttachment = att.token;
    $('text').placeholder = `Файл «${att.filename}» готов к отправке`;
  } catch (err) {
    alert('Файл не загружен: ' + err.message);
  }
  $('file').value = '';
};

$('composer').onsubmit = async (ev) => {
  ev.preventDefault();
  if (state.chatID === null) return;
  const text = $('text').value.trim();
  const attachments = state.pendingAttachment ? [state.pendingAttachment] : [];
  if (!text && !attachments.length) return;

  await act({action: 'send', text, attachments});
  $('text').value = '';
  $('text').placeholder = 'Сообщение от лица клиента';
  state.pendingAttachment = null;
};

// ---- вкладки ----

const panels = {
  chat: () => {
    $('messages').classList.remove('hidden');
    $('composer').classList.remove('hidden');
  },
  events: async () => {
    const box = $('events-panel');
    // Диалога ещё нет (свежий бот без клиентов) — /dialogs/null/events только
    // упадёт запросом. Показываем ту же фразу, что и заголовок диалога до
    // выбора клиента, а не тишину с необработанной ошибкой.
    if (state.chatID === null) {
      box.textContent = '';
      box.append(el('div', {className: 'empty', textContent: 'Выберите клиента.'}));
      return;
    }
    const entries = await getJSON(`/mock/api/dialogs/${state.chatID}/events?limit=200`);
    renderEvents(box, entries);
  },
  log: async () => {
    const entries = await getJSON(`/mock/api/bots/${botID}/log?limit=50`);
    renderLog($('log-panel'), entries, (e) => `${e.method} ${e.path}${e.query ? '?' + e.query : ''}`,
      (e) => e.status, (e) => e.response_body);
  },
  deliveries: async () => {
    const entries = await getJSON(`/mock/api/bots/${botID}/deliveries?limit=50`);
    renderLog($('deliveries-panel'), entries,
      (e) => `${e.update_type} → ${e.url || 'не отправлено'} (попытка ${e.attempt})`,
      (e) => e.status, (e) => e.error || e.body);
  },
};

function renderLog(box, entries, title, status, body) {
  box.textContent = '';
  if (!entries.length) {
    box.append(el('div', {className: 'empty', textContent: 'Пока пусто.'}));
    return;
  }
  for (const e of entries) {
    const code = status(e);
    const entry = el('div', {className: 'log-entry'}, [
      el('div', {className: 'row'}, [
        el('span', {className: 'badge ' + (code >= 200 && code < 400 ? 'ok' : 'err'),
                    textContent: code ? String(code) : 'нет ответа'}),
        el('code', {textContent: title(e)}),
        el('span', {className: 'muted', textContent: timeOf(e.ts)}),
      ]),
    ]);
    const payload = body(e);
    if (payload) entry.append(el('pre', {textContent: pretty(payload)}));
    box.append(entry);
  }
}

// BODY_LINES — сколько строк тела показывать до «развернуть». Один
// message_created целиком — это около сорока строк, и лента из двух таких
// перестала бы быть лентой.
const BODY_LINES = 18;

// renderEvents рисует ленту диалога: обмены между моком и ботом, новые
// сверху. Каждая запись — пара «одна сторона прислала JSON → другая
// ответила»; порознь их читать нельзя, а из двух журналов по отдельности
// пару не собрать.
//
// Действий тестировщика здесь нет: он только что совершил их сам, а отказ
// увидел алертом в момент клика.
function renderEvents(box, entries) {
  box.textContent = '';
  if (!entries.length) {
    box.append(el('div', {className: 'empty', textContent: 'Пока пусто.'}));
    return;
  }
  for (const e of entries) {
    box.append(e.kind === 'delivery' ? deliveryCard(e) : requestCard(e));
  }
}

// deliveryCard — обмен «max → бот»: мок отправил событие на стенд, стенд
// ответил.
function deliveryCard(e) {
  const d = e.delivery;
  const card = exchangeCard('max → бот', 'out', d.update_type, d.status, e.ts,
    d.attempt > 1 ? `попытка ${d.attempt}` : null);
  // url пуст — событие мок собрал, но никуда не отправил. Подпись «отправлено»
  // на нём была бы ложью, а тело показать всё равно надо: по нему видно, что
  // именно стенд не получил.
  card.append(d.url
    ? exchangePart('▸ отправлено', 'POST ' + d.url, d.body, '(пустое тело)')
    : exchangePart('▸ событие не отправлено', null, d.body, '(пустое тело)'));
  // Ошибка означает, что ответа не было: событие никуда не ушло (нет
  // подписки, не прошло валидацию) либо связь оборвалась. Пустой блок ответа
  // на этом месте читался бы как «стенд ответил пустотой».
  card.append(d.error
    ? exchangeFailure(d.error)
    : exchangePart('◂ стенд ответил', statusLine(d.status, d.duration_ms),
      d.response_body, '(пустое тело)'));
  return card;
}

// requestCard — обмен «бот → max»: бот вызвал Bot API, мок ответил.
function requestCard(e) {
  const q = e.request;
  const title = `${q.method} ${q.path}${q.query ? '?' + q.query : ''}`;
  const card = exchangeCard('бот → max', 'in', title, q.status, e.ts, null);
  // У GET и DELETE тела нет вовсе — сам запрос уже описан заголовком карточки.
  card.append(exchangePart('▸ бот прислал', null, q.request_body, '(без тела)'));
  card.append(exchangePart('◂ max ответил', statusLine(q.status, q.latency_ms),
    q.response_body, '(пустое тело)'));
  if (q.error) card.append(exchangeFailure(q.error));
  return card;
}

// exchangeCard — рамка обмена: полоса по направлению и шапка. Направление
// продублировано словами: цвет полосы один смысл нести не должен.
function exchangeCard(direction, side, title, status, ts, note) {
  return el('div', {className: 'exchange ' + side},
    el('div', {className: 'row exch-head'}, [
      el('span', {className: 'badge', textContent: direction}),
      el('span', {className: 'badge ' + (status >= 200 && status < 400 ? 'ok' : 'err'),
                  textContent: status ? String(status) : 'нет ответа'}),
      el('code', {textContent: title}),
      note ? el('span', {className: 'muted', textContent: note}) : null,
      el('span', {className: 'muted exch-time', textContent: timeOf(ts)}),
    ]));
}

// exchangePart — половина обмена: подпись, уточнение и тело JSON.
function exchangePart(label, detail, payload, emptyText) {
  const box = el('div', {className: 'exch-part'},
    el('div', {className: 'exch-label'}, [
      el('strong', {textContent: label}),
      detail ? el('span', {className: 'muted', textContent: detail}) : null,
    ]));
  if (!payload) {
    box.append(el('div', {className: 'muted exch-empty', textContent: emptyText}));
    return box;
  }

  // Длинное тело режется по строкам, а не прячется под max-height: блок с
  // ограниченной высотой становится собственным скролл-контейнером и
  // перехватывает колесо мыши — прокрутка ленты застревает на каждом теле по
  // очереди. Обрезанного текста просто нет в документе, и перехватывать
  // нечего.
  const full = pretty(payload);
  const lines = full.split('\n');
  const short = lines.slice(0, BODY_LINES).join('\n');
  const pre = el('pre', {textContent: lines.length > BODY_LINES ? short : full});
  box.append(pre);

  if (lines.length > BODY_LINES) {
    const collapsed = `развернуть (ещё ${lines.length - BODY_LINES} стр.)`;
    const more = el('button', {className: 'link exch-more', textContent: collapsed});
    more.onclick = () => {
      const open = pre.textContent === full;
      pre.textContent = open ? short : full;
      more.textContent = open ? collapsed : 'свернуть';
    };
    box.append(more);
  }
  return box;
}

// exchangeFailure — красная строка на месте блока ответа: ответу взяться
// неоткуда, и причина важнее пустоты.
function exchangeFailure(text) {
  return el('div', {className: 'exch-part'},
    el('div', {className: 'exch-label exch-fail'}, el('strong', {textContent: '✕ ' + text})));
}

// statusLine — «200 · 43 мс».
function statusLine(status, ms) {
  const code = status ? String(status) : 'ответа не было';
  return ms == null ? code : `${code} · ${ms} мс`;
}

function pretty(raw) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

let activeTab = 'chat';
for (const button of document.querySelectorAll('.tabs button')) {
  button.onclick = async () => {
    activeTab = button.dataset.tab;
    for (const b of document.querySelectorAll('.tabs button')) {
      b.classList.toggle('active', b === button);
    }
    $('messages').classList.toggle('hidden', activeTab !== 'chat');
    $('composer').classList.toggle('hidden', activeTab !== 'chat');
    $('events-panel').classList.toggle('active', activeTab === 'events');
    $('log-panel').classList.toggle('active', activeTab === 'log');
    $('deliveries-panel').classList.toggle('active', activeTab === 'deliveries');
    await panels[activeTab]();
  };
}

// ---- живое обновление ----

function connect() {
  const badge = $('ws-state');
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}/mock/ws?bot_id=${botID}`);

  ws.onopen = () => { badge.textContent = 'на связи'; badge.className = 'badge ok'; };
  ws.onclose = () => {
    badge.textContent = 'соединение потеряно';
    badge.className = 'badge err';
    setTimeout(connect, 2000);
  };
  ws.onmessage = async (ev) => {
    const event = JSON.parse(ev.data);
    if (['message', 'message_edited', 'message_removed'].includes(event.kind)) {
      if (event.chat_id === state.chatID && activeTab === 'chat') await loadMessages();
    }
    if (event.kind === 'dialog' || event.kind === 'client') await loadClients();
    if (event.kind === 'bot') await loadBot();
    if (event.kind === 'request' && activeTab === 'log') await panels.log();
    if (event.kind === 'delivery' && activeTab === 'deliveries') await panels.deliveries();
    // Лента показывает обмены только этого диалога. chat_id у события шины
    // помечен omitempty: у вызовов бота вне диалога (GET /me, PATCH
    // /me/commands, подписки, загрузки) его нет вовсе, и сравнение с
    // state.chatID отсекает их само — в ленте их больше нет, и перерисовывать
    // по ним нечего. Чужой диалог отсекается тем же сравнением.
    if (activeTab === 'events' && ['delivery', 'request'].includes(event.kind) &&
        event.chat_id === state.chatID) {
      await panels.events();
    }
  };
}

loadBot().catch((err) => alert('Бот не найден: ' + err.message));
loadClients();
connect();
