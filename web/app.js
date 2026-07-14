'use strict';

// ---------- tiny helpers ----------
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
const el = (tag, attrs = {}, ...children) => {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') n.className = v;
    else if (k === 'html') n.innerHTML = v;
    else if (k.startsWith('on') && typeof v === 'function') n.addEventListener(k.slice(2), v);
    else if (k === 'style' && typeof v === 'string') n.setAttribute(k, v);
    else if (v !== null && v !== undefined) n.setAttribute(k, v);
  }
  for (const c of children) {
    if (c == null || c === false) continue;
    n.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  return n;
};
const fmtTime = (s) => s ? s.slice(11, 19) : '--';
const todayStr = () => {
  const d = new Date();
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
};
const emptyHint = (msg) => el('div', { class: 'tl-empty' }, msg);

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const res = await fetch(path, opts);
  if (res.status === 204) return null;
  const text = await res.text();
  let data; try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!res.ok) throw new Error((data && data.error) || res.statusText);
  return data;
}

// ---------- state ----------
const state = {
  groups: [],
  todos: [],
  activeEntry: null,
  currentGroup: '',
  view: 'today',
  timelineDate: todayStr(),
  analysisScope: 'daily',
  analysisDate: todayStr(),
  analysisWeek: '',
  todayMode: 'timer',      // 'timer' | 'backfill'
  todayShowDone: false,    // 任务列表是否显示已完成
};

function groupColor(id) {
  const g = state.groups.find(x => x.id === id);
  return g ? g.color : '#9ca3af';
}
function groupName(id) {
  const g = state.groups.find(x => x.id === id);
  return g ? g.name : '未分组';
}

// ---------- init ----------
async function init() {
  startClock();
  bindNav();
  bindToday();
  bindTodos();
  bindTimeline();
  bindAnalysis();
  bindModal();
  await loadGroups();
  await loadTodos();
  await refreshActiveEntry();
  renderView('today');
}

function startClock() {
  const tick = () => { $('#clock').textContent = new Date().toLocaleTimeString('zh-CN', { hour12: false }); };
  tick(); setInterval(tick, 1000);
}

// ---------- navigation ----------
function bindNav() {
  $$('.tab').forEach(t => t.addEventListener('click', () => renderView(t.dataset.view)));
}
function renderView(view) {
  state.view = view;
  $$('.tab').forEach(t => t.classList.toggle('active', t.dataset.view === view));
  $$('.view').forEach(v => v.classList.add('hidden'));
  $('#view-' + view).classList.remove('hidden');
  if (view === 'today') renderToday();
  if (view === 'todos') renderTodos();
  if (view === 'timeline') renderTimeline();
  if (view === 'analysis') renderAnalysis();
}

// ---------- data loaders ----------
async function loadGroups() {
  state.groups = await api('GET', '/api/groups');
  if (state.groups.length === 0) {
    await api('POST', '/api/groups', { name: '默认', color: '#6366f1', sort_order: 0 });
    state.groups = await api('GET', '/api/groups');
  }
}
async function loadTodos() {
  state.todos = await api('GET', '/api/todos');
}
async function refreshActiveEntry() {
  state.activeEntry = await api('GET', '/api/time-entries/active');
}

// ============================================================
//  TODAY
// ============================================================
function bindToday() {
  // 计时 / 补录 模式切换
  $('#today-mode-seg').addEventListener('click', e => {
    const b = e.target.closest('.seg-btn');
    if (!b) return;
    setTodayMode(b.dataset.mode);
  });
  // 计时
  $('#timer-toggle').addEventListener('click', onTimerToggle);
  $('#timer-todo').addEventListener('change', e => {
    // 选中 todo 后自动带出其分组
    const t = state.todos.find(x => String(x.id) === e.target.value);
    if (t && t.group_id) $('#timer-group').value = String(t.group_id);
  });
  // 补录
  $('#bf-add').addEventListener('click', onBackfillAdd);
  // 任务与分组
  $('#today-add-group').addEventListener('click', () => openGroupModal(null));
  $('#today-add-todo').addEventListener('click', () => openTodoModal(null));
  $('#today-show-done').addEventListener('change', e => {
    state.todayShowDone = e.target.checked;
    renderTodayTasks();
  });
}

function setTodayMode(mode) {
  state.todayMode = mode;
  $$('#today-mode-seg .seg-btn').forEach(b => b.classList.toggle('active', b.dataset.mode === mode));
  $('#today-timer-body').classList.toggle('hidden', mode !== 'timer');
  $('#today-backfill-body').classList.toggle('hidden', mode !== 'backfill');
  if (mode === 'backfill' && !$('#bf-date').value) $('#bf-date').value = todayStr();
}

async function renderToday() {
  renderRecorder();
  renderTodayTasks();
  await renderTodayTimelineAnalysis();
  await renderTodayEntries();
}

// 填充计时/补录两套下拉 + 计时器显示
function renderRecorder() {
  fillGroupSelect($('#timer-group'), null);
  fillTodoSelect($('#timer-todo'), null);
  fillGroupSelect($('#bf-group'), null);
  fillTodoSelect($('#bf-todo'), null);
  if (!$('#bf-date').value) $('#bf-date').value = todayStr();
  renderTimer();
}

function fillGroupSelect(sel, selectedId) {
  const prev = sel.value;
  sel.innerHTML = '<option value="">未分组</option>';
  for (const g of state.groups) {
    const opt = el('option', { value: g.id }, g.name);
    if (selectedId && Number(selectedId) === g.id) opt.selected = true;
    sel.appendChild(opt);
  }
  if (prev) sel.value = prev;
}

function fillTodoSelect(sel, selectedId) {
  const prev = sel.value;
  sel.innerHTML = '<option value="">无</option>';
  for (const t of state.todos) {
    if (t.status === 'done') continue;
    const label = (t.children && t.children.length) ? t.title + ' …' : t.title;
    const opt = el('option', { value: t.id }, label);
    if (selectedId && Number(selectedId) === t.id) opt.selected = true;
    sel.appendChild(opt);
  }
  if (prev) sel.value = prev;
}

function renderTimer() {
  const active = state.activeEntry;
  const btn = $('#timer-toggle');
  if (active) {
    btn.textContent = '停止';
    btn.classList.add('btn-danger');
    const info = $('#timer-active-info');
    info.innerHTML = '';
    info.appendChild(document.createTextNode('正在记录：'));
    info.appendChild(el('b', {}, groupName(active.group_id)));
    if (active.todo_title) info.appendChild(document.createTextNode(' · ' + active.todo_title));
    if (active.note) info.appendChild(document.createTextNode(' · ' + active.note));
    // 补录模式下计时主体被隐藏，补一个始终可见的停止按钮
    const stopBtn = el('button', { class: 'btn btn-small btn-danger', style: 'margin-left:8px', onclick: onTimerToggle }, '停止计时');
    info.appendChild(stopBtn);
    updateElapsed();
  } else {
    btn.textContent = '开始';
    btn.classList.remove('btn-danger');
    $('#timer-active-info').textContent = '';
    $('#timer-elapsed').textContent = '--:--:--';
  }
}
function updateElapsed() {
  const a = state.activeEntry;
  if (!a) return;
  const start = new Date(a.start_time.replace(' ', 'T')).getTime();
  const secs = Math.max(0, Math.floor((Date.now() - start) / 1000));
  const h = String(Math.floor(secs / 3600)).padStart(2, '0');
  const m = String(Math.floor((secs % 3600) / 60)).padStart(2, '0');
  const s = String(secs % 60).padStart(2, '0');
  $('#timer-elapsed').textContent = `${h}:${m}:${s}`;
}
setInterval(() => { if (state.activeEntry && state.view === 'today') updateElapsed(); }, 1000);

async function onTimerToggle() {
  try {
    if (state.activeEntry) {
      await api('POST', '/api/time-entries/stop');
      state.activeEntry = null;
    } else {
      const groupId = $('#timer-group').value;
      const todoId = $('#timer-todo').value;
      const note = $('#timer-note').value.trim();
      state.activeEntry = await api('POST', '/api/time-entries/start', {
        group_id: groupId ? Number(groupId) : null,
        todo_id: todoId ? Number(todoId) : null,
        note,
      });
      $('#timer-note').value = '';
    }
    renderTimer();
    await renderTodayTimelineAnalysis();
    await renderTodayEntries();
  } catch (e) { alert(e.message); }
}

// 补录模式：手动配置起止时间
async function onBackfillAdd() {
  const d = $('#bf-date').value || todayStr();
  const st = $('#bf-start').value;
  const en = $('#bf-end').value;
  if (!st) { alert('请填写开始时间'); return; }
  if (en && en <= st) { alert('结束时间需晚于开始时间'); return; }
  if (!en && state.activeEntry) { alert('请先停止当前计时，再补录进行中的记录'); return; }
  const body = {
    start_time: `${d} ${st}:00`,
    end_time: en ? `${d} ${en}:00` : null,
    note: $('#bf-note').value.trim(),
    group_id: $('#bf-group').value ? Number($('#bf-group').value) : null,
    todo_id: $('#bf-todo').value ? Number($('#bf-todo').value) : null,
  };
  try {
    await api('POST', '/api/time-entries', body);
    $('#bf-note').value = '';
    $('#bf-end').value = '';
    await refreshActiveEntry();
    renderRecorder();
    await renderTodayTimelineAnalysis();
    await renderTodayEntries();
  } catch (e) { alert(e.message); }
}

// 时间轴 + 每日分析 合并展示
async function renderTodayTimelineAnalysis() {
  const date = todayStr();
  const [entries, r] = await Promise.all([
    api('GET', '/api/time-entries?date=' + date),
    api('GET', '/api/analysis/daily?date=' + date),
  ]);
  const wrap = $('#today-timeline-wrap');
  wrap.innerHTML = '';
  wrap.appendChild(buildTimeline(entries, true));
  const host = $('#today-analysis');
  host.innerHTML = '';
  host.appendChild(renderDailyAnalysis(r));
}

// 当日记录列表（编辑/删除）
async function renderTodayEntries() {
  const entries = await api('GET', '/api/time-entries?date=' + todayStr());
  const host = $('#today-entries');
  host.innerHTML = '';
  if (entries.length === 0) { host.appendChild(emptyHint('当天没有工时记录')); return; }
  for (const e of entries) host.appendChild(buildEntryRow(e, async () => {
    await refreshActiveEntry();
    renderRecorder();
    await renderTodayTimelineAnalysis();
    await renderTodayEntries();
  }));
}

// 任务与分组：分组 chips + 按分组展示任务
function renderTodayTasks() {
  // 分组 chips
  const chips = $('#today-groups');
  chips.innerHTML = '';
  for (const g of state.groups) {
    chips.appendChild(el('div', {
      class: 'group-chip',
      title: '点击用此分组开始计时',
      onclick: () => {
        setTodayMode('timer');
        $('#timer-group').value = String(g.id);
        $('#timer-todo').value = '';
        $('#timer-note').focus();
      },
    }, el('i', { style: `background:${g.color}` }), g.name));
  }

  // 按分组展示任务
  const host = $('#today-tasks');
  host.innerHTML = '';
  const visible = state.todos.filter(t => state.todayShowDone ? true : t.status !== 'done');
  if (visible.length === 0) { host.appendChild(emptyHint('暂无任务，点右上角新建')); return; }

  const renderSection = (titleNode, items) => {
    if (items.length === 0) return;
    host.appendChild(titleNode);
    for (const t of items) host.appendChild(buildTodoRow(t, false));
  };
  for (const g of state.groups) {
    const items = visible.filter(t => t.group_id === g.id);
    if (items.length === 0) continue;
    renderSection(el('div', { class: 'task-group-head' }, el('i', { style: `background:${g.color}` }), g.name), items);
  }
  const ungrouped = visible.filter(t => !t.group_id);
  if (ungrouped.length) {
    renderSection(el('div', { class: 'task-group-head' }, el('i', { style: 'background:#9ca3af' }), '未分组'), ungrouped);
  }
}

// ============================================================
//  TODOS
// ============================================================
function bindTodos() {
  $('#add-group-btn').addEventListener('click', () => toggleGroupEditor(null));
  $('#save-group-btn').addEventListener('click', saveGroup);
  $('#cancel-group-btn').addEventListener('click', () => $('#group-editor').classList.add('hidden'));
  $('#add-todo-btn').addEventListener('click', () => openTodoModal(null));
}

function renderTodos() {
  // groups list
  const ul = $('#group-list');
  ul.innerHTML = '';
  const all = el('li', {
    class: 'group-item' + (state.currentGroup === '' ? ' active' : ''),
    onclick: () => { state.currentGroup = ''; renderTodos(); },
  }, el('span', { class: 'dot', style: 'background:#9ca3af' }), '全部');
  ul.appendChild(all);
  for (const g of state.groups) {
    const li = el('li', {
      class: 'group-item' + (state.currentGroup == String(g.id) ? ' active' : ''),
      onclick: () => { state.currentGroup = String(g.id); renderTodos(); },
    });
    li.appendChild(el('span', { class: 'dot', style: `background:${g.color}` }));
    li.appendChild(document.createTextNode(g.name));
    const actions = el('span', { class: 'g-actions' });
    actions.appendChild(el('button', { class: 'btn btn-small', onclick: (e) => { e.stopPropagation(); toggleGroupEditor(g); } }, '改'));
    actions.appendChild(el('button', { class: 'btn btn-small btn-danger', onclick: async (e) => { e.stopPropagation(); if (confirm('删除分组？其下待办将变为未分组')) { await api('DELETE', '/api/groups/' + g.id); state.currentGroup=''; await loadGroups(); await loadTodos(); renderTodos(); } } }, '×'));
    li.appendChild(actions);
    ul.appendChild(li);
  }

  $('#todos-title').textContent = state.currentGroup ? groupName(Number(state.currentGroup)) : '全部待办';
  const list = $('#todo-list');
  list.innerHTML = '';
  const filtered = state.currentGroup
    ? state.todos.filter(t => t.group_id === Number(state.currentGroup))
    : state.todos;
  if (filtered.length === 0) { list.appendChild(emptyHint('没有待办，点右上角新建')); return; }
  for (const t of filtered) list.appendChild(buildTodoRow(t, false));
}

function buildTodoRow(t, compact) {
  const item = el('div', { class: 'todo-item' + (t.status === 'done' ? ' done' : '') });
  const row = el('div', { class: 'todo-row' });
  const check = el('div', {
    class: 'todo-check' + (t.status === 'done' ? ' done' : ''),
    onclick: async () => {
      const next = t.status === 'done' ? 'pending' : 'done';
      await api('PATCH', '/api/todos/' + t.id + '/status', { status: next });
      await loadTodos();
      renderView(state.view);
    },
  }, t.status === 'done' ? '✓' : '');
  row.appendChild(check);
  const main = el('div', { class: 'todo-main' });
  main.appendChild(el('div', { class: 'todo-title' }, t.title));
  const meta = el('div', { class: 'todo-meta' });
  if (t.group_id) meta.appendChild(el('span', { class: 'group-tag' }, el('i', { style: `background:${groupColor(t.group_id)}` }), groupName(t.group_id)));
  if (t.due_date) meta.appendChild(document.createTextNode(' · 截止 ' + t.due_date));
  main.appendChild(meta);
  row.appendChild(main);
  const actions = el('div', { class: 'todo-actions' });
  if (!compact) {
    actions.appendChild(el('button', { class: 'btn btn-small', onclick: () => openTodoModal({ parent_id: t.id }) }, '子任务'));
    actions.appendChild(el('button', { class: 'btn btn-small', onclick: () => openTodoModal(t) }, '编辑'));
    actions.appendChild(el('button', { class: 'btn btn-small btn-danger', onclick: async () => { if (confirm('删除该待办及其子任务？')) { await api('DELETE', '/api/todos/' + t.id); await loadTodos(); renderView(state.view); } } }, '删除'));
  }
  row.appendChild(actions);
  item.appendChild(row);
  if (t.children && t.children.length && !compact) {
    const childWrap = el('div', { class: 'todo-children' });
    for (const c of t.children) childWrap.appendChild(buildTodoRow(c, false));
    item.appendChild(childWrap);
  }
  return item;
}

function toggleGroupEditor(group) {
  const box = $('#group-editor');
  box.classList.remove('hidden');
  $('#group-name').value = group ? group.name : '';
  $('#group-color').value = group ? group.color : '#6366f1';
  $('#group-name').focus();
  $('#save-group-btn').dataset.id = group ? group.id : '';
}
async function saveGroup() {
  const id = $('#save-group-btn').dataset.id;
  const body = { name: $('#group-name').value.trim(), color: $('#group-color').value, sort_order: 0 };
  if (!body.name) return;
  if (id) await api('PUT', '/api/groups/' + id, body);
  else await api('POST', '/api/groups', body);
  $('#group-editor').classList.add('hidden');
  await loadGroups();
  renderTodos();
}

// ============================================================
//  TIMELINE
// ============================================================
function bindTimeline() {
  $('#timeline-date').value = state.timelineDate;
  $('#timeline-date').addEventListener('change', e => { state.timelineDate = e.target.value; renderTimeline(); });
  $('#timeline-prev').addEventListener('click', () => shiftDate(-1));
  $('#timeline-next').addEventListener('click', () => shiftDate(1));
  $('#timeline-today').addEventListener('click', () => { state.timelineDate = todayStr(); $('#timeline-date').value = state.timelineDate; renderTimeline(); });
  $('#timeline-add').addEventListener('click', () => openEntryModal(null, state.timelineDate));
}
function shiftDate(d) {
  const dt = new Date(state.timelineDate + 'T00:00:00');
  dt.setDate(dt.getDate() + d);
  state.timelineDate = dt.toISOString().slice(0, 10);
  $('#timeline-date').value = state.timelineDate;
  renderTimeline();
}

async function renderTimeline() {
  const date = state.timelineDate;
  $('#timeline-date-label').textContent = date + (date === todayStr() ? '（今天）' : '');
  const entries = await api('GET', '/api/time-entries?date=' + date);
  const host = $('#timeline-host');
  host.innerHTML = '';
  host.appendChild(buildTimeline(entries, date === todayStr()));

  const legend = $('#timeline-legend');
  legend.innerHTML = '';
  const gids = [...new Set(entries.map(e => e.group_id))];
  for (const gid of gids) {
    legend.appendChild(el('span', {}, el('i', { style: `background:${groupColor(gid)}` }), groupName(gid)));
  }

  const list = $('#timeline-entries');
  list.innerHTML = '';
  if (entries.length === 0) { list.appendChild(emptyHint('当天没有工时记录')); return; }
  for (const e of entries) list.appendChild(buildEntryRow(e, () => renderTimeline()));
}

function buildTimeline(entries, showNow) {
  const host = el('div', { class: 'timeline-host' });
  const wrap = el('div', {});
  if (entries.length === 0) {
    host.appendChild(el('div', { class: 'tl-empty', style: 'width:100%' }, '暂无记录'));
    wrap.appendChild(host);
    wrap.appendChild(buildAxis());
    return wrap;
  }
  for (const e of entries) {
    const start = new Date(e.start_time.replace(' ', 'T')).getTime();
    const endSecs = e.end_time ? new Date(e.end_time.replace(' ', 'T')).getTime() : Date.now();
    const dayStr = e.start_time.slice(0, 10);
    const dayStart = new Date(dayStr + 'T00:00:00').getTime();
    const dayEnd = dayStart + 24 * 3600 * 1000;
    const s = Math.max(start, dayStart);
    const en = Math.min(endSecs, dayEnd);
    if (en <= s) continue;
    const leftPct = ((s - dayStart) / (24 * 3600 * 1000)) * 100;
    const widthPct = ((en - s) / (24 * 3600 * 1000)) * 100;
    const block = el('div', {
      class: 'tl-block',
      style: `left:${leftPct}%; width:${widthPct}%; background:${groupColor(e.group_id)}`,
      title: `${fmtTime(e.start_time)} - ${e.end_time ? fmtTime(e.end_time) : '进行中'} · ${groupName(e.group_id)}${e.todo_title ? ' · ' + e.todo_title : ''}`,
    });
    if (widthPct > 6) block.textContent = groupName(e.group_id);
    block.addEventListener('click', () => openEntryModal(e));
    host.appendChild(block);
  }
  if (showNow) {
    const now = new Date();
    const dayStart = new Date(now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2, '0') + '-' + String(now.getDate()).padStart(2, '0') + 'T00:00:00').getTime();
    const pct = ((now.getTime() - dayStart) / (24 * 3600 * 1000)) * 100;
    host.appendChild(el('div', { class: 'tl-now', style: `left:${pct}%` }));
  }
  wrap.appendChild(host);
  wrap.appendChild(buildAxis());
  return wrap;
}
function buildAxis() {
  const axis = el('div', { class: 'tl-axis' });
  for (let h = 0; h < 24; h++) axis.appendChild(el('span', {}, h % 3 === 0 ? String(h) : ''));
  return axis;
}

function buildEntryRow(e, onChange) {
  const dur = entryDuration(e);
  const row = el('div', { class: 'entry-row' });
  const timeDiv = el('div', { class: 'entry-time' });
  timeDiv.appendChild(document.createTextNode(fmtTime(e.start_time) + ' - '));
  if (e.end_time) timeDiv.appendChild(document.createTextNode(fmtTime(e.end_time)));
  else timeDiv.appendChild(el('span', { class: 'entry-running' }, '进行中'));
  row.appendChild(timeDiv);
  row.appendChild(el('div', { class: 'entry-dur' }, dur));
  row.appendChild(el('div', { class: 'entry-group' }, el('i', { style: `background:${groupColor(e.group_id)}` }), groupName(e.group_id)));
  row.appendChild(el('div', { class: 'entry-note' }, e.todo_title ? '#' + e.todo_title : '', e.note ? ' · ' + e.note : ''));
  row.appendChild(el('div', { class: 'entry-actions' },
    el('button', { class: 'btn btn-small', onclick: () => openEntryModal(e) }, '编辑'),
    el('button', { class: 'btn btn-small btn-danger', onclick: async () => { if (confirm('删除该工时记录？')) { await api('DELETE', '/api/time-entries/' + e.id); if (onChange) onChange(); } } }, '删除')));
  return row;
}

function entryDuration(e) {
  const start = new Date(e.start_time.replace(' ', 'T')).getTime();
  const end = e.end_time ? new Date(e.end_time.replace(' ', 'T')).getTime() : Date.now();
  const secs = Math.max(0, Math.floor((end - start) / 1000));
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  if (h > 0) return h + 'h ' + m + 'm';
  if (m > 0) return m + 'm';
  return secs + 's';
}

// ============================================================
//  ANALYSIS
// ============================================================
function bindAnalysis() {
  $$('.seg-btn').forEach(b => b.addEventListener('click', () => {
    state.analysisScope = b.dataset.scope;
    $$('.seg-btn').forEach(x => x.classList.toggle('active', x === b));
    $('#analysis-date').classList.toggle('hidden', state.analysisScope !== 'daily');
    $('#analysis-week').classList.toggle('hidden', state.analysisScope !== 'weekly');
    if (state.analysisScope === 'weekly' && !state.analysisWeek) state.analysisWeek = currentISOWeek();
    $('#analysis-week').value = state.analysisWeek;
    renderAnalysis();
  }));
  $('#analysis-date').value = state.analysisDate;
  $('#analysis-date').addEventListener('change', e => { state.analysisDate = e.target.value; renderAnalysis(); });
  $('#analysis-week').addEventListener('change', e => { state.analysisWeek = e.target.value; renderAnalysis(); });
  $('#analysis-prev').addEventListener('click', () => shiftAnalysis(-1));
  $('#analysis-next').addEventListener('click', () => shiftAnalysis(1));
}
function currentISOWeek() {
  const d = new Date();
  const tmp = new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()));
  const dayNum = tmp.getUTCDay() || 7;
  tmp.setUTCDate(tmp.getUTCDate() + 4 - dayNum);
  const yearStart = new Date(Date.UTC(tmp.getUTCFullYear(), 0, 1));
  const week = Math.ceil(((tmp - yearStart) / 86400000 + 1) / 7);
  return tmp.getUTCFullYear() + '-W' + String(week).padStart(2, '0');
}
function shiftAnalysis(dir) {
  if (state.analysisScope === 'daily') {
    const dt = new Date(state.analysisDate + 'T00:00:00');
    dt.setDate(dt.getDate() + dir);
    state.analysisDate = dt.toISOString().slice(0, 10);
    $('#analysis-date').value = state.analysisDate;
  } else {
    state.analysisWeek = shiftISOWeek(state.analysisWeek, dir);
    $('#analysis-week').value = state.analysisWeek;
  }
  renderAnalysis();
}
function shiftISOWeek(week, dir) {
  const m = week.match(/^(\d{4})-W(\d{2})$/);
  if (!m) return currentISOWeek();
  const year = Number(m[1]); const w = Number(m[2]);
  const jan4 = new Date(year, 0, 4);
  const wd = jan4.getDay() || 7;
  const monday1 = new Date(jan4); monday1.setDate(jan4.getDate() - (wd - 1));
  const monday = new Date(monday1); monday1.setDate(monday1.getDate() + (w - 1) * 7);
  monday.setDate(monday.getDate() + dir * 7);
  // recompute ISO week of `monday`
  const tmp = new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + 3);
  const tmpDay = tmp.getDay() || 7;
  const yStart = new Date(tmp.getFullYear(), 0, 1);
  const wk = Math.ceil(((tmp - yStart) / 86400000 + 1) / 7);
  return tmp.getFullYear() + '-W' + String(wk).padStart(2, '0');
}

async function renderAnalysis() {
  const host = $('#analysis-content');
  host.innerHTML = '';
  try {
    if (state.analysisScope === 'daily') {
      const [r, entries] = await Promise.all([
        api('GET', '/api/analysis/daily?date=' + state.analysisDate),
        api('GET', '/api/time-entries?date=' + state.analysisDate),
      ]);
      // 给每日分析加上当日时间轴
      const tlCard = el('div', { class: 'card', style: 'margin-bottom:16px' },
        el('h3', {}, state.analysisDate + ' 时间轴'));
      tlCard.appendChild(buildTimeline(entries, state.analysisDate === todayStr()));
      host.appendChild(tlCard);
      host.appendChild(renderDailyAnalysis(r));
    } else {
      const w = state.analysisWeek || currentISOWeek();
      const r = await api('GET', '/api/analysis/weekly?week=' + w);
      host.appendChild(renderWeeklyAnalysis(r));
    }
  } catch (e) {
    host.appendChild(emptyHint('加载失败：' + e.message));
  }
}

function statCard(label, value, sub) {
  const s = el('div', { class: 'stat' });
  s.appendChild(el('div', { class: 'label' }, label));
  s.appendChild(el('div', { class: 'value' }, value));
  if (sub) s.appendChild(el('div', { class: 'sub' }, sub));
  return s;
}

function renderDailyAnalysis(r) {
  const wrap = el('div', {});
  wrap.appendChild(el('div', { class: 'summary-box' }, r.summary));
  const grid = el('div', { class: 'stat-grid' });
  grid.appendChild(statCard('总工时', r.total_duration, r.entry_count + ' 条记录'));
  grid.appendChild(statCard('完成任务', String(r.completed_todos), r.active_todo_count + ' 个进行中'));
  if (r.longest_focus) grid.appendChild(statCard('最长专注', r.longest_focus.duration, r.longest_focus.entry_count + ' 条连续'));
  if (r.vs_yesterday) {
    const d = r.vs_yesterday;
    const sign = d.delta_seconds > 0 ? '+' : '';
    grid.appendChild(statCard('对比昨日', sign + d.duration, d.delta_percent ? (d.delta_percent > 0 ? '+' : '') + d.delta_percent.toFixed(0) + '%' : '—'));
  }
  wrap.appendChild(grid);

  wrap.appendChild(el('div', { class: 'card', style: 'margin-top:16px' },
    el('h3', {}, '分组占比'),
    barsFromGroups(r.group_breakdown)));
  wrap.appendChild(el('div', { class: 'card', style: 'margin-top:16px' },
    el('h3', {}, '时段分布'),
    barsFromParts(r.part_of_day)));
  wrap.appendChild(el('div', { class: 'card', style: 'margin-top:16px' },
    el('h3', {}, '每小时工作分布'),
    buildHourBars(r.hourly_histogram)));

  // improvement editor
  wrap.appendChild(buildImprovementEditor(r.date, r.improvement, r.notes));
  return wrap;
}

function renderWeeklyAnalysis(r) {
  const wrap = el('div', {});
  wrap.appendChild(el('div', { class: 'summary-box' }, r.summary));
  const grid = el('div', { class: 'stat-grid' });
  grid.appendChild(statCard('本周总工时', r.total_duration, r.entry_count + ' 条记录'));
  grid.appendChild(statCard('完成任务', String(r.completed_todos), ''));
  if (r.best_day) grid.appendChild(statCard('最高产', r.best_day.weekday, r.best_day.duration));
  if (r.vs_last_week) {
    const d = r.vs_last_week;
    const sign = d.delta_seconds > 0 ? '+' : '';
    grid.appendChild(statCard('对比上周', sign + d.duration, d.delta_percent ? (d.delta_percent > 0 ? '+' : '') + d.delta_percent.toFixed(0) + '%' : '—'));
  }
  wrap.appendChild(grid);

  wrap.appendChild(el('div', { class: 'card', style: 'margin-top:16px' },
    el('h3', {}, '每日趋势'),
    buildTrendChart(r.daily_trend)));
  wrap.appendChild(el('div', { class: 'card', style: 'margin-top:16px' },
    el('h3', {}, '分组占比'),
    barsFromGroups(r.group_breakdown)));
  return wrap;
}

function barsFromGroups(groups) {
  const wrap = el('div', {});
  if (!groups || groups.length === 0) { wrap.appendChild(emptyHint('无数据')); return wrap; }
  for (const g of groups) {
    wrap.appendChild(buildBar(g.group_name, g.percent, g.duration, g.group_color));
  }
  return wrap;
}
function barsFromParts(parts) {
  const wrap = el('div', {});
  for (const p of parts) {
    wrap.appendChild(buildBar(p.part, p.percent, p.duration, '#6366f1'));
  }
  return wrap;
}
function buildBar(name, percent, duration, color) {
  const pct = percent || 0;
  return el('div', { class: 'bar-row' },
    el('div', { class: 'name' }, name),
    el('div', { class: 'bar-track' }, el('div', { class: 'bar-fill', style: `width:${Math.min(100, pct)}%; background:${color || '#6366f1'}` })),
    el('div', { class: 'pct' }, duration + ' · ' + pct.toFixed(0) + '%'));
}
function buildHourHistogram(entries) { /* placeholder */ }
function buildHourBars(bins) {
  const max = Math.max(1, ...bins.map(b => b.seconds));
  const wrap = el('div', {});
  for (const b of bins) {
    if (b.seconds < 1) continue;
    const dur = Math.round(b.seconds / 60) + 'm';
    wrap.appendChild(buildBar(String(b.hour).padStart(2, '0') + ':00', (b.seconds / max) * 100, dur, '#818cf8'));
  }
  return wrap;
}
function buildTrendChart(days) {
  const wrap = el('div', {});
  const chart = el('div', { class: 'trend-chart' });
  const labels = el('div', { class: 'trend-labels' });
  const max = Math.max(1, ...days.map(d => d.seconds));
  for (const d of days) {
    const col = el('div', { class: 'trend-col' });
    const bar = el('div', { class: 'trend-bar', style: `height:${Math.max(2, (d.seconds / max) * 100)}%`, title: d.date + ' ' + d.duration });
    col.appendChild(bar);
    chart.appendChild(col);
    labels.appendChild(el('span', {}, d.weekday));
  }
  wrap.appendChild(chart);
  wrap.appendChild(labels);
  return wrap;
}

function buildImprovementEditor(date, improvement, notes) {
  const card = el('div', { class: 'card improvement-box', style: 'margin-top:16px' });
  card.appendChild(el('h3', {}, '每日提升 / 反思'));
  card.appendChild(el('label', {}, '今日提升（记录成长与收获）'));
  const imp = el('textarea', { id: 'imp-improvement' }); imp.value = improvement || '';
  card.appendChild(imp);
  card.appendChild(el('label', {}, '其他备注'));
  const note = el('textarea', { id: 'imp-notes' }); note.value = notes || '';
  card.appendChild(note);
  card.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      try {
        await api('PUT', '/api/summaries/daily?date=' + date, { improvement: imp.value, notes: note.value });
        alert('已保存');
        renderAnalysis();
      } catch (e) { alert(e.message); }
    } }, '保存')));
  return card;
}

// ============================================================
//  MODALS
// ============================================================
function bindModal() {
  $('#modal-close').addEventListener('click', closeModal);
  $('#modal').addEventListener('click', e => { if (e.target.id === 'modal') closeModal(); });
}
function closeModal() { $('#modal').classList.add('hidden'); $('#modal-body').innerHTML = ''; }
function openModal(title, bodyNode) {
  $('#modal-title').textContent = title;
  const body = $('#modal-body'); body.innerHTML = '';
  body.appendChild(bodyNode);
  $('#modal').classList.remove('hidden');
}

function openTodoModal(t) {
  const isChild = t && t.parent_id;
  const isNew = !t || !(t.id);
  const titleField = field('标题', input('text', t && t.title ? t.title : ''));
  const groupField = field('分组', groupSelect(t && t.group_id ? t.group_id : (isChild ? null : Number(state.currentGroup) || null)));
  const descField = field('描述', textarea(t && t.description ? t.description : ''));
  const prioField = field('优先级', input('number', t && t.priority != null ? t.priority : 0));
  const dueField = field('截止日期', input('date', t && t.due_date ? t.due_date : ''));
  const form = el('div', {});
  form.appendChild(el('div', { class: 'field' }, el('label', {}, isNew ? (isChild ? '新建子任务' : '新建待办') : '编辑待办')));
  form.appendChild(titleField);
  if (!isChild) form.appendChild(groupField);
  form.appendChild(descField);
  form.appendChild(el('div', { class: 'row' }, prioField, dueField));
  form.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      const body = {
        title: titleField.querySelector('input').value.trim(),
        description: descField.querySelector('textarea').value,
        priority: Number(prioField.querySelector('input').value || 0),
        due_date: dueField.querySelector('input').value || null,
      };
      if (!isChild && groupField.querySelector('select').value) body.group_id = Number(groupField.querySelector('select').value);
      if (isChild) body.parent_id = t.parent_id;
      try {
        if (isNew) await api('POST', '/api/todos', body);
        else await api('PUT', '/api/todos/' + t.id, body);
        await loadTodos();
        closeModal();
        renderView(state.view);
      } catch (e) { alert(e.message); }
    } }, '保存')));
  openModal(isNew ? (isChild ? '新建子任务' : '新建待办') : '编辑待办', form);
}

function openEntryModal(e, defaultDate) {
  const isNew = !e;
  const date = e ? e.start_time.slice(0, 10) : (defaultDate || todayStr());
  const startField = field('开始', input('time', e ? e.start_time.slice(11, 19) : '09:00'));
  const endField = field('结束', input('time', e && e.end_time ? e.end_time.slice(11, 19) : ''));
  const dateField = field('日期', input('date', date));
  const groupField = field('分组', groupSelect(e && e.group_id ? e.group_id : Number(state.currentGroup) || null));
  const todoField = field('关联待办（可选）', todoSelect(e && e.todo_id ? e.todo_id : null));
  const noteField = field('备注', input('text', e && e.note ? e.note : ''));
  const form = el('div', {});
  form.appendChild(dateField);
  form.appendChild(el('div', { class: 'row' }, startField, endField));
  form.appendChild(groupField);
  form.appendChild(todoField);
  form.appendChild(noteField);
  form.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      const d = dateField.querySelector('input').value;
      const st = startField.querySelector('input').value;
      const en = endField.querySelector('input').value;
      const body = {
        start_time: `${d} ${st}:00`,
        end_time: en ? `${d} ${en}:00` : null,
        note: noteField.querySelector('input').value,
        group_id: groupField.querySelector('select').value ? Number(groupField.querySelector('select').value) : null,
        todo_id: todoField.querySelector('select').value ? Number(todoField.querySelector('select').value) : null,
      };
      try {
        if (isNew) await api('POST', '/api/time-entries', body);
        else await api('PUT', '/api/time-entries/' + e.id, body);
        closeModal();
        await refreshActiveEntry();
        renderView(state.view);
      } catch (err) { alert(err.message); }
    } }, '保存')));
  openModal(isNew ? '补录工时' : '编辑工时', form);
}

// 新建/编辑分组弹窗（今日页与待办页共用入口，独立 DOM，不依赖 #group-editor）
function openGroupModal(group) {
  const isNew = !group;
  const nameField = field('分组名', input('text', group ? group.name : ''));
  const colorI = el('input', { class: 'color-input', type: 'color', value: group ? group.color : '#6366f1' });
  const colorField = el('div', { class: 'field' }, el('label', {}, '颜色'), colorI);
  const form = el('div', {});
  form.appendChild(el('div', { class: 'field' }, el('label', {}, isNew ? '新建分组' : '编辑分组')));
  form.appendChild(nameField);
  form.appendChild(colorField);
  form.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      const name = nameField.querySelector('input').value.trim();
      if (!name) return;
      const body = { name, color: colorI.value, sort_order: 0 };
      try {
        if (isNew) await api('POST', '/api/groups', body);
        else await api('PUT', '/api/groups/' + group.id, body);
        closeModal();
        await loadGroups();
        if (state.view === 'today') renderToday(); else renderView(state.view);
      } catch (e) { alert(e.message); }
    } }, '保存')));
  openModal(isNew ? '新建分组' : '编辑分组', form);
}

function field(labelText, control) {
  const f = el('div', { class: 'field' });
  f.appendChild(el('label', {}, labelText));
  f.appendChild(control);
  return f;
}
function input(type, value) {
  const i = el('input', { class: 'input', type });
  if (value) i.value = value;
  return i;
}
function textarea(value) {
  const t = el('textarea', { class: 'input', style: 'min-height:60px' });
  if (value) t.value = value;
  return t;
}
function groupSelect(selectedId) {
  const s = el('select', { class: 'select' });
  s.appendChild(el('option', { value: '' }, '未分组'));
  for (const g of state.groups) {
    const opt = el('option', { value: g.id }, g.name);
    if (selectedId && Number(selectedId) === g.id) opt.selected = true;
    s.appendChild(opt);
  }
  return s;
}
function todoSelect(selectedId) {
  const s = el('select', { class: 'select' });
  s.appendChild(el('option', { value: '' }, '无'));
  for (const t of state.todos) {
    if (t.status === 'done') continue;
    const opt = el('option', { value: t.id }, t.title);
    if (selectedId && Number(selectedId) === t.id) opt.selected = true;
    s.appendChild(opt);
  }
  return s;
}

// ---------- boot ----------
document.addEventListener('DOMContentLoaded', init);
