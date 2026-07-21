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
  timelineZoom: '12h',     // '24h' | '12h' (9-21)
  weekHourPx: 90,          // weekly vertical timeline: pixels per hour (zoom)
  todoFilter: 'all',       // 'all' | 'pending' | 'done'
  todoSort: 'newest',      // 'newest' | 'oldest'
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
    // 选中 todo（含子任务）后自动带出其分组
    const t = findTodoById(e.target.value);
    if (t && t.group_id) $('#timer-group').value = String(t.group_id);
    // For sub-tasks without their own group_id, find the parent's group
    if (t && !t.group_id && t.parent_id) {
      const parent = findTodoById(t.parent_id);
      if (parent && parent.group_id) $('#timer-group').value = String(parent.group_id);
    }
  });
  // 分组选择变化时联动过滤待办列表
  $('#timer-group').addEventListener('change', () => {
    const gid = $('#timer-group').value ? Number($('#timer-group').value) : null;
    fillTodoSelect($('#timer-todo'), null, gid);
  });
  $('#bf-group').addEventListener('change', () => {
    const gid = $('#bf-group').value ? Number($('#bf-group').value) : null;
    fillTodoSelect($('#bf-todo'), null, gid);
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
  // 时间轴缩放
  $('#timeline-zoom-seg').addEventListener('click', e => {
    const b = e.target.closest('.seg-btn');
    if (!b) return;
    state.timelineZoom = b.dataset.zoom;
    renderTodayTimelineAnalysis();
  });
}

function setTodayMode(mode) {
  state.todayMode = mode;
  $$('#today-mode-seg .seg-btn').forEach(b => b.classList.toggle('active', b.dataset.mode === mode));
  $('#today-timer-body').classList.toggle('hidden', mode !== 'timer');
  $('#today-backfill-body').classList.toggle('hidden', mode !== 'backfill');
  if (mode === 'backfill' && !$('#bf-date').value) {
    $('#bf-date').value = todayStr();
    $('#bf-end-date').value = todayStr();
  }
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
  if (!$('#bf-date').value) { $('#bf-date').value = todayStr(); $('#bf-end-date').value = todayStr(); }
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

function fillTodoSelect(sel, selectedId, groupId) {
  const prev = sel.value;
  sel.innerHTML = '<option value="">无</option>';

  // Flatten the todo tree: top-level + children recursively
  const flatList = [];
  function walk(todos, depth, parentGroupId) {
    for (const t of todos) {
      if (t.status === 'done') continue;
      // For sub-tasks, use the parent's group_id for filtering
      const effectiveGroupId = t.parent_id ? (parentGroupId ?? t.group_id) : t.group_id;
      if (groupId != null) {
        if (effectiveGroupId !== groupId && effectiveGroupId != null) continue;
      }
      const indent = depth > 0 ? '  '.repeat(depth) + '└ ' : '';
      const hasKids = t.children && t.children.length;
      const label = indent + t.title + (hasKids && depth === 0 ? ' …' : '');
      flatList.push({ id: t.id, label, groupId: effectiveGroupId });
      if (t.children && t.children.length) {
        walk(t.children, depth + 1, t.group_id);
      }
    }
  }
  walk(state.todos, 0, null);

  for (const item of flatList) {
    const opt = el('option', { value: item.id }, item.label);
    if (selectedId && Number(selectedId) === item.id) opt.selected = true;
    sel.appendChild(opt);
  }
  if (prev) sel.value = prev;
}

// Find a todo anywhere in the tree by ID (including children)
function findTodoById(id) {
  function search(todos) {
    for (const t of todos) {
      if (String(t.id) === String(id)) return t;
      if (t.children && t.children.length) {
        const found = search(t.children);
        if (found) return found;
      }
    }
    return null;
  }
  return search(state.todos);
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
  const sd = $('#bf-date').value || todayStr();
  const st = $('#bf-start').value;
  const ed = $('#bf-end-date').value || sd;
  const en = $('#bf-end').value;
  if (!st) { alert('请填写开始时间'); return; }
  const startDt = `${sd} ${st}:00`;
  const endDt = en ? `${ed} ${en}:00` : null;
  if (endDt && endDt <= startDt) { alert('结束时间必须晚于开始时间'); return; }
  if (!endDt && state.activeEntry) { alert('请先停止当前计时，再补录进行中的记录'); return; }
  const body = {
    start_time: startDt,
    end_time: endDt,
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
  // Update zoom button active state
  $$('#timeline-zoom-seg .seg-btn').forEach(b => b.classList.toggle('active', b.dataset.zoom === state.timelineZoom));
  wrap.appendChild(buildTimeline(entries, true, date, state.timelineZoom));
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

  // Filter & sort bar
  const titleText = state.currentGroup ? groupName(Number(state.currentGroup)) : '全部待办';
  $('#todos-title').innerHTML = '';
  $('#todos-title').appendChild(document.createTextNode(titleText));
  $('#todos-title').appendChild(buildTodoFilterBar());

  const list = $('#todo-list');
  list.innerHTML = '';
  let filtered = state.currentGroup
    ? state.todos.filter(t => t.group_id === Number(state.currentGroup))
    : state.todos;

  // Apply status filter
  if (state.todoFilter === 'pending') filtered = filtered.filter(t => t.status !== 'done');
  else if (state.todoFilter === 'done') filtered = filtered.filter(t => t.status === 'done');

  // Apply sort by created_at
  filtered = [...filtered].sort((a, b) => {
    const cmp = new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    return state.todoSort === 'newest' ? cmp : -cmp;
  });

  if (filtered.length === 0) { list.appendChild(emptyHint('没有待办，点右上角新建')); return; }
  for (const t of filtered) list.appendChild(buildTodoRow(t, false));
}

function buildTodoFilterBar() {
  const bar = el('span', { class: 'todo-filter-bar' });
  // Filter buttons
  const filters = [
    { key: 'all', label: '全部' },
    { key: 'pending', label: '待办' },
    { key: 'done', label: '已完成' },
  ];
  for (const f of filters) {
    bar.appendChild(el('button', {
      class: 'btn btn-small filter-btn' + (state.todoFilter === f.key ? ' active' : ''),
      onclick: (ev) => {
        ev.stopPropagation();
        state.todoFilter = f.key;
        renderTodos();
      },
    }, f.label));
  }
  // Sort toggle
  const sortLabel = state.todoSort === 'newest' ? '↓最新' : '↑最早';
  bar.appendChild(el('button', {
    class: 'btn btn-small sort-btn',
    onclick: (ev) => {
      ev.stopPropagation();
      state.todoSort = state.todoSort === 'newest' ? 'oldest' : 'newest';
      renderTodos();
    },
  }, sortLabel));
  return bar;
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
  const titleEl = el('div', { class: 'todo-title clickable' }, t.title);
  titleEl.addEventListener('click', (ev) => {
    ev.stopPropagation();
    toggleInlineAnalysis(t, item);
  });
  main.appendChild(titleEl);
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

// ---- Inline Todo Analysis (below todo item) ----
let inlineAnalysisState = { todo: null, month: '', container: null };

function toggleInlineAnalysis(t, todoItem) {
  // Remove existing inline analysis if any
  const existing = todoItem.nextElementSibling;
  if (existing && existing.classList.contains('inline-analysis')) {
    existing.remove();
    return;
  }

  const container = el('div', { class: 'inline-analysis' });
  todoItem.insertAdjacentElement('afterend', container);
  renderInlineAnalysis(t, container);
}

async function renderInlineAnalysis(t, container) {
  inlineAnalysisState.todo = t;
  inlineAnalysisState.container = container;
  if (!inlineAnalysisState.month) inlineAnalysisState.month = todayStr().slice(0, 7);

  const descCount = countDescendants(t);

  try {
    const [allEntries, monthEntries] = await Promise.all([
      api('GET', '/api/todos/' + t.id + '/time-entries'),
      api('GET', '/api/todos/' + t.id + '/time-entries/monthly?month=' + inlineAnalysisState.month),
    ]);
    const totalSecs = allEntries.reduce((acc, e) => acc + entryDurationSecs(e), 0);
    const totalDur = fmtDurationSecs(totalSecs);
    const monthSecs = monthEntries.reduce((acc, e) => acc + entryDurationSecs(e), 0);

    container.appendChild(el('div', { class: 'analysis-head' },
      el('h3', {}, '📊 ' + t.title + ' 分析'),
      el('button', { class: 'btn btn-small', onclick: () => container.remove() }, '✕ 关闭')
    ));

    // Stats grid
    const grid = el('div', { class: 'stat-grid', style: 'margin-bottom:16px' });
    grid.appendChild(statCard('累计总工时', totalDur, allEntries.length + ' 条记录'));
    grid.appendChild(statCard('本月工时', fmtDurationSecs(monthSecs), monthEntries.length + ' 条记录'));
    grid.appendChild(statCard('子任务数', String(descCount), statusLabel(t.status)));
    container.appendChild(grid);

    // Monthly heatmap with navigation
    container.appendChild(el('h4', {}, '月度热力图'));
    container.appendChild(buildHeatmapNav());
    container.appendChild(buildHeatmap(monthEntries, inlineAnalysisState.month));

    // Legend
    container.appendChild(buildHeatmapLegend());

    // Show todo description
    if (t.description && t.description.trim()) {
      container.appendChild(el('div', { class: 'todo-desc' },
        el('h4', {}, '描述'),
        el('p', {}, t.description)
      ));
    }

    if (allEntries.length === 0) {
      container.appendChild(emptyHint('该待办暂没有时间记录'));
      return;
    }
    container.appendChild(el('h4', { style: 'margin-top:16px' }, '时间记录列表'));
    for (const e of monthEntries) {
      container.appendChild(buildEntryRow(e, () => renderInlineAnalysis(t, container)));
    }
  } catch (err) {
    container.appendChild(emptyHint('加载失败：' + err.message));
  }
}

function buildHeatmapNav() {
  const [y, m] = inlineAnalysisState.month.split('-').map(Number);
  const prevM = m === 1 ? 12 : m - 1;
  const prevY = m === 1 ? y - 1 : y;
  const nextM = m === 12 ? 1 : m + 1;
  const nextY = m === 12 ? y + 1 : y;
  const prevStr = prevY + '-' + String(prevM).padStart(2, '0');
  const nextStr = nextY + '-' + String(nextM).padStart(2, '0');
  const label = y + '年' + m + '月';

  return el('div', { class: 'heatmap-nav' },
    el('button', { class: 'btn btn-small', onclick: () => { inlineAnalysisState.month = prevStr; renderInlineAnalysis(inlineAnalysisState.todo, inlineAnalysisState.container); } }, '◀'),
    el('span', { style: 'font-weight:600;margin:0 8px' }, label),
    el('button', { class: 'btn btn-small', onclick: () => { inlineAnalysisState.month = nextStr; renderInlineAnalysis(inlineAnalysisState.todo, inlineAnalysisState.container); } }, '▶'),
  );
}

function buildHeatmap(entries, month) {
  const [year, mon] = month.split('-').map(Number);
  const daysInMonth = new Date(year, mon, 0).getDate(); // mon is 1-based, Date month 0-based → gets last day of previous month
  const firstDayOfWeek = new Date(year, mon - 1, 1).getDay(); // 0=Sunday

  // Aggregate seconds per day
  const secsByDay = {};
  for (const e of entries) {
    const d = e.start_time.slice(8, 10); // "DD"
    const dayNum = parseInt(d, 10);
    secsByDay[dayNum] = (secsByDay[dayNum] || 0) + entryDurationSecs(e);
  }

  // Find max for color scaling
  const allSecs = Object.values(secsByDay);
  const maxSecs = allSecs.length ? Math.max(...allSecs) : 1;

  // Day-of-week headers (Mon-Sun)
  const grid = el('div', { class: 'heatmap-grid' });
  const DAYS = ['日', '一', '二', '三', '四', '五', '六'];
  for (const d of DAYS) {
    grid.appendChild(el('div', { class: 'heatmap-header' }, d));
  }

  // Empty cells before first day
  for (let i = 0; i < firstDayOfWeek; i++) {
    grid.appendChild(el('div', { class: 'heatmap-cell empty' }));
  }

  for (let day = 1; day <= daysInMonth; day++) {
    const secs = secsByDay[day] || 0;
    const intensity = secs > 0 ? Math.min(1, secs / Math.max(maxSecs, 3600)) : 0;
    const dateStr = month + '-' + String(day).padStart(2, '0');
    const title = dateStr + (secs > 0 ? ' · ' + fmtDurationSecs(secs) : '');
    const isToday = dateStr === todayStr();

    const cell = el('div', {
      class: 'heatmap-cell' + (isToday ? ' today' : ''),
      style: `--intensity:${intensity}`,
      title,
    }, String(day));
    if (secs > 0) cell.classList.add('filled');
    grid.appendChild(cell);
  }

  return grid;
}

function buildHeatmapLegend() {
  return el('div', { class: 'heatmap-legend' },
    el('span', { class: 'legend-label' }, '少'),
    el('span', { class: 'legend-swatch', style: '--intensity:0.1' }),
    el('span', { class: 'legend-swatch', style: '--intensity:0.3' }),
    el('span', { class: 'legend-swatch', style: '--intensity:0.5' }),
    el('span', { class: 'legend-swatch', style: '--intensity:0.75' }),
    el('span', { class: 'legend-swatch', style: '--intensity:1' }),
    el('span', { class: 'legend-label' }, '多'),
  );
}

function countDescendants(t) {
  if (!t.children || t.children.length === 0) return 0;
  let n = t.children.length;
  for (const c of t.children) n += countDescendants(c);
  return n;
}

function entryDurationSecs(e) {
  const start = new Date(e.start_time.replace(' ', 'T')).getTime();
  const end = e.end_time ? new Date(e.end_time.replace(' ', 'T')).getTime() : Date.now();
  return Math.max(0, (end - start) / 1000);
}

function fmtDurationSecs(secs) {
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  if (h > 0) return h + 'h ' + m + 'm';
  if (m > 0) return m + 'm';
  return Math.floor(secs) + 's';
}

function statusLabel(s) {
  return s === 'done' ? '已完成' : s === 'in_progress' ? '进行中' : '待办';
}

// ---- 分组编辑 ----
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
//  ANALYSIS
// ============================================================
function buildTimeline(entries, showNow, viewDate, zoom) {
  const wrap = el('div', {});
  wrap.appendChild(buildTimelineTrack(entries, showNow, viewDate, zoom));
  wrap.appendChild(buildAxis(zoom));
  return wrap;
}

// buildTimelineTrack renders just the positioned block track (no axis). It is
// shared by the single-day timeline and the 7-day parallel timeline. Each
// block is clipped to the visible range of viewDate's day, so passing the
// full week's entries with a per-day viewDate correctly renders only the
// portion overlapping that day (cross-midnight entries show on both days).
function buildTimelineTrack(entries, showNow, viewDate, zoom) {
  const host = el('div', { class: 'timeline-host' });
  if (!entries || entries.length === 0) {
    host.appendChild(el('div', { class: 'tl-empty', style: 'width:100%' }, '暂无记录'));
    return host;
  }
  // Determine visible range
  const z = zoom || '12h';
  const rangeStart = z === '12h' ? 9 : 0;
  const rangeEnd = z === '12h' ? 21 : 24;
  const rangeSecs = (rangeEnd - rangeStart) * 3600;

  // Use the viewing date for dayStart calculation, not each entry's own date.
  const dateStr = viewDate || todayStr();
  const dayStart = new Date(dateStr + 'T00:00:00').getTime();
  const rangeStartMs = dayStart + rangeStart * 3600 * 1000;
  const rangeEndMs = dayStart + rangeEnd * 3600 * 1000;
  for (const e of entries) {
    const start = new Date(e.start_time.replace(' ', 'T')).getTime();
    const endSecs = e.end_time ? new Date(e.end_time.replace(' ', 'T')).getTime() : Date.now();
    const s = Math.max(start, rangeStartMs);
    const en = Math.min(endSecs, rangeEndMs);
    if (en <= s) continue;
    const leftPct = ((s - rangeStartMs) / (rangeSecs * 1000)) * 100;
    const widthPct = ((en - s) / (rangeSecs * 1000)) * 100;
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
    const now = Date.now();
    if (now >= rangeStartMs && now <= rangeEndMs) {
      const pct = ((now - rangeStartMs) / (rangeSecs * 1000)) * 100;
      host.appendChild(el('div', { class: 'tl-now', style: `left:${pct}%` }));
    }
  }
  return host;
}
function buildAxis(zoom) {
  const z = zoom || '12h';
  const startH = z === '12h' ? 9 : 0;
  const endH = z === '12h' ? 21 : 24;
  const totalSlots = endH - startH;
  const axis = el('div', { class: 'tl-axis', style: `grid-template-columns: repeat(${totalSlots}, 1fr)` });
  for (let h = startH; h < endH; h++) {
    const span = el('span', {}, String(h) + ':00');
    if (h % 3 === 0) span.classList.add('major');
    axis.appendChild(span);
  }
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
  const scopeButtons = $$('#view-analysis .seg-btn[data-scope]');
  scopeButtons.forEach(b => b.addEventListener('click', () => {
    state.analysisScope = b.dataset.scope;
    scopeButtons.forEach(x => x.classList.toggle('active', x === b));
    $('#analysis-date').classList.toggle('hidden', state.analysisScope !== 'daily');
    $('#analysis-week').classList.toggle('hidden', state.analysisScope !== 'weekly');
    if (state.analysisScope === 'weekly' && !state.analysisWeek) state.analysisWeek = currentISOWeek();
    $('#analysis-week').value = state.analysisWeek;
    renderAnalysis();
  }));
  $('#analysis-date').value = state.analysisDate;
  $('#analysis-date').addEventListener('change', e => {
    state.analysisDate = e.target.value || todayStr();
    e.target.value = state.analysisDate;
    renderAnalysis();
  });
  $('#analysis-week').addEventListener('change', e => {
    state.analysisWeek = e.target.value || currentISOWeek();
    e.target.value = state.analysisWeek;
    renderAnalysis();
  });
  $('#analysis-prev').addEventListener('click', () => shiftAnalysis(-1));
  $('#analysis-next').addEventListener('click', () => shiftAnalysis(1));
}
function currentISOWeek() {
  const d = new Date();
  return isoWeekForUTCDate(new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate())));
}
function shiftDate(date, dir) {
  const m = (date || '').match(/^(\d{4})-(\d{2})-(\d{2})$/);
  if (!m) return todayStr();
  const dt = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
  dt.setUTCDate(dt.getUTCDate() + dir);
  return dt.toISOString().slice(0, 10);
}
function shiftAnalysis(dir) {
  if (state.analysisScope === 'daily') {
    state.analysisDate = shiftDate(state.analysisDate, dir);
    $('#analysis-date').value = state.analysisDate;
  } else {
    state.analysisWeek = shiftISOWeek(state.analysisWeek || currentISOWeek(), dir);
    $('#analysis-week').value = state.analysisWeek;
  }
  renderAnalysis();
}
function shiftISOWeek(week, dir) {
  const m = (week || '').match(/^(\d{4})-W(\d{2})$/);
  if (!m) return currentISOWeek();
  const jan4 = new Date(Date.UTC(Number(m[1]), 0, 4));
  const monday = new Date(jan4);
  monday.setUTCDate(jan4.getUTCDate() - ((jan4.getUTCDay() || 7) - 1) + (Number(m[2]) - 1) * 7 + dir * 7);
  return isoWeekForUTCDate(monday);
}
function isoWeekForUTCDate(date) {
  const tmp = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()));
  const dayNum = tmp.getUTCDay() || 7;
  tmp.setUTCDate(tmp.getUTCDate() + 4 - dayNum);
  const year = tmp.getUTCFullYear();
  const yearStart = new Date(Date.UTC(year, 0, 1));
  const week = Math.ceil(((tmp - yearStart) / 86400000 + 1) / 7);
  return year + '-W' + String(week).padStart(2, '0');
}

async function renderAnalysis() {
  const host = $('#analysis-content');
  host.innerHTML = '';
  try {
    if (state.analysisScope === 'daily') {
      state.analysisDate = state.analysisDate || todayStr();
      $('#analysis-date').value = state.analysisDate;
      const [r, entries] = await Promise.all([
        api('GET', '/api/analysis/daily?date=' + state.analysisDate),
        api('GET', '/api/time-entries?date=' + state.analysisDate),
      ]);
      // 给每日分析加上当日时间轴
      const tlCard = el('div', { class: 'card', style: 'margin-bottom:16px' });
      const tlHead = el('div', { class: 'panel-head', style: 'justify-content:space-between' },
        el('h3', {}, state.analysisDate + ' 时间轴'),
        el('div', { class: 'seg' },
          el('button', { class: 'seg-btn' + (state.timelineZoom === '24h' ? ' active' : ''), onclick: () => { state.timelineZoom = '24h'; renderAnalysis(); } }, '全天'),
          el('button', { class: 'seg-btn' + (state.timelineZoom === '12h' ? ' active' : ''), onclick: () => { state.timelineZoom = '12h'; renderAnalysis(); } }, '9-21'),
        ));
      tlCard.appendChild(tlHead);
      tlCard.appendChild(buildTimeline(entries, state.analysisDate === todayStr(), state.analysisDate, state.timelineZoom));
      host.appendChild(tlCard);
      host.appendChild(renderDailyAnalysis(r));
    } else {
      const w = state.analysisWeek || currentISOWeek();
      state.analysisWeek = w;
      $('#analysis-week').value = w;
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

  // Stats + Pie chart side-by-side
  const flexRow = el('div', { class: 'analysis-flex-row' });

  const leftGrid = el('div', { class: 'stat-grid', style: 'flex:1' });
  leftGrid.appendChild(statCard('总工时', r.total_duration, r.entry_count + ' 条记录'));
  leftGrid.appendChild(statCard('完成任务', String(r.completed_todos), r.active_todo_count + ' 个进行中'));
  if (r.longest_focus) leftGrid.appendChild(statCard('最长专注', r.longest_focus.duration, r.longest_focus.entry_count + ' 条连续'));
  if (r.vs_yesterday) {
    const d = r.vs_yesterday;
    const sign = d.delta_seconds > 0 ? '+' : '';
    leftGrid.appendChild(statCard('对比昨日', sign + d.duration, d.delta_percent ? (d.delta_percent > 0 ? '+' : '') + d.delta_percent.toFixed(0) + '%' : '—'));
  }
  flexRow.appendChild(leftGrid);

  flexRow.appendChild(el('div', { class: 'card', style: 'flex:1; margin-left:16px' },
    el('h3', {}, '分组占比'),
    buildPieChart(r.group_breakdown)));

  wrap.appendChild(flexRow);


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

  // 7-day parallel timeline: one horizontal track per weekday.
  const tlCard = el('div', { class: 'card', style: 'margin-top:16px' });
  tlCard.appendChild(el('div', { class: 'panel-head', style: 'justify-content:space-between' },
    el('h3', {}, '本周时间轴 · 7天'),
    el('div', { style: 'display:flex;gap:8px' },
      el('div', { class: 'seg' },
        el('button', { class: 'seg-btn' + (state.timelineZoom === '24h' ? ' active' : ''), onclick: () => { state.timelineZoom = '24h'; renderAnalysis(); } }, '全天'),
        el('button', { class: 'seg-btn' + (state.timelineZoom === '12h' ? ' active' : ''), onclick: () => { state.timelineZoom = '12h'; renderAnalysis(); } }, '9-21'),
      ),
      el('div', { class: 'seg', title: '缩放时间轴' },
        el('button', { class: 'seg-btn week-cal-zoom-out', title: '缩小', disabled: state.weekHourPx <= 32 ? true : null, onclick: () => setWeekZoom(state.weekHourPx - 16) }, '－'),
        el('button', { class: 'seg-btn week-cal-zoom-in', title: '放大', disabled: state.weekHourPx >= 192 ? true : null, onclick: () => setWeekZoom(state.weekHourPx + 16) }, '＋'),
      ),
    )));
  tlCard.appendChild(buildWeekTimeline(r.daily_trend, r.weekly_entries, state.timelineZoom));
  wrap.appendChild(tlCard);

  wrap.appendChild(el('div', { class: 'card', style: 'margin-top:16px' },
    el('h3', {}, '每日趋势'),
    buildTrendChart(r.daily_trend)));
  wrap.appendChild(el('div', { class: 'card', style: 'margin-top:16px' },
    el('h3', {}, '分组占比'),
    buildPieChart(r.group_breakdown)));
  return wrap;
}

function barsFromGroups(groups) {
  return buildPieChart(groups);
}
function barsFromParts(parts) {
  const wrap = el('div', {});
  for (const p of parts) {
    wrap.appendChild(buildBar(p.part, p.percent, p.duration, '#6366f1'));
  }
  return wrap;
}
function buildPieChart(groups) {
  const wrap = el('div', { class: 'pie-chart-wrap' });
  if (!groups || groups.length === 0 || groups.every(g => g.seconds < 1)) {
    wrap.appendChild(emptyHint('无数据'));
    return wrap;
  }

  const meaningful = groups.filter(g => g.percent >= 0.5);
  const size = 160;
  const r = 60;
  const cx = 80, cy = 80;

  // Build SVG
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${size} ${size}`);
  svg.setAttribute('class', 'pie-svg');
  svg.style.width = size + 'px';

  if (meaningful.length === 1) {
    // Single group: draw a full circle (avoids degenerate arc when only 1 data point)
    const g = meaningful[0];
    const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
    circle.setAttribute('cx', cx);
    circle.setAttribute('cy', cy);
    circle.setAttribute('r', r);
    circle.setAttribute('fill', g.group_color);
    circle.setAttribute('stroke', 'var(--panel)');
    circle.setAttribute('stroke-width', '2');
    circle.setAttribute('class', 'pie-slice');
    const pctLabel = g.percent.toFixed(0) + '%';
    const title = document.createElementNS('http://www.w3.org/2000/svg', 'title');
    title.textContent = g.group_name + ' · ' + g.duration + ' · ' + pctLabel;
    circle.appendChild(title);
    svg.appendChild(circle);

    // Center text
    const totalDur = tutilFormatDuration(g.seconds);
    if (totalDur) {
      const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
      text.setAttribute('x', cx);
      text.setAttribute('y', cy + 4);
      text.setAttribute('text-anchor', 'middle');
      text.setAttribute('class', 'pie-center-text');
      text.textContent = totalDur;
      svg.appendChild(text);
    }
  } else {
    let totalPercent = 0;
    let angle = -Math.PI / 2; // Start from top
    for (const g of meaningful) {
      const sliceDeg = (g.percent / 100) * Math.PI * 2;
      const x1 = cx + r * Math.cos(angle);
      const y1 = cy + r * Math.sin(angle);
      angle += sliceDeg;
      const x2 = cx + r * Math.cos(angle);
      const y2 = cy + r * Math.sin(angle);
      const large = sliceDeg > Math.PI ? 1 : 0;
      const d = `M${cx},${cy} L${x1},${y1} A${r},${r} 0 ${large} 1 ${x2},${y2} Z`;

      const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      path.setAttribute('d', d);
      path.setAttribute('fill', g.group_color);
      path.setAttribute('stroke', 'var(--panel)');
      path.setAttribute('stroke-width', '2');
      path.setAttribute('class', 'pie-slice');
      const pctLabel = g.percent.toFixed(0) + '%';
      path.appendChild(document.createElementNS('http://www.w3.org/2000/svg', 'title')).textContent = g.group_name + ' · ' + g.duration + ' · ' + pctLabel;
      svg.appendChild(path);
      totalPercent += g.percent;
    }

    // Remaining slice if < 100%
    if (totalPercent < 100) {
      const remDeg = ((100 - totalPercent) / 100) * Math.PI * 2;
      const x1 = cx + r * Math.cos(angle);
      const y1 = cy + r * Math.sin(angle);
      angle += remDeg;
      const x2 = cx + r * Math.cos(angle);
      const y2 = cy + r * Math.sin(angle);
      const d = `M${cx},${cy} L${x1},${y1} A${r},${r} 0 0 1 ${x2},${y2} Z`;
      const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      path.setAttribute('d', d);
      path.setAttribute('fill', 'var(--panel-2)');
      path.setAttribute('stroke', 'var(--panel)');
      path.setAttribute('stroke-width', '2');
      svg.appendChild(path);
    }

    // Center text
    const totalDur = groups.reduce((acc, g) => acc + g.duration.length, 0) > 0
      ? tutilFormatDuration(groups.reduce((acc, g) => acc + g.seconds, 0))
      : '';
    if (totalDur) {
      const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
      text.setAttribute('x', cx);
      text.setAttribute('y', cy + 4);
      text.setAttribute('text-anchor', 'middle');
      text.setAttribute('class', 'pie-center-text');
      text.textContent = totalDur;
      svg.appendChild(text);
    }
  }

  wrap.appendChild(svg);

  // Legend
  const legend = el('div', { class: 'pie-legend' });
  for (const g of groups) {
    legend.appendChild(el('div', { class: 'pie-legend-item' },
      el('span', { class: 'pie-legend-dot', style: `background:${g.group_color}` }),
      el('span', { class: 'pie-legend-name' }, g.group_name),
      el('span', { class: 'pie-legend-pct' }, g.duration + ' · ' + g.percent.toFixed(0) + '%'),
    ));
  }
  wrap.appendChild(legend);
  return wrap;
}

function tutilFormatDuration(secs) {
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  if (h > 0) return h + 'h ' + m + 'm';
  if (m > 0) return m + 'm';
  return Math.floor(secs) + 's';
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

// buildWeekTimeline renders the 7 days of a week as a vertical, scrollable
// calendar grid: the hour axis runs top→bottom in a left gutter, each weekday
// is a column, and work blocks are positioned vertically by their start/end
// time (clipped per day, so cross-midnight entries show on both days).
//
// Hour height is driven by the CSS custom property --hour-px (set on the root
// from state.weekHourPx), so zooming is a live CSS-var change — no re-render,
// no scroll jump. Wheel scrolling is native (overflow-y: auto).
function buildWeekTimeline(trend, entries, zoom) {
  const z = zoom || '12h';
  const rangeStart = z === '12h' ? 9 : 0;
  const rangeEnd = z === '12h' ? 21 : 24;
  const totalHours = rangeEnd - rangeStart;
  const today = todayStr();
  const hasEntries = entries && entries.length > 0;

  // Fixed day-header row (stays put while the grid scrolls).
  const head = el('div', { class: 'week-cal-head' });
  head.appendChild(el('div', { class: 'week-cal-head-spacer' }));
  for (const d of trend) {
    const isToday = d.date === today;
    head.appendChild(el('div', { class: 'week-cal-col-head' + (isToday ? ' is-today' : '') },
      el('span', { class: 'week-cal-wd' }, d.weekday),
      el('span', { class: 'week-cal-date' }, d.date.slice(5)),
      el('span', { class: 'week-cal-dur' }, d.seconds > 0 ? d.duration : '')));
  }

  // Scrollable grid: hour gutter + 7 day columns.
  const scroll = el('div', { class: 'week-cal-scroll' });
  const inner = el('div', { class: 'week-cal-inner' });

  const gutter = el('div', { class: 'week-cal-gutter' });
  for (let h = rangeStart; h < rangeEnd; h++) {
    gutter.appendChild(el('div', { class: 'week-cal-hour', style: `--hh:${h - rangeStart}` },
      String(h).padStart(2, '0') + ':00'));
  }
  inner.appendChild(gutter);

  for (const d of trend) {
    const isToday = d.date === today;
    const col = el('div', { class: 'week-cal-col' + (isToday ? ' is-today' : '') });
    const dayStart = new Date(d.date + 'T00:00:00').getTime();
    const rangeStartMs = dayStart + rangeStart * 3600 * 1000;
    const rangeEndMs = dayStart + rangeEnd * 3600 * 1000;
    if (hasEntries) {
      for (const e of entries) {
        const start = new Date(e.start_time.replace(' ', 'T')).getTime();
        const endSecs = e.end_time ? new Date(e.end_time.replace(' ', 'T')).getTime() : Date.now();
        const s = Math.max(start, rangeStartMs);
        const en = Math.min(endSecs, rangeEndMs);
        if (en <= s) continue;
        const topH = (s - rangeStartMs) / 3600000;
        const heightH = (en - s) / 3600000;
        const block = el('div', {
          class: 'week-cal-block',
          style: `--top-h:${topH.toFixed(3)};--h-h:${heightH.toFixed(3)};background:${groupColor(e.group_id)}`,
          title: `${fmtTime(e.start_time)} - ${e.end_time ? fmtTime(e.end_time) : '进行中'} · ${groupName(e.group_id)}${e.todo_title ? ' · ' + e.todo_title : ''}`,
        });
        block.textContent = groupName(e.group_id);
        block.addEventListener('click', () => openEntryModal(e));
        col.appendChild(block);
      }
    }
    if (isToday) {
      const now = Date.now();
      if (now >= rangeStartMs && now <= rangeEndMs) {
        const nowH = (now - rangeStartMs) / 3600000;
        col.appendChild(el('div', { class: 'week-cal-now', style: `--top-h:${nowH.toFixed(3)}` }));
      }
    }
    inner.appendChild(col);
  }
  scroll.appendChild(inner);

  const wrap = el('div', { class: 'week-cal', style: `--hour-px:${state.weekHourPx}px;--hours:${totalHours}` });
  wrap.appendChild(head);
  wrap.appendChild(scroll);
  return wrap;
}

// setWeekZoom changes the weekly timeline's hour height live (CSS variable),
// so the grid scales instantly without re-rendering or losing scroll position.
function setWeekZoom(px) {
  state.weekHourPx = Math.max(32, Math.min(192, Math.round(px)));
  const cal = document.querySelector('#analysis-content .week-cal');
  if (cal) cal.style.setProperty('--hour-px', state.weekHourPx + 'px');
  const inBtn = document.querySelector('#analysis-content .week-cal-zoom-in');
  const outBtn = document.querySelector('#analysis-content .week-cal-zoom-out');
  if (inBtn) inBtn.disabled = state.weekHourPx >= 192;
  if (outBtn) outBtn.disabled = state.weekHourPx <= 32;
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
  const startDate = e ? e.start_time.slice(0, 10) : (defaultDate || todayStr());
  const endDate = e && e.end_time ? e.end_time.slice(0, 10) : startDate;
  const startTime = e ? e.start_time.slice(11, 16) : '09:00';
  const endTime = e && e.end_time ? e.end_time.slice(11, 16) : '';
  const initialGroupId = e && e.group_id ? e.group_id : Number(state.currentGroup) || null;
  const initialTodoId = e && e.todo_id ? e.todo_id : null;

  const startDateField = field('开始日期', input('date', startDate));
  const startTimeField = field('开始时间', input('time', startTime));
  const endDateField = field('结束日期', input('date', endDate));
  const endTimeField = field('结束时间', input('time', endTime));

  // Build group select
  const gs = groupSelect(initialGroupId);
  const todoWrapper = el('div', { class: 'field' }, el('label', {}, '关联待办（可选）'));
  function buildTodoField(groupId) {
    todoWrapper.querySelectorAll('select').forEach(s => s.remove());
    todoWrapper.appendChild(todoSelect(initialTodoId, groupId));
  }
  buildTodoField(initialGroupId);
  gs.addEventListener('change', function () {
    const gid = this.value ? Number(this.value) : null;
    buildTodoField(gid);
  });

  const noteField = field('备注', input('text', e && e.note ? e.note : ''));
  const form = el('div', {});
  form.appendChild(el('div', { class: 'row' }, startDateField, startTimeField));
  form.appendChild(el('div', { class: 'row' }, endDateField, endTimeField));
  form.appendChild(el('div', { class: 'field' }, el('label', {}, '分组'), gs));
  form.appendChild(todoWrapper);
  form.appendChild(noteField);
  form.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      const sd = startDateField.querySelector('input').value;
      const st = startTimeField.querySelector('input').value;
      const ed = endDateField.querySelector('input').value;
      const en = endTimeField.querySelector('input').value;
      const startDt = `${sd} ${st}:00`;
      const endDt = en ? `${ed} ${en}:00` : null;
      if (!st) { alert('请填写开始时间'); return; }
      if (endDt && endDt <= startDt) { alert('结束时间必须晚于开始时间'); return; }
      const body = {
        start_time: startDt,
        end_time: endDt,
        note: noteField.querySelector('input').value,
        group_id: gs.value ? Number(gs.value) : null,
        todo_id: todoWrapper.querySelector('select').value ? Number(todoWrapper.querySelector('select').value) : null,
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
function todoSelect(selectedId, groupId) {
  const s = el('select', { class: 'select' });
  s.appendChild(el('option', { value: '' }, '无'));
  // Flatten the todo tree so sub-tasks are selectable (indented under their
  // parent). Sub-tasks inherit the parent's group for filtering.
  const flatList = [];
  function walk(todos, depth, parentGroupId) {
    for (const t of todos) {
      if (t.status === 'done') continue;
      const effectiveGroupId = t.parent_id ? (parentGroupId ?? t.group_id) : t.group_id;
      if (groupId != null) {
        if (effectiveGroupId !== groupId && effectiveGroupId != null) continue;
      }
      const indent = depth > 0 ? '  '.repeat(depth) + '└ ' : '';
      flatList.push({ id: t.id, label: indent + t.title });
      if (t.children && t.children.length) {
        walk(t.children, depth + 1, t.group_id);
      }
    }
  }
  walk(state.todos, 0, null);
  for (const item of flatList) {
    const opt = el('option', { value: item.id }, item.label);
    if (selectedId && Number(selectedId) === item.id) opt.selected = true;
    s.appendChild(opt);
  }
  return s;
}

// ---------- boot ----------
document.addEventListener('DOMContentLoaded', init);
