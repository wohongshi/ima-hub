/* ImaHub — view-peers.js：节点页（P2P 局域网发现 / 邀请码互联） */
'use strict';

/* 昵称规则：中英文/数字/_-，≤24 字（与后端一致） */
const PEER_NICK_RE = /^[\u4e00-\u9fffA-Za-z0-9_-]{1,24}$/;

/* ============ 视图入口 ============ */
async function renderPeersView(root) {
  root.innerHTML =
    '<div class="peers-page">' +
    '  <div class="card" id="peer-me"></div>' +
    '  <div class="card" id="peer-list"></div>' +
    '  <div class="card" id="peer-access"></div>' +
    '</div>';
  const loading = '<div class="card-body"><div class="loading-center"><div class="spinner"></div></div></div>';
  $('#peer-me', root).innerHTML = loading;
  $('#peer-list', root).innerHTML = loading;
  $('#peer-access', root).innerHTML = loading;
  await Promise.all([loadPeersView(root), loadAccessCard(root)]);
}

/* 重新拉取节点数据（改名/增删后整体刷新，与后端保持一致） */
async function loadPeersView(root) {
  let data = null;
  try {
    data = await apiGet('/api/peers');
  } catch (e) {
    const errHtml = '<div class="card-body"><div class="hint">节点信息加载失败，请稍后重试</div></div>';
    $('#peer-me', root).innerHTML = errHtml;
    $('#peer-list', root).innerHTML = errHtml;
    return;
  }
  renderPeerMeCard($('#peer-me', root), data || {}, root);
  renderPeerListCard($('#peer-list', root), data || {}, root);
}

/* ============ 卡一：我的节点 ============ */
function renderPeerMeCard(box, data, root) {
  const me = data.me || {};
  const idPretty = me.id_pretty || me.id || '';
  box.innerHTML =
    '<div class="card-head">' + icon('user', 18) + '<h3>我的节点</h3><span class="sub">私钥不出本机</span></div>' +
    '<div class="card-body"><dl class="dl">' +
    '<dt>昵称</dt><dd><strong id="peer-me-nick">' + escapeHtml(me.nickname || '未命名') + '</strong> ' +
    '<button class="btn sm" id="peer-nick-edit">' + icon('edit', 13) + '编辑</button></dd>' +
    '<dt>节点 ID</dt><dd><span class="mono peer-id">' + escapeHtml(idPretty || '—') + '</span> ' +
    '<button class="btn sm" id="peer-id-copy">' + icon('copy', 13) + '复制</button></dd>' +
    '<dt>局域网发现</dt><dd>' + (data.lan_port
      ? '端口 <span class="mono">' + escapeHtml(String(data.lan_port)) + '</span><span class="muted"> · 同网段自动发现</span>'
      : '<span class="muted">未启用</span>') + '</dd>' +
    '</dl>' +
    '<div class="peer-invite-row"><button class="btn primary" id="peer-invite-gen">' + icon('key', 15) + '生成邀请码</button></div>' +
    '</div>';
  $('#peer-nick-edit', box).addEventListener('click', function () {
    openPeerNickModal(root, '修改昵称', me.nickname, function (nick) {
      return apiPatch('/api/peers/me', { nickname: nick });
    });
  });
  $('#peer-id-copy', box).addEventListener('click', function () { copyText(idPretty); });
  $('#peer-invite-gen', box).addEventListener('click', function () { openInviteModal(); });
}

/* ============ 卡二：节点列表 ============ */
function renderPeerListCard(box, data, root) {
  const peers = data.peers || [];
  box.innerHTML =
    '<div class="card-head">' + icon('users', 18) + '<h3>节点列表</h3><span class="sub">局域网发现 · 邀请互联</span></div>' +
    '<div class="card-body">' +
    '<div class="peer-add-row">' +
    '<input class="input" id="peer-invite-code" placeholder="粘贴对方邀请码（IH1. 开头）" autocomplete="off">' +
    '<button class="btn primary" id="peer-invite-add">' + icon('plus', 15) + '添加</button>' +
    '</div>' +
    (peers.length
      ? '<div class="peer-list">' + peers.map(peerRowHtml).join('') + '</div>'
      : emptyStateHtml('users', '暂无节点', '局域网内自动发现，或粘贴对方邀请码添加')) +
    '</div>';
  mountIcons(box);

  // 添加邀请码：成功刷新列表，失败由 api 层 toast msg
  $('#peer-invite-add', box).addEventListener('click', async function () {
    const input = $('#peer-invite-code', box);
    const code = input.value.trim();
    if (!code) { toast('请粘贴邀请码', 'warn'); return; }
    const btn = $('#peer-invite-add', box);
    btn.disabled = true;
    try {
      const res = await apiPost('/api/peers/invite', { code: code });
      const p = (res && res.peer) || {};
      toast('已添加节点 ' + (p.nickname || p.id_pretty || ''), 'success');
      loadPeersView(root);
    } catch (e) {
      btn.disabled = false; // api 层已提示
    }
  });

  // 行内操作：备注 / 删除
  $$('.peer-row', box).forEach(function (row) {
    const id = row.getAttribute('data-id');
    const p = peers.filter(function (x) { return x.id === id; })[0];
    if (!p) return;
    $('[data-act="nick"]', row).addEventListener('click', function () {
      openPeerNickModal(root, '节点备注', p.nickname, function (nick) {
        return apiPatch('/api/peers/' + encodeURIComponent(p.id), { nickname: nick });
      });
    });
    $('[data-act="del"]', row).addEventListener('click', async function () {
      const ok = await confirmDialog(
        '确定删除节点 <strong>' + escapeHtml(p.nickname || p.id_pretty || p.id) + '</strong> 吗？',
        { title: '删除节点', danger: true, okText: '删除' }
      );
      if (!ok) return;
      try {
        await apiDel('/api/peers/' + encodeURIComponent(p.id));
        toast('已删除节点', 'success');
        loadPeersView(root);
      } catch (e) { /* api 层已提示 */ }
    });
  });
}

/* 节点行：在线徽章 / 昵称 / ID 前 10 位 / 来源徽章 / 已授范围 / 最后可见 / 信任标记 + 操作 */
function peerRowHtml(p) {
  const srcBadge = p.source === 'invite'
    ? '<span class="badge">邀请</span>'
    : '<span class="badge muted">局域网</span>';
  const presence = p.online
    ? '<span class="peer-presence on" title="在线"><i></i>在线</span>'
    : '<span class="peer-presence" title="离线"><i></i>离线</span>';
  const trusted = p.trusted
    ? '<span class="peer-trusted" title="已信任">' + icon('shield', 13) + '</span>'
    : '';
  const scopes = (Array.isArray(p.scopes) ? p.scopes : [])
    .map(function (s) { return '<span class="badge">' + escapeHtml(ACCESS_SCOPE_LABELS[s] || s) + '</span>'; })
    .join('');
  return '<div class="peer-row" data-id="' + escapeHtml(p.id || '') + '">' +
    '<div class="peer-main">' +
    '<div class="peer-name"><span class="nm">' + escapeHtml(p.nickname || '未命名') + '</span>' + trusted + presence + '</div>' +
    '<div class="peer-meta">' +
    '<span class="mono">' + escapeHtml(String(p.id || '').slice(0, 10)) + '</span>' +
    srcBadge +
    (scopes ? '<span class="peer-scopes" title="已授予的访问范围">' + scopes + '</span>' : '') +
    '<span title="' + escapeHtml(formatTime(p.last_seen)) + '">' + escapeHtml(p.last_seen ? relativeTime(p.last_seen) : '从未') + '</span>' +
    (p.addr ? '<span>' + escapeHtml(p.addr) + '</span>' : '') +
    '</div></div>' +
    '<div class="peer-actions">' +
    '<button class="btn sm" data-act="nick">备注</button>' +
    '<button class="btn sm danger" data-act="del">删除</button>' +
    '</div></div>';
}

/* ============ 弹窗 ============ */
/* 昵称小弹窗：我的昵称 / 节点备注共用，onSave 返回 Promise */
function openPeerNickModal(root, title, current, onSave) {
  showModal({
    title: title,
    body:
      '<div class="field"><label>昵称</label><input class="input" id="pn-nick" maxlength="24" placeholder="中英文 / 数字 / _-" autocomplete="off"></div>' +
      '<div class="hint">限中英文、数字、_ -，最长 24 字</div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="pn-save">保存</button>',
    onMount: function (m, close) {
      const input = $('#pn-nick', m);
      input.value = current || '';
      input.focus();
      $('#pn-save', m).addEventListener('click', async function () {
        const nick = input.value.trim();
        if (!PEER_NICK_RE.test(nick)) { toast('昵称限中英文/数字/_-，≤24 字', 'warn'); return; }
        const btn = $('#pn-save', m);
        btn.disabled = true;
        try {
          await onSave(nick);
          close();
          loadPeersView(root);
        } catch (e) {
          btn.disabled = false; // api 层已提示
        }
      });
    },
  });
}

/* 生成邀请码弹窗：可选填对外地址，生成后展示 IH1. 码 + 复制 */
function openInviteModal() {
  showModal({
    title: '生成邀请码',
    body:
      '<div class="field"><label>对外地址（可选）</label><input class="input" id="iv-addr" placeholder="ip:port" autocomplete="off"></div>' +
      '<div class="hint">对方凭此码添加你为节点，添加即互相信任</div>' +
      '<div id="iv-out"></div>',
    footer: '<button class="btn" data-close>关闭</button><button class="btn primary" id="iv-go">' + icon('key', 15) + '生成</button>',
    onMount: function (m) {
      $('#iv-go', m).addEventListener('click', async function () {
        const addr = $('#iv-addr', m).value.trim();
        if (addr && !/^[^\s:]+:\d{1,5}$/.test(addr)) { toast('地址格式为 ip:port', 'warn'); return; }
        const btn = $('#iv-go', m);
        btn.disabled = true;
        try {
          const res = await apiPost('/api/peers/me/invite', addr ? { addrs: [addr] } : {});
          const code = (res && res.code) || '';
          $('#iv-out', m).innerHTML =
            '<div class="mcp-endpoint"><span class="mono">' + escapeHtml(code) + '</span>' +
            '<button class="btn sm" id="iv-copy">' + icon('copy', 13) + '复制</button></div>';
          $('#iv-copy', m).addEventListener('click', function () { copyText(code); });
        } catch (e) { /* api 层已提示 */ }
        btn.disabled = false;
      });
    },
  });
}

/* ============ 卡三：访问申请 ============ */
/* 范围/状态文案映射（与后端 scopes、status 取值一致） */
const ACCESS_SCOPE_LABELS = { search: '搜索', browse: '浏览', download: '下载' };
const ACCESS_STATUS_META = {
  pending: { label: '待处理', cls: 'pending' },
  approved: { label: '已批准', cls: 'approved' },
  denied: { label: '已拒绝', cls: 'denied' },
  revoked: { label: '已撤销', cls: 'revoked' },
  expired: { label: '已过期', cls: 'expired' },
};
const peersAccessState = { tab: 'received' };

/* 双向拉取申请列表（received / sent），失败静默由调用方渲染提示 */
function fetchAccessRequests() {
  return Promise.all([
    apiGet('/api/access/requests?dir=received', { silent: true }),
    apiGet('/api/access/requests?dir=sent', { silent: true }),
  ]).then(function (arr) {
    return { received: arr[0] || {}, sent: arr[1] || {} };
  });
}

/* 刷新访问申请卡（发起/决策后局部刷新，不动节点列表） */
async function loadAccessCard(root) {
  const box = $('#peer-access', root);
  if (!box) return;
  let data = null;
  try {
    data = await fetchAccessRequests();
  } catch (e) {
    box.innerHTML =
      '<div class="card-head">' + icon('inbox', 18) + '<h3>访问申请</h3></div>' +
      '<div class="card-body"><div class="hint">访问申请加载失败，请稍后重试</div></div>';
    return;
  }
  renderAccessCard(box, data, root);
}

function renderAccessCard(box, data, root) {
  const tab = peersAccessState.tab === 'sent' ? 'sent' : 'received';
  const recList = (data.received && data.received.requests) || [];
  const sentList = (data.sent && data.sent.requests) || [];
  const outbox = Number(
    (data.received && data.received.outbox_pending) ||
    (data.sent && data.sent.outbox_pending) || 0
  );
  const list = tab === 'sent' ? sentList : recList;

  box.innerHTML =
    '<div class="card-head">' + icon('inbox', 18) + '<h3>访问申请</h3>' +
    '<span class="sub">节点间访问授权</span>' +
    '<div class="card-head-actions"><button class="btn sm primary" id="acc-new">' + icon('plus', 13) + '发起申请</button></div></div>' +
    '<div class="card-body">' +
    (outbox > 0 ? '<div class="acc-outbox">待投递 ' + outbox + ' 条</div>' : '') +
    '<div class="acc-tabs">' +
    '<button class="acc-tab' + (tab === 'received' ? ' active' : '') + '" data-tab="received">收到的' +
    (recList.length ? '<span class="acc-count">' + recList.length + '</span>' : '') + '</button>' +
    '<button class="acc-tab' + (tab === 'sent' ? ' active' : '') + '" data-tab="sent">发出的' +
    (sentList.length ? '<span class="acc-count">' + sentList.length + '</span>' : '') + '</button>' +
    '</div>' +
    (list.length
      ? '<div class="acc-list">' + list.map(function (r) { return accRowHtml(r, tab); }).join('') + '</div>'
      : '<div class="acc-empty">' + (tab === 'sent' ? '还没有发出的申请' : '暂无收到的申请') + '</div>') +
    '</div>';
  mountIcons(box);

  $('#acc-new', box).addEventListener('click', function () { openAccessAskModal(root); });
  $$('.acc-tab', box).forEach(function (b) {
    b.addEventListener('click', function () {
      peersAccessState.tab = b.getAttribute('data-tab');
      renderAccessCard(box, data, root);
    });
  });
  bindAccRows(box, root, tab, list);
}

/* 申请行：对方 / 范围徽章 / 附言 / 状态 + 行尾操作 */
function accRowHtml(r, dir) {
  const st = ACCESS_STATUS_META[r.status] || { label: r.status || '—', cls: 'muted' };
  const scopes = (Array.isArray(r.scopes) ? r.scopes : [])
    .map(function (s) { return '<span class="badge">' + escapeHtml(ACCESS_SCOPE_LABELS[s] || s) + '</span>'; })
    .join('');
  const acts = [];
  if (dir === 'received' && r.status === 'pending') {
    acts.push('<button class="btn sm primary" data-act="approve">批准</button>');
    acts.push('<button class="btn sm" data-act="deny">拒绝</button>');
  } else if (dir === 'received' && r.status === 'approved') {
    acts.push('<button class="btn sm danger" data-act="revoke">撤销</button>');
  } else if (dir === 'sent' && r.status === 'pending') {
    acts.push('<button class="btn sm" data-act="revoke">撤回</button>');
  } else if (dir === 'sent' && r.status === 'approved') {
    acts.push('<button class="btn sm danger" data-act="revoke">放弃</button>');
  }
  return '<div class="acc-row" data-id="' + escapeHtml(String(r.id)) + '">' +
    '<div class="acc-main">' +
    '<div class="acc-name"><span class="nm">' + escapeHtml(r.peer_nick || r.peer_id || '未知节点') + '</span>' +
    '<span class="badge ' + st.cls + '">' + st.label + '</span></div>' +
    '<div class="acc-meta">' + (scopes || '<span class="muted">无范围</span>') +
    (r.note ? '<span class="acc-note" title="' + escapeHtml(r.note) + '">' + escapeHtml(r.note) + '</span>' : '') +
    '<span title="' + escapeHtml(formatTime(r.updated_at || r.created_at)) + '">' +
    escapeHtml(relativeTime(r.updated_at || r.created_at) || '—') + '</span>' +
    '</div></div>' +
    (acts.length ? '<div class="acc-actions">' + acts.join('') + '</div>' : '') +
    '</div>';
}

/* 行内操作绑定：批准（弹窗）/ 拒绝、撤销、撤回、放弃（confirm 后执行） */
function bindAccRows(box, root, dir, list) {
  $$('.acc-row', box).forEach(function (row) {
    const id = row.getAttribute('data-id');
    const r = list.filter(function (x) { return String(x.id) === id; })[0];
    if (!r) return;
    const approve = $('[data-act="approve"]', row);
    if (approve) approve.addEventListener('click', function () { openAccessApproveModal(root, r); });
    const deny = $('[data-act="deny"]', row);
    if (deny) deny.addEventListener('click', function () {
      decideAccess(root, r, 'deny', { msg: '确定拒绝该申请吗？', title: '拒绝申请', okText: '拒绝', toastText: '已拒绝' });
    });
    const revoke = $('[data-act="revoke"]', row);
    if (revoke) revoke.addEventListener('click', function () {
      const withdraw = dir === 'sent' && r.status === 'pending';
      decideAccess(root, r, 'revoke', {
        msg: withdraw ? '确定撤回该申请吗？' : (dir === 'sent' ? '确定放弃已获批准的访问吗？' : '确定撤销已授予的访问吗？'),
        title: withdraw ? '撤回申请' : '撤销授权',
        okText: '确认',
        danger: true,
        toastText: withdraw ? '已撤回' : '已撤销',
      });
    });
  });
}

/* 决策统一入口：confirm → POST /decide → 局部刷新 */
async function decideAccess(root, r, action, opts) {
  opts = opts || {};
  const ok = await confirmDialog(escapeHtml(opts.msg || '确认执行该操作吗？'), {
    title: opts.title || '确认操作',
    danger: !!opts.danger,
    okText: opts.okText || '确认',
  });
  if (!ok) return;
  try {
    await apiPost('/api/access/requests/' + encodeURIComponent(r.id) + '/decide', { action: action });
    toast(opts.toastText || '已完成', 'success');
    loadAccessCard(root);
  } catch (e) { /* api 层已提示 */ }
}

/* 范围勾选：读写三档（search/browse/download） */
function scopeCheckboxHtml(s, checked) {
  return '<label class="checkbox"><input type="checkbox" data-scope="' + s + '"' +
    (checked ? ' checked' : '') + '>' + escapeHtml(ACCESS_SCOPE_LABELS[s] || s) + '</label>';
}

function readScopeChecks(root) {
  return $$('input[data-scope]', root)
    .filter(function (i) { return i.checked; })
    .map(function (i) { return i.getAttribute('data-scope'); });
}

/* 批准弹窗：勾选授予范围 + 可选有效期 */
function openAccessApproveModal(root, r) {
  showModal({
    title: '批准访问申请',
    body:
      '<div class="hint" style="margin:0 0 12px">' +
      escapeHtml(r.peer_nick || r.peer_id || '') + ' 申请的访问范围</div>' +
      '<div class="field"><label>授予范围</label><div class="acc-scopes">' +
      ['search', 'browse', 'download'].map(function (s) {
        return scopeCheckboxHtml(s, (r.scopes || []).indexOf(s) >= 0);
      }).join('') +
      '</div></div>' +
      '<div class="field"><label>有效期</label><select class="select" id="ac-exp">' +
      '<option value="0">永久</option><option value="7">7 天</option><option value="30">30 天</option>' +
      '<option value="90">90 天</option><option value="180">180 天</option></select></div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="ac-ok">批准</button>',
    onMount: function (m, close) {
      $('#ac-ok', m).addEventListener('click', async function () {
        const scopes = readScopeChecks(m);
        if (!scopes.length) { toast('至少勾选一个范围', 'warn'); return; }
        const days = Number($('#ac-exp', m).value) || 0;
        const body = { action: 'approve', scopes: scopes };
        if (days > 0) body.expires_at = Math.floor(Date.now() / 1000) + days * 86400;
        const btn = $('#ac-ok', m);
        btn.disabled = true;
        try {
          await apiPost('/api/access/requests/' + encodeURIComponent(r.id) + '/decide', body);
          close();
          toast('已批准', 'success');
          loadAccessCard(root);
        } catch (e) {
          btn.disabled = false; // api 层已提示
        }
      });
    },
  });
}

/* 发起申请弹窗：选节点 + 勾范围 + 附言（对方掉线由服务端排队补投） */
async function openAccessAskModal(root) {
  let peers = [];
  try {
    const d = await apiGet('/api/peers', { silent: true });
    peers = (d && d.peers) || [];
  } catch (e) {
    toast('节点列表加载失败', 'error');
    return;
  }
  if (!peers.length) { toast('暂无节点，先添加节点', 'warn'); return; }
  showModal({
    title: '发起访问申请',
    body:
      '<div class="field"><label>申请节点</label><select class="select" id="ac-to">' +
      peers.map(function (p) {
        return '<option value="' + escapeHtml(p.id) + '">' +
          escapeHtml(p.nickname || p.id_pretty || p.id) + '</option>';
      }).join('') +
      '</select></div>' +
      '<div class="field"><label>申请范围</label><div class="acc-scopes">' +
      scopeCheckboxHtml('search', true) + scopeCheckboxHtml('browse', true) + scopeCheckboxHtml('download', false) +
      '</div></div>' +
      '<div class="field"><label>附言（可选）</label>' +
      '<input class="input" id="ac-note" maxlength="200" placeholder="给对方的说明" autocomplete="off"></div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="ac-send">发送</button>',
    onMount: function (m, close) {
      $('#ac-send', m).addEventListener('click', async function () {
        const scopes = readScopeChecks(m);
        if (!scopes.length) { toast('至少勾选一个范围', 'warn'); return; }
        const btn = $('#ac-send', m);
        btn.disabled = true;
        try {
          await apiPost('/api/access/requests', {
            to: $('#ac-to', m).value,
            scopes: scopes,
            note: $('#ac-note', m).value.trim(),
          });
          close();
          toast('申请已发出', 'success');
          loadAccessCard(root);
        } catch (e) {
          btn.disabled = false; // api 层已提示
        }
      });
    },
  });
}
