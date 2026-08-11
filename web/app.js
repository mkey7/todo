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
  todayTaskTag: '',
  timelineZoom: '12h',     // '24h' | configured work range
  weekHourPx: 90,          // weekly vertical timeline: pixels per hour (zoom)
  todoFilter: 'all',       // 'all' | 'pending' | 'done'
  todoSort: 'newest',      // 'newest' | 'oldest'
  todoTagIDs: [],          // selected tag ids for the todo list
  todoExcludedTagIDs: [],  // selected NOT tag ids for the todo list
  todoTagRule: 'or',       // 'or' matches any tag; 'and' matches every tag
  theme: localStorage.getItem('todo-theme') || 'light',
  timelineWorkStart: Number(localStorage.getItem('todo-timeline-work-start') || 9),
  timelineWorkEnd: Number(localStorage.getItem('todo-timeline-work-end') || 21),
};

function groupColor(id) {
  const g = state.groups.find(x => x.id === id);
  return g ? g.color : '#9ca3af';
}
function groupName(id) {
  const g = state.groups.find(x => x.id === id);
  return g ? g.name : '未分组';
}
function entryColor(entry) {
  return entry.todo_primary_color || groupColor(entry.tag_id);
}
function tagIDByName(name) {
  const tag = state.groups.find(t => t.name === name);
  return tag ? tag.id : null;
}
async function ensureTag(name, color) {
  const id = tagIDByName(name);
  if (id) return id;
  const tag = await api('POST', '/api/tags', { name, description: '系统状态标签', color, include_in_stats: false });
  await loadGroups();
  return tag.id;
}
function entryTitle(entry) {
  return entry.todo_title || '未关联任务';
}

// ---------- init ----------
async function init() {
  applyTheme();
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
  if (!$('#clock')) return;
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
  if (view === 'settings') renderSettings();
}

function applyTheme() {
  document.documentElement.dataset.theme = state.theme;
}
function saveSettings() {
  localStorage.setItem('todo-theme', state.theme);
  localStorage.setItem('todo-timeline-work-start', String(state.timelineWorkStart));
  localStorage.setItem('todo-timeline-work-end', String(state.timelineWorkEnd));
  applyTheme();
}
function renderSettings() {
  const host = $('#settings-content');
  host.innerHTML = '';
  host.appendChild(el('h3', {}, '设置'));
  const themeField = el('div', { class: 'settings-field' }, el('label', {}, '主题'));
  const themeOptions = el('div', { class: 'seg' });
  for (const option of [{ key: 'light', label: '白色' }, { key: 'dark', label: '黑色' }]) {
    themeOptions.appendChild(el('button', { class: 'seg-btn' + (state.theme === option.key ? ' active' : ''), onclick: () => { state.theme = option.key; saveSettings(); renderSettings(); } }, option.label));
  }
  themeField.appendChild(themeOptions);
  host.appendChild(themeField);

  const rangeField = el('div', { class: 'settings-field' }, el('label', {}, '工作时段时间轴'));
  const range = el('div', { class: 'row' });
  const start = el('select', { class: 'select', 'aria-label': '工作时段开始时间' });
  const end = el('select', { class: 'select', 'aria-label': '工作时段结束时间' });
  for (let h = 0; h < 24; h++) start.appendChild(el('option', { value: h }, String(h).padStart(2, '0') + ':00'));
  for (let h = 1; h <= 24; h++) end.appendChild(el('option', { value: h }, String(h).padStart(2, '0') + ':00'));
  start.value = String(state.timelineWorkStart); end.value = String(state.timelineWorkEnd);
  const saveRange = () => {
    const s = Number(start.value), e = Number(end.value);
    if (e <= s) { alert('结束时间必须晚于开始时间'); return; }
    state.timelineWorkStart = s; state.timelineWorkEnd = e; saveSettings();
  };
  start.addEventListener('change', saveRange); end.addEventListener('change', saveRange);
  range.append(start, el('span', { class: 'settings-range-sep' }, '至'), end);
  rangeField.appendChild(range);
  rangeField.appendChild(el('span', { class: 'settings-hint' }, '“工作时段”模式使用此范围；“全天”模式固定展示 24 小时。'));
  host.appendChild(rangeField);
}

function timelineRange(zoom) {
  return zoom === '24h'
    ? { start: 0, end: 24 }
    : { start: state.timelineWorkStart, end: state.timelineWorkEnd };
}

// ---------- data loaders ----------
async function loadGroups() {
  state.groups = await api('GET', '/api/tags');
  if (state.groups.length === 0) {
    await api('POST', '/api/tags', { name: '默认', description: '默认标签', color: '#6366f1' });
    state.groups = await api('GET', '/api/tags');
  }
}
async function loadTodos() {
  state.todos = await api('GET', '/api/todos');
}
async function refreshActiveEntry() {
  state.activeEntry = await api('GET', '/api/time-entries/active');
  renderNavTimer();
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
  $('#nav-active-timer-stop').addEventListener('click', onTimerToggle);
  // 标签只用于筛选；实际记录优先由选中的任务决定。
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
  $('#today-task-filter').addEventListener('change', e => { state.todayTaskTag = e.target.value; renderTodayTasks(); });
  $('#today-quick-add').addEventListener('click', () => openTodoModal(null));
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
  // Refresh source data before calculating the overview; never render it
  // from a stale todo list or an old active-timer snapshot.
  await Promise.all([loadTodos(), refreshActiveEntry()]);
  renderRecorder();
  renderTodayTasks();
  await renderTodayTimelineAnalysis();
  await renderTodayEntries();
}

// 填充计时/补录两套下拉 + 计时器显示
function renderRecorder() {
  fillGroupSelect($('#timer-group'), null);
  fillTodoSelect($('#timer-todo'), null, $('#timer-group').value ? Number($('#timer-group').value) : null);
  fillGroupSelect($('#bf-group'), null);
  fillTodoSelect($('#bf-todo'), null, $('#bf-group').value ? Number($('#bf-group').value) : null);
  if (!$('#bf-date').value) { $('#bf-date').value = todayStr(); $('#bf-end-date').value = todayStr(); }
  renderTimer();
}

function fillGroupSelect(sel, selectedId) {
  const prev = sel.value;
  sel.innerHTML = '<option value="">全部标签</option>';
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
      const effectiveGroupId = t.parent_id ? (parentGroupId ?? (t.tag_ids || [])[0]) : (t.tag_ids || [])[0];
      const tagIDs = t.tag_ids || (effectiveGroupId ? [effectiveGroupId] : []);
      if (groupId != null) {
        if (!tagIDs.includes(groupId)) continue;
      }
      const indent = depth > 0 ? '  '.repeat(depth) + '└ ' : '';
      const hasKids = t.children && t.children.length;
      const label = indent + t.title + (hasKids && depth === 0 ? ' …' : '');
      flatList.push({ id: t.id, label, groupId: effectiveGroupId });
      if (t.children && t.children.length) {
        walk(t.children, depth + 1, (t.tag_ids || [])[0]);
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
    btn.textContent = '结束计时';
    btn.classList.add('btn-danger');
    const info = $('#timer-active-info');
    info.innerHTML = '';
    info.appendChild(document.createTextNode('正在计时：'));
    info.appendChild(el('b', {}, active.todo_title || groupName(active.tag_id)));
    if (active.todo_title && active.tag_id) info.appendChild(document.createTextNode(' · ' + groupName(active.tag_id)));
    if (active.note) info.appendChild(document.createTextNode(' · ' + active.note));
    updateElapsed();
  } else {
    btn.textContent = '开始计时';
    btn.classList.remove('btn-danger');
    $('#timer-active-info').textContent = '';
    $('#timer-elapsed').textContent = '--:--:--';
  }
  renderNavTimer();
}
function renderNavTimer() {
  const wrap = $('#nav-active-timer');
  if (!wrap) return;
  const active = state.activeEntry;
  wrap.classList.toggle('hidden', !active);
  if (!active) return;
  $('#nav-active-timer-title').textContent = active.todo_title || groupName(active.tag_id);
  updateElapsed();
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
  const navElapsed = $('#nav-active-timer-elapsed');
  if (navElapsed) navElapsed.textContent = `${h}:${m}:${s}`;
  $$('[data-task-elapsed]').forEach(node => { node.textContent = '计时中 ' + `${h}:${m}:${s}`; });
}
setInterval(() => {
  if (state.activeEntry) updateElapsed();
  updateTimelineNow();
}, 1000);

// Recalculate the overview while the user is on Today. Active entries use
// the current time in the analysis service, so this keeps total hours live.
setInterval(() => {
  if (state.view === 'today') void renderTodayTimelineAnalysis().catch(() => {});
}, 60 * 1000);

async function onTimerToggle() {
  try {
    if (state.activeEntry) {
      await api('POST', '/api/time-entries/stop');
      state.activeEntry = null;
    } else {
      const todoId = $('#timer-todo').value;
      const note = $('#timer-note').value.trim();
      state.activeEntry = await api('POST', '/api/time-entries/start', {
        // With a task selected, the server inherits its primary tag. The tag
        // selector above is intentionally a filter rather than an assignment.
        tag_id: todoId ? null : ($('#timer-group').value ? Number($('#timer-group').value) : null),
        todo_id: todoId ? Number(todoId) : null,
        note,
      });
      $('#timer-note').value = '';
    }
    await loadTodos();
    renderTimer();
    renderTodayTasks();
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
    todo_id: $('#bf-todo').value ? Number($('#bf-todo').value) : null,
    tag_id: $('#bf-todo').value ? null : ($('#bf-group').value ? Number($('#bf-group').value) : null),
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
  const [entries, r, weekly] = await Promise.all([
    api('GET', '/api/time-entries?date=' + date),
    api('GET', '/api/analysis/daily?date=' + date),
    api('GET', '/api/analysis/weekly?week=' + currentISOWeek()),
  ]);
  const wrap = $('#today-timeline-wrap');
  wrap.innerHTML = '';
  // Update zoom button active state
  $$('#timeline-zoom-seg .seg-btn').forEach(b => b.classList.toggle('active', b.dataset.zoom === state.timelineZoom));
  wrap.appendChild(buildTimeline(entries, true, date, state.timelineZoom));
  const host = $('#today-analysis');
  host.innerHTML = '';
  renderTodayOverview(r, weekly);
  host.appendChild(renderTodayInsights(r, weekly));
  updateDashboardKPIs(r, weekly);
}

// 当日记录使用与每周分析一致的纵向时间轴；点击时间块可编辑或删除记录。
async function renderTodayEntries() {
  const entries = await api('GET', '/api/time-entries?date=' + todayStr());
  const host = $('#today-entries');
  host.innerHTML = '';
  if (entries.length === 0) { host.appendChild(emptyHint('当天没有工时记录')); return; }
  const seconds = entries.reduce((sum, e) => {
    const start = new Date(e.start_time.replace(' ', 'T')).getTime();
    const end = e.end_time ? new Date(e.end_time.replace(' ', 'T')).getTime() : Date.now();
    return sum + Math.max(0, (end - start) / 1000);
  }, 0);
  host.appendChild(buildWeekTimeline([{
    date: todayStr(), weekday: '今日', seconds, duration: tutilFormatDuration(seconds),
  }], entries, '24h'));
}

// 今日看板只展示当天明确加入的任务；若仅子任务被加入，保留其父级以显示层级。
function renderTodayTasks() {
  const filterTree = (todos) => todos.flatMap(t => {
    const children = filterTree(t.children || []);
    const matchesTag = !state.todayTaskTag || (t.tag_ids || []).includes(Number(state.todayTaskTag));
    const isToday = (t.tag_ids || []).includes(tagIDByName('进行中'));
    if ((!isToday || !matchesTag) && children.length === 0) return [];
    return [{ ...t, children }];
  });
  const filter = $('#today-task-filter');
  filter.innerHTML = '<option value="">全部标签</option>';
  for (const tag of state.groups) filter.appendChild(el('option', { value: tag.id }, tag.name));
  filter.value = state.todayTaskTag;
  const host = $('#today-tasks');
  host.innerHTML = '';
  const items = filterTree(state.todos);
  const activeItems = items.filter(t => t.status !== 'done');
  const doneItems = items.filter(t => t.status === 'done');

  if (items.length === 0) {
    host.appendChild(emptyHint('暂无今日任务，请在待办中将任务加入今日'));
    return;
  }
  for (const t of activeItems) host.appendChild(buildTodoRow(t, false));
  if (doneItems.length) {
    const completed = el('details', { class: 'today-completed' }, el('summary', {}, `已完成任务 (${doneItems.length})`));
    for (const t of doneItems) completed.appendChild(buildTodoRow(t, false));
    host.appendChild(completed);
  }
}

async function startTimerForTodo(todo) {
  if (state.activeEntry && String(state.activeEntry.todo_id) === String(todo.id)) {
    await onTimerToggle();
    return;
  }
  if (state.activeEntry) {
    alert('已有进行中的计时，请先停止后再开始新的任务。');
    return;
  }
  setTodayMode('timer');
  // A quick-start task should always be selectable, even when a tag filter
  // from an earlier selection is still active.
  $('#timer-group').value = '';
  fillTodoSelect($('#timer-todo'), todo.id, null);
  $('#timer-todo').value = String(todo.id);
  $('#timer-note').value = '';
  await onTimerToggle();
}

async function toggleTodoToday(todo) {
  const progressID = await ensureTag('进行中', '#3b82f6');
  const isToday = (todo.tag_ids || []).includes(progressID);
  const tagIDs = isToday ? todo.tag_ids.filter(id => id !== progressID) : [...(todo.tag_ids || []), progressID];
  try {
    await api('PUT', '/api/todos/' + todo.id, {
      title: todo.title,
      description: todo.description,
      status: todo.status,
      priority: todo.priority,
      due_date: todo.due_date,
      parent_id: todo.parent_id,
      tag_ids: tagIDs,
    });
    await loadTodos();
    renderTodos();
  } catch (e) { alert(e.message); }
}

function countTodoTree(todos, predicate = () => true) {
  return todos.reduce((count, todo) => count + (predicate(todo) ? 1 : 0) + countTodoTree(todo.children || [], predicate), 0);
}

function renderTodayOverview(r) {
  const host = $('#today-overview');
  host.innerHTML = '';
  const totalTasks = countTodoTree(state.todos);
  const doneTasks = countTodoTree(state.todos, t => t.status === 'done');
  const activeTasks = countTodoTree(state.todos, t => t.status === 'in_progress');
  const completion = totalTasks ? Math.round(doneTasks / totalTasks * 100) : 0;
  const grid = el('div', { class: 'overview-grid' });
  [['总工时', r.total_duration], ['任务', `${doneTasks}/${totalTasks}`], ['完成率', completion + '%'], ['对比昨日', r.vs_yesterday ? (r.vs_yesterday.delta_seconds >= 0 ? '+' : '') + r.vs_yesterday.duration : '—'], ['最长专注', r.longest_focus ? r.longest_focus.duration : '—'], ['进行中', String(activeTasks)]]
    .forEach(([label, value]) => grid.appendChild(el('div', { class: 'overview-stat' }, el('div', { class: 'label' }, label), el('div', { class: 'value' }, value))));
  host.appendChild(grid);
  host.appendChild(el('div', { class: 'overview-section' }, el('h4', {}, '标签分布'), buildPieChart(r.group_breakdown)));
}

function renderTodayInsights(r, weekly) {
  const wrap = el('div', {});
  wrap.appendChild(el('div', { class: 'overview-section' }, el('h4', {}, '任务时间分布'), buildPieChart(r.todo_breakdown)));
  wrap.appendChild(el('div', { class: 'overview-section' }, el('h4', {}, '本周趋势'), buildTrendChart(weekly.daily_trend || [])));
  return wrap;
}

function updateDashboardKPIs(r, weekly) {
  const total = countTodoTree(state.todos);
  const done = countTodoTree(state.todos, t => t.status === 'done');
  const active = countTodoTree(state.todos, t => t.status === 'in_progress');
  const score = total ? Math.round(done / total * 60 + Math.min(40, r.total_seconds / 3600 * 10)) : 0;
  $('#kpi-date').textContent = todayStr().slice(5);
  $('#kpi-hours').textContent = r.total_duration;
  // These optional KPI slots exist in older page layouts. The current header
  // only shows date and work duration, so don't fail rendering when absent.
  const activeKPI = $('#kpi-active');
  const doneKPI = $('#kpi-done');
  const scoreKPI = $('#kpi-score');
  if (activeKPI) activeKPI.textContent = active;
  if (doneKPI) doneKPI.textContent = done;
  if (scoreKPI) scoreKPI.textContent = score + ' 分';
  $('#footer-score').textContent = score + ' 分';
  $('#footer-week-hours').textContent = weekly.total_duration || '0m';
  $('#footer-streak').textContent = r.total_seconds > 0 ? '1' : '0';
}

// ============================================================
//  TODOS
// ============================================================
function bindTodos() {
  $('#add-group-btn').addEventListener('click', () => openGroupModal(null));
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
    onclick: () => { state.currentGroup = ''; state.todoTagIDs = []; state.todoExcludedTagIDs = []; renderTodos(); },
  }, el('span', { class: 'dot', style: 'background:#9ca3af' }), '全部');
  ul.appendChild(all);
  for (const g of state.groups) {
    const li = el('li', {
      class: 'group-item' + (state.currentGroup == String(g.id) ? ' active' : ''),
      onclick: () => { state.currentGroup = String(g.id); state.todoTagIDs = [g.id]; state.todoExcludedTagIDs = []; renderTodos(); },
    });
    li.appendChild(el('span', { class: 'dot', style: `background:${g.color}` }));
    li.appendChild(document.createTextNode(g.name));
    const actions = el('span', { class: 'g-actions' });
    actions.appendChild(el('button', { class: 'btn btn-small', onclick: (e) => { e.stopPropagation(); openGroupModal(g); } }, '改'));
    actions.appendChild(el('button', { class: 'btn btn-small btn-danger', onclick: async (e) => { e.stopPropagation(); if (confirm('删除标签？关联任务将移除该标签')) { await api('DELETE', '/api/tags/' + g.id); state.currentGroup=''; state.todoTagIDs = state.todoTagIDs.filter(id => id !== g.id); state.todoExcludedTagIDs = state.todoExcludedTagIDs.filter(id => id !== g.id); await loadGroups(); await loadTodos(); renderTodos(); } } }, '×'));
    li.appendChild(actions);
    ul.appendChild(li);
  }

  // Filter & sort bar
  const titleText = state.todoTagIDs.length ? '标签筛选' : '全部待办';
  $('#todos-title').innerHTML = '';
  $('#todos-title').appendChild(document.createTextNode(titleText));
  $('#todos-title').appendChild(buildTodoFilterBar());
  const tagFilter = $('#todo-tag-filter');
  tagFilter.innerHTML = '';
  tagFilter.appendChild(buildTodoTagFilter());

  const list = $('#todo-list');
  list.innerHTML = '';
  let filtered = filterTodoTreeByTags(state.todos);

  // Apply status filter
  if (state.todoFilter === 'today') filtered = filtered.filter(t => (t.tag_ids || []).includes(tagIDByName('进行中')));
  else if (state.todoFilter === 'pending') filtered = filtered.filter(t => t.status !== 'done');
  else if (state.todoFilter === 'done') filtered = filtered.filter(t => t.status === 'done');

  // Apply sort by created_at
  filtered = [...filtered].sort((a, b) => {
    const cmp = new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    return state.todoSort === 'newest' ? cmp : -cmp;
  });

  if (filtered.length === 0) { list.appendChild(emptyHint('没有待办，点右上角新建')); return; }
  for (const t of filtered) list.appendChild(buildTodoRow(t, false));
}

function todoMatchesTagFilter(todo) {
  const selected = state.todoTagIDs;
  const excluded = state.todoExcludedTagIDs;
  const tags = todo.tag_ids || [];
  if (excluded.some(id => tags.includes(id))) return false;
  if (selected.length === 0) return true;
  return state.todoTagRule === 'and'
    ? selected.every(id => tags.includes(id))
    : selected.some(id => tags.includes(id));
}

// Keep a matching subtask visible even when only it (rather than its parent)
// has the selected tag. This matters for subtasks that add tags of their own.
function filterTodoTreeByTags(todos) {
  return todos.reduce((result, todo) => {
    const children = filterTodoTreeByTags(todo.children || []);
    if (todoMatchesTagFilter(todo)) {
      result.push(todo);
    } else if (children.length) {
      result.push({ ...todo, children });
    }
    return result;
  }, []);
}

function buildTodoTagFilter() {
  const wrap = el('div', { class: 'todo-tag-filter' });
  wrap.appendChild(el('span', { class: 'todo-tag-filter-label' }, '标签筛选'));

  const rule = el('div', { class: 'seg', 'aria-label': '多标签筛选规则' });
  for (const option of [{ key: 'or', label: '任一标签 (OR)' }, { key: 'and', label: '全部标签 (AND)' }]) {
    rule.appendChild(el('button', {
      class: 'seg-btn' + (state.todoTagRule === option.key ? ' active' : ''),
      onclick: () => { state.todoTagRule = option.key; renderTodos(); },
    }, option.label));
  }
  wrap.appendChild(rule);

  const tags = el('div', { class: 'todo-tag-options', 'aria-label': '选择标签' });
  for (const tag of state.groups) {
    const selected = state.todoTagIDs.includes(tag.id);
    const excluded = state.todoExcludedTagIDs.includes(tag.id);
    tags.appendChild(el('button', {
      class: 'todo-tag-option' + (selected ? ' active' : '') + (excluded ? ' excluded' : ''),
      title: tag.description || '',
      onclick: () => {
        // Click cycles: include → exclude (NOT) → unselected.
        if (!selected && !excluded) state.todoTagIDs = [...state.todoTagIDs, tag.id];
        else if (selected) { state.todoTagIDs = state.todoTagIDs.filter(id => id !== tag.id); state.todoExcludedTagIDs = [...state.todoExcludedTagIDs, tag.id]; }
        else state.todoExcludedTagIDs = state.todoExcludedTagIDs.filter(id => id !== tag.id);
        state.currentGroup = state.todoTagIDs.length === 1 ? String(state.todoTagIDs[0]) : '';
        renderTodos();
      },
    }, el('i', { style: `background:${tag.color}` }), tag.name));
  }
  wrap.appendChild(tags);
  if (state.todoTagIDs.length || state.todoExcludedTagIDs.length) {
    wrap.appendChild(el('button', {
      class: 'btn btn-small',
      onclick: () => { state.todoTagIDs = []; state.todoExcludedTagIDs = []; state.currentGroup = ''; renderTodos(); },
    }, '清除标签'));
  }
  return wrap;
}

function buildTodoFilterBar() {
  const bar = el('span', { class: 'todo-filter-bar' });
  // Filter buttons
  const filters = [
    { key: 'all', label: '全部' },
    { key: 'today', label: '进行中' },
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
  if (state.view === 'today' && !t.parent_id) bindTodayTaskDrag(item, t);
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
  if (state.activeEntry && String(state.activeEntry.todo_id) === String(t.id)) {
    main.appendChild(el('span', { class: 'task-elapsed', 'data-task-elapsed': t.id }, '计时中 ' + formatElapsedSeconds(activeElapsedSeconds())));
  }
  const meta = el('div', { class: 'todo-meta' });
  for (const tag of (t.tags || [])) {
    meta.appendChild(el('span', { class: 'group-tag', title: tag.description || '' }, el('i', { style: `background:${tag.color}` }), tag.name));
  }
  if (t.due_date) meta.appendChild(document.createTextNode(' · 截止 ' + t.due_date));
  main.appendChild(meta);
  row.appendChild(main);
  const actions = el('div', { class: 'todo-actions' });
  if (!compact) {
    if (state.view === 'today') {
      const isActive = state.activeEntry && String(state.activeEntry.todo_id) === String(t.id);
      actions.appendChild(el('button', { class: 'btn btn-small ' + (isActive ? 'btn-danger' : 'btn-primary'), onclick: () => startTimerForTodo(t) }, isActive ? '结束' : '计时'));
    } else {
      const isToday = (t.tag_ids || []).includes(tagIDByName('进行中'));
      actions.appendChild(el('button', { class: 'btn btn-small ' + (isToday ? 'btn-primary' : ''), onclick: () => toggleTodoToday(t) }, isToday ? '移出进行中' : '加入进行中'));
    }
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

function activeElapsedSeconds() {
  return state.activeEntry ? Math.max(0, Math.floor((Date.now() - new Date(state.activeEntry.start_time.replace(' ', 'T')).getTime()) / 1000)) : 0;
}
function formatElapsedSeconds(secs) {
  return String(Math.floor(secs / 3600)).padStart(2, '0') + ':' + String(Math.floor(secs % 3600 / 60)).padStart(2, '0') + ':' + String(secs % 60).padStart(2, '0');
}
function bindTodayTaskDrag(item, todo) {
  item.draggable = true;
  item.addEventListener('dragstart', e => { item.classList.add('dragging'); e.dataTransfer.setData('text/plain', String(todo.id)); });
  item.addEventListener('dragend', () => item.classList.remove('dragging'));
  item.addEventListener('dragover', e => e.preventDefault());
  item.addEventListener('drop', async e => {
    e.preventDefault();
    const from = state.todos.findIndex(x => String(x.id) === e.dataTransfer.getData('text/plain'));
    const to = state.todos.findIndex(x => x.id === todo.id);
    if (from < 0 || to < 0 || from === to) return;
    const [moved] = state.todos.splice(from, 1); state.todos.splice(to, 0, moved);
    await Promise.all(state.todos.map((task, index) => api('PUT', '/api/todos/' + task.id, { title: task.title, description: task.description, status: task.status, priority: state.todos.length - index, due_date: task.due_date, parent_id: task.parent_id, tag_ids: task.tag_ids })));
    await loadTodos(); renderTodayTasks();
  });
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
  if (id) await api('PUT', '/api/tags/' + id, body);
  else await api('POST', '/api/tags', body);
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
  // Determine visible range
  const z = zoom || '12h';
  const { start: rangeStart, end: rangeEnd } = timelineRange(z);
  const rangeSecs = (rangeEnd - rangeStart) * 3600;

  // Use the viewing date for dayStart calculation, not each entry's own date.
  const dateStr = viewDate || todayStr();
  const dayStart = new Date(dateStr + 'T00:00:00').getTime();
  const rangeStartMs = dayStart + rangeStart * 3600 * 1000;
  const rangeEndMs = dayStart + rangeEnd * 3600 * 1000;
  if (!entries || entries.length === 0) {
    host.appendChild(el('div', { class: 'tl-empty', style: 'width:100%' }, '暂无记录'));
  }
  for (const e of entries || []) {
    const start = new Date(e.start_time.replace(' ', 'T')).getTime();
    const isActive = !e.end_time;
    const endSecs = e.end_time ? new Date(e.end_time.replace(' ', 'T')).getTime() : Date.now();
    const s = Math.max(start, rangeStartMs);
    const en = Math.min(endSecs, rangeEndMs);
    // Keep an active block in the DOM even before it enters the visible range:
    // updateTimelineNow will make it appear as soon as its current endpoint does.
    if (en <= s && !(isActive && start < rangeEndMs)) continue;
    const leftPct = ((s - rangeStartMs) / (rangeSecs * 1000)) * 100;
    const widthPct = Math.max(0, ((en - s) / (rangeSecs * 1000)) * 100);
    const block = el('div', {
      class: 'tl-block',
      style: `left:${leftPct}%; width:${widthPct}%; background:${entryColor(e)}`,
      title: `${fmtTime(e.start_time)} - ${e.end_time ? fmtTime(e.end_time) : '进行中'} · ${groupName(e.tag_id)}${e.todo_title ? ' · ' + e.todo_title : ''}`,
    });
    if (isActive) {
      block.dataset.timelineActiveBlock = 'true';
      block.dataset.timelineLayout = 'horizontal';
      block.dataset.entryStartMs = String(start);
      block.dataset.rangeStartMs = String(rangeStartMs);
      block.dataset.rangeEndMs = String(rangeEndMs);
    }
    block.textContent = entryTitle(e);
    block.addEventListener('click', () => openEntryModal(e));
    host.appendChild(block);
  }
  if (showNow) {
    host.dataset.nowTrack = 'true';
    host.dataset.rangeStartMs = String(rangeStartMs);
    host.dataset.rangeEndMs = String(rangeEndMs);
    host.appendChild(el('div', { class: 'tl-now', 'data-timeline-now': 'true' }));
    updateTimelineNow(host);
  }
  return host;
}

// Keeps the red "now" line moving even when no timer is running. Tracks carry
// their own visible time range so the line can be hidden outside it.
function updateTimelineNow(track) {
  const tracks = track ? [track] : $$('[data-now-track="true"]');
  const now = Date.now();
  for (const host of tracks) {
    const start = Number(host.dataset.rangeStartMs);
    const end = Number(host.dataset.rangeEndMs);
    const line = $('[data-timeline-now="true"]', host);
    if (!line || !Number.isFinite(start) || !Number.isFinite(end) || end <= start) continue;
    const visible = now >= start && now <= end;
    line.style.display = visible ? '' : 'none';
    if (visible) line.style.left = `${(now - start) / (end - start) * 100}%`;
  }
  for (const col of $$('[data-week-now-track="true"]')) {
    const start = Number(col.dataset.rangeStartMs);
    const end = Number(col.dataset.rangeEndMs);
    const line = $('[data-timeline-now="true"]', col);
    if (!line || !Number.isFinite(start) || !Number.isFinite(end) || end <= start) continue;
    const visible = now >= start && now <= end;
    line.style.display = visible ? '' : 'none';
    if (visible) line.style.setProperty('--top-h', ((now - start) / 3600000).toFixed(3));
  }
  for (const block of $$('[data-timeline-active-block="true"]')) {
    const entryStart = Number(block.dataset.entryStartMs);
    const rangeStart = Number(block.dataset.rangeStartMs);
    const rangeEnd = Number(block.dataset.rangeEndMs);
    if (![entryStart, rangeStart, rangeEnd].every(Number.isFinite) || rangeEnd <= rangeStart) continue;
    const start = Math.max(entryStart, rangeStart);
    const end = Math.min(now, rangeEnd);
    const visible = end > start;
    block.style.display = visible ? '' : 'none';
    if (!visible) continue;
    if (block.dataset.timelineLayout === 'vertical') {
      block.style.setProperty('--top-h', ((start - rangeStart) / 3600000).toFixed(3));
      block.style.setProperty('--h-h', ((end - start) / 3600000).toFixed(3));
    } else {
      block.style.left = `${(start - rangeStart) / (rangeEnd - rangeStart) * 100}%`;
      block.style.width = `${(end - start) / (rangeEnd - rangeStart) * 100}%`;
    }
  }
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
  row.appendChild(el('div', { class: 'entry-task' }, e.todo_title || '未关联任务'));
  if (e.note) row.appendChild(el('div', { class: 'entry-note' }, '· ' + e.note));
  row.appendChild(el('div', { class: 'entry-actions' },
    el('button', { class: 'btn btn-small', onclick: () => openEntryModal(e) }, '编辑')));
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
          el('button', { class: 'seg-btn' + (state.timelineZoom === '12h' ? ' active' : ''), onclick: () => { state.timelineZoom = '12h'; renderAnalysis(); } }, '工作时段'),
        ));
      tlCard.appendChild(tlHead);
      tlCard.appendChild(buildTimeline(entries, state.analysisDate === todayStr(), state.analysisDate, state.timelineZoom));
      host.appendChild(tlCard);
      host.appendChild(renderDailyAnalysis(r));
    } else {
      const w = state.analysisWeek || currentISOWeek();
      state.analysisWeek = w;
      $('#analysis-week').value = w;
      const [r, summary] = await Promise.all([
        api('GET', '/api/analysis/weekly?week=' + w),
        api('GET', '/api/summaries/weekly?week=' + w),
      ]);
      r.user_summary = summary.content;
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
    el('h3', {}, '任务占比'),
    buildPieChart(r.todo_breakdown)));
  flexRow.appendChild(el('div', { class: 'card', style: 'flex:1; margin-left:16px' },
    el('h3', {}, '标签占比'),
    buildPieChart(r.group_breakdown)));

  wrap.appendChild(flexRow);


  wrap.appendChild(buildDailySummaryEditor(r.date, r.content));
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

  // Keep the hourly timeline immediately after the per-day bar chart so the
  // aggregate and the underlying time distribution can be read together.
  wrap.appendChild(el('div', { class: 'card', style: 'margin-top:16px' },
    el('h3', {}, '每日趋势'),
    buildTrendChart(r.daily_trend)));

  // 7-day timeline: one column per weekday, with hourly grid lines.
  const tlCard = el('div', { class: 'card', style: 'margin-top:16px' });
  tlCard.appendChild(el('div', { class: 'panel-head', style: 'justify-content:space-between' },
    el('h3', {}, '本周时间轴 · 7天'),
    el('div', { style: 'display:flex;gap:8px' },
      el('div', { class: 'seg' },
        el('button', { class: 'seg-btn' + (state.timelineZoom === '24h' ? ' active' : ''), onclick: () => { state.timelineZoom = '24h'; renderAnalysis(); } }, '全天'),
        el('button', { class: 'seg-btn' + (state.timelineZoom === '12h' ? ' active' : ''), onclick: () => { state.timelineZoom = '12h'; renderAnalysis(); } }, '工作时段'),
      ),
      el('div', { class: 'seg', title: '缩放时间轴' },
        el('button', { class: 'seg-btn week-cal-zoom-out', title: '缩小', disabled: state.weekHourPx <= 32 ? true : null, onclick: () => setWeekZoom(state.weekHourPx - 16) }, '－'),
        el('button', { class: 'seg-btn week-cal-zoom-in', title: '放大', disabled: state.weekHourPx >= 192 ? true : null, onclick: () => setWeekZoom(state.weekHourPx + 16) }, '＋'),
      ),
    )));
  tlCard.appendChild(buildWeekTimeline(r.daily_trend, r.weekly_entries, state.timelineZoom));
  wrap.appendChild(tlCard);

  const breakdowns = el('div', { class: 'analysis-flex-row', style: 'margin-top:16px' });
  breakdowns.appendChild(el('div', { class: 'card', style: 'flex:1' },
    el('h3', {}, '任务占比'), buildPieChart(r.todo_breakdown)));
  breakdowns.appendChild(el('div', { class: 'card', style: 'flex:1; margin-left:16px' },
    el('h3', {}, '标签占比'), buildPieChart(r.group_breakdown)));
  wrap.appendChild(breakdowns);
  // Keep the editable reflection as the final section of weekly analysis.
  wrap.appendChild(buildWeeklySummaryEditor(r.week_label, r.user_summary));
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
  // Both task and tag API breakdowns share the chart renderer.
  groups = (groups || []).map(g => ({
    ...g,
    group_name: g.group_name || g.todo_name,
    group_color: g.group_color || g.todo_color || '#9ca3af',
  }));
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
  const maxSeconds = Math.max(1, ...days.map(d => d.seconds));
  const maxHours = Math.max(1, Math.ceil(maxSeconds / 3600));
  const axis = el('div', { class: 'trend-y-axis' });
  for (let hour = maxHours; hour >= 0; hour--) {
    axis.appendChild(el('span', { style: `bottom:${(hour / maxHours) * 100}%` }, hour + 'h'));
  }
  const chart = el('div', { class: 'trend-chart', style: `--trend-hours:${maxHours}` });
  const labels = el('div', { class: 'trend-labels' });
  for (const d of days) {
    const col = el('div', { class: 'trend-col' });
    const bar = el('div', { class: 'trend-bar', style: `height:${Math.max(2, (d.seconds / (maxHours * 3600)) * 100)}%`, title: d.date + ' ' + d.duration });
    col.appendChild(bar);
    chart.appendChild(col);
    labels.appendChild(el('span', {}, d.weekday));
  }
  wrap.appendChild(el('div', { class: 'trend-body' }, axis, chart));
  wrap.appendChild(el('div', { class: 'trend-footer' }, el('span', { class: 'trend-y-axis-spacer' }), labels));
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
  const { start: rangeStart, end: rangeEnd } = timelineRange(z);
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
        const isActive = !e.end_time;
        const endSecs = e.end_time ? new Date(e.end_time.replace(' ', 'T')).getTime() : Date.now();
        const s = Math.max(start, rangeStartMs);
        const en = Math.min(endSecs, rangeEndMs);
        if (en <= s && !(isActive && start < rangeEndMs)) continue;
        const topH = (s - rangeStartMs) / 3600000;
        const heightH = Math.max(0, (en - s) / 3600000);
        const block = el('div', {
          class: 'week-cal-block',
          style: `--top-h:${topH.toFixed(3)};--h-h:${heightH.toFixed(3)};background:${entryColor(e)}`,
          title: `${fmtTime(e.start_time)} - ${e.end_time ? fmtTime(e.end_time) : '进行中'} · ${groupName(e.tag_id)}${e.todo_title ? ' · ' + e.todo_title : ''}`,
        });
        if (isActive) {
          block.dataset.timelineActiveBlock = 'true';
          block.dataset.timelineLayout = 'vertical';
          block.dataset.entryStartMs = String(start);
          block.dataset.rangeStartMs = String(rangeStartMs);
          block.dataset.rangeEndMs = String(rangeEndMs);
        }
        block.textContent = entryTitle(e);
        block.addEventListener('click', () => openEntryModal(e));
        col.appendChild(block);
      }
    }
    if (isToday) {
      col.dataset.weekNowTrack = 'true';
      col.dataset.rangeStartMs = String(rangeStartMs);
      col.dataset.rangeEndMs = String(rangeEndMs);
      const now = Date.now();
      if (now >= rangeStartMs && now <= rangeEndMs) {
        const nowH = (now - rangeStartMs) / 3600000;
        col.appendChild(el('div', { class: 'week-cal-now', 'data-timeline-now': 'true', style: `--top-h:${nowH.toFixed(3)}` }));
      }
    }
    inner.appendChild(col);
  }
  scroll.appendChild(inner);

  // The single-day timeline always shows the full day and opens around 10:00,
  // where most work records begin, while keeping native scrolling available.
  setTimeout(() => {
    if (trend.length === 1) {
      const now = new Date();
      const nowHour = now.getHours() + now.getMinutes() / 60;
      scroll.scrollTop = Math.max(0, (nowHour - rangeStart) * state.weekHourPx - scroll.clientHeight / 2);
    }
  }, 0);

  const wrap = el('div', { class: 'week-cal' + (trend.length === 1 ? ' week-cal-single-day' : ''), style: `--hour-px:${state.weekHourPx}px;--hours:${totalHours}` });
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

function buildDailySummaryEditor(date, content) {
  const card = el('div', { class: 'card improvement-box', style: 'margin-top:16px' });
  card.appendChild(el('h3', {}, '当日总结'));
  card.appendChild(el('label', {}, '总结内容'));
  const input = el('textarea', { id: 'daily-summary', placeholder: '写下当日总结…' }); input.value = content || '';
  card.appendChild(input);
  card.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      try {
        await api('PUT', '/api/summaries/daily?date=' + date, { content: input.value });
        alert('已保存');
        renderAnalysis();
      } catch (e) { alert(e.message); }
    } }, '保存')));
  return card;
}

function buildWeeklySummaryEditor(week, content) {
  const card = el('div', { class: 'card improvement-box', style: 'margin:16px 0' });
  card.appendChild(el('h3', {}, '本周总结'));
  card.appendChild(el('label', {}, '记录本周成果、问题与下周计划'));
  const input = el('textarea', { id: 'weekly-summary', placeholder: '写下本周总结…' }); input.value = content || '';
  card.appendChild(input);
  card.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      try {
        await api('PUT', '/api/summaries/weekly?week=' + week, { content: input.value });
        alert('已保存');
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
  const parent = isChild ? findTodoById(t.parent_id) : null;
  const initialTagIDs = t && t.tag_ids ? t.tag_ids : (parent ? parent.tag_ids : (state.currentGroup ? [Number(state.currentGroup)] : []));
  const tagsField = field('标签（第一个用于时间轴颜色）', tagSelector(initialTagIDs));
  const descField = field('描述', textarea(t && t.description ? t.description : ''));
  const prioField = field('优先级', input('number', t && t.priority != null ? t.priority : 0));
  const dueField = field('截止日期', input('date', t && t.due_date ? t.due_date : ''));
  const initiallyToday = t ? (t.tag_ids || []).includes(tagIDByName('进行中')) : state.view === 'today';
  const todayCheck = el('input', { type: 'checkbox' });
  todayCheck.checked = initiallyToday;
  const todayField = el('label', { class: 'today-task-check' }, todayCheck, ' 添加“进行中”标签');
  const form = el('div', {});
  form.appendChild(el('div', { class: 'field' }, el('label', {}, isNew ? (isChild ? '新建子任务' : '新建待办') : '编辑待办')));
  form.appendChild(titleField);
  form.appendChild(tagsField);
  form.appendChild(descField);
  form.appendChild(el('div', { class: 'row' }, prioField, dueField));
  form.appendChild(todayField);
  form.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      const body = {
        title: titleField.querySelector('input').value.trim(),
        description: descField.querySelector('textarea').value,
        priority: Number(prioField.querySelector('input').value || 0),
        due_date: dueField.querySelector('input').value || null,
        status: t && t.status ? t.status : 'pending',
      };
      body.tag_ids = tagsField.querySelector('.tag-selector').getTagIDs();
      const progressID = await ensureTag('进行中', '#3b82f6');
      body.tag_ids = todayCheck.checked
        ? (body.tag_ids.includes(progressID) ? body.tag_ids : [...body.tag_ids, progressID])
        : body.tag_ids.filter(id => id !== progressID);
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
  const initialGroupId = e && e.tag_id ? e.tag_id : Number(state.currentGroup) || null;
  const initialTodoId = e && e.todo_id ? e.todo_id : null;

  const startDateField = field('开始日期', input('date', startDate));
  const startTimeField = field('开始时间', input('time', startTime));
  const endDateField = field('结束日期', input('date', endDate));
  const endTimeField = field('结束时间', input('time', endTime));

  // Build tag and task selectors. The selected task's tags remain visible.
  const gs = groupSelect(initialGroupId);
  const todoWrapper = el('div', { class: 'field entry-task-field' }, el('label', {}, '关联任务（可选）'));
  function buildTodoField(groupId) {
    todoWrapper.querySelectorAll('.entry-task-control').forEach(n => n.remove());
    const control = el('div', { class: 'entry-task-control' });
    const select = todoSelect(initialTodoId, groupId);
    const meta = el('div', { class: 'entry-task-tags' });
    const renderTaskMeta = () => {
      meta.innerHTML = '';
      const todo = findTodoById(select.value ? Number(select.value) : null);
      if (!todo) { meta.appendChild(el('span', { class: 'entry-task-empty' }, '选择任务后显示其标签')); return; }
      for (const tag of (todo.tags || [])) {
        meta.appendChild(el('span', { class: 'group-tag' }, el('i', { style: `background:${tag.color}` }), tag.name));
      }
      meta.appendChild(el('button', { type: 'button', class: 'btn btn-small', onclick: () => openTodoModal(todo) }, '编辑任务'));
    };
    select.addEventListener('change', renderTaskMeta);
    control.append(select, meta);
    todoWrapper.appendChild(control);
    renderTaskMeta();
  }
  buildTodoField(initialGroupId);
  gs.addEventListener('change', function () {
    const gid = this.value ? Number(this.value) : null;
    buildTodoField(gid);
  });

  const noteInput = textarea(e && e.note ? e.note : '');
  noteInput.rows = 3;
  noteInput.classList.add('entry-note-input');
  const noteField = field('备注', noteInput);
  const form = el('div', { class: 'entry-editor' });
  form.appendChild(el('div', { class: 'entry-editor-section' }, el('div', { class: 'entry-editor-section-title' }, '时间'),
    el('div', { class: 'row' }, startDateField, startTimeField),
    el('div', { class: 'row' }, endDateField, endTimeField)));
  form.appendChild(el('div', { class: 'entry-editor-section' }, el('div', { class: 'entry-editor-section-title' }, '任务与标签'),
    el('div', { class: 'field' }, el('label', {}, '记录标签'), gs), todoWrapper));
  form.appendChild(noteField);
  const actions = el('div', { class: 'save-row' });
  if (!isNew) {
    actions.appendChild(el('button', { class: 'btn btn-danger', onclick: async () => {
      if (!confirm('删除这条工时记录？此操作不会删除关联任务。')) return;
      try {
        await api('DELETE', '/api/time-entries/' + e.id);
        closeModal();
        await refreshActiveEntry();
        renderView(state.view);
      } catch (err) { alert(err.message); }
    } }, '删除记录'));
  }
  actions.appendChild(el('button', { class: 'btn btn-primary', onclick: async () => {
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
        note: noteField.querySelector('textarea').value,
        tag_id: gs.value ? Number(gs.value) : null,
        todo_id: todoWrapper.querySelector('select').value ? Number(todoWrapper.querySelector('select').value) : null,
      };
      try {
        if (isNew) await api('POST', '/api/time-entries', body);
        else await api('PUT', '/api/time-entries/' + e.id, body);
        closeModal();
        await refreshActiveEntry();
        renderView(state.view);
      } catch (err) { alert(err.message); }
    } }, '保存'));
  form.appendChild(actions);
  openModal(isNew ? '补录工时' : '编辑工时', form);
}

// 新建/编辑分组弹窗（今日页与待办页共用入口，独立 DOM，不依赖 #group-editor）
function openGroupModal(group) {
  const isNew = !group;
  const nameField = field('分组名', input('text', group ? group.name : ''));
  const descriptionField = field('标签描述', textarea(group ? group.description || '' : ''));
  const colorI = el('input', { class: 'color-input', type: 'color', value: group ? group.color : '#6366f1' });
  const colorField = el('div', { class: 'field' }, el('label', {}, '颜色'), colorI);
  const statsCheck = el('input', { type: 'checkbox' });
  statsCheck.checked = !group || group.include_in_stats !== false;
  const statsField = el('label', { class: 'today-task-check' }, statsCheck, ' 计入标签分布统计');
  const form = el('div', {});
  form.appendChild(el('div', { class: 'field' }, el('label', {}, isNew ? '新建标签' : '编辑标签')));
  form.appendChild(nameField);
  form.appendChild(descriptionField);
  form.appendChild(colorField);
  form.appendChild(statsField);
  form.appendChild(el('div', { class: 'save-row' },
    el('button', { class: 'btn btn-primary', onclick: async () => {
      const name = nameField.querySelector('input').value.trim();
      if (!name) return;
      const body = { name, description: descriptionField.querySelector('textarea').value.trim(), color: colorI.value, include_in_stats: statsCheck.checked };
      try {
        if (isNew) await api('POST', '/api/tags', body);
        else await api('PUT', '/api/tags/' + group.id, body);
        closeModal();
        await loadGroups();
        if (state.view === 'today') renderToday(); else renderView(state.view);
      } catch (e) { alert(e.message); }
    } }, '保存')));
  openModal(isNew ? '新建标签' : '编辑标签', form);
}

function tagSelector(selectedIDs) {
  let selected = Array.from(new Set(selectedIDs || []));
  const wrap = el('div', { class: 'tag-selector' });
  const selectedWrap = el('div', { class: 'selected-tags' });
  const select = el('select', { class: 'select tag-add-select', 'aria-label': '添加标签' });
  const render = () => {
    selectedWrap.innerHTML = '';
    selected.forEach((id, index) => {
      const tag = state.groups.find(x => x.id === id);
      if (!tag) return;
      selectedWrap.appendChild(el('span', { class: 'group-tag', title: tag.description || '' },
        el('i', { style: `background:${tag.color}` }), tag.name,
        el('button', { type: 'button', class: 'tag-move', title: '上移标签', disabled: index === 0 ? true : null, onclick: () => { [selected[index - 1], selected[index]] = [selected[index], selected[index - 1]]; render(); } }, '↑'),
        el('button', { type: 'button', class: 'tag-move', title: '下移标签', disabled: index === selected.length - 1 ? true : null, onclick: () => { [selected[index], selected[index + 1]] = [selected[index + 1], selected[index]]; render(); } }, '↓'),
        el('button', { type: 'button', class: 'tag-remove', title: `移除 ${tag.name}`, onclick: () => { selected = selected.filter(x => x !== id); render(); } }, '×')));
    });
    select.innerHTML = '';
    select.appendChild(el('option', { value: '' }, '添加标签…'));
    for (const tag of state.groups.filter(x => !selected.includes(x.id))) {
      select.appendChild(el('option', { value: tag.id }, tag.name));
    }
  };
  select.addEventListener('change', () => { if (select.value) { selected.push(Number(select.value)); render(); } });
  wrap.getTagIDs = () => [...selected];
  wrap.append(selectedWrap, select);
  render();
  return wrap;
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
      const effectiveGroupId = t.parent_id ? (parentGroupId ?? (t.tag_ids || [])[0]) : (t.tag_ids || [])[0];
      const tagIDs = t.tag_ids || (effectiveGroupId ? [effectiveGroupId] : []);
      if (groupId != null) {
        if (!tagIDs.includes(groupId)) continue;
      }
      const indent = depth > 0 ? '  '.repeat(depth) + '└ ' : '';
      flatList.push({ id: t.id, label: indent + t.title });
      if (t.children && t.children.length) {
        walk(t.children, depth + 1, (t.tag_ids || [])[0]);
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
