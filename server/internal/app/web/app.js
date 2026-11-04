'use strict';

// All text returned by the API is rendered with textContent, never innerHTML.
const state = { me: null, channels: [], sources: [], messages: [], cursor: '', hasMore: false, stream: null, tokenSource: null, editingToken: null, resetUser: null, editingEntity: null };
const $ = (id) => document.getElementById(id);
const el = (tag, text, cls) => {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = String(text);
  if (cls) node.className = cls;
  return node;
};
const formData = (form) => Object.fromEntries(new FormData(form));
let statusTimer;
function notice(message, error = false) {
  const node = $('status');
  node.textContent = message;
  node.classList.toggle('error', error);
  clearTimeout(statusTimer);
  if (message) statusTimer = setTimeout(() => { node.textContent = ''; }, 6500);
}
async function api(path, options = {}) {
  const headers = { ...options.headers };
  if (options.body && !(options.body instanceof FormData)) headers['Content-Type'] = 'application/json';
  const response = await fetch('/api/v1' + path, { ...options, headers, credentials: 'same-origin' });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error?.message || `HTTP ${response.status}`);
  return data;
}
const button = (label, callback, type = 'secondary') => {
  const node = el('button', label, type);
  node.type = 'button';
  node.addEventListener('click', callback);
  return node;
};
function showTab(id) {
  document.querySelectorAll('.nav-item').forEach((node) => node.classList.toggle('active', node.dataset.tab === id));
  document.querySelectorAll('.view').forEach((node) => { node.hidden = node.id !== id; });
  window.scrollTo({ top: 0, behavior: 'instant' });
}
function setAuth(user) {
  state.me = user;
  $('login-panel').hidden = Boolean(user);
  $('app-panel').hidden = !user;
  $('logout').hidden = !user;
  $('change-password').hidden = !user;
  $('identity').textContent = user ? `${user.username} · ${user.role === 'admin' ? 'администратор' : 'пользователь'}` : '';
  $('admin-tab').hidden = !user || user.role !== 'admin';
}
function updateOverview() {
  $('nav-channels').textContent = String(state.channels.length);
  const unread = state.messages.filter((m) => !m.read_at).length;
  $('nav-unread').textContent = String(unread);
  $('nav-unread').hidden = unread === 0;
  $('message-count').textContent = `${state.messages.length} сообщений · ${unread} непрочитанных`;
  $('onboarding').hidden = state.channels.length > 0;
  $('source-helper').hidden = state.channels.length > 0;
}
async function refreshAll() {
  const [channels, sources, messages] = await Promise.all([
    api('/channels'), api('/sources'), api('/messages?limit=200')
  ]);
  state.channels = channels.items;
  state.sources = sources.items;
  state.messages = messages.items;
  state.cursor = messages.next_cursor;
  state.hasMore = messages.has_more;
  renderChannels();
  renderSources();
  renderMessages();
  if (state.me?.role === 'admin') await renderUsers();
}

// Channel names are user-facing; slugs are generated for API compatibility.
const ruLetters = {
  а:'a',б:'b',в:'v',г:'g',д:'d',е:'e',ё:'e',ж:'zh',з:'z',и:'i',й:'y',к:'k',л:'l',м:'m',н:'n',о:'o',п:'p',р:'r',с:'s',т:'t',у:'u',ф:'f',х:'h',ц:'ts',ч:'ch',ш:'sh',щ:'sch',ъ:'',ы:'y',ь:'',э:'e',ю:'yu',я:'ya'
};
function autoSlug(name) {
  let text = Array.from(name.toLowerCase(), (c) => ruLetters[c] ?? c).join('');
  text = text.normalize('NFKD').replace(/[\u0300-\u036f]/g, '');
  text = text.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 80).replace(/-+$/, '');
  return text || 'channel';
}
function openForm(name) {
  const panel = $(`${name}-form-panel`);
  panel.hidden = false;
  panel.querySelector('input')?.focus();
  panel.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
}
function closeForm(name) {
  $(`${name}-form-panel`).hidden = true;
  $(`${name}-form`).reset();
  if (name === 'channel') slugEdited = false;
}
function renderChannels() {
  const list = $('channel-list');
  const filter = $('filter-channel');
  const selected = filter.value;
  list.replaceChildren();
  filter.replaceChildren(new Option('Все каналы', ''));
  for (const channel of state.channels) {
    filter.add(new Option(channel.name, channel.slug));
    const card = el('article', undefined, 'list-card');
    const icon = el('div', '◫', 'item-icon');
    const body = el('div', undefined, 'item-body');
    body.append(el('h3', channel.name), el('code', channel.slug, 'slug'));
    if (channel.description) body.append(el('p', channel.description, 'muted-line'));
    const url = new URL('/api/v1/messages', window.location.origin);
    url.searchParams.set('channel', channel.slug);
    const urlRow = el('div', undefined, 'channel-url');
    urlRow.append(el('span', 'Адрес для отправки', 'meta'), el('code', url.toString(), 'channel-endpoint'));
    urlRow.append(button('Копировать адрес', async () => {
      try { await navigator.clipboard.writeText(url.toString()); notice('Адрес канала скопирован'); }
      catch (err) { notice('Не удалось скопировать адрес: ' + err.message, true); }
    }, 'secondary'));
    body.append(urlRow);
    const actions = el('div', undefined, 'actions');
    actions.append(
      button('Изменить', () => openEntityEditor('channels', channel)),
      button('Токены', () => showChannelTokens(channel)),
      button('Уведомления', () => { filter.value = channel.slug; showTab('inbox'); renderMessages(); })
    );
    card.append(icon, body, actions);
    const details = el('div', undefined, 'channel-token-details');
    details.id = 'channel-tokens-' + channel.id;
    details.hidden = true;
    card.append(details);
    list.append(card);
  }
  filter.value = state.channels.some((c) => c.slug === selected) ? selected : '';
  if (!state.channels.length) {
    const empty = el('div', undefined, 'panel empty-state');
    empty.append(el('div', '◫', 'empty-icon'), el('h2', 'Каналов пока нет'), el('p', 'Создайте первый канал. Например, «Дом», «Серверы» или «Резервные копии».', 'hint'));
    empty.append(button('+ Создать канал', () => openForm('channel'), 'primary'));
    list.append(empty);
  }
  updateOverview();
}

async function showChannelTokens(channel, force = false) {
  const host = $('channel-tokens-' + channel.id);
  if (!host) return;
  if (!force && !host.hidden) { host.hidden = true; host.replaceChildren(); return; }
  host.hidden = false;
  host.replaceChildren(el('p', 'Получаем список токенов…', 'hint'));
  try {
    const results = await Promise.all(state.sources.map(async (src) => {
      const data = await api('/sources/' + src.id + '/tokens');
      return data.items
        .filter((token) => !token.revoked_at && Array.isArray(token.allowed_channels) && token.allowed_channels.includes(channel.slug))
        .map((token) => ({ src, token }));
    }));
    const entries = results.flat();
    host.replaceChildren(el('h4', 'Токены с доступом к «' + channel.name + '»'));
    if (!entries.length) {
      host.append(el('p', 'У этого канала пока нет токенов отправки. Создайте токен в разделе «Источники».', 'hint'));
      return;
    }
    for (const { src, token } of entries) {
      const row = el('div', undefined, 'token-row');
      const info = el('div', undefined, 'item-body');
      const expired = token.expires_at && new Date(token.expires_at) <= new Date();
      const title = el('div', undefined, 'token-title');
      title.append(el('strong', token.label), el('span', expired ? 'Истёк' : 'Активен', expired ? 'tag dim' : 'tag ok'));
      info.append(title, el('p', 'Источник: ' + src.name + ' · ' + token.prefix + '…', 'meta'));
      if (token.description) info.append(el('p', token.description, 'muted-line'));
      const secret = el('code', '', 'token-secret');
      secret.hidden = true;
      const actions = el('div', undefined, 'actions');
      let visible = false;
      const reveal = button('Показать', async () => {
        if (visible) { visible = false; secret.hidden = true; secret.textContent = ''; reveal.textContent = 'Показать'; return; }
        try {
          const result = await api('/sources/' + src.id + '/tokens/' + token.id + '/secret');
          secret.textContent = result.token;
          secret.hidden = false;
          visible = true;
          reveal.textContent = 'Скрыть';
        } catch (err) { notice(err.message, true); }
      });
      actions.append(reveal, button('Копировать', async () => {
        try {
          const result = await api('/sources/' + src.id + '/tokens/' + token.id + '/secret');
          await navigator.clipboard.writeText(result.token);
          notice('Токен скопирован');
        } catch (err) { notice('Не удалось скопировать токен: ' + err.message, true); }
      }));
      actions.append(button('Редактировать', () => openPermissionsDialog(src, token, channel)));
      info.append(secret);
      row.append(info, actions);
      host.append(row);
    }
  } catch (err) {
    host.replaceChildren(el('p', 'Не удалось получить токены: ' + err.message, 'field-error'));
    notice(err.message, true);
  }
}

function renderSources() {
  const list = $('source-list');
  list.replaceChildren();
  for (const src of state.sources) {
    const card = el('article', undefined, 'source-card panel');
    const top = el('div', undefined, 'source-top');
    const info = el('div', undefined, 'item-body');
    info.append(el('h3', src.name), el('p', src.description || 'Приложение или скрипт, который отправляет уведомления.', 'hint'));
    const actions = el('div', undefined, 'actions');
    actions.append(
      button('Изменить', () => openEntityEditor('sources', src)),
      button('Токены и права', () => showTokens(src)),
      button('+ Создать токен', () => openTokenDialog(src), 'primary')
    );
    top.append(info, actions);
    card.append(top);
    const details = el('div', undefined, 'token-details');
    details.id = `tokens-${src.id}`;
    details.hidden = true;
    card.append(details);
    list.append(card);
  }
  if (!state.sources.length) {
    const empty = el('div', undefined, 'panel empty-state');
    empty.append(el('div', '⌁', 'empty-icon'), el('h2', 'Источников пока нет'), el('p', 'Добавьте Home Assistant, мониторинг или скрипт, затем выдайте ему токен.', 'hint'));
    empty.append(button('+ Добавить источник', () => openForm('source'), 'primary'));
    list.append(empty);
  }
  updateOverview();
}
async function showTokens(src, force = false) {
  const host = $(`tokens-${src.id}`);
  if (!host) return;
  if (!force && !host.hidden) { host.hidden = true; host.replaceChildren(); return; }
  host.hidden = false;
  host.replaceChildren(el('p', 'Загружаем токены…', 'hint'));
  try {
    const data = await api(`/sources/${src.id}/tokens`);
    host.replaceChildren();
    const heading = el('div', undefined, 'token-heading');
    heading.append(el('h4', `Токены источника «${src.name}»`), button('+ Новый токен', () => openTokenDialog(src)));
    host.append(heading);
    if (!data.items.length) {
      host.append(el('p', 'Токенов пока нет. Создайте первый токен, чтобы источник мог отправлять уведомления.', 'hint'));
    }
    for (const token of data.items) {
      const row = el('div', undefined, 'token-row');
      const info = el('div', undefined, 'item-body');
      const expired = token.expires_at && new Date(token.expires_at) <= new Date();
      const status = token.revoked_at ? 'Отозван' : expired ? 'Истёк' : 'Активен';
      const title = el('div', undefined, 'token-title');
      title.append(el('strong', token.label), el('span', status, token.revoked_at || expired ? 'tag dim' : 'tag ok'));
      info.append(title);
      if (token.description) info.append(el('p', token.description, 'muted-line'));
      info.append(el('p', `Токен: ${token.prefix}…`, 'meta'));
      const scopes = Array.isArray(token.allowed_channels) ? token.allowed_channels : [];
      info.append(el('p', scopes.length ? 'Разрешено отправлять: ' + scopes.join(', ') : 'Права каналов недоступны', 'meta'));
      if (token.expires_at) info.append(el('p', 'Действует до: ' + new Date(token.expires_at).toLocaleString(), 'meta'));
      row.append(info);
      if (!token.revoked_at) {
        const actions = el('div', undefined, 'actions');
        actions.append(button('Изменить права', () => openPermissionsDialog(src, token)));
        let secretVisible = false;
        const value = el('code', '••••••••••••', 'token-secret');
        value.hidden = true;
        const reveal = button('Показать', async () => {
          if (secretVisible) { secretVisible = false; value.hidden = true; value.textContent = ''; reveal.textContent = 'Показать'; return; }
          try { const data = await api(`/sources/${src.id}/tokens/${token.id}/secret`); secretVisible = true; value.textContent = data.token; value.hidden = false; reveal.textContent = 'Скрыть'; }
          catch (err) { notice(err.message, true); }
        });
        actions.append(reveal, button('Копировать', async () => {
          try { const data = await api(`/sources/${src.id}/tokens/${token.id}/secret`); await navigator.clipboard.writeText(data.token); notice('Токен скопирован'); }
          catch (err) { notice('Не удалось скопировать токен: ' + err.message, true); }
        }));
        actions.append(button('Отозвать', async () => {
          if (!confirm(`Отозвать токен «${token.label}»? После этого отправка по нему прекратится.`)) return;
          try { await api(`/sources/${src.id}/tokens/${token.id}`, { method: 'DELETE' }); notice('Токен отозван'); await showTokens(src, true); }
          catch (err) { notice(err.message, true); }
        }, 'danger-button'));
        info.append(value);
        row.append(actions);
      }
      host.append(row);
    }
  } catch (err) { host.replaceChildren(el('p', 'Не удалось загрузить токены: ' + err.message, 'field-error')); notice(err.message, true); }
}
function openPermissionsDialog(src, token, channel = null) {
  const allowed = new Set(Array.isArray(token.allowed_channels) ? token.allowed_channels : []);
  state.editingToken = { src, token, channel };
  $('permissions-heading').textContent = `Права · ${token.label}`;
  $('permissions-source').textContent = `Источник: ${src.name}`;
  const picker = $('permissions-channel-list');
  picker.replaceChildren();
  for (const channel of state.channels) {
    const label = el('label', undefined, 'channel-option');
    const check = el('input');
    check.type = 'checkbox';
    check.value = String(channel.id);
    check.name = 'permission';
    check.checked = allowed.has(channel.slug);
    const caption = el('span');
    caption.append(el('strong', channel.name), el('small', channel.slug));
    label.append(check, caption);
    picker.append(label);
  }
  updatePermissionsButton();
  $('permissions-dialog').showModal();
}
function updatePermissionsButton() {
  const nodes = [...$('permissions-channel-list').querySelectorAll('input[type=checkbox]')];
  const allChecked = nodes.length > 0 && nodes.every((node) => node.checked);
  $('toggle-all-permissions').textContent = allChecked ? 'Снять все' : 'Выбрать все';
  $('permissions-error').hidden = nodes.some((node) => node.checked);
}
async function savePermissions(event) {
  event.preventDefault();
  const selection = state.editingToken;
  if (!selection) return;
  const channel_ids = [...$('permissions-channel-list').querySelectorAll('input:checked')].map((node) => Number(node.value));
  if (!channel_ids.length) { $('permissions-error').hidden = false; return; }
  const save = $('save-permissions');
  save.disabled = true;
  try {
    await api(`/sources/${selection.src.id}/tokens/${selection.token.id}`, {
      method: 'PATCH', body: JSON.stringify({ channel_ids })
    });
    $('permissions-dialog').close();
    if (selection.channel) await showChannelTokens(selection.channel, true);
    else await showTokens(selection.src, true);
    notice('Права токена обновлены. Перевыпускать токен не нужно.');
  } catch (err) { notice('Не удалось изменить права: ' + err.message, true); }
  finally { save.disabled = false; }
}

function openTokenDialog(src) {
  if (!state.channels.length) {
    showTab('channels'); openForm('channel');
    notice('Сначала создайте хотя бы один канал.', true);
    return;
  }
  state.tokenSource = src;
  $('token-heading').textContent = `Новый токен · ${src.name}`;
  const form = $('token-form');
  form.reset();
  $('token-channel-error').hidden = true;
  const picker = $('token-channel-list');
  picker.replaceChildren();
  for (const channel of state.channels) {
    const label = el('label', undefined, 'channel-option');
    const check = el('input');
    check.type = 'checkbox'; check.name = 'channel'; check.value = String(channel.id);
    if (state.channels.length === 1) check.checked = true;
    const text = el('span');
    text.append(el('strong', channel.name), el('small', channel.slug));
    label.append(check, text);
    picker.append(label);
  }
  updateAllChannelsButton();
  $('token-dialog').showModal();
  form.elements.label.focus();
}
function updateAllChannelsButton() {
  const nodes = [...$('token-channel-list').querySelectorAll('input[type=checkbox]')];
  const all = nodes.length && nodes.every((node) => node.checked);
  $('toggle-all-channels').textContent = all ? 'Снять все' : 'Выбрать все';
  $('token-channel-error').hidden = nodes.some((node) => node.checked);
}
async function createToken(event) {
  event.preventDefault();
  const src = state.tokenSource;
  if (!src) return;
  const form = $('token-form');
  const channel_ids = [...$('token-channel-list').querySelectorAll('input:checked')].map((node) => Number(node.value));
  if (!channel_ids.length) { $('token-channel-error').hidden = false; return; }
  const payload = { label: form.elements.label.value.trim(), description: form.elements.description.value.trim(), channel_ids };
  const dateValue = form.elements.expires_at.value;
  if (dateValue) {
    const date = new Date(dateValue);
    if (!Number.isFinite(date.getTime()) || date <= new Date()) { notice('Укажите будущую дату истечения токена.', true); return; }
    payload.expires_at = date.toISOString();
  }
  const save = $('save-token');
  save.disabled = true;
  try {
    const response = await api(`/sources/${src.id}/tokens`, { method: 'POST', body: JSON.stringify(payload) });
    $('token-dialog').close();
    $('secret-title').textContent = 'Токен создан';
    $('secret-value').textContent = response.token;
    $('secret-dialog').showModal();
    notice('Токен создан и готов к использованию');
    const host = $(`tokens-${src.id}`);
    if (host) { host.hidden = true; await showTokens(src, true); }
  } catch (err) { notice('Не удалось создать токен: ' + err.message, true); }
  finally { save.disabled = false; }
}
function renderMessages() {
  const list = $('messages');
  const filter = $('filter-channel').value;
  list.replaceChildren();
  const messages = [...state.messages].reverse().filter((m) => !filter || m.channel === filter);
  const priorities = { low: 'Низкий', normal: 'Обычный', high: 'Высокий', urgent: 'Срочный' };
  for (const msg of messages) {
    const card = el('article', undefined, 'message panel' + (msg.read_at ? '' : ' unread'));
    const heading = el('div', undefined, 'message-heading');
    const title = el('div');
    title.append(el('h2', msg.title), el('p', `${msg.channel} · ${msg.source}`, 'meta'));
    heading.append(title);
    if (!msg.read_at) heading.append(button('Отметить прочитанным', async () => {
      try { await api(`/messages/${msg.id}/read`, { method: 'POST' }); msg.read_at = new Date().toISOString(); renderMessages(); }
      catch (err) { notice(err.message, true); }
    }));
    card.append(heading, el('p', msg.body, 'message-body'));
    const footer = el('div', undefined, 'message-footer');
    footer.append(el('span', priorities[msg.priority] || msg.priority, 'tag ' + (msg.priority === 'urgent' ? 'urgent' : msg.priority === 'high' ? 'high' : 'dim')));
    footer.append(el('time', new Date(msg.created_at).toLocaleString(), 'meta'));
    card.append(footer);
    if (msg.url) { const link = el('a', 'Открыть ссылку ↗', 'message-link'); link.href = msg.url; link.target = '_blank'; link.rel = 'noopener noreferrer'; card.append(link); }
    list.append(card);
  }
  if (!messages.length) {
    const empty = el('div', undefined, 'panel empty-state');
    empty.append(el('div', '✉', 'empty-icon'), el('h2', filter ? 'В этом канале пока пусто' : 'Уведомлений пока нет'), el('p', filter ? 'Когда источник отправит сообщение в этот канал, оно появится здесь.' : 'Создайте канал, затем источник и токен — и отправьте первое сообщение через API.', 'hint'));
    if (!state.channels.length) empty.append(button('Создать первый канал', () => { showTab('channels'); openForm('channel'); }, 'primary'));
    else if (!state.sources.length) empty.append(button('Добавить источник', () => { showTab('sources'); openForm('source'); }, 'primary'));
    list.append(empty);
  }
  $('load-more').hidden = !state.hasMore;
  updateOverview();
}
async function fetchNew() {
  if (!state.me) return;
  try {
    const data = await api('/messages?limit=200&after=' + encodeURIComponent(state.cursor));
    const known = new Set(state.messages.map((m) => m.id));
    state.messages.push(...data.items.filter((m) => !known.has(m.id)));
    state.cursor = data.next_cursor;
    state.hasMore = data.has_more;
    renderMessages();
  } catch (err) { notice('Не удалось синхронизировать сообщения: ' + err.message, true); }
}
function startStream() {
  state.stream?.close();
  if (!state.me) return;
  state.stream = new EventSource('/api/v1/events');
  state.stream.addEventListener('messages_available', fetchNew);
  state.stream.addEventListener('open', fetchNew);
}
function openEntityEditor(kind, item) {
  state.editingEntity = { kind, id: item.id };
  const form = $('edit-entity-form');
  form.reset();
  form.elements.name.value = item.name;
  form.elements.description.value = item.description || '';
  $('edit-entity-heading').textContent = kind === 'channels' ? 'Изменить канал' : 'Изменить источник';
  $('edit-entity-hint').textContent = kind === 'channels'
    ? 'Технический адрес (' + item.slug + ') не меняется — интеграции и токены продолжат работать.'
    : 'Токены и разрешения источника сохраняются без изменений.';
  $('edit-entity-dialog').showModal();
}
async function renderUsers() {
  if (state.me?.role !== 'admin') return;
  try {
    const data = await api('/admin/users');
    const list = $('user-list');
    list.replaceChildren();
    for (const user of data.items) {
      const row = el('article', undefined, 'list-card');
      const icon = el('div', user.username.slice(0, 1).toUpperCase(), 'item-icon');
      const info = el('div', undefined, 'item-body');
      info.append(el('h3', user.username), el('p', user.role === 'admin' ? 'Администратор' : 'Пользователь', 'meta'));
      const actions = el('div', undefined, 'actions');
      if (user.id !== state.me.id) {
        actions.append(
          button('Сбросить пароль', () => {
            state.resetUser = user;
            $('reset-password-title').textContent = 'Пароль пользователя «' + user.username + '»';
            $('reset-password-form').reset();
            $('reset-password-dialog').showModal();
          }),
          button(user.disabled ? 'Разблокировать' : 'Заблокировать', async () => {
            const word = user.disabled ? 'Разблокировать' : 'Заблокировать';
            if (!confirm(word + ' пользователя «' + user.username + '»?' +
              (user.disabled ? '' : ' Его сеансы завершатся, а токены источников перестанут работать.'))) return;
            try {
              await api('/admin/users/' + user.id, { method: 'PATCH', body: JSON.stringify({ disabled: !user.disabled }) });
              await renderUsers();
              notice(word + ': ' + user.username);
            } catch (err) { notice('Не удалось изменить состояние: ' + err.message, true); }
          }, user.disabled ? 'secondary' : 'danger-button')
        );
      }
      row.append(icon, info, el('span', user.disabled ? 'Отключён' : 'Активен', user.disabled ? 'tag dim' : 'tag ok'), actions);
      list.append(row);
    }
  } catch (err) { notice('Не удалось загрузить пользователей: ' + err.message, true); }
}
async function connect() {
  try { const me = await api('/me'); setAuth(me); await refreshAll(); startStream(); }
  catch { setAuth(null); }
}

$('login-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  try {
    await api('/auth/login', { method: 'POST', body: JSON.stringify({ ...formData(event.target), mode: 'web' }) });
    event.target.reset(); notice(''); await connect();
  } catch (err) { notice('Ошибка входа: ' + err.message, true); }
});
$('cancel-edit-entity').addEventListener('click', () => $('edit-entity-dialog').close());
$('close-edit-entity').addEventListener('click', () => $('edit-entity-dialog').close());
$('edit-entity-dialog').addEventListener('close', () => {
  $('edit-entity-form').reset();
  state.editingEntity = null;
});
$('edit-entity-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const selection = state.editingEntity;
  if (!selection) return;
  const name = event.target.elements.name.value.trim();
  const description = event.target.elements.description.value;
  try {
    await api('/' + selection.kind + '/' + selection.id, {
      method: 'PATCH', body: JSON.stringify({ name, description })
    });
    $('edit-entity-dialog').close();
    await refreshAll();
    notice('Изменения сохранены');
  } catch (err) { notice('Не удалось сохранить: ' + err.message, true); }
});
$('change-password').addEventListener('click', () => {
  $('password-form').reset();
  $('password-dialog').showModal();
});
$('cancel-password').addEventListener('click', () => $('password-dialog').close());
$('close-password').addEventListener('click', () => $('password-dialog').close());
$('password-dialog').addEventListener('close', () => $('password-form').reset());
$('password-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.target;
  const current = form.elements.current_password.value;
  const next = form.elements.new_password.value;
  if (next !== form.elements.repeat_password.value) {
    notice('Новые пароли не совпадают', true);
    return;
  }
  try {
    await api('/me/change-password', {
      method: 'POST',
      body: JSON.stringify({ current_password: current, new_password: next })
    });
    $('password-dialog').close();
    state.stream?.close(); state.stream = null; state.messages = [];
    setAuth(null);
    notice('Пароль изменён. Войдите заново на всех устройствах.');
  } catch (err) { notice('Не удалось изменить пароль: ' + err.message, true); }
});
$('cancel-reset-password').addEventListener('click', () => $('reset-password-dialog').close());
$('close-reset-password').addEventListener('click', () => $('reset-password-dialog').close());
$('reset-password-dialog').addEventListener('close', () => {
  $('reset-password-form').reset();
  state.resetUser = null;
});
$('reset-password-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const target = state.resetUser;
  if (!target) return;
  const form = event.target;
  const next = form.elements.new_password.value;
  if (next !== form.elements.repeat_password.value) {
    notice('Пароли не совпадают', true);
    return;
  }
  try {
    await api('/admin/users/' + target.id + '/reset-password', {
      method: 'POST', body: JSON.stringify({ new_password: next })
    });
    $('reset-password-dialog').close();
    notice('Пароль для «' + target.username + '» сброшен. Все его сеансы завершены.');
  } catch (err) { notice('Не удалось сбросить пароль: ' + err.message, true); }
});
$('logout').addEventListener('click', async () => {
  try { await api('/auth/logout', { method: 'POST' }); } catch (err) { notice(err.message, true); }
  state.stream?.close(); state.stream = null; state.messages = []; setAuth(null);
});
document.querySelectorAll('.nav-item').forEach((node) => node.addEventListener('click', () => showTab(node.dataset.tab)));
$('refresh').addEventListener('click', async () => { try { await refreshAll(); notice('Обновлено'); } catch (err) { notice(err.message, true); } });
$('filter-channel').addEventListener('change', renderMessages);
$('load-more').addEventListener('click', fetchNew);
$('start-channel').addEventListener('click', () => { showTab('channels'); openForm('channel'); });
$('go-channels').addEventListener('click', () => { showTab('channels'); openForm('channel'); });
for (const name of ['channel', 'source', 'user']) {
  $(`new-${name}`).addEventListener('click', () => openForm(name));
  $(`cancel-${name}`).addEventListener('click', () => closeForm(name));
  $(`close-${name}`).addEventListener('click', () => closeForm(name));
}
let slugEdited = false;
$('channel-form').elements.name.addEventListener('input', (event) => {
  if (!slugEdited) $('channel-form').elements.slug.value = autoSlug(event.target.value);
});
$('channel-form').elements.slug.addEventListener('input', () => { slugEdited = true; });
$('channel-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const data = formData(event.target);
  data.slug = data.slug.trim() || autoSlug(data.name);
  try { await api('/channels', { method: 'POST', body: JSON.stringify(data) }); closeForm('channel'); slugEdited = false; await refreshAll(); notice(`Канал «${data.name}» создан`); }
  catch (err) { notice('Не удалось создать канал: ' + err.message, true); }
});
$('source-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const data = formData(event.target);
  try { await api('/sources', { method: 'POST', body: JSON.stringify(data) }); closeForm('source'); await refreshAll(); notice(`Источник «${data.name}» создан`); }
  catch (err) { notice('Не удалось создать источник: ' + err.message, true); }
});
$('user-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  try { const data = formData(event.target); await api('/admin/users', { method: 'POST', body: JSON.stringify(data) }); closeForm('user'); await renderUsers(); notice(`Пользователь «${data.username}» создан`); }
  catch (err) { notice('Не удалось создать пользователя: ' + err.message, true); }
});
$('token-form').addEventListener('submit', createToken);
$('permissions-form').addEventListener('submit', savePermissions);
$('permissions-channel-list').addEventListener('change', updatePermissionsButton);
$('toggle-all-permissions').addEventListener('click', () => {
  const nodes = [...$('permissions-channel-list').querySelectorAll('input[type=checkbox]')];
  const selectAll = !nodes.every((node) => node.checked);
  nodes.forEach((node) => { node.checked = selectAll; });
  updatePermissionsButton();
});
$('revoke-permissions').addEventListener('click', async () => {
  const selection = state.editingToken;
  if (!selection || !confirm('Отозвать токен «' + selection.token.label + '»? Отправка по нему прекратится.')) return;
  try {
    await api('/sources/' + selection.src.id + '/tokens/' + selection.token.id, { method: 'DELETE' });
    $('permissions-dialog').close();
    if (selection.channel) await showChannelTokens(selection.channel, true);
    else await showTokens(selection.src, true);
    notice('Токен отозван');
  } catch (err) { notice('Не удалось отозвать токен: ' + err.message, true); }
});
$('cancel-permissions').addEventListener('click', () => $('permissions-dialog').close());
$('close-permissions').addEventListener('click', () => $('permissions-dialog').close());
$('permissions-dialog').addEventListener('close', () => { state.editingToken = null; });
$('toggle-all-channels').addEventListener('click', () => {
  const nodes = [...$('token-channel-list').querySelectorAll('input[type=checkbox]')];
  const selectAll = !nodes.every((node) => node.checked);
  nodes.forEach((node) => { node.checked = selectAll; });
  updateAllChannelsButton();
});
$('token-channel-list').addEventListener('change', updateAllChannelsButton);
$('cancel-token').addEventListener('click', () => $('token-dialog').close());
$('close-token').addEventListener('click', () => $('token-dialog').close());
$('close-secret').addEventListener('click', () => $('secret-dialog').close());
$('dismiss-secret').addEventListener('click', () => $('secret-dialog').close());
$('secret-dialog').addEventListener('close', () => { $('secret-value').textContent = ''; });
$('copy-secret').addEventListener('click', async () => {
  try { await navigator.clipboard.writeText($('secret-value').textContent); notice('Токен скопирован'); }
  catch (err) { notice('Не удалось скопировать: ' + err.message, true); }
});
connect();
