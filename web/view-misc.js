/* ImaHub — view-misc.js：设置页 + 管理后台 */
'use strict';

/* ============ 设置页 ============ */
async function renderSettingsView(root) {
  root.innerHTML =
    '<div class="settings-page">' +
    '  <div class="card" id="set-account"></div>' +
    '  <div class="card" id="set-password"></div>' +
    (isClientMode() ? '  <div class="card" id="set-server"></div>' : '') +
    '  <div class="card" id="set-myima"></div>' +
    '  <div class="card" id="set-ima"></div>' +
    '  <div class="card" id="set-ask"></div>' +
    '  <div class="card" id="set-mcp"></div>' +
    '  <div class="card" id="set-queue"></div>' +
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

  renderImaCard(root);
  renderMyImaCard(root);
  if (isClientMode()) renderServerCard(root);
  renderAskCard(root);
  renderMcpCard(root);
  renderQueueCard(root);
}

/* ============ 上传队列（离线排队，恢复后自动续传） ============ */
async function renderQueueCard(root) {
  const box = $('#set-queue', root);
  if (!box) return;
  box.innerHTML =
    '<div class="card-head">' + icon('upload', 18) + '<h3>上传队列</h3><span class="sub">离线排队，恢复自动传</span></div>' +
    '<div class="card-body"><div class="loading-center"><div class="spinner"></div></div></div>';
  let data = {};
  try {
    data = await apiGet('/api/queue', { silent: true });
  } catch (e) {
    $('.card-body', box).innerHTML = '<div class="hint">队列加载失败</div>';
    return;
  }
  const items = data.items || [];
  const body = $('.card-body', box);
  if (!items.length) {
    body.innerHTML = '<div class="hint">队列为空</div>';
    return;
  }
  body.innerHTML =
    '<div class="queue-list">' +
    items.map(function (it) {
      const st = it.status === 'failed'
        ? '<span class="badge danger">失败</span>'
        : '<span class="badge warn">待传</span>';
      return '<div class="queue-item" data-id="' + escapeHtml(it.id) + '">' +
        '<div class="queue-item-main"><div class="queue-item-name">' + escapeHtml(it.name) + '</div>' +
        '<div class="muted">' + (it.size ? humanSize(it.size) + ' · ' : '') +
        (it.error ? escapeHtml(it.error) : '等待续传') + '</div></div>' +
        st +
        '<button class="btn sm" data-retry>重试</button>' +
        '<button class="btn sm danger" data-del>删除</button>' +
        '</div>';
    }).join('') +
    '</div>' +
    '<div style="margin-top:10px"><button class="btn primary sm" id="queue-drain">' + icon('refresh', 14) + '全部续传</button></div>';
  $$('.queue-item', box).forEach(function (el) {
    const id = el.getAttribute('data-id');
    const retry = $('[data-retry]', el);
    const del = $('[data-del]', el);
    retry.addEventListener('click', async function () {
      retry.disabled = true;
      try {
        await apiPost('/api/queue/' + encodeURIComponent(id) + '/retry', {});
        await renderQueueCard(root);
      } catch (e) { retry.disabled = false; }
    });
    del.addEventListener('click', async function () {
      try {
        await apiDel('/api/queue/' + encodeURIComponent(id));
        await renderQueueCard(root);
      } catch (e) { /* api 已提示 */ }
    });
  });
  const drain = $('#queue-drain', box);
  drain.addEventListener('click', async function () {
    drain.disabled = true;
    try {
      const r = await apiPost('/api/queue/drain', {});
      toast('续传 ' + (r.sent || 0) + ' 个', 'success');
      await renderQueueCard(root);
    } catch (e) { drain.disabled = false; }
  });
}

/* ============ 服务端（客户端模式） ============ */
async function renderServerCard(root) {
  const box = $('#set-server', root);
  if (!box) return;
  box.innerHTML =
    '<div class="card-head">' + icon('wifi', 18) + '<h3>服务端</h3><span class="sub">客户端模式</span></div>' +
    '<div class="card-body"><div class="loading-center"><div class="spinner"></div><div>加载状态…</div></div></div>';

  let st = { connected: false, server: '', last_sync: 0 };
  try {
    st = (await apiGet('/api/client/status', { silent: true })) || st;
  } catch (e) { /* 离线也照常展示 */ }
  const body = $('.card-body', box);

  function draw() {
    body.innerHTML =
      '<dl class="dl">' +
      '<dt>地址</dt><dd class="mono">' + escapeHtml(st.server || '—') + '</dd>' +
      '<dt>状态</dt><dd>' + (st.connected ? '<span class="badge ok">已连接</span>' : '<span class="badge warn">离线</span>') + '</dd>' +
      '<dt>上次同步</dt><dd>' + escapeHtml(formatTime(st.last_sync)) + '</dd>' +
      '</dl>' +
      '<div style="display:flex;gap:10px;margin-top:12px">' +
      '<button class="btn primary" id="cli-sync">' + icon('refresh', 15) + '立即同步</button>' +
      '<button class="btn" id="cli-dis">' + icon('logout', 15) + '断开</button>' +
      '</div>';
    $('#cli-sync', body).addEventListener('click', async function () {
      const b = $('#cli-sync', body);
      b.disabled = true;
      try {
        const r = await apiPost('/api/client/sync', {});
        if (r) st = r;
        toast('同步完成', 'success');
        draw();
        if (typeof updateConnBadge === 'function') updateConnBadge();
      } catch (e) { /* api 层已提示 */ }
      b.disabled = false;
    });
    $('#cli-dis', body).addEventListener('click', function () {
      const lb = $('#logout-btn'); // 走现有登出流程
      if (lb) lb.click();
    });
  }
  draw();
}

/* ============ 知识库问答模型（内置 agent 查询合成） ============ */
async function renderAskCard(root) {
  const box = $('#set-ask', root);
  if (!box) return;
  const isAdmin = (typeof state !== 'undefined' && state.user && state.user.role === 'admin') && !isClientMode();
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
      ? '<div class="hint">问答模型已配置（' + escapeHtml(status.model || '') + '）</div>'
      : '<div class="hint">未配置问答模型，提问仅返回检索片段</div>';
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
    '<div class="hint" style="margin:0 0 10px">问答自动检索知识库合成回答；API Key 留空不改</div>' +
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

/* ============ 我的 ima（1 人 1 号，成员自助） ============ */
async function renderMyImaCard(root) {
  const box = $('#set-myima', root);
  if (!box) return;
  box.innerHTML =
    '<div class="card-head">' + icon('key', 18) + '<h3>我的 ima</h3><span class="sub">1 人 1 号，重复保存即更新</span></div>' +
    '<div class="card-body"><div class="loading-center"><div class="spinner"></div><div>加载…</div></div></div>';

  // 我的号（me.ima.accounts 的 own 项）+ 完整凭证（ima-keys 全员可取）
  let myAcc = null;
  let myKey = null;
  try {
    const me2 = await apiGet('/api/me', { silent: true });
    const accs = (me2 && me2.ima && me2.ima.accounts) || [];
    myAcc = accs.filter(function (a) { return a.own; })[0] || null;
  } catch (e) { /* 离线：表单留空 */ }
  try {
    const kd = await apiGet('/api/me/ima-keys', { silent: true });
    const keys = (kd && kd.keys) || [];
    const uid = state.user ? String(state.user.id) : '';
    myKey = keys.filter(function (k) { return String(k.owner_id) === uid; })[0] || null;
  } catch (e) { /* 同上 */ }
  if (!myAcc && myKey) myAcc = { id: myKey.id, name: myKey.name, upload_kb_id: myKey.upload_kb_id };

  const body = $('.card-body', box);
  const kbFieldHtml = myAcc
    ? '<select class="select" id="mia-kb"><option value="">未设置</option></select>'
    : '<input class="input mono" id="mia-kb" placeholder="知识库 ID">';
  body.innerHTML =
    '<div class="form-row">' +
    '  <div class="field"><label>名称</label><input class="input" id="mia-name" placeholder="我的 ima"></div>' +
    '  <div class="field"><label>Client ID</label><input class="input mono" id="mia-cid" autocomplete="off"></div>' +
    '  <div class="field"><label>API Key</label><input class="input mono" type="password" id="mia-key" autocomplete="off"></div>' +
    '</div>' +
    '<div class="form-row"><div class="field"><label>默认上传知识库</label>' + kbFieldHtml + '</div></div>' +
    '<button class="btn primary" id="mia-save">' + icon('save', 15) + '保存</button>' +
    (myAcc ? '<button class="btn" id="mia-unbind" style="margin-left:10px">' + icon('trash', 15) + '解绑</button>' : '');

  $('#mia-name', body).value = (myAcc && myAcc.name) || '';
  $('#mia-cid', body).value = (myKey && myKey.client_id) || '';
  $('#mia-key', body).value = (myKey && myKey.api_key) || '';

  // 默认上传知识库：有号时列出自己账号下的知识库
  const kbSel = $('select#mia-kb', body);
  if (kbSel && myAcc && myAcc.id) {
    apiGet(qs('/api/ima/kbs', { ia: String(myAcc.id), limit: 50 })).then(function (data) {
      let opts = '<option value="">未设置</option>';
      pickList(data).forEach(function (kb) {
        opts += '<option value="' + escapeHtml(String(kb.id)) + '"' +
          (String(myAcc.upload_kb_id || '') === String(kb.id) ? ' selected' : '') + '>' +
          escapeHtml(kb.name || kb.id) + '</option>';
      });
      kbSel.innerHTML = opts;
    }).catch(function () {
      kbSel.innerHTML = '<option value="">加载失败</option>';
    });
  } else if (!kbSel) {
    $('#mia-kb', body).value = (myAcc && myAcc.upload_kb_id) || '';
  }

  $('#mia-save', body).addEventListener('click', async function () {
    const name = $('#mia-name', body).value.trim();
    const cid = $('#mia-cid', body).value.trim();
    const key = $('#mia-key', body).value.trim();
    const kb = $('#mia-kb', body).value.trim();
    if (!cid || !key) { toast('请填写 Client ID 与 API Key', 'warn'); return; }
    const btn = $('#mia-save', body);
    btn.disabled = true;
    try {
      const payload = { name: name, client_id: cid, api_key: key };
      if (kb) payload.upload_kb_id = kb;
      await apiPost('/api/me/ima', payload);
      toast('已保存', 'success');
      await refreshImaAfterChange(root);
    } catch (e) { btn.disabled = false; }
  });

  const unBtn = $('#mia-unbind', body);
  if (unBtn) unBtn.addEventListener('click', async function () {
    const ok = await confirmDialog('确定解绑我的 ima 账号？', { title: '解绑账号', danger: true, okText: '解绑' });
    if (!ok) return;
    try {
      const id = (myAcc && myAcc.id) || (myKey && myKey.id);
      await apiDel(id ? '/api/me/ima?id=' + encodeURIComponent(String(id)) : '/api/me/ima');
      toast('已解绑', 'success');
      await refreshImaAfterChange(root);
    } catch (e) { /* api 层已提示 */ }
  });
}

/* ============ 团队账号（只读归属列表） ============ */
async function renderImaCard(root) {
  const box = $('#set-ima', root);
  if (!box) return;
  box.innerHTML =
    '<div class="card-head">' + icon('database', 18) + '<h3>团队账号</h3><span class="sub" id="ima-count"></span></div>' +
    '<div class="card-body"><div class="loading-center"><div class="spinner"></div><div>加载账号…</div></div></div>';

  // 归属清单：GET /api/me 的 ima.accounts（owner / own / source）
  let accounts = (state.me && state.me.ima && state.me.ima.accounts) || [];
  if (!accounts.length) {
    try {
      const data = await apiGet('/api/ima/accounts', { silent: true });
      accounts = (data && data.accounts) || [];
    } catch (e) { /* 空列表 */ }
  }
  const cnt = $('#ima-count', box);
  if (cnt) cnt.textContent = '共 ' + accounts.length + ' 个';
  const body = $('.card-body', box);

  if (!accounts.length) {
    body.innerHTML = emptyStateHtml('key', '暂无团队账号', '在「我的 ima」绑定后显示在这里');
    mountIcons(body);
    return;
  }

  let html = '<div class="acct-list">';
  accounts.forEach(function (a, idx) {
    const label = a.own ? '我的' : (a.source === 'server_default' ? '服务端默认' : (a.owner || '团队'));
    html +=
      '<div class="acct-item" data-idx="' + idx + '">' +
      '  <div class="acct-main">' +
      '    <div class="kb-cover">' + escapeHtml((a.name || a.client_id_preview || '?').charAt(0)) + '</div>' +
      '    <div class="acct-info">' +
      '      <div class="acct-name">' + escapeHtml(a.name || '(未命名账号)') +
      ' <span class="badge' + (a.own ? '' : ' muted') + '">' + escapeHtml(label) + '</span>' +
      (a.bound === false ? ' <span class="badge warn">未绑定</span>' : '') +
      '      </div>' +
      '      <div class="acct-sub mono">' + escapeHtml(a.client_id_preview || String(a.id)) + '</div>' +
      '    </div>' +
      '    <div class="acct-actions">' +
      '      <button class="btn sm" data-act="test">' + icon('wifi', 14) + '测试连接</button>' +
      '    </div>' +
      '  </div>' +
      '  <div class="acct-test hidden"></div>' +
      '</div>';
  });
  html += '</div>' +
    '<div class="hint">知识库 / 笔记本为全部账号合并视图，全团队数据互通。</div>';
  body.innerHTML = html;

  $$('.acct-item', body).forEach(function (item) {
    const a = accounts[Number(item.getAttribute('data-idx'))];
    const testBox = $('.acct-test', item);
    $('[data-act]', item).addEventListener('click', async function (ev) {
      const b = ev.currentTarget;
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
    });
  });
}

/** 账号增删改后：刷新账号缓存与设置页 */
async function refreshImaAfterChange(root) {
  await refreshMe({ silent: true });
  await loadIaAccounts();
  renderSettingsView(root);
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
    '    <div class="hint">重新生成后旧 Token 失效</div>' +
    '    <div class="hint">account 参数可选：省略时搜索聚合全部账号，上传自动分配</div>' +
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
    const ok = await confirmDialog('旧 Token 将立即失效，确定重新生成？', { title: '重新生成 Token', danger: true, okText: '重新生成' });
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
    '确定' + (disable ? '禁用' : '启用') + '用户 <strong>' + escapeHtml(u.username) + '</strong> 吗？',
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
    '    <span class="muted" style="font-size:12.5px">开启后可自助注册</span></div></div>' +
    '</div>' +
    '<div class="field"><label>默认 IMA Client ID</label><input class="input mono" id="as-cid" value="' + escapeHtml(s.default_ima_client_id || '') + '" placeholder="未设置"></div>' +
    '<div class="field"><label>默认 IMA API Key</label>' +
    '  <input class="input mono" type="password" id="as-key" placeholder="' + (s.default_ima_api_key_set ? '已设置，留空不改' : '未设置') + '" autocomplete="off">' +
    '  <div class="hint">' + (s.default_ima_api_key_set ? '留空保持不变' : '未绑定凭证时使用') + '</div>' +
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
