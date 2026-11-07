/* ImaHub — view-misc.js：设置页 + 管理后台 */
'use strict';

/* ============ 设置页 ============ */
async function renderSettingsView(root) {
  root.innerHTML =
    '<div class="settings-page">' +
    '  <div class="card" id="set-account"></div>' +
    '  <div class="card" id="set-password"></div>' +
    '  <div class="card" id="set-ima"></div>' +
    '  <div class="card" id="set-ask"></div>' +
    '  <div class="card" id="set-mcp"></div>' +
    '</div>';

  // 账号信息
  const me = state.me || { user: {} };
  const u = me.user || {};
  const acc = $('#set-account', root);
  acc.innerHTML =
    '<div class="card-head">' + icon('user', 18) + '<h3>账号信息</h3></div>' +
    '<div class="card-body"><dl class="dl">' +
    '<dt>用户名</dt><dd><strong>' + escapeHtml(u.username || '—') + '</strong> <span class="badge' + (u.role === 'admin' ? '' : ' muted') + '">' + escapeHtml(u.role === 'admin' ? '管理员' : '普通用户') + '</span></dd>' +
    '<dt>用户 ID</dt><dd class="mono">' + escapeHtml(String(u.id !== undefined ? u.id : '—')) + '</dd>' +
    '<dt>创建时间</dt><dd>' + escapeHtml(formatTime(u.created_at)) + '</dd>' +
    '<dt>账号状态</dt><dd>' + (u.disabled ? '<span class="badge danger">已禁用</span>' : '<span class="badge ok">正常</span>') + '</dd>' +
    '</dl></div>';

  // 修改密码
  const pw = $('#set-password', root);
  pw.innerHTML =
    '<div class="card-head">' + icon('lock', 18) + '<h3>修改密码</h3></div>' +
    '<div class="card-body">' +
    '<div class="form-row">' +
    '  <div class="field"><label>当前密码</label><input class="input" type="password" id="pw-old" autocomplete="current-password"></div>' +
    '  <div class="field"><label>新密码</label><input class="input" type="password" id="pw-new" autocomplete="new-password"></div>' +
    '  <div class="field"><label>确认新密码</label><input class="input" type="password" id="pw-new2" autocomplete="new-password"></div>' +
    '</div>' +
    '<button class="btn primary" id="pw-save">' + icon('save', 15) + '保存新密码</button>' +
    '</div>';
  $('#pw-save', pw).addEventListener('click', async function () {
    const oldP = $('#pw-old', pw).value;
    const n1 = $('#pw-new', pw).value;
    const n2 = $('#pw-new2', pw).value;
    if (!oldP || !n1) { toast('请填写当前密码和新密码', 'warn'); return; }
    if (n1 !== n2) { toast('两次输入的新密码不一致', 'warn'); return; }
    if (n1.length < 6) { toast('新密码至少 6 位', 'warn'); return; }
    const btn = $('#pw-save', pw);
    btn.disabled = true;
    try {
      await apiPost('/api/me/password', { old_password: oldP, new_password: n1 });
      toast('密码修改成功', 'success');
      $('#pw-old', pw).value = ''; $('#pw-new', pw).value = ''; $('#pw-new2', pw).value = '';
    } catch (e) { /* api 层已提示 */ }
    btn.disabled = false;
  });

  renderImaCard(root, me);
  renderAskCard(root);
  renderMcpCard(root);
}

/* ============ 知识库问答模型（内置 agent 查询合成） ============ */
async function renderAskCard(root) {
  const box = $('#set-ask', root);
  if (!box) return;
  const isAdmin = (typeof state !== 'undefined' && state.user && state.user.role === 'admin');
  box.innerHTML =
    '<div class="card-head">' + icon('search', 18) + '<h3>知识库问答模型</h3><span class="sub">agent 级查询能力，内置合成回答</span></div>' +
    '<div class="card-body"><div class="loading-center"><div class="spinner"></div><div>加载配置…</div></div></div>';

  let status = { configured: false, model: '' };
  try {
    status = await apiGet('/api/ask/status', { silent: true });
  } catch (e) { /* 次要 */ }

  const body = $('.card-body', box);
  if (!isAdmin) {
    body.innerHTML = status.configured
      ? '<div class="hint">问答模型已配置（' + escapeHtml(status.model || '') + '），可在首页直接提问。</div>'
      : '<div class="hint">管理员尚未配置问答模型，首页提问将只返回知识库检索片段。</div>';
    return;
  }

  let cfg = {};
  try {
    cfg = (await apiGet('/api/admin/settings', { silent: true })) || {};
  } catch (e) {
    // 加载失败必须停在错误态：空表单保存会把已配置的问答模型清空
    body.innerHTML = emptyStateHtml('alert', '问答模型配置加载失败', e.message || '请稍后重试');
    mountIcons(body);
    return;
  }
  body.innerHTML =
    '<div class="hint" style="margin:0 0 10px">配置 OpenAI 兼容端点后，首页问答 / MCP ima_ask 会自动检索团队知识库并合成回答（工具内置调用，不依赖外部 agent）。留空 API Key 表示不修改。</div>' +
    '<div class="form-row">' +
    '  <div class="field"><label>Base URL</label><input class="input mono" id="ask-base" placeholder="https://api.openai.com/v1" value="' + escapeHtml(cfg.ask_base_url || '') + '"></div>' +
    '  <div class="field"><label>模型名</label><input class="input mono" id="ask-model" placeholder="如 deepseek-chat / gpt-4o-mini" value="' + escapeHtml(cfg.ask_model || '') + '"></div>' +
    '</div>' +
    '<div class="form-row">' +
    '  <div class="field"><label>API Key' + (cfg.ask_api_key_set ? '（已设置，留空不改）' : '') + '</label><input class="input mono" type="password" id="ask-key" placeholder="' + (cfg.ask_api_key_set ? '留空保持不变' : 'sk-...') + '" autocomplete="off"></div>' +
    '</div>' +
    '<button class="btn primary" id="ask-save">' + icon('save', 15) + '保存问答模型配置</button>' +
    '<span class="hint" style="margin-left:10px">' + (status.configured ? '当前：' + escapeHtml(status.model) : '当前未配置') + '</span>';
  $('#ask-save', body).addEventListener('click', async function () {
    const btn = $('#ask-save', body);
    btn.disabled = true;
    try {
      await apiPut('/api/admin/settings', {
        ask_base_url: $('#ask-base', body).value.trim(),
        ask_model: $('#ask-model', body).value.trim(),
        ask_api_key: $('#ask-key', body).value.trim(),
      });
      toast('问答模型配置已保存', 'success');
      renderAskCard(root);
    } catch (e) { /* api 层已提示 */ }
    btn.disabled = false;
  });
}

async function renderImaCard(root) {
  const box = $('#set-ima', root);
  const isAdmin = (typeof state !== 'undefined' && state.user && state.user.role === 'admin');
  box.innerHTML =
    '<div class="card-head">' + icon('database', 18) + '<h3>团队容量池</h3><span class="sub">全团队共享，知识库数据互通</span><span class="sub" id="ima-count"></span>' +
    '<span style="flex:1"></span>' +
    (isAdmin ? '<button class="btn primary sm" id="ima-add">' + icon('plus', 14) + '添加账号</button>' : '') +
    '</div>' +
    '<div class="card-body">' +
    '<div class="loading-center"><div class="spinner"></div><div>加载团队 ima 账号…</div></div>' +
    '</div>';
  const addBtn = $('#ima-add', box);
  if (addBtn) addBtn.addEventListener('click', function () { openImaAddModal(root); });

  let accounts = [];
  try {
    const data = await apiGet('/api/ima/accounts');
    accounts = (data && data.accounts) || [];
  } catch (e) {
    $('.card-body', box).innerHTML = emptyStateHtml('alert', '账号列表加载失败', e.message || '请稍后重试');
    mountIcons(box);
    return;
  }

  const cnt = $('#ima-count', box);
  if (cnt) cnt.textContent = '共 ' + accounts.length + ' 个可用账号';
  const body = $('.card-body', box);

  if (!accounts.length) {
    body.innerHTML = emptyStateHtml('key', '团队尚未配置 IMA 账号',
      '团队共享的 ima 开放平台凭证（Client ID + API Key）由组长（管理员）添加后，全团队即可浏览和上传',
      isAdmin ? '<button class="btn primary" id="ima-empty-add">' + icon('plus', 15) + '添加账号</button>' : '');
    mountIcons(body);
    const b = $('#ima-empty-add', body);
    if (b) b.addEventListener('click', function () { openImaAddModal(root); });
    return;
  }

  let html = '<div class="acct-list">';
  accounts.forEach(function (a, idx) {
    const isDefault = a.source === 'server_default';
    html +=
      '<div class="acct-item" data-idx="' + idx + '">' +
      '  <div class="acct-main">' +
      '    <div class="kb-cover">' + escapeHtml((a.name || a.client_id_preview || '?').charAt(0)) + '</div>' +
      '    <div class="acct-info">' +
      '      <div class="acct-name">' + escapeHtml(a.name || '(未命名账号)') +
      (isDefault ? ' <span class="badge muted">服务端默认</span>' : ' <span class="badge">团队共享</span>') +
      (a.bound === false ? ' <span class="badge warn">未绑定</span>' : '') +
      '      </div>' +
      '      <div class="acct-sub mono">' + escapeHtml(a.client_id_preview || String(a.id)) + '</div>' +
      '    </div>' +
      '    <div class="acct-actions">' +
      '      <button class="btn sm" data-act="test">' + icon('wifi', 14) + '测试连接</button>' +
      (isDefault || !isAdmin ? '' :
      '      <button class="btn sm" data-act="edit">' + icon('edit', 14) + '编辑凭证</button>' +
      '      <button class="btn sm" data-act="rename">改名</button>' +
      '      <button class="btn icon sm" data-act="del" title="删除" style="color:var(--danger)">' + icon('trash', 14) + '</button>') +
      '    </div>' +
      '  </div>' +
      (isDefault || !isAdmin ? '' :
      '  <div class="acct-upkb">' +
      '    <span class="lbl">默认上传知识库</span>' +
      '    <select class="select" data-f="upload_kb"><option value="">加载中…</option></select>' +
      '    <span class="hint" style="margin:0">自动分配上传时存入该知识库</span>' +
      '  </div>' +
      '  <div class="acct-edit hidden">' +
      '    <div class="form-row">' +
      '      <div class="field"><label>Client ID（留空不改）</label><input class="input mono" data-f="client_id" placeholder="留空保持不变"></div>' +
      '      <div class="field"><label>API Key（留空不改）</label><input class="input mono" type="password" data-f="api_key" placeholder="留空保持不变" autocomplete="off"></div>' +
      '    </div>' +
      '    <button class="btn primary sm" data-act="save-edit">' + icon('save', 14) + '保存修改</button>' +
      '  </div>') +
      '  <div class="acct-test hidden"></div>' +
      '</div>';
  });
  html += '</div>' +
    '<div class="hint">团队容量池：知识库 / 笔记本为全部账号的合并视图，全团队数据互通。上传由服务端自动分配账号；凭证由组长（管理员）维护，API Key 保存后不再回显。</div>';
  body.innerHTML = html;

  $$('.acct-item', body).forEach(function (item) {
    const a = accounts[Number(item.getAttribute('data-idx'))];
    const testBox = $('.acct-test', item);

    // 默认上传知识库下拉（仅用户账号）
    const upSel = $('[data-f="upload_kb"]', item);
    if (upSel) {
      apiGet(qs('/api/ima/kbs', { ia: String(a.id), limit: 50 })).then(function (data) {
        const kbs = pickList(data);
        let opts = '<option value="">未设置</option>';
        kbs.forEach(function (kb) {
          opts += '<option value="' + escapeHtml(String(kb.id)) + '"' +
            (String(a.upload_kb_id || '') === String(kb.id) ? ' selected' : '') + '>' +
            escapeHtml(kb.name || kb.id) + '</option>';
        });
        upSel.innerHTML = opts;
      }).catch(function () {
        upSel.innerHTML = '<option value="">加载失败</option>';
      });
      upSel.addEventListener('change', async function () {
        const v = upSel.value;
        if (!v) { toast('请选择一个知识库作为默认上传目标', 'warn'); return; }
        upSel.disabled = true;
        try {
          await apiPatch('/api/me/ima/' + encodeURIComponent(String(a.id)), { upload_kb_id: v });
          toast('已设置默认上传知识库', 'success');
        } catch (e) { /* api 层已提示 */ }
        upSel.disabled = false;
      });
    }
    $$('[data-act]', item).forEach(function (b) {
      b.addEventListener('click', async function () {
        const act = b.getAttribute('data-act');
        if (act === 'test') {
          b.disabled = true;
          testBox.classList.remove('hidden');
          testBox.innerHTML = '<div class="loading-center" style="padding:10px 0"><div class="spinner"></div><div>测试中…</div></div>';
          try {
            const data = await apiGet(qs('/api/ima/test', { ia: String(a.id) }));
            const ok = data && data.ok;
            testBox.innerHTML =
              '<div style="padding:10px 14px;border-radius:10px;background:' + (ok ? 'var(--ok-soft)' : 'var(--warn-soft)') + '">' +
              '<div style="display:flex;align-items:center;gap:8px;font-weight:600;color:' + (ok ? 'var(--ok)' : 'var(--warn)') + '">' +
              icon(ok ? 'checkCircle' : 'alert', 15) + (ok ? '连接正常' : '连接异常') + '</div>' +
              (data && data.note ? '<div class="hint" style="margin-top:4px">' + escapeHtml(data.note) + '</div>' : '') +
              '</div>';
          } catch (e) {
            testBox.innerHTML =
              '<div style="padding:10px 14px;border-radius:10px;background:var(--danger-soft)">' +
              '<div style="display:flex;align-items:center;gap:8px;font-weight:600;color:var(--danger)">' + icon('alert', 15) + '测试失败</div>' +
              '<div class="hint" style="margin-top:4px">' + escapeHtml(e.message || '未知错误') + '</div></div>';
          }
          b.disabled = false;
        } else if (act === 'edit') {
          $('.acct-edit', item).classList.toggle('hidden');
        } else if (act === 'rename') {
          openImaRenameModal(root, a);
        } else if (act === 'del') {
          const okDel = await confirmDialog(
            '确定删除账号 <strong>' + escapeHtml(a.name || a.client_id_preview || a.id) + '</strong> 吗？删除后该凭证立即失效。',
            { title: '删除 IMA 账号', danger: true, okText: '删除' }
          );
          if (!okDel) return;
          try {
            await apiDel('/api/me/ima?id=' + encodeURIComponent(String(a.id)));
            toast('账号已删除', 'success');
            await refreshImaAfterChange(root);
          } catch (e) { /* api 层已提示 */ }
        } else if (act === 'save-edit') {
          const cid = $('[data-f="client_id"]', item).value.trim();
          const key = $('[data-f="api_key"]', item).value.trim();
          if (!cid && !key) { toast('至少填写一项（留空表示不修改）', 'warn'); return; }
          const body2 = {};
          if (cid) body2.client_id = cid;
          if (key) body2.api_key = key;
          b.disabled = true;
          try {
            await apiPatch('/api/me/ima/' + encodeURIComponent(String(a.id)), body2);
            toast('账号凭证已更新', 'success');
            await refreshImaAfterChange(root);
          } catch (e) { b.disabled = false; }
        }
      });
    });
  });
}

/** 账号增删改后：刷新账号缓存与设置页 */
async function refreshImaAfterChange(root) {
  await refreshMe({ silent: true });
  await loadIaAccounts();
  renderSettingsView(root);
}

function openImaAddModal(root) {
  showModal({
    title: '添加 IMA 账号',
    body:
      '<div class="field"><label>账号名称（可选）</label><input class="input" id="ia-name" placeholder="例如：工作号 / 个人号" autocomplete="off"></div>' +
      '<div class="field"><label>Client ID</label><input class="input mono" id="ia-cid" placeholder="ima 开放平台 Client ID" autocomplete="off"></div>' +
      '<div class="field"><label>API Key</label><input class="input mono" type="password" id="ia-key" placeholder="ima 开放平台 API Key" autocomplete="off">' +
      '<div class="hint">API Key 仅用于服务端调用 ima 开放平台，保存后不再回显。</div></div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="ia-go">' + icon('plus', 15) + '添加</button>',
    onMount: function (mroot, close) {
      $('#ia-go', mroot).addEventListener('click', async function () {
        const name = $('#ia-name', mroot).value.trim();
        const cid = $('#ia-cid', mroot).value.trim();
        const key = $('#ia-key', mroot).value.trim();
        if (!cid || !key) { toast('请填写 Client ID 和 API Key', 'warn'); return; }
        const btn = $('#ia-go', mroot);
        btn.disabled = true;
        try {
          const body = { client_id: cid, api_key: key };
          if (name) body.name = name;
          await apiPost('/api/me/ima', body);
          toast('IMA 账号已添加', 'success');
          close();
          await refreshImaAfterChange(root);
        } catch (e) {
          btn.disabled = false;
        }
      });
    },
  });
}

function openImaRenameModal(root, a) {
  showModal({
    title: '重命名账号',
    body: '<div class="field"><label>账号名称</label><input class="input" id="ir-name" value="' + escapeHtml(a.name || '') + '" placeholder="账号名称"></div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="ir-go">保存</button>',
    onMount: function (mroot, close) {
      $('#ir-go', mroot).addEventListener('click', async function () {
        const name = $('#ir-name', mroot).value.trim();
        if (!name) { toast('请输入账号名称', 'warn'); return; }
        try {
          await apiPatch('/api/me/ima/' + encodeURIComponent(String(a.id)), { name: name });
          toast('账号已重命名', 'success');
          close();
          await refreshImaAfterChange(root);
        } catch (e) { /* api 层已提示 */ }
      });
    },
  });
}


function renderMcpCard(root) {
  const box = $('#set-mcp', root);
  const endpoint = location.origin + '/mcp';
  const tokenPlaceholder = '<' + 'your-mcp-token' + '>';
  const snippetObj = {
    mcpServers: {
      imahub: {
        url: endpoint,
        headers: { Authorization: 'Bearer ' + tokenPlaceholder }
      }
    }
  };
  const snippet = JSON.stringify(snippetObj, null, 2);
  box.innerHTML =
    '<div class="card-head">' + icon('code', 18) + '<h3>MCP 访问</h3><span class="sub">把 ImaHub 接入 AI 客户端（Claude / Cursor 等）</span></div>' +
    '<div class="card-body">' +
    '  <div class="field"><label>MCP 端点 URL</label>' +
    '    <div class="mcp-endpoint"><span class="mono">' + escapeHtml(endpoint) + '</span>' +
    '      <button class="btn icon sm" id="mcp-copy-url" title="复制">' + icon('copy', 14) + '</button></div>' +
    '  </div>' +
    '  <div class="field"><label>我的访问 Token</label>' +
    '    <div class="mcp-endpoint"><span class="mono" id="mcp-token-text" style="flex:1">加载中…</span>' +
    '      <button class="btn icon sm" id="mcp-toggle-token" title="显示 / 隐藏">' + icon('eye', 14) + '</button>' +
    '      <button class="btn icon sm" id="mcp-copy-token" title="复制">' + icon('copy', 14) + '</button>' +
    '      <button class="btn icon sm" id="mcp-rotate" title="重新生成">' + icon('refresh', 14) + '</button>' +
    '    </div>' +
    '    <div class="hint">Token 是你的私密凭据，重新生成后旧 Token 立即失效。</div>' +
    '    <div class="hint">所有 ima_* 工具均支持可选 account 参数（ima 账号 id 或名称）；省略时：搜索知识库 / 笔记本聚合全部账号，上传自动分配账号，其余工具用默认账号。</div>' +
    '  </div>' +
    '  <div class="field"><label>接入示例（MCP 客户端配置片段）</label>' +
    '    <div class="code-block"><button class="btn sm copy-btn" id="mcp-copy-snippet">' + icon('copy', 13) + '复制</button>' +
    '      <pre>' + escapeHtml(snippet) + '</pre></div>' +
    '  </div>' +
    '</div>';

  let token = '';
  let tokenVisible = false;

  function renderToken() {
    const t = $('#mcp-token-text', box);
    if (!t) return;
    if (!token) { t.textContent = '（未获取）'; return; }
    t.textContent = tokenVisible ? token : token.slice(0, 8) + '••••••••••••••••••••••••';
    const eye = $('#mcp-toggle-token', box);
    if (eye) eye.innerHTML = icon(tokenVisible ? 'eyeOff' : 'eye', 14);
  }

  apiGet('/api/me/mcp-token').then(function (data) {
    token = (data && data.token) || '';
    renderToken();
  }).catch(function (e) {
    const t = $('#mcp-token-text', box);
    if (t) t.textContent = '获取失败：' + (e.message || '');
  });

  $('#mcp-copy-url', box).addEventListener('click', function () { copyText(endpoint); });
  $('#mcp-toggle-token', box).addEventListener('click', function () { tokenVisible = !tokenVisible; renderToken(); });
  $('#mcp-copy-token', box).addEventListener('click', function () {
    if (!token) { toast('Token 尚未加载', 'warn'); return; }
    copyText(token);
  });
  $('#mcp-copy-snippet', box).addEventListener('click', function () { copyText(snippet); });
  $('#mcp-rotate', box).addEventListener('click', async function () {
    const ok = await confirmDialog('重新生成 Token 后，使用旧 Token 的 MCP 连接会立即失效。确定继续吗？', { title: '重新生成 Token', danger: true, okText: '重新生成' });
    if (!ok) return;
    try {
      const data = await apiPost('/api/me/mcp-token/rotate', {});
      token = (data && data.token) || '';
      tokenVisible = true;
      renderToken();
      toast('Token 已重新生成', 'success');
    } catch (e) { /* api 层已提示 */ }
  });
}

/* ============ 管理后台 ============ */
async function renderAdminView(root) {
  root.innerHTML =
    '<div class="admin-page">' +
    '  <div class="card">' +
    '    <div class="card-head">' + icon('users', 18) + '<h3>用户管理</h3><span class="sub" id="admin-user-count"></span>' +
    '      <span style="flex:1"></span>' +
    '      <button class="btn primary sm" id="admin-user-new">' + icon('plus', 14) + '新建用户</button>' +
    '    </div>' +
    '    <div class="table-wrap" id="admin-users"><div class="loading-center"><div class="spinner"></div><div>加载用户列表…</div></div></div>' +
    '  </div>' +
    '  <div class="card">' +
    '    <div class="card-head">' + icon('settings', 18) + '<h3>服务设置</h3></div>' +
    '    <div class="card-body" id="admin-settings"><div class="loading-center"><div class="spinner"></div><div>加载设置…</div></div></div>' +
    '  </div>' +
    '</div>';

  $('#admin-user-new', root).addEventListener('click', openNewUserModal);
  loadAdminUsers();
  loadAdminSettings();
}

async function loadAdminUsers() {
  const box = $('#admin-users');
  if (!box) return;
  try {
    const data = await apiGet('/api/admin/users');
    const users = (data && data.users) || [];
    const cnt = $('#admin-user-count');
    if (cnt) cnt.textContent = '共 ' + users.length + ' 个用户';
    if (!users.length) {
      box.innerHTML = emptyStateHtml('users', '还没有用户', '点击「新建用户」创建第一个账号');
      mountIcons(box);
      return;
    }
    let rows = '';
    users.forEach(function (u, idx) {
      const isMe = state.user && String(u.id) === String(state.user.id);
      rows +=
        '<tr data-uid="' + escapeHtml(String(u.id)) + '">' +
        '<td><div class="cell-name"><div class="avatar" style="width:30px;height:30px;font-size:12px">' + escapeHtml((u.username || '?').charAt(0).toUpperCase()) + '</div>' +
        '<span class="nm">' + escapeHtml(u.username) + '</span>' + (isMe ? '<span class="badge">我</span>' : '') + '</div></td>' +
        '<td>' + (u.role === 'admin' ? '<span class="badge">管理员</span>' : '<span class="badge muted">普通用户</span>') + '</td>' +
        '<td>' + (u.disabled ? '<span class="badge danger">已禁用</span>' : '<span class="badge ok">正常</span>') + '</td>' +
        '<td class="muted" style="white-space:nowrap">' + escapeHtml(formatTime(u.created_at)) + '</td>' +
        '<td><div class="actions">' +
        '<button class="btn icon sm" data-act="pw" title="改密码">' + icon('lock', 14) + '</button>' +
        '<button class="btn icon sm" data-act="role" title="切换角色">' + icon('shield', 14) + '</button>' +
        '<button class="btn icon sm" data-act="toggle" title="' + (u.disabled ? '启用' : '禁用') + '">' + icon(u.disabled ? 'check' : 'x', 14) + '</button>' +
        '<button class="btn icon sm" data-act="del" title="删除" style="color:var(--danger)">' + icon('trash', 14) + '</button>' +
        '</div></td></tr>';
    });
    box.innerHTML = '<table class="tbl"><thead><tr><th>用户名</th><th style="width:110px">角色</th><th style="width:100px">状态</th><th style="width:150px">创建时间</th><th style="width:170px;text-align:right">操作</th></tr></thead><tbody>' + rows + '</tbody></table>';

    $$('tbody tr', box).forEach(function (tr) {
      const uid = tr.getAttribute('data-uid');
      const u = users.filter(function (x) { return String(x.id) === uid; })[0];
      $$('[data-act]', tr).forEach(function (b) {
        b.addEventListener('click', function () {
          const act = b.getAttribute('data-act');
          if (act === 'pw') adminResetPassword(u);
          else if (act === 'role') adminToggleRole(u);
          else if (act === 'toggle') adminToggleDisabled(u);
          else if (act === 'del') adminDeleteUser(u);
        });
      });
    });
  } catch (e) {
    box.innerHTML = emptyStateHtml('alert', '用户列表加载失败', e.message || '请稍后重试');
    mountIcons(box);
  }
}

function openNewUserModal() {
  showModal({
    title: '新建用户',
    body:
      '<div class="field"><label>用户名</label><input class="input" id="nu-name" placeholder="登录用户名" autocomplete="off"></div>' +
      '<div class="field"><label>初始密码</label><input class="input" type="password" id="nu-pass" placeholder="至少 6 位" autocomplete="new-password"></div>' +
      '<div class="field"><label>角色</label><select class="select" id="nu-role"><option value="user">普通用户</option><option value="admin">管理员</option></select></div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="nu-go">' + icon('plus', 15) + '创建</button>',
    onMount: function (root, close) {
      $('#nu-go', root).addEventListener('click', async function () {
        const name = $('#nu-name', root).value.trim();
        const pass = $('#nu-pass', root).value;
        const role = $('#nu-role', root).value;
        if (!name) { toast('请输入用户名', 'warn'); return; }
        if (pass.length < 6) { toast('密码至少 6 位', 'warn'); return; }
        const btn = $('#nu-go', root);
        btn.disabled = true;
        try {
          await apiPost('/api/admin/users', { username: name, password: pass, role: role });
          toast('用户创建成功', 'success');
          close();
          loadAdminUsers();
        } catch (e) { btn.disabled = false; }
      });
    },
  });
}

function adminResetPassword(u) {
  showModal({
    title: '重置密码 · ' + u.username,
    body: '<div class="field"><label>新密码</label><input class="input" type="password" id="rp-pass" placeholder="至少 6 位" autocomplete="new-password"></div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="rp-go">重置</button>',
    onMount: function (root, close) {
      $('#rp-go', root).addEventListener('click', async function () {
        const pass = $('#rp-pass', root).value;
        if (pass.length < 6) { toast('密码至少 6 位', 'warn'); return; }
        try {
          await apiPatch('/api/admin/users/' + encodeURIComponent(u.id), { password: pass });
          toast('已重置 ' + u.username + ' 的密码', 'success');
          close();
        } catch (e) { /* api 层已提示 */ }
      });
    },
  });
}

async function adminToggleRole(u) {
  const toAdmin = u.role !== 'admin';
  const ok = await confirmDialog(
    '确定把用户 <strong>' + escapeHtml(u.username) + '</strong> ' + (toAdmin ? '提升为管理员' : '降级为普通用户') + ' 吗？',
    { title: '切换角色', okText: toAdmin ? '提升为管理员' : '降级为普通用户' }
  );
  if (!ok) return;
  try {
    await apiPatch('/api/admin/users/' + encodeURIComponent(u.id), { role: toAdmin ? 'admin' : 'user' });
    toast('角色已更新', 'success');
    loadAdminUsers();
  } catch (e) { /* api 层已提示 */ }
}

async function adminToggleDisabled(u) {
  const disable = !u.disabled;
  const ok = await confirmDialog(
    '确定' + (disable ? '禁用' : '启用') + '用户 <strong>' + escapeHtml(u.username) + '</strong> 吗？' + (disable ? '禁用后该账号将无法登录。' : ''),
    { title: disable ? '禁用用户' : '启用用户', danger: disable, okText: disable ? '禁用' : '启用' }
  );
  if (!ok) return;
  try {
    await apiPatch('/api/admin/users/' + encodeURIComponent(u.id), { disabled: disable });
    toast('已' + (disable ? '禁用' : '启用') + ' ' + u.username, 'success');
    loadAdminUsers();
  } catch (e) { /* api 层已提示 */ }
}

async function adminDeleteUser(u) {
  const ok = await confirmDialog(
    '确定删除用户 <strong>' + escapeHtml(u.username) + '</strong> 吗？此操作不可恢复。',
    { title: '删除用户', danger: true, okText: '删除' }
  );
  if (!ok) return;
  try {
    await apiDel('/api/admin/users/' + encodeURIComponent(u.id));
    toast('已删除 ' + u.username, 'success');
    loadAdminUsers();
  } catch (e) { /* api 层已提示 */ }
}

async function loadAdminSettings() {
  const box = $('#admin-settings');
  if (!box) return;
  try {
    const data = await apiGet('/api/admin/settings');
    renderAdminSettingsForm(box, data || {});
  } catch (e) {
    box.innerHTML = emptyStateHtml('alert', '服务设置加载失败', e.message || '请稍后重试');
    mountIcons(box);
  }
}

function renderAdminSettingsForm(box, s) {
  box.innerHTML =
    '<div class="field"><label>站点名称</label><input class="input" id="as-site" value="' + escapeHtml(s.site_name || '') + '" placeholder="ImaHub"></div>' +
    '<div class="field">' +
    '  <div class="setting-row"><div class="lbl">开放注册</div><div class="val" style="display:flex;align-items:center;gap:10px">' +
    '    <label class="switch"><input type="checkbox" id="as-register"' + (s.allow_register ? ' checked' : '') + '><span class="track"></span></label>' +
    '    <span class="muted" style="font-size:12.5px">开启后任何人可自助注册账号</span></div></div>' +
    '</div>' +
    '<div class="field"><label>默认 IMA Client ID</label><input class="input mono" id="as-cid" value="' + escapeHtml(s.default_ima_client_id || '') + '" placeholder="未设置"></div>' +
    '<div class="field"><label>默认 IMA API Key</label>' +
    '  <input class="input mono" type="password" id="as-key" placeholder="' + (s.default_ima_api_key_set ? '已设置（留空表示不修改）' : '未设置') + '" autocomplete="off">' +
    '  <div class="hint">' + (s.default_ima_api_key_set ? '已设置默认 API Key。输入新值可覆盖，留空保持不变。' : '用户未绑定个人凭证时使用该默认凭证。') + '</div>' +
    '</div>' +
    '<button class="btn primary" id="as-save">' + icon('save', 15) + '保存服务设置</button>';

  $('#as-save', box).addEventListener('click', async function () {
    const body = {
      site_name: $('#as-site', box).value.trim(),
      allow_register: $('#as-register', box).checked,
      default_ima_client_id: $('#as-cid', box).value.trim(),
      default_ima_api_key: $('#as-key', box).value.trim(), // 空字符串（含纯空白）= 不改
    };
    const btn = $('#as-save', box);
    btn.disabled = true;
    try {
      await apiPut('/api/admin/settings', body);
      toast('服务设置已保存', 'success');
      $('#as-key', box).value = '';
      await refreshServerInfo();
      applyBranding();
      loadAdminSettings();
    } catch (e) { /* api 层已提示 */ }
    btn.disabled = false;
  });
}
