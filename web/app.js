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

/* ============ 401 统一处理（api.js 回调） ============ */
function onUnauthorized() {
  const appHidden = $('#app-view').classList.contains('hidden');
  state.user = null;
  state.me = null;
  if (!appHidden) toast('登录已过期，请重新登录', 'warn');
  showLogin(state.needSetup ? 'setup' : 'login');
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
    if (b.id === 'login-theme-btn') b.innerHTML = svg;
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

/* ============ 登录 / 注册 / 初始化 ============ */
function showLogin(mode) {
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
  const allowRegister = !state.serverInfo || state.serverInfo.allow_register !== false;

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
        await apiPost('/api/login', { username: username, password: password });
        await refreshMe();
        showApp();
      }
    } catch (err) {
      /* api 层已提示 */
    }
    btn.disabled = false;
    btn.textContent = oldText;
    if (mode === 'setup') btn.textContent = '创建管理员并进入';
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
  $('#nav-admin').classList.toggle('hidden', u.role !== 'admin');

  applyBranding();
  mountIcons();

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
  const routeName = PAGE_TITLES[name] ? name : 'home';

  if (routeName === 'admin' && (!state.user || state.user.role !== 'admin')) {
    location.hash = '#/files';
    return;
  }
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
  try {
    const st = await apiGet('/api/setup/status', { silent: true });
    state.needSetup = !!st.need_setup;
  } catch (e) { state.needSetup = false; }

  try {
    await refreshMe({ silent: true });
    showApp();
  } catch (e) {
    showLogin(state.needSetup ? 'setup' : 'login');
  }
}

document.addEventListener('DOMContentLoaded', boot);
