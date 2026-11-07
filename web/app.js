/* ImaHub — app.js：状态、路由、认证、主题、启动 */
'use strict';

const state = {
  user: null,
  me: null,
  serverInfo: null,
  needSetup: false,
  route: 'files',
  authMode: 'login', // login | register | setup
};

const PAGE_TITLES = {
  home: '首页',
  files: '文件 / 知识库',
  notes: '笔记',
  settings: '设置',
  admin: '管理后台',
};

/* 是否客户端模式（双模式之一：serve | client） */
function isClientMode() {
  return !!(state.serverInfo && state.serverInfo.mode === 'client');
}

/* ============ 401 统一处理（api.js 回调） ============ */
function onUnauthorized() {
  const appHidden = $('#app-view').classList.contains('hidden');
  state.user = null;
  state.me = null;
  if (!appHidden) toast('登录已过期，请重新登录', 'warn');
  showLogin(isClientMode() ? 'login' : (state.needSetup ? 'setup' : 'login'));
}

/* ============ 主题 ============ */
function currentThemeDark() {
  const pref = localStorage.getItem('imahub-theme');
  if (pref === 'dark') return true;
  if (pref === 'light') return false;
  return !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
}

function applyTheme() {
  const dark = currentThemeDark();
  document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light');
  const name = dark ? 'sun' : 'moon';
  ['#theme-btn', '#top-theme-btn', '#login-theme-btn'].forEach(function (sel) {
    const b = $(sel);
    if (!b) return;
    const svg = icon(name, 16);
    if (b.classList.contains('icon') && !b.textContent.trim()) b.innerHTML = svg;
    else b.innerHTML = svg + (b.id === 'theme-btn' ? '主题' : '');
    b.title = dark ? '切换到浅色模式' : '切换到深色模式';
  });
}

function toggleTheme() {
  const dark = currentThemeDark();
  localStorage.setItem('imahub-theme', dark ? 'light' : 'dark');
  applyTheme();
}

/* ============ 品牌 ============ */
function applyBranding() {
  const siteName = (state.serverInfo && state.serverInfo.site_name) || 'ImaHub';
  document.title = siteName;
  const heroName = $('#hero-site-name');
  if (heroName) heroName.textContent = siteName;
  const sideName = $('#side-site-name');
  if (sideName) sideName.textContent = siteName;
  const ver = state.serverInfo && state.serverInfo.version;
  const heroVer = $('#hero-version');
  if (heroVer) heroVer.textContent = ver ? ('IMA HUB · v' + ver) : 'IMA KNOWLEDGE HUB';
  const sideVer = $('#side-version');
  if (sideVer) sideVer.textContent = ver ? ('v' + ver) : 'IMA HUB';
}

/* ============ ima 多账号（容量池）：账号清单 ============ */
async function loadIaAccounts() {
  try {
    const data = await apiGet('/api/ima/accounts', { silent: true });
    imaAccountsState.accounts = (data && data.accounts) || [];
    imaAccountsState.defaultId = String((data && data.default_id) || '');
  } catch (e) {
    imaAccountsState.accounts = [];
    imaAccountsState.defaultId = '';
  }
}

/* ============ 多服务端列表（本地记住，一键填入） ============ */
function loadServerEntries() {
  try {
    return JSON.parse(localStorage.getItem('imahub_servers') || '[]') || [];
  } catch (e) {
    return [];
  }
}

function saveServerEntry(server, username) {
  if (!server) return;
  const list = loadServerEntries().filter(function (x) {
    return !(x.server === server && x.username === username);
  });
  list.unshift({ server: server, username: username, at: Date.now() });
  try {
    localStorage.setItem('imahub_servers', JSON.stringify(list.slice(0, 8)));
  } catch (e) { /* 存储满忽略 */ }
}

function renderServerChips(srvField) {
  const list = loadServerEntries();
  let box = $('#auth-server-list');
  if (!box) {
    box = document.createElement('div');
    box.id = 'auth-server-list';
    srvField.parentNode.insertBefore(box, srvField.nextSibling);
  }
  if (!list.length) {
    box.innerHTML = '';
    return;
  }
  box.innerHTML = '<div class="chips" style="margin-top:6px">' + list.map(function (x, i) {
    return '<button type="button" class="chip" data-srv-idx="' + i + '" title="' + escapeHtml(x.server) + '">' +
      escapeHtml(x.server) + (x.username ? ' · ' + escapeHtml(x.username) : '') + '</button>';
  }).join('') + '</div>';
  $$('#auth-server-list .chip').forEach(function (el) {
    el.addEventListener('click', function () {
      const x = loadServerEntries()[Number(el.getAttribute('data-srv-idx'))];
      if (!x) return;
      $('#auth-server').value = x.server;
      $('#auth-username').value = x.username || '';
      $('#auth-password').focus();
    });
  });
}

/* ============ 登录 / 注册 / 初始化 ============ */
function showLogin(mode) {
  if (isClientMode()) mode = 'login'; // 客户端固定「连接服务端」，无注册 / 初始化
  state.authMode = mode || (state.needSetup ? 'setup' : 'login');
  $('#app-view').classList.add('hidden');
  $('#login-view').classList.remove('hidden');

  // hero 图标
  $$('[data-hero-icon]').forEach(function (n) {
    if (n.firstChild && n.firstChild.nodeType === 1) return;
    n.innerHTML = icon(n.getAttribute('data-hero-icon'), 17);
    n.removeAttribute('data-hero-icon');
  });
  const heroLogo = $('#hero-logo');
  if (heroLogo && !heroLogo.firstChild) heroLogo.innerHTML = icon('database', 24);
  const sideLogo = $('#side-logo');
  if (sideLogo && !sideLogo.firstChild) sideLogo.innerHTML = icon('database', 19);

  renderAuthForm();
}

function renderAuthForm() {
  const mode = state.authMode;
  const tabs = $('#auth-tabs');
  const title = $('#auth-title');
  const sub = $('#auth-sub');
  const submit = $('#auth-submit');
  const pw2 = $('#auth-password2-field');
  const pw2Input = $('#auth-password2');
  const srvField = $('#auth-server-field');
  const srvInput = $('#auth-server');
  const allowRegister = !state.serverInfo || state.serverInfo.allow_register !== false;

  if (isClientMode()) {
    // 客户端模式：连接服务端（注册 / 初始化入口隐藏）
    tabs.classList.add('hidden');
    srvField.classList.remove('hidden');
    if (!srvInput.value && state.serverInfo && state.serverInfo.server) srvInput.value = state.serverInfo.server;
    title.textContent = '连接服务端';
    sub.textContent = '登录服务端账号';
    submit.textContent = '连 接';
    pw2.classList.add('hidden');
    pw2Input.required = false;
    $('#auth-password').setAttribute('autocomplete', 'current-password');
    renderServerChips(srvField);
    mountIcons($('#login-view'));
    return;
  }
  srvField.classList.add('hidden');

  if (mode === 'setup') {
    tabs.classList.add('hidden');
    title.textContent = '初始化管理员';
    sub.textContent = '首次使用，请创建管理员账号（自动登录）';
    submit.textContent = '创建管理员并进入';
    pw2.classList.remove('hidden');
    pw2Input.required = true;
    $('#auth-password').setAttribute('autocomplete', 'new-password');
  } else {
    tabs.classList.remove('hidden');
    $$('#auth-tabs button').forEach(function (b) {
      b.classList.toggle('active', b.getAttribute('data-tab') === mode);
    });
    $('#auth-tabs button[data-tab="register"]').classList.toggle('hidden', !allowRegister);
    if (mode === 'register') {
      title.textContent = '创建账号';
      sub.textContent = '注册一个 ImaHub 账号开始使用';
      submit.textContent = '注 册';
      pw2.classList.remove('hidden');
      pw2Input.required = true;
      $('#auth-password').setAttribute('autocomplete', 'new-password');
    } else {
      title.textContent = '欢迎回来';
      sub.textContent = '登录以访问你的 ima 知识库';
      submit.textContent = '登 录';
      pw2.classList.add('hidden');
      pw2Input.required = false;
      $('#auth-password').setAttribute('autocomplete', 'current-password');
    }
  }
  mountIcons($('#login-view'));
}

function bindAuthEvents() {
  $$('#auth-tabs button').forEach(function (b) {
    b.addEventListener('click', function () {
      state.authMode = b.getAttribute('data-tab');
      renderAuthForm();
    });
  });

  $('#auth-form').addEventListener('submit', async function (e) {
    e.preventDefault();
    const username = $('#auth-username').value.trim();
    const password = $('#auth-password').value;
    const password2 = $('#auth-password2').value;
    const mode = state.authMode;

    if (!username) { toast('请输入用户名', 'warn'); return; }
    if (!password) { toast('请输入密码', 'warn'); return; }
    if ((mode === 'register' || mode === 'setup') && password !== password2) {
      toast('两次输入的密码不一致', 'warn');
      return;
    }
    if ((mode === 'register' || mode === 'setup') && password.length < 6) {
      toast('密码至少 6 位', 'warn');
      return;
    }

    const btn = $('#auth-submit');
    btn.disabled = true;
    const oldText = btn.textContent;
    btn.textContent = '请稍候…';
    try {
      if (mode === 'setup') {
        await apiPost('/api/setup', { username: username, password: password });
        state.needSetup = false;
        toast('管理员创建成功，欢迎使用 ImaHub', 'success');
        await refreshMe();
        showApp();
      } else if (mode === 'register') {
        await apiPost('/api/register', { username: username, password: password });
        toast('注册成功，请登录', 'success');
        state.authMode = 'login';
        renderAuthForm();
        $('#auth-password').value = '';
      } else {
        const loginBody = { username: username, password: password };
        if (isClientMode()) loginBody.server = $('#auth-server').value.trim(); // 留空由服务端用上次地址
        await apiPost('/api/login', loginBody);
        if (isClientMode()) saveServerEntry(loginBody.server, username);
        await refreshMe();
        showApp();
      }
    } catch (err) {
      /* api 层已提示 */
    }
    btn.disabled = false;
    // 注册成功后表单已切登录态（renderAuthForm 定稿文案），模式未变才回写旧文案
    if (state.authMode === mode) btn.textContent = oldText;
  });

  $('#login-theme-btn').addEventListener('click', toggleTheme);
  $('#theme-btn').addEventListener('click', toggleTheme);
  $('#top-theme-btn').addEventListener('click', toggleTheme);
}

/* ============ 主界面 ============ */
async function refreshMe(opts) {
  const data = await apiGet('/api/me', opts);
  state.me = data;
  state.user = data.user;
  return data;
}

async function refreshServerInfo() {
  try {
    state.serverInfo = await apiGet('/api/server-info', { silent: true });
  } catch (e) { /* 忽略 */ }
  return state.serverInfo;
}

async function showApp() {
  $('#login-view').classList.add('hidden');
  $('#app-view').classList.remove('hidden');

  const u = state.user || {};
  $('#side-username').textContent = u.username || '—';
  $('#side-userrole').textContent = u.role === 'admin' ? '管理员' : '普通用户';
  $('#side-avatar').textContent = (u.username || 'U').charAt(0).toUpperCase();
  $('#nav-admin').classList.toggle('hidden', u.role !== 'admin' || isClientMode());

  applyBranding();
  mountIcons();
  startConnPoll();

  if (!location.hash || location.hash === '#/' || location.hash === '#') {
    location.hash = '#/home';
  } else {
    route();
  }
}

function route() {
  if ($('#app-view').classList.contains('hidden')) return;
  const hash = (location.hash || '#/home').replace(/^#\/?/, '');
  const name = (hash.split('?')[0] || 'home').split('/')[0] || 'home';

  if (name === 'admin' && (isClientMode() || !state.user || state.user.role !== 'admin')) {
    location.hash = '#/files';
    return;
  }
  const routeName = PAGE_TITLES[name] ? name : 'home';
  state.route = routeName;
  $('#page-title').textContent = PAGE_TITLES[routeName];
  $$('.side-item').forEach(function (b) {
    b.classList.toggle('active', b.getAttribute('data-route') === routeName);
  });
  closeSidebar();
  if (routeName !== 'home' && typeof stopHomePoll === 'function') stopHomePoll();

  const root = $('#view-root');
  if (routeName === 'home') renderHomeView(root);
  else if (routeName === 'files') renderFilesView(root);
  else if (routeName === 'notes') renderNotesView(root);
  else if (routeName === 'settings') renderSettingsView(root);
  else if (routeName === 'admin') renderAdminView(root);
}

/* ============ 移动端侧栏 ============ */
function openSidebar() {
  $('#sidebar').classList.add('open');
  $('#sidebar-overlay').classList.add('show');
}
function closeSidebar() {
  $('#sidebar').classList.remove('open');
  $('#sidebar-overlay').classList.remove('show');
}

/* ============ 启动 ============ */
async function boot() {
  applyTheme();
  bindAuthEvents();
  mountIcons();

  $('#hamburger').addEventListener('click', openSidebar);
  $('#sidebar-overlay').addEventListener('click', closeSidebar);
  $$('.side-item').forEach(function (b) {
    b.addEventListener('click', function () {
      const r = b.getAttribute('data-route');
      if (location.hash === '#/' + r) route();
      else location.hash = '#/' + r;
    });
  });
  $('#logout-btn').addEventListener('click', async function () {
    const ok = await confirmDialog('确定退出当前账号吗？', { title: '退出登录', okText: '退出' });
    if (!ok) return;
    try { await apiPost('/api/logout', {}); } catch (e) { /* 忽略 */ }
    state.user = null;
    state.me = null;
    showLogin('login');
  });
  window.addEventListener('hashchange', route);

  await refreshServerInfo();
  applyBranding();
  if (isClientMode()) {
    delete PAGE_TITLES.admin; // 客户端模式无管理后台
    state.needSetup = false; // 初始化只在服务端做
  } else {
    try {
      const st = await apiGet('/api/setup/status', { silent: true });
      state.needSetup = !!st.need_setup;
    } catch (e) { state.needSetup = false; }
  }

  try {
    await refreshMe({ silent: true });
    showApp();
  } catch (e) {
    showLogin(isClientMode() ? 'login' : (state.needSetup ? 'setup' : 'login'));
  }
}

/* ============ 客户端模式：服务端连接状态（30s 轮询，只刷标识） ============ */
let connPollTimer = null;

function setConnBadge(connected, server) {
  const pill = $('#conn-pill');
  if (!pill) return;
  pill.classList.toggle('off', !connected);
  const txt = $('#conn-text');
  if (txt) txt.textContent = connected ? '已连接' : '离线';
  pill.title = server ? ('服务端：' + server) : '服务端连接状态';
}

async function updateConnBadge() {
  if (!isClientMode() || $('#app-view').classList.contains('hidden')) return;
  try {
    const st = await apiGet('/api/client/status', { silent: true });
    if (state.serverInfo) {
      state.serverInfo.connected = !!st.connected;
      state.serverInfo.server = st.server || state.serverInfo.server;
      state.serverInfo.last_sync = st.last_sync;
    }
    setConnBadge(!!st.connected, st.server);
  } catch (e) {
    setConnBadge(false);
  }
}

function startConnPoll() {
  const pill = $('#conn-pill');
  if (!pill) return;
  if (!isClientMode()) { pill.classList.add('hidden'); return; }
  pill.classList.remove('hidden');
  updateConnBadge();
  if (!connPollTimer) connPollTimer = setInterval(updateConnBadge, 30000);
}

document.addEventListener('DOMContentLoaded', boot);
