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
    '</div>';
  const loading = '<div class="card-body"><div class="loading-center"><div class="spinner"></div></div></div>';
  $('#peer-me', root).innerHTML = loading;
  $('#peer-list', root).innerHTML = loading;
  await loadPeersView(root);
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

/* 节点行：昵称 / ID 前 10 位 / 来源徽章 / 最后可见 / 信任标记 + 操作 */
function peerRowHtml(p) {
  const srcBadge = p.source === 'invite'
    ? '<span class="badge">邀请</span>'
    : '<span class="badge muted">局域网</span>';
  const trusted = p.trusted
    ? '<span class="peer-trusted" title="已信任">' + icon('shield', 13) + '</span>'
    : '';
  return '<div class="peer-row" data-id="' + escapeHtml(p.id || '') + '">' +
    '<div class="peer-main">' +
    '<div class="peer-name"><span class="nm">' + escapeHtml(p.nickname || '未命名') + '</span>' + trusted + '</div>' +
    '<div class="peer-meta">' +
    '<span class="mono">' + escapeHtml(String(p.id || '').slice(0, 10)) + '</span>' +
    srcBadge +
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
