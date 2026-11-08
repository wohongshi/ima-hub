/* ImaHub — view-home.js：首页仪表盘（hero + 知识库分析 / 任务计划 / 近期添加 / 账号贡献） */
'use strict';

const homeState = {
  dashboard: null,   // {stats,by_type,by_account,recent,scan}
  tasks: [],
  events: [],        // 团队日程
  calMonth: null,    // 月历当前显示的月份（Date，取年月）
  calSelected: '',   // 选中的日期 YYYY-MM-DD
  loading: false,
  tasksLoading: false,
  eventsLoading: false,
};
let homePollTimer = null;

const TYPE_COLORS = ['#6366f1', '#8b5cf6', '#06b6d4', '#22c55e', '#f59e0b', '#ef4444', '#ec4899', '#84cc16', '#14b8a6', '#64748b'];
const ACCT_COLORS = ['linear-gradient(135deg,#6366f1,#8b5cf6)', 'linear-gradient(135deg,#06b6d4,#22c55e)', 'linear-gradient(135deg,#f59e0b,#ef4444)', 'linear-gradient(135deg,#ec4899,#8b5cf6)', 'linear-gradient(135deg,#14b8a6,#0ea5e9)'];

/* ============ 工具 ============ */
function fmtNum(n) {
  const v = Number(n || 0);
  return isFinite(v) ? v.toLocaleString('zh-CN') : String(n || 0);
}

function humanSize(bytes) {
  let b = Number(bytes || 0);
  if (!isFinite(b) || b <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (b >= 1024 && i < units.length - 1) { b /= 1024; i++; }
  return (i === 0 ? String(Math.round(b)) : b.toFixed(1)) + ' ' + units[i];
}

function relativeTime(ts) {
  if (!ts && ts !== 0) return '';
  let n = Number(ts);
  if (!isFinite(n) || n <= 0) return String(ts);
  if (n < 1e12) n = n * 1000;
  const diff = Date.now() - n;
  if (diff < 0) return formatTime(ts);
  const min = Math.floor(diff / 60000);
  if (min < 1) return '刚刚';
  if (min < 60) return min + ' 分钟前';
  const h = Math.floor(min / 60);
  if (h < 24) return h + ' 小时前';
  const d = Math.floor(h / 24);
  if (d < 30) return d + ' 天前';
  return formatTime(ts);
}

function parseDue(due) {
  if (!due) return null;
  if (typeof due === 'number' || /^\d+$/.test(String(due))) {
    let n = Number(due);
    if (n < 1e12) n = n * 1000;
    const d = new Date(n);
    return isNaN(d.getTime()) ? null : d;
  }
  const d = new Date(due);
  return isNaN(d.getTime()) ? null : d;
}

function formatDue(due) {
  const d = parseDue(due);
  if (!d) return String(due || '');
  const p = (x) => (x < 10 ? '0' + x : '' + x);
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate());
}

function isOverdue(task) {
  if (!task || task.status === 'done' || !task.due) return false;
  const d = parseDue(task.due);
  if (!d) return false;
  const end = new Date(d.getFullYear(), d.getMonth(), d.getDate(), 23, 59, 59);
  return end.getTime() < Date.now();
}

function homeGreeting() {
  const h = new Date().getHours();
  if (h < 5) return '夜深了';
  if (h < 9) return '早上好';
  if (h < 12) return '上午好';
  if (h < 14) return '中午好';
  if (h < 18) return '下午好';
  return '晚上好';
}

/* ============ 视图入口 ============ */
function renderHomeView(root) {
  root.innerHTML =
    '<div class="home-page">' +
    '  <section class="home-hero" id="home-hero"></section>' +
    '  <section class="home-announce" id="home-announce"></section>' +
    '  <div class="home-grid">' +
    '    <section class="card home-panel" id="home-today"></section>' +
    '    <section class="card home-panel" id="home-schedule"></section>' +
    '    <section class="card home-panel" id="home-analysis"></section>' +
    '    <section class="card home-panel" id="home-tasks"></section>' +
    '    <section class="card home-panel" id="home-recent"></section>' +
    '    <section class="card home-panel" id="home-accounts"></section>' +
    '  </div>' +
    '</div>';
  renderHomeHero();
  renderHomeToday();
  renderHomeSchedule();
  renderHomeAnalysis();
  renderHomeTasks();
  renderHomeRecent();
  renderHomeAccounts();
  loadHomeDashboard();
  loadHomeTasks();
  loadHomeEvents();
  loadAnnouncements();
}

function homeSkeleton(rows) {
  let html = '';
  for (let i = 0; i < (rows || 4); i++) {
    html += '<div class="sk-row" style="padding:10px 2px">' +
      '<div class="skeleton" style="width:32px;height:32px;border-radius:8px"></div>' +
      '<div style="flex:1"><div class="skeleton sk-line" style="width:' + (30 + ((i * 19) % 50)) + '%"></div></div>' +
      '</div>';
  }
  return '<div class="card-body">' + html + '</div>';
}

/* ============ Hero 横幅 ============ */
function renderHomeHero() {
  const box = $('#home-hero');
  if (!box) return;
  const u = (typeof state !== 'undefined' && state.user) ? state.user : {};
  const d = homeState.dashboard;
  const s = (d && d.stats) || {};
  const dateStr = new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' });
  const nowTs = Date.now();
  const nextEv = homeState.events
    .filter(function (ev) { return evStart(ev).getTime() >= nowTs; })
    .sort(function (a, b) { return evStart(a) - evStart(b); })[0];
  const evChip = nextEv
    ? '<span class="hero-chip hero-chip-ev">' + icon('inbox', 13) + ' ' + escapeHtml(daysUntilText(nextEv)) + ' ' + escapeHtml(fmtEventTime(nextEv)) + ' · ' + escapeHtml(nextEv.title) + '</span>'
    : '';
  const summary = d
    ? '<div class="hero-chips">' +
      '<span class="hero-chip">' + icon('users', 13) + ' ' + fmtNum(s.accounts) + ' 个账号</span>' +
      '<span class="hero-chip">' + icon('database', 13) + ' ' + fmtNum(s.kbs) + ' 个知识库</span>' +
      '<span class="hero-chip">' + icon('file', 13) + ' ' + fmtNum(s.files) + ' 个文件</span>' +
      '<span class="hero-chip">' + icon('upload', 13) + ' 已上传 ' + fmtNum(s.uploads) + ' 个 · ' + humanSize(s.bytes) + '</span>' +
      evChip +
      '</div>'
    : '<div class="hero-chips"><span class="hero-chip">正在加载概览…</span></div>';

  const actionHtml =
    '    <button class="btn hero-btn" id="hero-upload">' + icon('upload', 15) + '上传文件</button>' +
    '    <button class="btn hero-btn" id="hero-import">' + icon('link', 15) + '导入链接</button>' +
    '    <button class="btn hero-btn" id="hero-note">' + icon('note', 15) + '新建笔记</button>' +
    '    <button class="btn hero-btn" id="hero-scan">' + icon('refresh', 15) + '重新扫描</button>';

  box.innerHTML =
    '<div class="hero-decor"></div>' +
    '<div class="hero-main">' +
    '  <div class="hero-text">' +
    '    <div class="hero-date">' + escapeHtml(dateStr) + '</div>' +
    '    <h2 class="hero-title">' + homeGreeting() + '，' + escapeHtml(u.username || '朋友') + ' 👋</h2>' +
    '    <div class="hero-sub">容量池已就绪，随时开始整理你的知识库</div>' +
    summary +
    '    <div class="hero-ask">' +
    '      <input class="hero-ask-input" id="hero-ask-input" placeholder="问问团队知识库…" maxlength="200">' +
    '      <button class="btn hero-ask-btn" id="hero-ask-btn">' + icon('search', 15) + '提问</button>' +
    '    </div>' +
    '  </div>' +
    '  <div class="hero-actions">' +
    actionHtml +
    '  </div>' +
    '</div>';

  const bind = function (id, fn) {
    const b = $('#' + id, box);
    if (b) b.addEventListener('click', fn);
  };
  bind('hero-upload', function () { openUploadModal(null); });
  bind('hero-import', function () {
    // 不赌渲染时机：挂起导入请求，由文件页在知识库就绪后弹出导入框
    filesState.pendingImport = true;
    if (location.hash !== '#/files') location.hash = '#/files';
    else renderFilesView($('#view-root'));
  });
  bind('hero-note', function () { openNewNoteModal(); });
  bind('hero-scan', function () { startScan(); });
  const askRun = function () {
    const input = $('#hero-ask-input', box);
    const q = input ? input.value.trim() : '';
    if (!q) { toast('请输入要问的问题', 'warn'); return; }
    openAskModal(q);
  };
  bind('hero-ask-btn', askRun);
  const askInput = $('#hero-ask-input', box);
  if (askInput) {
    askInput.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') askRun();
    });
  }
}

/* ============ 知识库问答（agent 级查询） ============ */
function openAskModal(question) {
  const bodyHtml =
    '<div class="ask-q">' + icon('search', 15) + '<span>' + escapeHtml(question) + '</span></div>' +
    '<div id="ask-loading" class="loading-center" style="padding:22px 0"><div class="spinner"></div><div>正在检索团队知识库…</div></div>' +
    '<div id="ask-answer"></div>' +
    '<div id="ask-sources"></div>';
  const m = showModal({
    title: '知识库问答',
    body: bodyHtml,
    footer: '<button class="btn primary" data-close>关闭</button>',
  });
  apiPost('/api/ask', { q: question }).then(function (data) {
    const loading = $('#ask-loading', m.root);
    if (loading) loading.remove();
    const answerBox = $('#ask-answer', m.root);
    const srcBox = $('#ask-sources', m.root);
    if (!answerBox || !srcBox) return;
    const mode = data.mode;
    let aHtml = '';
    if (data.error) {
      aHtml += '<div class="hint" style="color:var(--danger)">' + escapeHtml(data.error) + '</div>';
    }
    if (data.answer) {
      aHtml += '<div class="ask-answer-body">' + renderMarkdown(data.answer) + '</div>';
    } else if (!data.error) {
      aHtml += '<div class="hint">' +
        (mode === 'retrieval'
          ? '未配置问答模型（管理页可配置），以下是知识库检索到的相关资料。'
          : '知识库中没有找到相关内容。') + '</div>';
    }
    answerBox.innerHTML = aHtml;
    const sources = data.sources || [];
    if (sources.length) {
      srcBox.innerHTML = '<div class="home-sub-title" style="margin-top:12px">引用来源（' + sources.length + '）</div>' +
        sources.map(function (s, i) {
          return '<div class="ask-source" data-idx="' + i + '">' +
            '<div class="ask-source-title">' + icon('file', 13) + '《' + escapeHtml(s.title || '无标题') + '》</div>' +
            '<div class="ask-source-meta">' +
            (s.account_name ? '<span class="chip">' + escapeHtml(s.account_name) + '</span>' : '') +
            (s.kb_name ? '<span class="muted">' + escapeHtml(s.kb_name) + '</span>' : '') +
            '</div>' +
            (s.snippet ? '<div class="ask-source-snip">' + escapeHtml(s.snippet.slice(0, 160)) + '</div>' : '') +
            '</div>';
        }).join('');
      $$('.ask-source', srcBox).forEach(function (el) {
        const s = sources[Number(el.getAttribute('data-idx'))];
        if (s && s.media_id) {
          el.classList.add('clickable');
          el.addEventListener('click', function () {
            openMediaDetail(String(s.media_id), s.ia || '');
          });
        }
      });
    }
  }).catch(function () {
    const loading = $('#ask-loading', m.root);
    if (loading) loading.innerHTML = '<div class="hint">查询失败，请稍后重试</div>';
  });
}

/* ============ 数据加载 ============ */
async function loadHomeDashboard() {
  homeState.loading = true;
  try {
    homeState.dashboard = await apiGet('/api/dashboard');
  } catch (e) {
    homeState.dashboard = null;
  }
  homeState.loading = false;
  renderHomeHero();
  renderHomeAnalysis();
  renderHomeRecent();
  renderHomeAccounts();
  const sc = homeState.dashboard && homeState.dashboard.scan;
  if (sc && sc.status === 'running') scheduleHomePoll();
}

function scheduleHomePoll() {
  clearTimeout(homePollTimer);
  if (state.route !== 'home') return;
  homePollTimer = setTimeout(function () {
    if (state.route !== 'home') return;
    loadHomeDashboard();
  }, 3000);
}

function stopHomePoll() {
  clearTimeout(homePollTimer);
  homePollTimer = null;
}

/* ============ 板块一：知识库分析 ============ */
function renderHomeAnalysis() {
  const box = $('#home-analysis');
  if (!box) return;
  const head = '<div class="card-head">' + icon('database', 18) + '<h3>知识库分析</h3><span class="sub">容量池全景</span></div>';
  const d = homeState.dashboard;
  if (!d) {
    box.innerHTML = head + homeSkeleton(5);
    return;
  }
  const s = d.stats || {};
  const byType = Array.isArray(d.by_type) ? d.by_type : [];
  const sc = d.scan || {};
  const running = sc.status === 'running';

  let html = '<div class="card-body">';
  // 统计卡片（彩色图标）
  const cards = [
    { num: s.accounts, lbl: '账号', icon: 'users', color: '#6366f1', sub: '聚合扩容' },
    { num: s.kbs, lbl: '知识库', icon: 'database', color: '#8b5cf6', sub: '合并视图' },
    { num: s.files, lbl: '文件', icon: 'file', color: '#06b6d4', sub: byType.length + ' 种类型' },
    { num: s.folders, lbl: '文件夹', icon: 'folder', color: '#22c55e', sub: '层级浏览' },
  ];
  html += '<div class="stat-row">';
  cards.forEach(function (c) {
    html += '<div class="stat-card stat-card-2">' +
      '<div class="stat-icon" style="background:' + c.color + '1c;color:' + c.color + '">' + icon(c.icon, 17) + '</div>' +
      '<div><div class="num">' + fmtNum(c.num) + '</div><div class="lbl">' + c.lbl + '</div>' +
      '<div class="stat-sub">' + escapeHtml(c.sub) + '</div></div>' +
      '</div>';
  });
  html += '</div>';

  // 文件类型分布：甜甜圈 + 图例
  html += '<div class="home-sub-title">文件类型分布</div>';
  if (!byType.length) {
    html += '<div class="hint" style="margin:6px 2px">暂无数据，完成一次扫描后可见分布。</div>';
  } else {
    const total = byType.reduce(function (m, t) { return m + (Number(t.count) || 0); }, 0) || 1;
    let acc = 0;
    const segs = byType.slice(0, 10).map(function (t, i) {
      const p = (Number(t.count) || 0) / total * 100;
      const seg = TYPE_COLORS[i % TYPE_COLORS.length] + ' ' + acc.toFixed(2) + '% ' + (acc + p).toFixed(2) + '%';
      acc += p;
      return seg;
    });
    if (acc < 100) segs.push('#e2e8f0 ' + acc.toFixed(2) + '% 100%');
    html += '<div class="donut-wrap">' +
      '<div class="donut" style="background:conic-gradient(' + segs.join(',') + ')">' +
      '<div class="donut-hole"><div class="donut-num">' + fmtNum(total) + '</div><div class="donut-lbl">个文件</div></div>' +
      '</div>' +
      '<div class="donut-legend">';
    byType.slice(0, 10).forEach(function (t, i) {
      html += '<div class="legend-row">' +
        '<span class="legend-dot" style="background:' + TYPE_COLORS[i % TYPE_COLORS.length] + '"></span>' +
        '<span class="legend-lbl">' + escapeHtml(t.label || t.type || '其他') + '</span>' +
        '<span class="legend-val">' + fmtNum(t.count) + ' · ' + ((Number(t.pct) || 0) * 100).toFixed(1) + '%</span>' +
        '</div>';
    });
    html += '</div></div>';
  }

  // 扫描状态条
  const statusMap = { idle: '空闲', running: '扫描中', done: '已完成', error: '失败' };
  html += '<div class="scan-bar">' +
    '<span class="badge' + (running ? '' : sc.status === 'error' ? ' danger' : ' ok') + '">' +
    icon(running ? 'loader' : sc.status === 'error' ? 'alert' : 'checkCircle', 13) +
    (statusMap[sc.status] || sc.status || '空闲') + '</span>' +
    '<span class="muted" style="font-size:12.5px">上次扫描：' + (sc.last_scan_at ? escapeHtml(formatTime(sc.last_scan_at)) : '从未') + '</span>';
  if (running && sc.progress) {
    const total = Number(sc.progress.total) || 0;
    const done = Number(sc.progress.done) || 0;
    const ratio = total ? Math.min(1, done / total) : 0;
    html += '<div class="progress" style="flex:1;min-width:120px"><div class="bar" style="width:' + (ratio * 100).toFixed(1) + '%"></div></div>' +
      '<span class="muted" style="font-size:12px">' + fmtNum(done) + ' / ' + fmtNum(total) + '</span>';
  }
  if (sc.error) {
    html += '<span class="badge danger">' + escapeHtml(sc.error) + '</span>';
  }
  html += '<span style="flex:1"></span>' +
    '<button class="btn primary sm" id="home-scan-btn"' + (running ? ' disabled' : '') + '>' +
    icon('refresh', 14) + (running ? '扫描中…' : '重新扫描') + '</button>' +
    '</div>';

  html += '</div>';
  box.innerHTML = head + html;

  const btn = $('#home-scan-btn', box);
  if (btn && !running) btn.addEventListener('click', startScan);
}

async function startScan() {
  const btn = $('#home-scan-btn');
  if (btn) { btn.disabled = true; btn.textContent = '提交中…'; }
  try {
    await apiPost('/api/scan', {});
    toast('知识库扫描已开始', 'success');
    await loadHomeDashboard();
  } catch (e) {
    if (btn) { btn.disabled = false; btn.innerHTML = icon('refresh', 14) + '重新扫描'; }
  }
}

/* ============ 板块二：任务计划 ============ */
async function loadHomeTasks() {
  homeState.tasksLoading = true;
  renderHomeTasks();
  try {
    const data = await apiGet('/api/tasks');
    homeState.tasks = (data && data.tasks) || [];
  } catch (e) {
    homeState.tasks = [];
  }
  homeState.tasksLoading = false;
  renderHomeTasks();
}

/* ============ 日程数据 ============ */
async function loadHomeEvents() {
  homeState.eventsLoading = true;
  try {
    const data = await apiGet('/api/events');
    homeState.events = (data && data.events) || [];
  } catch (e) {
    homeState.events = [];
  }
  homeState.eventsLoading = false;
  renderHomeSchedule();
  renderHomeToday();
  renderHomeHero();
}

/* ============ 团队公告 ============ */
async function loadAnnouncements() {
  try {
    const data = await apiGet('/api/announcements', { silent: true });
    renderHomeAnnouncements((data && data.announcements) || []);
  } catch (e) { /* 次要 */ }
}

function renderHomeAnnouncements(list) {
  const box = $('#home-announce');
  if (!box) return;
  // 客户端模式公告只读：发布 / 管理入口隐藏
  const canManage = (typeof state !== 'undefined' && state.user && state.user.role === 'admin') && !isClientMode();
  const latest = list[0];
  if (!latest && !canManage) {
    box.classList.add('hidden');
    return;
  }
  box.classList.remove('hidden');
  let html = '<div class="announce-inner">' +
    '<span class="announce-icon">' + icon('inbox', 15) + '</span>' +
    '<div class="announce-body">';
  if (latest) {
    html += '<div class="announce-text">' + renderMarkdown(latest.content) + '</div>' +
      '<span class="announce-meta">' + escapeHtml(latest.by || '') + ' · ' + escapeHtml(relativeTime(latest.created_at)) + '</span>';
    if (list.length > 1) {
      html += '<button class="btn sm ghost" id="ann-more">共 ' + list.length + ' 条</button>';
    }
  } else {
    html += '<span class="announce-text muted">暂无公告' + (canManage ? '，点右侧发布' : '') + '</span>';
  }
  html += '</div>';
  if (canManage) {
    html += '<button class="btn sm" id="ann-manage">' + icon('edit', 13) + '公告管理</button>';
  }
  html += '</div>';
  box.innerHTML = html;
  mountIcons(box);
  const bind = function (id, fn) {
    const b = $('#' + id, box);
    if (b) b.addEventListener('click', fn);
  };
  bind('ann-manage', function () { openAnnouncementModal(list); });
  bind('ann-more', function () { openAnnouncementModal(list, true); });
}

function openAnnouncementModal(list, readOnly) {
  // 客户端模式公告只读（发布接口在服务端管理）
  const canManage = (typeof state !== 'undefined' && state.user && state.user.role === 'admin') && !isClientMode();
  const rowsHtml = (list || []).map(function (a) {
    return '<div class="ann-item" data-id="' + escapeHtml(String(a.id)) + '">' +
      '<div class="ann-item-body"><div>' + renderMarkdown(a.content) + '</div>' +
      '<div class="muted" style="font-size:11.5px;margin-top:4px">' + escapeHtml(a.by || '') + ' · ' + escapeHtml(formatTime(a.created_at)) + '</div></div>' +
      (canManage ? '<button class="btn icon sm ghost" data-del title="删除" style="color:var(--danger)">' + icon('trash', 13) + '</button>' : '') +
      '</div>';
  }).join('') || '<div class="hint">暂无公告</div>';

  showModal({
    title: '团队公告',
    body:
      (canManage && !readOnly
        ? '<div class="field"><label>发布新公告</label>' +
          '<textarea class="textarea" id="ann-content" maxlength="500" style="min-height:80px" placeholder="输入公告内容…"></textarea></div>' +
          '<button class="btn primary sm" id="ann-publish">' + icon('plus', 14) + '发布</button><div style="height:12px"></div>'
        : '') +
      '<div class="home-sub-title">历史公告</div>' + rowsHtml,
    footer: '<button class="btn primary" data-close>关闭</button>',
    onMount: function (root, close) {
      const pub = $('#ann-publish', root);
      if (pub) pub.addEventListener('click', async function () {
        const content = $('#ann-content', root).value.trim();
        if (!content) { toast('请输入公告内容', 'warn'); return; }
        pub.disabled = true;
        try {
          await apiPost('/api/announcements', { content: content });
          toast('公告已发布', 'success');
          close();
          loadAnnouncements();
        } catch (e) { pub.disabled = false; }
      });
      $$('[data-del]', root).forEach(function (b) {
        b.addEventListener('click', async function () {
          const id = b.closest('.ann-item').getAttribute('data-id');
          const ok = await confirmDialog('确定删除这条公告吗？', { title: '删除公告', danger: true, okText: '删除' });
          if (!ok) return;
          try {
            await apiDel('/api/announcements/' + encodeURIComponent(id));
            toast('公告已删除', 'success');
            close();
            loadAnnouncements();
          } catch (e) { /* api 层已提示 */ }
        });
      });
    },
  });
}

/* 日期工具：YYYY-MM-DD / 时间戳 */
function ymd(d) {
  const p = (x) => (x < 10 ? '0' + x : '' + x);
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate());
}
function parseYmd(s) {
  const parts = String(s || '').split('-');
  return new Date(Number(parts[0]), Number(parts[1]) - 1, Number(parts[2]));
}
function evStart(ev) {
  let n = Number(ev.start_at) || 0;
  if (n < 1e12) n = n * 1000;
  return new Date(n);
}
function fmtEventTime(ev) {
  const d = evStart(ev);
  const p = (x) => (x < 10 ? '0' + x : '' + x);
  if (ev.all_day) return '全天';
  let s = p(d.getHours()) + ':' + p(d.getMinutes());
  if (ev.end_at) {
    const e2 = evStart({ start_at: ev.end_at });
    s += ' - ' + p(e2.getHours()) + ':' + p(e2.getMinutes());
  }
  return s;
}
function daysUntilText(ev) {
  const today = parseYmd(ymd(new Date()));
  const day = parseYmd(ymd(evStart(ev)));
  const diff = Math.round((day - today) / 86400000);
  if (diff < 0) return '已过';
  if (diff === 0) return '今天';
  if (diff === 1) return '明天';
  return '还有 ' + diff + ' 天';
}
function eventsOn(dateStr) {
  return homeState.events.filter(function (ev) {
    return ymd(evStart(ev)) === dateStr;
  });
}

function taskStatusLabel(s) {
  return s === 'doing' ? '进行中' : s === 'done' ? '已完成' : '待办';
}

function taskStatusClass(s) {
  if (s === 'done') return 'ok';
  if (s === 'doing') return '';
  return 'muted';
}

function renderHomeTasks() {
  const box = $('#home-tasks');
  if (!box) return;
  const head = '<div class="card-head">' + icon('check', 18) + '<h3>任务计划</h3><span class="sub">' + homeState.tasks.length + ' 项</span></div>';
  const quick =
    '<div class="task-input-row">' +
    '<input class="input" id="task-quick-title" placeholder="添加任务，回车确认" maxlength="120">' +
    '<button class="btn primary" id="task-quick-add">' + icon('plus', 15) + '添加</button>' +
    '</div>';

  if (homeState.tasksLoading && !homeState.tasks.length) {
    box.innerHTML = head + homeSkeleton(4);
    return;
  }

  let list = '';
  if (!homeState.tasks.length) {
    list = emptyStateHtml('check', '暂无任务', '在上方输入框添加你的第一项任务');
  } else {
    list = '<div class="task-list">';
    homeState.tasks.forEach(function (t, idx) {
      const done = t.status === 'done';
      const over = isOverdue(t);
      list +=
        '<div class="task-item" data-idx="' + idx + '">' +
        '<button class="task-dot dot-' + escapeHtml(t.status || 'todo') + '" data-act="cycle" title="点击切换状态（' + taskStatusLabel(t.status) + '）">' +
        (done ? icon('check', 12) : '') + '</button>' +
        '<div class="task-body">' +
        '  <div class="task-title' + (done ? ' done' : '') + '" data-act="edit">' + escapeHtml(t.title || '(无标题)') + '</div>' +
        (t.note ? '  <div class="task-note">' + escapeHtml(t.note) + '</div>' : '') +
        '  <div class="task-meta">' +
        '    <span class="badge ' + taskStatusClass(t.status) + '">' + taskStatusLabel(t.status) + '</span>' +
        (t.by ? '<span class="muted">' + icon('user', 11) + ' ' + escapeHtml(t.by) + '</span>' : '') +
        (t.due ? '<span class="' + (over ? 'due-over' : 'muted') + '">' + (over ? '已过期 · ' : '截止 ') + escapeHtml(formatDue(t.due)) + '</span>' : '') +
        '  </div>' +
        '</div>' +
        '<button class="btn icon sm ghost" data-act="del" title="删除">' + icon('trash', 14) + '</button>' +
        '</div>';
    });
    list += '</div>';
  }

  box.innerHTML = head + '<div class="card-body">' + quick + list + '</div>';

  const input = $('#task-quick-title', box);
  const addBtn = $('#task-quick-add', box);
  async function quickAdd() {
    const title = (input.value || '').trim();
    if (!title) { toast('请输入任务标题', 'warn'); return; }
    addBtn.disabled = true;
    try {
      const data = await apiPost('/api/tasks', { title: title });
      if (data && data.task) homeState.tasks.unshift(data.task);
      input.value = '';
      renderHomeTasks();
      if (!(data && data.task)) await loadHomeTasks(); // 响应缺 task 时回源刷新
    } catch (e) { /* api 层已提示 */ }
    addBtn.disabled = false;
    const again = $('#task-quick-title');
    if (again) again.focus();
  }
  addBtn.addEventListener('click', quickAdd);
  input.addEventListener('keydown', function (e) {
    if (e.key === 'Enter') quickAdd();
  });

  $$('.task-item', box).forEach(function (item) {
    const t = homeState.tasks[Number(item.getAttribute('data-idx'))];
    $$('[data-act]', item).forEach(function (b) {
      b.addEventListener('click', function () {
        const act = b.getAttribute('data-act');
        if (act === 'cycle') cycleTask(t);
        else if (act === 'edit') openTaskEditModal(t);
        else if (act === 'del') deleteTask(t);
      });
    });
  });
  renderHomeToday();
}

async function cycleTask(t) {
  const order = ['todo', 'doing', 'done'];
  const next = order[(order.indexOf(t.status || 'todo') + 1) % order.length];
  try {
    await apiPatch('/api/tasks/' + encodeURIComponent(String(t.id)), { status: next });
    t.status = next;
    renderHomeTasks();
  } catch (e) { /* api 层已提示 */ }
}

async function deleteTask(t) {
  const ok = await confirmDialog('确定删除任务「' + escapeHtml(t.title || '') + '」吗？', { title: '删除任务', danger: true, okText: '删除' });
  if (!ok) return;
  try {
    await apiDel('/api/tasks/' + encodeURIComponent(String(t.id)));
    homeState.tasks = homeState.tasks.filter(function (x) { return String(x.id) !== String(t.id); });
    renderHomeTasks();
    toast('任务已删除', 'success');
  } catch (e) { /* api 层已提示 */ }
}

function openTaskEditModal(t) {
  const dueVal = t.due && parseDue(t.due) ? formatDue(t.due) : '';
  showModal({
    title: '编辑任务',
    body:
      '<div class="field"><label>标题</label><input class="input" id="te-title" value="' + escapeHtml(t.title || '') + '"></div>' +
      '<div class="field"><label>备注</label><textarea class="textarea" id="te-note" style="min-height:80px">' + escapeHtml(t.note || '') + '</textarea></div>' +
      '<div class="form-row">' +
      '  <div class="field"><label>截止日期</label><input class="input" type="date" id="te-due" value="' + escapeHtml(dueVal) + '"></div>' +
      '  <div class="field"><label>状态</label><select class="select" id="te-status">' +
      '    <option value="todo"' + (t.status === 'todo' ? ' selected' : '') + '>待办</option>' +
      '    <option value="doing"' + (t.status === 'doing' ? ' selected' : '') + '>进行中</option>' +
      '    <option value="done"' + (t.status === 'done' ? ' selected' : '') + '>已完成</option>' +
      '  </select></div>' +
      '</div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="te-go">' + icon('save', 15) + '保存</button>',
    onMount: function (root, close) {
      $('#te-go', root).addEventListener('click', async function () {
        const title = $('#te-title', root).value.trim();
        if (!title) { toast('任务标题不能为空', 'warn'); return; }
        const btn = $('#te-go', root);
        btn.disabled = true;
        try {
          await apiPatch('/api/tasks/' + encodeURIComponent(String(t.id)), {
            title: title,
            note: $('#te-note', root).value,
            due: $('#te-due', root).value,
            status: $('#te-status', root).value,
          });
          toast('任务已更新', 'success');
          close();
          loadHomeTasks();
        } catch (e) {
          btn.disabled = false;
        }
      });
    },
  });
}

/* ============ 板块三：近期添加 ============ */
const RECENT_KINDS = {
  upload: { i: 'upload', c: '#6366f1', t: '上传' },
  import: { i: 'link', c: '#0ea5e9', t: '导入' },
  scan_new: { i: 'inbox', c: '#12a672', t: '新发现' },
  note: { i: 'note', c: '#f59e0b', t: '笔记' },
};

function renderHomeRecent() {
  const box = $('#home-recent');
  if (!box) return;
  const head = '<div class="card-head">' + icon('inbox', 18) + '<h3>近期添加</h3><span class="sub">最近 30 条</span></div>';
  const d = homeState.dashboard;
  if (!d) {
    box.innerHTML = head + homeSkeleton(5);
    return;
  }
  const recent = Array.isArray(d.recent) ? d.recent : [];
  if (!recent.length) {
    box.innerHTML = head + emptyStateHtml('inbox', '暂无新增内容', '上传文件、导入链接或完成扫描后，这里会展示最新入库的内容');
    mountIcons(box);
    return;
  }
  let list = '<div class="recent-list timeline">';
  recent.slice(0, 30).forEach(function (r, idx) {
    const k = RECENT_KINDS[r.kind] || { i: 'file', c: '#8b93a7', t: r.kind || '内容' };
    const clickable = !!r.media_id;
    list +=
      '<div class="recent-item' + (clickable ? ' clickable' : '') + '" data-idx="' + idx + '">' +
      '<div class="recent-icon" style="background:' + k.c + '1f;color:' + k.c + '">' + icon(k.i, 15) + '</div>' +
      '<div class="recent-body">' +
      '  <div class="recent-title">' + escapeHtml(r.title || '(无标题)') + '</div>' +
      '  <div class="recent-meta">' +
      '    <span class="badge muted">' + k.t + '</span>' +
      (r.account_name ? '<span class="chip">' + escapeHtml(r.account_name) + '</span>' : '') +
      (r.by ? '<span class="chip">' + icon('user', 11) + escapeHtml(r.by) + '</span>' : '') +
      (r.kb_name ? '<span class="muted">' + escapeHtml(r.kb_name) + '</span>' : '') +
      (r.size ? '<span class="muted">' + humanSize(r.size) + '</span>' : '') +
      '    <span style="flex:1"></span>' +
      '    <span class="muted">' + escapeHtml(relativeTime(r.at)) + '</span>' +
      '  </div>' +
      '</div>' +
      (clickable ? icon('chevronRight', 15) : '') +
      '</div>';
  });
  list += '</div>';
  box.innerHTML = head + '<div class="card-body">' + list + '</div>';

  $$('.recent-item', box).forEach(function (item) {
    const r = recent[Number(item.getAttribute('data-idx'))];
    if (!r.media_id) return;
    item.addEventListener('click', function () {
      openMediaDetail(String(r.media_id), r.ia || '');
    });
  });
}

/* ============ 板块四：账号贡献 ============ */
function renderHomeAccounts() {
  const box = $('#home-accounts');
  if (!box) return;
  const head = '<div class="card-head">' + icon('users', 18) + '<h3>账号贡献</h3><span class="sub">容量池构成</span></div>';
  const d = homeState.dashboard;
  if (!d) {
    box.innerHTML = head + homeSkeleton(3);
    return;
  }
  const accounts = Array.isArray(d.by_account) ? d.by_account : [];
  if (!accounts.length) {
    box.innerHTML = head + emptyStateHtml('users', '暂无账号数据', '绑定 IMA 账号并完成扫描后可见贡献分布');
    mountIcons(box);
    return;
  }
  let list = '<div class="acct-contrib">';
  accounts.forEach(function (a, i) {
    const share = Math.max(0, Math.min(1, Number(a.share) || 0));
    const grad = ACCT_COLORS[i % ACCT_COLORS.length];
    const initial = escapeHtml((a.name || a.ia || '?').charAt(0).toUpperCase());
    list +=
      '<div class="acct-row">' +
      '<div class="acct-row-head">' +
      '  <span class="acct-avatar" style="background:' + grad + '">' + initial + '</span>' +
      '  <strong>' + escapeHtml(a.name || a.ia || '(账号)') + '</strong>' +
      '  <span style="flex:1"></span>' +
      '  <span class="muted" style="font-size:12px">' + (share * 100).toFixed(1) + '% 文件占比</span>' +
      '</div>' +
      '<div class="bar-track" style="margin:8px 0"><div class="bar-fill" style="width:' + Math.max(1.5, share * 100).toFixed(1) + '%;background:' + grad + '"></div></div>' +
      '<div class="acct-stats">' +
      '<span class="mini-chip">知识库 ' + fmtNum(a.kbs) + '</span>' +
      '<span class="mini-chip">文件 ' + fmtNum(a.files) + '</span>' +
      '<span class="mini-chip">文件夹 ' + fmtNum(a.folders) + '</span>' +
      '<span class="mini-chip">上传 ' + fmtNum(a.uploads) + '</span>' +
      '</div>' +
      (a.error ? '<div class="acct-error">' + icon('alert', 13) + '扫描失败：' + escapeHtml(a.error) + '</div>' : '') +
      '</div>';
  });
  list += '</div>';
  box.innerHTML = head + '<div class="card-body">' + list + '</div>';
  mountIcons(box);
}

/* ============ 板块五：今日待办（任务到期 + 当日日程聚合） ============ */
function renderHomeToday() {
  const box = $('#home-today');
  if (!box) return;
  const todayStr = ymd(new Date());
  const now = new Date();
  const dateLabel = (now.getMonth() + 1) + ' 月 ' + now.getDate() + ' 日';
  const head = '<div class="card-head">' + icon('check', 18) + '<h3>今日待办</h3><span class="sub">' + dateLabel + '</span></div>';

  const todayEvents = eventsOn(todayStr);
  const overdue = [];
  const dueToday = [];
  homeState.tasks.forEach(function (t) {
    if (t.status === 'done') return;
    const d = parseDue(t.due);
    if (!d) return;
    const ds = ymd(d);
    if (ds < todayStr) overdue.push(t);
    else if (ds === todayStr) dueToday.push(t);
  });

  if (!overdue.length && !dueToday.length && !todayEvents.length) {
    box.innerHTML = head + emptyStateHtml('sun', '今天没有待办事项', '到期任务和今天的日程会自动汇总到这里');
    mountIcons(box);
    return;
  }

  let list = '<div class="today-list">';
  overdue.forEach(function (t) {
    list += '<div class="today-item" data-kind="task" data-id="' + escapeHtml(String(t.id)) + '">' +
      '<button class="today-dot dot-todo" title="点击切换状态"></button>' +
      '<div class="today-body">' +
      '<div class="today-title">' + escapeHtml(t.title) + '</div>' +
      '<div class="today-meta"><span class="badge danger">已过期</span><span class="muted">截止 ' + escapeHtml(formatDue(t.due)) + '</span>' +
      (t.by ? '<span class="muted">' + escapeHtml(t.by) + '</span>' : '') + '</div>' +
      '</div></div>';
  });
  dueToday.forEach(function (t) {
    list += '<div class="today-item" data-kind="task" data-id="' + escapeHtml(String(t.id)) + '">' +
      '<button class="today-dot dot-doing" title="点击切换状态"></button>' +
      '<div class="today-body">' +
      '<div class="today-title">' + escapeHtml(t.title) + '</div>' +
      '<div class="today-meta"><span class="badge">今天截止</span>' +
      (t.by ? '<span class="muted">' + escapeHtml(t.by) + '</span>' : '') + '</div>' +
      '</div></div>';
  });
  todayEvents.forEach(function (ev) {
    list += '<div class="today-item ev clickable" data-kind="event" data-id="' + escapeHtml(String(ev.id)) + '">' +
      '<div class="today-dot" style="background:#f59e0b"></div>' +
      '<div class="today-body">' +
      '<div class="today-title">' + escapeHtml(ev.title) + '</div>' +
      '<div class="today-meta"><span class="badge warn">' + escapeHtml(fmtEventTime(ev)) + '</span>' +
      (ev.location ? '<span class="muted">' + escapeHtml(ev.location) + '</span>' : '') +
      (ev.by ? '<span class="muted">' + escapeHtml(ev.by) + '</span>' : '') + '</div>' +
      '</div></div>';
  });
  list += '</div>';
  box.innerHTML = head + '<div class="card-body">' + list + '</div>';
  mountIcons(box);

  $$('.today-item', box).forEach(function (el) {
    const id = el.getAttribute('data-id');
    const kind = el.getAttribute('data-kind');
    if (kind === 'task') {
      const t = homeState.tasks.find(function (x) { return String(x.id) === id; });
      if (!t) return;
      const dot = $('.today-dot', el);
      if (dot) dot.addEventListener('click', function () { cycleTask(t); });
      $('.today-title', el).addEventListener('click', function () { openTaskEditModal(t); });
    } else {
      const ev = homeState.events.find(function (x) { return String(x.id) === id; });
      if (ev) el.addEventListener('click', function () { openEventModal(ev); });
    }
  });
}

/* ============ 板块六：日程（月历 + 即将到来） ============ */
function renderHomeSchedule() {
  const box = $('#home-schedule');
  if (!box) return;
  const head = '<div class="card-head">' + icon('inbox', 18) + '<h3>日程</h3><span class="sub">团队共享</span>' +
    '<span style="flex:1"></span>' +
    '<button class="btn primary sm" id="ev-add">' + icon('plus', 14) + '添加日程</button></div>';

  const month = homeState.calMonth || new Date();
  homeState.calMonth = month;
  const y = month.getFullYear();
  const m = month.getMonth();
  const startOffset = (new Date(y, m, 1).getDay() + 6) % 7; // 周一起
  const daysInMonth = new Date(y, m + 1, 0).getDate();
  const todayStr = ymd(new Date());

  let cal = '<div class="cal">' +
    '<div class="cal-head">' +
    '<button class="btn icon sm ghost" id="cal-prev">' + icon('chevronLeft', 14) + '</button>' +
    '<strong>' + y + ' 年 ' + (m + 1) + ' 月</strong>' +
    '<button class="btn icon sm ghost" id="cal-next">' + icon('chevronRight', 14) + '</button>' +
    '<span style="flex:1"></span>' +
    '<button class="btn sm ghost" id="cal-today">今天</button>' +
    '</div><div class="cal-grid">';
  ['一', '二', '三', '四', '五', '六', '日'].forEach(function (w) {
    cal += '<div class="cal-w">' + w + '</div>';
  });
  for (let i = 0; i < startOffset; i++) cal += '<div class="cal-day empty"></div>';
  for (let d = 1; d <= daysInMonth; d++) {
    const mm = (m + 1 < 10 ? '0' : '') + (m + 1);
    const dd = (d < 10 ? '0' : '') + d;
    const ds = y + '-' + mm + '-' + dd;
    const evs = eventsOn(ds);
    let dots = '';
    for (let k = 0; k < Math.min(evs.length, 3); k++) dots += '<i></i>';
    cal += '<div class="cal-day' + (ds === todayStr ? ' today' : '') + (ds === homeState.calSelected ? ' selected' : '') + '" data-date="' + ds + '">' +
      '<span class="cal-num">' + d + '</span>' +
      (dots ? '<span class="cal-dots">' + dots + '</span>' : '') +
      '</div>';
  }
  cal += '</div></div>';

  let ag = '';
  const evRow = function (ev) {
    const d = evStart(ev);
    return '<div class="ev-item" data-id="' + escapeHtml(String(ev.id)) + '">' +
      '<div class="ev-when"><div class="ev-day">' + (d.getMonth() + 1) + '/' + d.getDate() + '</div>' +
      '<div class="ev-time">' + escapeHtml(fmtEventTime(ev)) + '</div></div>' +
      '<div class="ev-body"><div class="ev-title">' + escapeHtml(ev.title) + '</div>' +
      '<div class="ev-meta">' +
      (ev.location ? '<span class="muted">' + escapeHtml(ev.location) + '</span>' : '') +
      (ev.by ? '<span class="chip">' + escapeHtml(ev.by) + '</span>' : '') +
      '</div></div>' +
      '<span class="badge ' + (daysUntilText(ev) === '今天' ? 'warn' : 'muted') + '">' + escapeHtml(daysUntilText(ev)) + '</span>' +
      '</div>';
  };

  if (homeState.calSelected) {
    const evs = eventsOn(homeState.calSelected);
    ag += '<div class="home-sub-title">' + escapeHtml(homeState.calSelected) + ' 的日程</div>';
    ag += evs.length
      ? '<div class="ev-list">' + evs.map(evRow).join('') + '</div>'
      : '<div class="hint">这天还没有日程</div>';
    ag += '<button class="btn sm" id="ev-add-day" style="margin-top:8px">' + icon('plus', 13) + '添加到这天</button>';
  } else {
    const upcoming = homeState.events.filter(function (ev) {
      return ymd(evStart(ev)) >= todayStr;
    }).slice(0, 6);
    ag += '<div class="home-sub-title">即将到来</div>';
    ag = upcoming.length
      ? ag + '<div class="ev-list">' + upcoming.map(evRow).join('') + '</div>'
      : ag + '<div class="hint">暂无即将到来的日程，点右上角添加</div>';
  }

  box.innerHTML = head + '<div class="card-body">' + cal + ag + '</div>';
  mountIcons(box);

  const bind = function (id, fn) {
    const b = $('#' + id, box);
    if (b) b.addEventListener('click', fn);
  };
  bind('ev-add', function () { openEventModal(null, homeState.calSelected || ymd(new Date())); });
  bind('ev-add-day', function () { openEventModal(null, homeState.calSelected); });
  bind('cal-prev', function () { homeState.calMonth = new Date(y, m - 1, 1); renderHomeSchedule(); });
  bind('cal-next', function () { homeState.calMonth = new Date(y, m + 1, 1); renderHomeSchedule(); });
  bind('cal-today', function () { homeState.calMonth = new Date(); homeState.calSelected = ''; renderHomeSchedule(); });
  $$('.cal-day[data-date]', box).forEach(function (el) {
    el.addEventListener('click', function () {
      const ds = el.getAttribute('data-date');
      homeState.calSelected = homeState.calSelected === ds ? '' : ds;
      renderHomeSchedule();
    });
  });
  $$('.ev-item', box).forEach(function (el) {
    const ev = homeState.events.find(function (x) { return String(x.id) === el.getAttribute('data-id'); });
    if (ev) el.addEventListener('click', function () { openEventModal(ev); });
  });
}

/* ============ 日程编辑弹窗 ============ */
function openEventModal(ev, presetDate) {
  const isEdit = !!ev;
  const d = ev ? evStart(ev) : (presetDate ? parseYmd(presetDate) : new Date());
  const p = (x) => (x < 10 ? '0' + x : '' + x);
  const dateVal = ymd(d);
  const timeVal = p(d.getHours()) + ':' + p(d.getMinutes());
  let endVal = '';
  if (ev && ev.end_at) {
    const e2 = evStart({ start_at: ev.end_at });
    endVal = p(e2.getHours()) + ':' + p(e2.getMinutes());
  }
  showModal({
    title: isEdit ? '编辑日程' : '添加日程',
    body:
      '<div class="field"><label>标题</label><input class="input" id="ev-title" maxlength="120" value="' + escapeHtml(ev ? ev.title : '') + '" placeholder="如：数学答疑 / 团队周会 / 模拟考"></div>' +
      '<div class="form-row">' +
      '  <div class="field"><label>日期</label><input class="input" type="date" id="ev-date" value="' + dateVal + '"></div>' +
      '  <div class="field"><label>开始时间</label><input class="input" type="time" id="ev-start" value="' + timeVal + '"></div>' +
      '  <div class="field"><label>结束时间（可选）</label><input class="input" type="time" id="ev-end" value="' + endVal + '"></div>' +
      '</div>' +
      '<div class="field"><label class="switch-line"><input type="checkbox" id="ev-allday"' + (ev && ev.all_day ? ' checked' : '') + '> 全天事件</label></div>' +
      '<div class="field"><label>地点（可选）</label><input class="input" id="ev-loc" maxlength="120" value="' + escapeHtml(ev ? (ev.location || '') : '') + '"></div>' +
      '<div class="field"><label>备注（可选）</label><textarea class="textarea" id="ev-note" style="min-height:70px">' + escapeHtml(ev ? (ev.note || '') : '') + '</textarea></div>',
    footer:
      (isEdit ? '<button class="btn" id="ev-del" style="color:var(--danger)">' + icon('trash', 14) + '删除</button>' : '') +
      '<button class="btn" data-close>取消</button>' +
      '<button class="btn primary" id="ev-save">' + icon('save', 15) + '保存</button>',
    onMount: function (root, close) {
      const allDay = $('#ev-allday', root);
      const startI = $('#ev-start', root);
      const endI = $('#ev-end', root);
      const syncTimes = function () {
        startI.disabled = allDay.checked;
        endI.disabled = allDay.checked;
      };
      allDay.addEventListener('change', syncTimes);
      syncTimes();

      $('#ev-save', root).addEventListener('click', async function () {
        const title = $('#ev-title', root).value.trim();
        const date = $('#ev-date', root).value;
        if (!title) { toast('请输入日程标题', 'warn'); return; }
        if (!date) { toast('请选择日期', 'warn'); return; }
        const isAll = allDay.checked;
        const st = isAll ? new Date(date + 'T00:00') : new Date(date + 'T' + (startI.value || '00:00'));
        const startAt = Math.floor(st.getTime() / 1000);
        let endAt = 0;
        if (!isAll && endI.value) {
          const e2 = new Date(date + 'T' + endI.value);
          endAt = Math.floor(e2.getTime() / 1000);
          if (endAt < startAt) { toast('结束时间早于开始时间', 'warn'); return; }
        }
        const payload = {
          title: title, start_at: startAt, end_at: endAt, all_day: isAll,
          location: $('#ev-loc', root).value.trim(), note: $('#ev-note', root).value,
        };
        const btn = $('#ev-save', root);
        btn.disabled = true;
        try {
          if (isEdit) {
            await apiPatch('/api/events/' + encodeURIComponent(String(ev.id)), payload);
            toast('日程已更新', 'success');
          } else {
            await apiPost('/api/events', payload);
            toast('日程已添加', 'success');
          }
          close();
          loadHomeEvents();
        } catch (e) { btn.disabled = false; }
      });

      const del = $('#ev-del', root);
      if (del) del.addEventListener('click', async function () {
        const ok = await confirmDialog('确定删除日程「' + escapeHtml(ev.title) + '」吗？', { title: '删除日程', danger: true, okText: '删除' });
        if (!ok) return;
        try {
          await apiDel('/api/events/' + encodeURIComponent(String(ev.id)));
          toast('日程已删除', 'success');
          close();
          loadHomeEvents();
        } catch (e) { /* api 层已提示 */ }
      });
    },
  });
}
