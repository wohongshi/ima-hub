/* ImaHub — view-files.js：文件 / 知识库页 */
'use strict';

const filesState = {
  kbs: [], kbCursor: null, kbEnd: true, kbQuery: '', kbLoading: false, kbErrors: [],
  kb: null,                 // 当前知识库 {id,name,ia,account_name,...}
  kbIa: '',                 // 当前知识库所属账号（单点请求回传）
  folder: null,             // 当前文件夹 id（根 = kb.id）
  path: [],                 // 当前面包屑（current_path）
  items: [], cursor: null, isEnd: true, loading: false,
  selected: {},            // 多选：media_id -> {media_id,title}
  searchMode: false, searchQuery: '',
  searchItems: [], searchCursor: null, searchEnd: true, searchLoading: false,
  listPending: false, kbPending: false, // 在途请求期间的新导航/搜索，完成后补跑
  pendingImport: false,                 // 首页「导入链接」跳转后待弹导入框
};

function renderFilesView(root) {
  root.innerHTML =
    '<div class="files-page">' +
    '  <aside class="kb-panel">' +
    '    <div class="kb-panel-head"><h3>知识库</h3>' +
    '      <button class="btn icon sm ghost" id="kb-refresh" title="刷新">' + icon('refresh', 15) + '</button>' +
    '      <button class="btn icon sm ghost" id="kb-more" title="加载更多知识库">' + icon('chevronDown', 15) + '</button>' +
    '    </div>' +
    '    <div class="search-box kb-search">' + icon('search', 15) + '<input id="kb-q" placeholder="搜索知识库"></div>' +
    '    <div class="kb-list" id="kb-list"></div>' +
    '  </aside>' +
    '  <section class="files-main" id="files-main">' +
    '    <div class="files-toolbar">' +
    '      <nav class="breadcrumb" id="crumbs"></nav>' +
    '      <div class="toolbar-actions">' +
    '        <div class="search-box" id="kb-search-box">' + icon('search', 15) +
    '          <input id="file-q" placeholder="在知识库内搜索">' +
    '        </div>' +
    '<button class="btn" id="btn-zip">' + icon('archive', 15) + '打包下载</button>' +
    '<button class="btn" id="btn-import">' + icon('link', 15) + '导入链接</button>' +
    '        <button class="btn primary" id="btn-upload">' + icon('upload', 15) + '上传文件</button>' +
    '      </div>' +
    '    </div>' +
    '    <div class="files-body" id="files-body"></div>' +
    '    <div class="drop-overlay" id="drop-overlay">' + icon('upload', 40) + '<div>释放鼠标，上传到当前目录</div></div>' +
    '  </section>' +
    '</div>';

  bindFilesEvents(root);
  renderKbList();
  renderFilesBody();
  if (!filesState.kbs.length && !filesState.kbLoading) loadKbs(true);
  // 首页「导入链接」挂起请求：知识库已就绪则在此弹出，否则等 selectKb
  if (filesState.pendingImport && filesState.kb) {
    filesState.pendingImport = false;
    openImportModal();
  }
}

/* ============ 知识库列表 ============ */
async function loadKbs(reset) {
  if (filesState.kbLoading) {
    if (reset) filesState.kbPending = true; // 在途期间的新查询/刷新置位，完成后补跑
    return;
  }
  filesState.kbLoading = true;
  if (reset) { filesState.kbs = []; filesState.kbCursor = null; filesState.kbEnd = true; }
  renderKbList();
  try {
    const data = await apiGet(qs('/api/ima/kbs', {
      q: filesState.kbQuery, cursor: reset ? '' : filesState.kbCursor, limit: 20,
    }), { silent: true });
    const page = pickPage(data);
    filesState.kbs = filesState.kbs.concat(page.items);
    filesState.kbEnd = page.isEnd;
    filesState.kbCursor = page.nextCursor;
    if (reset) filesState.kbErrors = (data && data.errors) || [];
  } catch (e) {
    if (e.unauthorized) {
      filesState.kbPending = false; // 跳登录后不再补跑
      return;
    }
    // 静默失败，列表显示重试
  } finally {
    filesState.kbLoading = false;
    renderKbList();
    if (filesState.kbPending) {
      filesState.kbPending = false;
      loadKbs(true); // 补跑在途期间被吞掉的最新查询/刷新
    }
  }
}

function renderKbList() {
  const box = $('#kb-list');
  if (!box) return;
  if (filesState.kbLoading && !filesState.kbs.length) {
    box.innerHTML = '<div style="padding:8px">' +
      '<div class="skeleton" style="height:44px;border-radius:9px;margin-bottom:8px"></div>' +
      '<div class="skeleton" style="height:44px;border-radius:9px;margin-bottom:8px"></div>' +
      '<div class="skeleton" style="height:44px;border-radius:9px"></div></div>';
    return;
  }
  let errHtml = '';
  (filesState.kbErrors || []).forEach(function (e) {
    errHtml += '<div class="kb-error-row" title="' + escapeHtml(e.error || '') + '">' +
      icon('alert', 13) + '<span>' + escapeHtml(e.account_name || e.ia || '账号') + ' 加载失败</span></div>';
  });
  if (!filesState.kbs.length) {
    box.innerHTML = errHtml + emptyStateHtml('database', '暂无知识库', filesState.kbQuery ? '没有匹配的知识库' : '在 ima 中创建知识库后这里会显示');
    mountIcons(box);
    return;
  }
  let html = errHtml;
  filesState.kbs.forEach(function (kb, idx) {
    const active = filesState.kb && String(filesState.kb.id) === String(kb.id);
    const cover = kb.cover_url
      ? '<div class="kb-cover"><img src="' + escapeHtml(kb.cover_url) + '" alt="" data-fallback="' + escapeHtml((kb.name || '?').charAt(0)) + '"></div>'
      : '<div class="kb-cover">' + escapeHtml((kb.name || '?').charAt(0)) + '</div>';
    html += '<button class="kb-item' + (active ? ' active' : '') + '" data-kb-idx="' + idx + '">' + cover +
      '<div class="kb-name" title="' + escapeHtml(kb.name || '') + '">' + escapeHtml(kb.name || '(未命名知识库)') + '</div>' +
      (kb.account_name ? '<span class="chip" title="所属账号">' + escapeHtml(kb.account_name) + '</span>' : '') +
      (active ? icon('chevronRight', 14) : '') + '</button>';
  });
  if (!filesState.kbEnd) {
    html += '<div style="padding:8px 4px"><button class="btn sm" id="kb-load-more" style="width:100%">加载更多</button></div>';
  }
  box.innerHTML = html;
  $$('.kb-cover img', box).forEach(function (img) {
    img.addEventListener('error', function () {
      img.parentNode.textContent = img.getAttribute('data-fallback') || '?';
    });
  });
  $$('.kb-item', box).forEach(function (b) {
    b.addEventListener('click', function () {
      const kb = filesState.kbs[Number(b.getAttribute('data-kb-idx'))];
      selectKb(kb);
    });
  });
  const more = $('#kb-load-more', box);
  if (more) more.addEventListener('click', function () { loadKbs(false); });
}

function selectKb(kb) {
  if (!kb) return;
  filesState.kb = kb;
  filesState.kbIa = String(kb.ia || '');
  filesState.folder = String(kb.id);
  filesState.path = [];
  filesState.selected = {}; // 切库即清空多选，避免批量条带上旧库 media_id
  filesState.searchMode = false;
  filesState.searchQuery = '';
  const q = $('#file-q');
  if (q) q.value = '';
  loadList(true);
  renderKbList();
  // 首页「导入链接」挂起请求：知识库刚就绪，弹出导入框
  if (filesState.pendingImport) {
    filesState.pendingImport = false;
    openImportModal();
  }
}

/* ============ 目录列表 ============ */
async function loadList(reset) {
  if (!filesState.kb) {
    renderFilesBody();
    return;
  }
  if (filesState.loading) {
    if (reset) filesState.listPending = true; // 在途期间的目录切换/刷新置位，完成后补跑
    return;
  }
  filesState.loading = true;
  if (reset) { filesState.items = []; filesState.cursor = null; filesState.isEnd = true; }
  renderFilesBody();
  // 请求快照：响应返回时校验，期间切过目录/知识库就丢弃过期结果
  const reqKb = filesState.kb, reqFolder = filesState.folder;
  try {
    const data = await apiGet(qs('/api/ima/list', {
      kb: filesState.kb.id, folder: filesState.folder || filesState.kb.id,
      ia: filesState.kbIa,
      cursor: reset ? '' : filesState.cursor, limit: 50,
    }));
    if (reqKb === filesState.kb && reqFolder === filesState.folder) {
      const page = pickPage(data);
      filesState.items = filesState.items.concat(page.items);
      filesState.isEnd = page.isEnd;
      filesState.cursor = page.nextCursor;
      if (reset && data && Array.isArray(data.current_path)) filesState.path = data.current_path;
    }
  } catch (e) { /* toast 由 api 层处理 */ }
  filesState.loading = false;
  renderCrumbs();
  renderFilesBody();
  if (filesState.listPending) {
    filesState.listPending = false;
    loadList(true); // 补跑在途期间被丢弃的目录切换/刷新
  }
}

function pathEntryId(f) {
  return f.id !== undefined ? f.id : (f.folder_id !== undefined ? f.folder_id : (f.media_id !== undefined ? f.media_id : ''));
}
function pathEntryName(f) {
  return f.name || f.title || f.folder_name || '文件夹';
}

function renderCrumbs() {
  const box = $('#crumbs');
  if (!box) return;
  if (!filesState.kb) { box.innerHTML = '<span class="crumb current">未选择知识库</span>'; return; }
  const sep = '<span class="crumb-sep">' + icon('chevronRight', 13) + '</span>';
  let html = '<button class="crumb" data-crumb="root">' + escapeHtml(filesState.kb.name || '知识库') + '</button>';
  (filesState.path || []).forEach(function (f, i) {
    const id = String(pathEntryId(f));
    if (id && id === String(filesState.kb.id)) return; // 跳过根
    html += sep + '<button class="crumb" data-crumb="' + i + '">' + escapeHtml(pathEntryName(f)) + '</button>';
  });
  box.innerHTML = html;
  $$('.crumb', box).forEach(function (b, idx, all) {
    if (idx === all.length - 1) { b.classList.add('current'); return; }
    b.addEventListener('click', function () {
      const key = b.getAttribute('data-crumb');
      if (key === 'root') { filesState.folder = String(filesState.kb.id); filesState.path = []; loadList(true); return; }
      const f = filesState.path[Number(key)];
      if (!f) return;
      filesState.folder = String(pathEntryId(f));
      filesState.path = filesState.path.slice(0, Number(key) + 1);
      loadList(true);
    });
  });
}

function renderFilesBody() {
  const body = $('#files-body');
  if (!body) return;
  if (filesState.searchMode) { renderSearchResults(); return; }

  if (!filesState.kb) {
    body.innerHTML = emptyStateHtml('database', '选择一个知识库', '从左侧选择要浏览的 ima 知识库，或在顶部搜索知识库');
    mountIcons(body);
    renderCrumbs();
    return;
  }
  if (filesState.loading && !filesState.items.length) {
    body.innerHTML = '<div class="card">' + skeletonRows(6) + '</div>';
    return;
  }
  if (!filesState.items.length) {
    body.innerHTML = emptyStateHtml('folderPlus', '这个文件夹是空的',
      '拖拽文件到此处上传，或点击右上角「上传文件」',
      '<button class="btn primary" id="empty-upload">' + icon('upload', 15) + '上传文件</button>');
    mountIcons(body);
    const b = $('#empty-upload', body);
    if (b) b.addEventListener('click', openUploadModal);
    return;
  }

  let rows = '';
  filesState.items.forEach(function (it, idx) {
    const isFolder = !!it.is_folder;
    const title = itemTitle(it);
    const v = fileVisual(title, isFolder);
    let count = '—';
    if (isFolder) {
      const fn = it.file_number !== undefined ? it.file_number : '—';
      const dn = it.folder_number !== undefined ? it.folder_number : '—';
      count = fn + ' 文件 / ' + dn + ' 文件夹';
    }
    rows +=
      '<tr data-idx="' + idx + '">' +
      '<td class="sel-cell">' + (isFolder ? '' : '<input type="checkbox" class="sel-box"' + (filesState.selected[itemId(it)] ? ' checked' : '') + '>') + '</td>' +
      '<td><div class="cell-name' + (isFolder ? ' clickable' : '') + '" data-open="1">' + fileIconHtml(it) +
      '<span class="nm" title="' + escapeHtml(title) + '">' + escapeHtml(title) + '</span></div></td>' +
      '<td><span class="badge' + (isFolder ? ' warn' : ' muted') + '">' + escapeHtml(v.t) + '</span></td>' +
      '<td class="muted" style="white-space:nowrap">' + count + '</td>' +
      '<td><div class="actions">' +
      (isFolder
        ? '<button class="btn icon sm" data-act="enter" title="打开">' + icon('chevronRight', 14) + '</button>'
        : '<button class="btn icon sm" data-act="download" title="下载">' + icon('download', 14) + '</button>' +
          '<button class="btn icon sm" data-act="detail" title="详情">' + icon('eye', 14) + '</button>') +
      '</div></td></tr>';
  });

  const selCount = Object.keys(filesState.selected).length;
  const batchBar = selCount
    ? '<div class="batch-bar">' + icon('archive', 14) + '<span>已选 <strong>' + selCount + '</strong> 项</span>' +
      '<span style="flex:1"></span>' +
      '<button class="btn primary sm" id="batch-dl">' + icon('download', 14) + '打包下载（zip）</button>' +
      '<button class="btn sm ghost" id="batch-clear">取消选择</button></div>'
    : '';

  body.innerHTML =
    '<div class="card">' + batchBar + '<div class="table-wrap"><table class="tbl">' +
    '<thead><tr><th style="width:34px"></th><th>名称</th><th style="width:110px">类型</th><th style="width:150px">子项</th><th style="width:110px;text-align:right">操作</th></tr></thead>' +
    '<tbody>' + rows + '</tbody></table></div></div>' +
    (filesState.isEnd ? '' : '<div class="load-more"><button class="btn" id="files-more">加载更多</button></div>');

  const batchDl = $('#batch-dl', body);
  if (batchDl) batchDl.addEventListener('click', function () {
    downloadZip(Object.values(filesState.selected));
  });
  const batchClear = $('#batch-clear', body);
  if (batchClear) batchClear.addEventListener('click', function () {
    filesState.selected = {};
    renderFilesBody();
  });

  $$('tbody tr', body).forEach(function (tr) {
    const idx = Number(tr.getAttribute('data-idx'));
    const it = filesState.items[idx];
    const openEl = $('[data-open]', tr);
    const selBox = $('.sel-box', tr);
    if (selBox) selBox.addEventListener('change', function () {
      const id = itemId(it);
      if (selBox.checked) filesState.selected[id] = { media_id: id, title: itemTitle(it) };
      else delete filesState.selected[id];
      renderFilesBody();
    });
    if (it.is_folder) {
      openEl.addEventListener('click', function () { enterFolder(it); });
    } else {
      openEl.addEventListener('click', function () { openMediaDetail(itemId(it), filesState.kbIa); });
    }
    $$('[data-act]', tr).forEach(function (b) {
      b.addEventListener('click', function (e) {
        e.stopPropagation();
        const act = b.getAttribute('data-act');
        if (act === 'enter') enterFolder(it);
        else if (act === 'download') triggerDownload(itemId(it), filesState.kbIa);
        else if (act === 'detail') openMediaDetail(itemId(it), filesState.kbIa);
      });
    });
  });
  const moreBtn = $('#files-more', body);
  if (moreBtn) moreBtn.addEventListener('click', function () { loadList(false); });
}

function enterFolder(it) {
  const id = String(it.folder_id !== undefined ? it.folder_id : itemId(it));
  if (id === String(filesState.folder)) return; // 双击/重复点击同一层，不重复入栈
  filesState.folder = id;
  const p = filesState.path.slice();
  p.push({ id: filesState.folder, name: itemTitle(it) });
  filesState.path = p;
  loadList(true);
}

/* ============ 知识库内搜索 ============ */
async function runSearch(reset) {
  const q = ($('#file-q') && $('#file-q').value || '').trim();
  if (!q) {
    filesState.searchMode = false;
    renderFilesBody();
    return;
  }
  if (!filesState.kb) { toast('请先选择一个知识库', 'warn'); return; }
  filesState.searchMode = true;
  filesState.searchQuery = q;
  if (reset) { filesState.searchItems = []; filesState.searchCursor = null; filesState.searchEnd = true; }
  filesState.searchLoading = true;
  renderSearchResults();
  try {
    const data = await apiGet(qs('/api/ima/search', {
      kb: filesState.kb.id, q: q, ia: filesState.kbIa,
      cursor: reset ? '' : filesState.searchCursor,
    }));
    const page = pickPage(data);
    filesState.searchItems = filesState.searchItems.concat(page.items);
    filesState.searchEnd = page.isEnd;
    filesState.searchCursor = page.nextCursor;
  } catch (e) { /* api 层已提示 */ }
  filesState.searchLoading = false;
  renderSearchResults();
}

function renderSearchResults() {
  const body = $('#files-body');
  if (!body) return;
  if (filesState.searchLoading && !filesState.searchItems.length) {
    body.innerHTML = '<div class="card">' + skeletonRows(4) + '</div>';
    return;
  }
  const head =
    '<div style="display:flex;align-items:center;gap:10px;margin-bottom:12px">' +
    '<button class="btn sm" id="search-back">' + icon('chevronLeft', 14) + '返回文件列表</button>' +
    '<span class="muted" style="font-size:12.5px">「' + escapeHtml(filesState.searchQuery) + '」的搜索结果 · ' + filesState.searchItems.length + ' 条</span></div>';

  if (!filesState.searchItems.length) {
    body.innerHTML = head + '<div class="card">' + emptyStateHtml('search', '没有找到结果', '换个关键词试试，或检查知识库是否正确') + '</div>';
  } else {
    let rows = '';
    filesState.searchItems.forEach(function (it, idx) {
      const title = itemTitle(it);
      rows +=
        '<tr data-idx="' + idx + '"><td><div class="cell-name clickable" data-open="1">' + fileIconHtml(it) +
        '<span class="nm" title="' + escapeHtml(title) + '">' + escapeHtml(title) + '</span></div></td>' +
        '<td style="width:110px"><span class="badge muted">' + escapeHtml(fileVisual(title, it.is_folder).t) + '</span></td>' +
        '<td style="width:90px;text-align:right"><button class="btn icon sm" data-act="detail" title="详情">' + icon('eye', 14) + '</button></td></tr>';
    });
    body.innerHTML = head +
      '<div class="card"><div class="table-wrap"><table class="tbl">' +
      '<thead><tr><th>名称</th><th>类型</th><th style="text-align:right">操作</th></tr></thead><tbody>' + rows + '</tbody></table></div></div>' +
      (filesState.searchEnd ? '' : '<div class="load-more"><button class="btn" id="search-more">加载更多</button></div>');
    $$('tbody tr', body).forEach(function (tr) {
      const it = filesState.searchItems[Number(tr.getAttribute('data-idx'))];
      $('[data-open]', tr).addEventListener('click', function () { openMediaDetail(itemId(it), filesState.kbIa); });
      const d = $('[data-act="detail"]', tr);
      if (d) d.addEventListener('click', function () { openMediaDetail(itemId(it), filesState.kbIa); });
    });
  }
  mountIcons(body);
  const back = $('#search-back', body);
  if (back) back.addEventListener('click', function () {
    filesState.searchMode = false;
    const q = $('#file-q'); if (q) q.value = '';
    renderFilesBody();
  });
  const more = $('#search-more', body);
  if (more) more.addEventListener('click', function () { runSearch(false); });
}

/* ============ 媒体详情 ============ */
/* ============ 批量打包下载（zip） ============ */
function downloadZip(items) {
  if (!filesState.kb) { toast('请先选择一个知识库', 'warn'); return; }
  const payload = {
    ia: filesState.kbIa,
    kb: String(filesState.kb.id),
    folder: filesState.folder || undefined,
    items: items && items.length ? items : undefined,
  };
  toast(items && items.length ? '正在打包所选文件…' : '正在打包当前文件夹…', 'success');
  fetch('/api/ima/download-zip', {
    method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  }).then(function (r) {
    if (!r.ok) throw new Error('HTTP ' + r.status);
    return r.blob();
  }).then(function (blob) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'imahub-' + filesState.kb.id + '.zip';
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(function () { URL.revokeObjectURL(url); }, 3000);
    toast('打包下载已开始', 'success');
  }).catch(function (e) {
    toast('打包下载失败：' + (e.message || e), 'error');
  });
}

/** 上传网络失败 → 转入离线队列（网络/ima 恢复后自动续传） */
async function queueUploadFallback(file, fields) {
  try {
    await apiUploadTo('/api/queue/upload', file, {
      ia: fields.ia === 'auto' ? '' : fields.ia,
      kbId: fields.kbId,
      folderId: fields.folderId,
    });
    return true;
  } catch (e) {
    return false;
  }
}

function openMediaDetail(mediaId, ia) {
  const m = showModal({
    title: '文件详情',
    body: '<div class="loading-center"><div class="spinner"></div><div>加载中…</div></div>',
    footer: '<button class="btn" data-close>关闭</button><button class="btn primary" data-dl>' + icon('download', 15) + '下载</button>',
    onMount: function (root, close) {
      $('[data-dl]', root).addEventListener('click', function () { triggerDownload(mediaId, ia); });
    },
  });
  apiGet(qs('/api/ima/media', { media_id: mediaId, ia: ia })).then(function (data) {
    const body = $('.modal-body', m.root);
    const typeMap = { 11: '笔记', 1: '文件' };
    const mt = data.media_type;
    let html = '<dl class="dl">';
    html += '<dt>类型</dt><dd><span class="badge">' + escapeHtml(String(mt) + (typeMap[mt] ? ' · ' + typeMap[mt] : '')) + '</span></dd>';
    const ui = data.url_info;
    let url = '';
    if (typeof ui === 'string') url = ui;
    else if (ui && typeof ui === 'object') url = ui.url || '';
    if (url) html += '<dt>链接</dt><dd><a class="md-link mono" href="' + escapeHtml(url) + '" target="_blank" rel="noopener noreferrer" style="font-size:12px">' + escapeHtml(url) + '</a></dd>';
    html += '</dl>';
    // 在线预览：图片 / PDF / 文本类
    const previewKind = ({ 1: 'pdf', 9: 'image', 7: 'text', 13: 'text', 20: 'text' })[mt] || '';
    const src = '/api/ima/download?media_id=' + encodeURIComponent(mediaId) + (ia ? '&ia=' + encodeURIComponent(ia) : '') + '&inline=1';
    if (previewKind === 'image') {
      html += '<div class="preview-box"><img class="preview-img" src="' + src + '" alt="预览"></div>';
    } else if (previewKind === 'pdf') {
      html += '<div class="preview-box"><iframe class="preview-frame" src="' + src + '"></iframe></div>';
    } else if (previewKind === 'text') {
      html += '<div class="preview-box" id="preview-box"><div class="loading-center" style="padding:18px 0"><div class="spinner"></div><div>加载预览…</div></div></div>';
    }
    if (data.note_content) {
      html += '<div style="margin-top:16px"><div style="font-size:12.5px;font-weight:600;color:var(--muted);margin-bottom:8px">笔记内容</div>' +
        '<div class="md-code note-plain" style="max-height:320px;overflow:auto">' + escapeHtml(data.note_content) + '</div></div>';
    }
    body.innerHTML = html;
    if (previewKind === 'text') {
      fetch(src, { credentials: 'same-origin' }).then(function (r) {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.text();
      }).then(function (txt) {
        const box = $('#preview-box', body);
        if (!box) return;
        const clipped = txt.length > 120000 ? txt.slice(0, 120000) + '\n…（预览截断，下载查看全文）' : txt;
        box.innerHTML = mt === 7
          ? '<div class="md-code note-plain" style="max-height:360px;overflow:auto">' + renderMarkdown(clipped) + '</div>'
          : '<pre class="preview-pre">' + escapeHtml(clipped) + '</pre>';
      }).catch(function () {
        const box = $('#preview-box', body);
        if (box) box.innerHTML = '<div class="hint">预览加载失败，可点右下角下载查看</div>';
      });
    }
  }).catch(function () { m.close(); });
}

/* ============ 上传 ============ */
function isDuplicateError(err) {
  return (err && err.code === 409) || (err && /同名|重名|已存在|重复|repeated/i.test(err.message || ''));
}

function openUploadModal(presetFiles) {
  // 知识库选项（聚合列表，带账号标签）；自动分配模式不需要知识库
  const kbOptions = filesState.kbs.map(function (kb) {
    return '<option value="' + escapeHtml(kb.ia || '') + '|' + escapeHtml(kb.id) + '"' +
      (filesState.kb && String(kb.id) === String(filesState.kb.id) ? ' selected' : '') + '>' +
      escapeHtml((kb.name || kb.id) + (kb.account_name ? '（' + kb.account_name + '）' : '')) + '</option>';
  }).join('');
  const curFolder = filesState.folder && filesState.kb && String(filesState.folder) !== String(filesState.kb.id) ? String(filesState.folder) : '';
  const folders = filesState.items.filter(function (it) { return it.is_folder; });
  let folderOptions = '<option value="">根目录</option>';
  if (curFolder) {
    // 当前目录本身不在子目录列表里，必须显式暴露一项，才能命中下方预选
    folderOptions += '<option value="' + escapeHtml(curFolder) + '">当前目录</option>';
  }
  folders.forEach(function (f) {
    const id = f.folder_id !== undefined ? f.folder_id : itemId(f);
    folderOptions += '<option value="' + escapeHtml(id) + '">' + escapeHtml(itemTitle(f)) + '</option>';
  });

  showModal({
    title: '上传文件到知识库',
    wide: true,
    body:
      '<div class="field"><label>上传方式</label>' +
      '  <select class="select" id="up-mode">' +
      '    <option value="auto">自动分配到各账号（扩大容量，推荐）</option>' +
      '    <option value="manual"' + (filesState.kbs.length ? '' : ' disabled') + '>指定知识库</option>' +
      '  </select>' +
      '  <div class="hint" id="up-mode-hint">自动轮询分配账号，存入各账号的「默认上传知识库」</div>' +
      '</div>' +
      '<div id="up-target">' +
      '  <div class="form-row">' +
      '    <div class="field"><label>目标知识库</label><select class="select" id="up-kb">' + kbOptions + '</select></div>' +
      '    <div class="field"><label>目标文件夹</label><select class="select" id="up-folder">' + folderOptions + '</select></div>' +
      '  </div>' +
      '</div>' +
      '<div class="field">' +
      '  <label>选择文件（可多选，支持拖拽到页面任意位置）</label>' +
      '  <input class="input" type="file" id="up-files" multiple style="padding:7px 10px;height:auto">' +
      '  <div class="hint">大小限制：Excel / TXT / Xmind / MD / HTML 10MB；图片 30MB；EPUB 50MB；PDF / Word / PPT / 音频等 200MB。支持 zip / 7z / rar 解压上传；同名文件提示覆盖。</div>' +
      '</div>' +
      '<div class="upl-list" id="up-list"></div>',
    footer:
      '<button class="btn" data-close>取消</button>' +
      '<button class="btn primary" id="up-start">' + icon('upload', 15) + '开始上传</button>',
    onMount: function (root, close) {
      const fileInput = $('#up-files', root);
      const listEl = $('#up-list', root);
      const modeSel = $('#up-mode', root);
      const targetEl = $('#up-target', root);
      const hintEl = $('#up-mode-hint', root);
      let queue = presetFiles ? Array.prototype.slice.call(presetFiles) : [];
      const folderSel = $('#up-folder', root);
      if (curFolder) {
        const has = Array.prototype.some.call(folderSel.options, function (o) { return o.value === curFolder; });
        if (has) folderSel.value = curFolder;
      }

      function syncMode() {
        const manual = modeSel.value === 'manual';
        targetEl.classList.toggle('hidden', !manual);
        hintEl.textContent = manual
          ? '上传到指定知识库（与所属账号），适合精确归档。'
          : '服务端会在各账号间轮询分配，存入每个账号配置的「默认上传知识库」，总容量为所有账号之和。';
      }
      modeSel.addEventListener('change', syncMode);
      syncMode();

      function renderQueue() {
        if (!queue.length) { listEl.innerHTML = ''; return; }
        listEl.innerHTML = queue.map(function (f, i) {
          return '<div class="upl-item" data-i="' + i + '">' +
            '<div class="row1"><span class="fname">' + escapeHtml(f.name) + '</span>' +
            '<span class="muted">' + (f.size ? (f.size / 1024 / 1024).toFixed(2) + ' MB' : '') + '</span>' +
            '<span class="up-status muted">等待上传</span></div>' +
            '<div class="progress"><div class="bar"></div></div></div>';
        }).join('');
      }
      fileInput.addEventListener('change', function () {
        queue = queue.concat(Array.prototype.slice.call(fileInput.files));
        renderQueue();
      });
      renderQueue();

      function uploadFields(force) {
        if (modeSel.value === 'manual') {
          const parts = ($('#up-kb', root).value || '').split('|');
          return { ia: parts[0] || '', kbId: parts[1] || '', folderId: $('#up-folder', root).value, force: force };
        }
        return { ia: 'auto', force: force };
      }

      $('#up-start', root).addEventListener('click', async function () {
        if (!queue.length) { toast('请先选择要上传的文件', 'warn'); return; }
        const btn = $('#up-start', root);
        btn.disabled = true;
        let ok = 0, fail = 0, queued = 0;
        const queuedIdx = [];
        const usedAccounts = [];
        const archiveFailures = [];
        for (let i = 0; i < queue.length; i++) {
          const itemEl = $('.upl-item[data-i="' + i + '"]', listEl);
          const bar = $('.bar', itemEl);
          const status = $('.up-status', itemEl);
          status.textContent = '上传中…';
          status.className = 'up-status';
          const markOk = function (res, label) {
            itemEl.classList.add('done');
            const acct = res && res.account_name ? res.account_name : '';
            if (res && res.archive) {
              const up = (res.uploaded || []).length, fa = (res.failed || []).length;
              status.textContent = '解压 ' + res.extracted + ' 个 → 成功 ' + up +
                (fa ? '，失败 ' + fa : '') + (acct ? '（' + acct + '）' : '');
              status.className = 'up-status badge ' + (fa ? 'warn' : 'ok');
              if (up > 0) ok++; else fail++;
              if (fa) archiveFailures.push({ name: res.file_name, failed: res.failed });
              if (acct && usedAccounts.indexOf(acct) < 0) usedAccounts.push(acct);
              return;
            }
            status.textContent = (label || '完成') + (acct ? '，已存入 ' + acct : '');
            status.className = 'up-status badge ok';
            if (acct && usedAccounts.indexOf(acct) < 0) usedAccounts.push(acct);
            ok++;
          };
          try {
            const res = await apiUpload(queue[i], uploadFields(false), function (r) { bar.style.width = Math.round(r * 100) + '%'; });
            markOk(res, null);
          } catch (e) {
            if (isDuplicateError(e)) {
              status.textContent = '同名文件';
              status.className = 'up-status badge warn';
              const go = await confirmDialog('目标位置已存在同名文件「' + escapeHtml(queue[i].name) + '」，是否覆盖？', { title: '同名文件', okText: '覆盖上传', danger: true });
              if (go) {
                try {
                  const res = await apiUpload(queue[i], uploadFields(true), function (r) { bar.style.width = Math.round(r * 100) + '%'; });
                  markOk(res, '已覆盖');
                  continue;
                } catch (e2) {
                  itemEl.classList.add('fail');
                  status.textContent = '失败：' + e2.message;
                  status.className = 'up-status badge danger';
                  fail++;
                  continue;
                }
              }
            }
            const permanent = isDuplicateError(e) || /不支持的文件类型|超过大小上限|文件名无效/.test(e.message || '');
            if (!permanent) {
              status.textContent = '正在入队…';
              if (await queueUploadFallback(queue[i], uploadFields(false))) {
                status.textContent = '已排队，恢复后自动传';
                status.className = 'up-status badge warn';
                queued++;
                queuedIdx.push(i);
                continue;
              }
            }
            itemEl.classList.add('fail');
            status.textContent = '失败：' + (e.message || '上传失败');
            status.className = 'up-status badge danger';
            fail++;
          }
        }
        // 已入队条目移出本次列表，防「再点一次」重复入队/重复上传
        for (let j = queuedIdx.length - 1; j >= 0; j--) queue.splice(queuedIdx[j], 1);
        if (queuedIdx.length) renderQueue();
        btn.disabled = false;
        toast('上传完成：成功 ' + ok + ' 个' + (fail ? '，失败 ' + fail + ' 个' : '') +
          (queued ? '，排队 ' + queued + ' 个' : '') +
          (usedAccounts.length ? '（存入：' + usedAccounts.join('、') + '）' : ''), (fail || queued) ? 'warn' : 'success');
        if (archiveFailures.length) {
          let rowsHtml = '';
          archiveFailures.forEach(function (af) {
            rowsHtml += '<div style="margin:6px 0"><strong>' + escapeHtml(af.name) + '</strong><ul style="margin:4px 0 0 18px">' +
              af.failed.slice(0, 20).map(function (f) {
                return '<li>' + escapeHtml(f.title) + '：' + escapeHtml(f.error) + '</li>';
              }).join('') + '</ul></div>';
          });
          showModal({
            title: '压缩包内上传失败明细',
            body: '<div style="max-height:50vh;overflow:auto">' + rowsHtml + '</div>',
            footer: '<button class="btn primary" data-close>知道了</button>',
          });
        }
        if (ok) {
          loadList(true);
          close();
        }
      });
    },
  });
}

/* ============ 导入 URL ============ */
function openImportModal() {
  if (!filesState.kb) { toast('请先选择一个知识库', 'warn'); return; }
  showModal({
    title: '导入网页 / 公众号文章链接',
    body:
      '<div class="field">' +
      '  <label>链接（每行一个，1–10 个）</label>' +
      '  <textarea class="textarea" id="imp-urls" placeholder="https://example.com/article&#10;https://mp.weixin.qq.com/s/..."></textarea>' +
      '  <div class="hint">目标位置：' + escapeHtml((filesState.kb.name || '') + ' / ' + (filesState.path.length ? filesState.path.map(pathEntryName).join(' / ') : '根目录')) + '</div>' +
      '</div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="imp-go">' + icon('link', 15) + '开始导入</button>',
    onMount: function (root, close) {
      $('#imp-go', root).addEventListener('click', async function () {
        const raw = $('#imp-urls', root).value || '';
        const urls = raw.split('\n').map(function (s) { return s.trim(); }).filter(Boolean);
        if (!urls.length) { toast('请输入至少一个链接', 'warn'); return; }
        if (urls.length > 10) { toast('一次最多导入 10 个链接', 'warn'); return; }
        const bad = urls.filter(function (u) { return !/^https?:\/\//i.test(u); });
        if (bad.length) { toast('存在不是 http(s) 开头的链接，请检查', 'warn'); return; }
        const btn = $('#imp-go', root);
        btn.disabled = true;
        btn.textContent = '导入中…';
        try {
          const data = await apiPost('/api/ima/import-urls', {
            kb_id: String(filesState.kb.id),
            folder_id: filesState.folder || String(filesState.kb.id),
            ia: filesState.kbIa,
            urls: urls,
          });
          const results = data && data.results;
          // 服务端 results 是 {url: {url, ret_code, media_id}} 形状的对象映射（非数组）
          const list = results && typeof results === 'object' ? Object.values(results) : [];
          const okCount = list.filter(function (r) { return r && r.ret_code === 0; }).length;
          toast('导入完成：提交 ' + urls.length + ' 个链接' + (list.length ? '（成功 ' + okCount + ' 个）' : ''), 'success');
          close();
          loadList(true);
        } catch (e) {
          btn.disabled = false;
          btn.innerHTML = icon('link', 15) + '开始导入';
        }
      });
    },
  });
}

/* ============ 拖拽上传 ============ */
function bindFilesEvents(root) {
  $('#kb-refresh', root).addEventListener('click', function () { loadKbs(true); });
  $('#kb-more', root).addEventListener('click', function () { loadKbs(false); });
  $('#kb-q', root).addEventListener('input', debounce(function (e) {
    filesState.kbQuery = (e.target.value || '').trim();
    loadKbs(true);
  }, 350));
  $('#btn-upload', root).addEventListener('click', function () { openUploadModal(); });
  $('#btn-import', root).addEventListener('click', openImportModal);
  const zipBtn = $('#btn-zip', root);
  if (zipBtn) zipBtn.addEventListener('click', function () {
    if (!filesState.kb) { toast('请先选择一个知识库', 'warn'); return; }
    downloadZip(null); // 整个当前文件夹
  });
  $('#file-q', root).addEventListener('keydown', function (e) {
    if (e.key === 'Enter') runSearch(true);
    if (e.key === 'Escape') {
      e.target.value = '';
      filesState.searchMode = false;
      renderFilesBody();
    }
  });
  $('#file-q', root).addEventListener('input', debounce(function (e) {
    if (!e.target.value.trim() && filesState.searchMode) {
      filesState.searchMode = false;
      renderFilesBody();
    }
  }, 400));

  // 拖拽上传
  const main = $('#files-main', root);
  const overlay = $('#drop-overlay', root);
  let dragDepth = 0;
  main.addEventListener('dragenter', function (e) {
    e.preventDefault();
    dragDepth++;
    if (e.dataTransfer && Array.prototype.some.call(e.dataTransfer.types || [], function (t) { return t === 'Files'; })) {
      overlay.classList.add('show');
    }
  });
  main.addEventListener('dragover', function (e) { e.preventDefault(); });
  main.addEventListener('dragleave', function (e) {
    e.preventDefault();
    dragDepth--;
    if (dragDepth <= 0) { dragDepth = 0; overlay.classList.remove('show'); }
  });
  main.addEventListener('drop', function (e) {
    e.preventDefault();
    dragDepth = 0;
    overlay.classList.remove('show');
    const files = e.dataTransfer && e.dataTransfer.files;
    if (files && files.length) openUploadModal(files);
  });
}
