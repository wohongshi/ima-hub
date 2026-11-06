/* ImaHub — view-notes.js：笔记页（笔记本 / 笔记列表 / 阅读 / 新建 / 追加 / 搜索） */
'use strict';

const notesState = {
  notebooks: [], nbCursor: null, nbEnd: true, nbLoading: false, nbErrors: [],
  notebook: null,            // 当前笔记本 {id,name,ia,account_name,...}
  nbIa: '',                  // 当前笔记本所属账号（单点请求回传）
  notes: [], cursor: null, isEnd: true, loading: false,
  allSources: [],            // 未选笔记本时按账号聚合：{ia, account_name, cursor, isEnd}
  searchMode: false, searchQuery: '', searchType: 0,
  searchResults: [], searchLoading: false,
  currentNote: null,         // {id,title,content,ia}
};

function renderNotesView(root) {
  root.innerHTML =
    '<div class="notes-page">' +
    '  <aside class="nb-panel">' +
    '    <div class="kb-panel-head"><h3>笔记本</h3>' +
    '      <button class="btn icon sm ghost" id="nb-refresh" title="刷新">' + icon('refresh', 15) + '</button>' +
    '    </div>' +
    '    <div class="kb-list" id="nb-list"></div>' +
    '  </aside>' +
    '  <section class="notes-list-pane">' +
    '    <div class="notes-list-head">' +
    '      <div style="display:flex;gap:8px">' +
    '        <div class="search-box" style="flex:1;min-width:0">' + icon('search', 15) +
    '          <input id="note-q" placeholder="搜索笔记">' +
    '          <button class="btn icon sm ghost" id="note-search-type" title="切换搜索范围：标题">' + icon('chevronDown', 13) + '</button>' +
    '        </div>' +
    '        <button class="btn primary icon sm" id="note-new" title="新建笔记">' + icon('plus', 15) + '</button>' +
    '      </div>' +
    '      <div style="display:flex;align-items:center;gap:8px">' +
    '        <span class="muted" style="font-size:12px" id="notes-count">全部笔记</span>' +
    '        <span style="flex:1"></span>' +
    '        <button class="btn sm ghost hidden" id="note-search-back">' + icon('chevronLeft', 13) + '返回列表</button>' +
    '      </div>' +
    '    </div>' +
    '    <div class="notes-list" id="notes-list"></div>' +
    '  </section>' +
    '  <section class="note-viewer" id="note-viewer">' +
    '    <div class="note-viewer-head hidden" id="note-viewer-head">' +
    '      <h3 id="note-viewer-title"></h3>' +
    '      <button class="btn sm" id="note-append-btn">' + icon('edit', 14) + '追加内容</button>' +
    '      <button class="btn icon sm ghost" id="note-close-btn" title="关闭">' + icon('x', 15) + '</button>' +
    '    </div>' +
    '    <div class="note-content" id="note-content"></div>' +
    '  </section>' +
    '</div>';

  bindNotesEvents(root);
  renderNotebookList();
  renderNotesList();
  renderNoteViewer();
  if (!notesState.notebooks.length && !notesState.nbLoading) loadNotebooks(true);
}

/* ============ 笔记本（聚合列表，带账号标签） ============ */
async function loadNotebooks(reset) {
  if (notesState.nbLoading) return;
  notesState.nbLoading = true;
  if (reset) { notesState.notebooks = []; notesState.nbCursor = null; notesState.nbEnd = true; }
  renderNotebookList();
  try {
    // 省略 ia = 聚合全部账号，每项自带 ia / account_name
    const data = await apiGet(qs('/api/ima/notebooks', {
      cursor: reset ? '0' : notesState.nbCursor, limit: 50,
    }), { silent: true });
    const page = pickPage(data);
    notesState.notebooks = notesState.notebooks.concat(page.items);
    notesState.nbEnd = page.isEnd;
    notesState.nbCursor = page.nextCursor;
    if (reset) notesState.nbErrors = (data && data.errors) || [];
  } catch (e) { /* 静默 */ }
  notesState.nbLoading = false;
  renderNotebookList();
}

function notebookId(nb) {
  return nb.folder_id !== undefined ? nb.folder_id : (nb.id !== undefined ? nb.id : itemId(nb));
}

function renderNotebookList() {
  const box = $('#nb-list');
  if (!box) return;
  if (notesState.nbLoading && !notesState.notebooks.length) {
    box.innerHTML = '<div style="padding:8px">' +
      '<div class="skeleton" style="height:40px;border-radius:9px;margin-bottom:8px"></div>' +
      '<div class="skeleton" style="height:40px;border-radius:9px"></div></div>';
    return;
  }
  let nbErrHtml = '';
  (notesState.nbErrors || []).forEach(function (e) {
    nbErrHtml += '<div class="kb-error-row" title="' + escapeHtml(e.error || '') + '">' +
      icon('alert', 13) + '<span>' + escapeHtml(e.account_name || e.ia || '账号') + ' 加载失败</span></div>';
  });
  let html = nbErrHtml + '<button class="kb-item' + (!notesState.notebook ? ' active' : '') + '" data-nb="__all">' +
    '<div class="kb-cover" style="background:var(--accent-soft);color:var(--accent)">' + icon('note', 16) + '</div>' +
    '<div class="kb-name">全部笔记</div></button>';
  notesState.notebooks.forEach(function (nb, idx) {
    const active = notesState.notebook && String(notebookId(notesState.notebook)) === String(notebookId(nb));
    html += '<button class="kb-item' + (active ? ' active' : '') + '" data-nb-idx="' + idx + '">' +
      '<div class="kb-cover">' + escapeHtml((nb.name || nb.title || 'N').charAt(0)) + '</div>' +
      '<div class="kb-name" title="' + escapeHtml(nb.name || nb.title || '') + '">' + escapeHtml(nb.name || nb.title || '(未命名笔记本)') + '</div>' +
      (nb.account_name ? '<span class="chip" title="所属账号">' + escapeHtml(nb.account_name) + '</span>' : '') +
      '</button>';
  });
  if (!notesState.nbEnd) {
    html += '<div style="padding:8px 4px"><button class="btn sm" id="nb-load-more" style="width:100%">加载更多</button></div>';
  }
  box.innerHTML = html;
  $$('.kb-item', box).forEach(function (b) {
    b.addEventListener('click', function () {
      if (b.getAttribute('data-nb') === '__all') {
        notesState.notebook = null;
        notesState.nbIa = '';
      } else {
        notesState.notebook = notesState.notebooks[Number(b.getAttribute('data-nb-idx'))];
        notesState.nbIa = String(notesState.notebook.ia || '');
      }
      notesState.searchMode = false;
      const q = $('#note-q'); if (q) q.value = '';
      loadNotes(true);
      renderNotebookList();
    });
  });
  const moreBtn = $('#nb-load-more', box);
  if (moreBtn) moreBtn.addEventListener('click', function () { loadNotebooks(false); });
}

/* ============ 笔记列表（选笔记本=单点；未选=按账号聚合） ============ */
async function loadNotes(reset) {
  if (notesState.loading) return;
  notesState.loading = true;
  if (reset) {
    notesState.notes = []; notesState.cursor = null; notesState.isEnd = true;
    notesState.allSources = [];
  }
  renderNotesList();
  try {
    if (notesState.notebook) {
      const data = await apiGet(qs('/api/ima/notes', {
        folder: notebookId(notesState.notebook), ia: notesState.nbIa,
        cursor: reset ? '' : notesState.cursor, limit: 50,
      }));
      const page = pickPage(data);
      const name = notesState.notebook.account_name || '';
      notesState.notes = notesState.notes.concat(page.items.map(function (t) { return tagIa(t, notesState.nbIa, name); }));
      notesState.isEnd = page.isEnd;
      notesState.cursor = page.nextCursor;
    } else {
      if (reset) {
        await ensureImaAccounts();
        notesState.allSources = imaAccountsState.accounts.map(function (a) {
          return {
            ia: String(a.id),
            account_name: a.name || a.client_id_preview || String(a.id),
            cursor: null, isEnd: false,
          };
        });
      }
      const pending = notesState.allSources.filter(function (s) { return !s.isEnd; });
      const results = await Promise.all(pending.map(function (s) {
        return apiGet(qs('/api/ima/notes', { ia: s.ia, cursor: s.cursor || '', limit: 25 }), { silent: true })
          .catch(function () { return null; });
      }));
      results.forEach(function (data, i) {
        const s = pending[i];
        if (!data) { s.isEnd = true; return; }
        const page = pickPage(data);
        notesState.notes = notesState.notes.concat(page.items.map(function (t) { return tagIa(t, s.ia, s.account_name); }));
        s.cursor = page.nextCursor;
        s.isEnd = page.isEnd;
      });
      notesState.isEnd = !notesState.allSources.length ||
        notesState.allSources.every(function (s) { return s.isEnd; });
    }
  } catch (e) { /* api 层已提示 */ }
  notesState.loading = false;
  renderNotesList();
}

function noteTitleOf(it) { return itemTitle(it); }
function noteIdOf(it) { return itemId(it); }

function renderNotesList() {
  const box = $('#notes-list');
  if (!box) return;
  const countEl = $('#notes-count');
  const list = notesState.searchMode ? notesState.searchResults : notesState.notes;

  if (notesState.loading && !notesState.notes.length && !notesState.searchMode) {
    box.innerHTML = skeletonRows(5);
    return;
  }
  if (notesState.searchLoading) {
    box.innerHTML = skeletonRows(4);
    return;
  }
  if (!list.length) {
    box.innerHTML = emptyStateHtml('note',
      notesState.searchMode ? '没有搜索到笔记' : '还没有笔记',
      notesState.searchMode ? '换个关键词试试' : '点击右上角「+」新建一篇 Markdown 笔记');
    mountIcons(box);
    return;
  }
  let html = '';
  list.forEach(function (it, idx) {
    const active = notesState.currentNote && String(notesState.currentNote.id) === String(noteIdOf(it));
    html += '<button class="note-item' + (active ? ' active' : '') + '" data-idx="' + idx + '">' +
      '<div class="nt">' + escapeHtml(noteTitleOf(it)) + '</div>' +
      '<div class="nd">' +
      (it.account_name ? '<span class="chip" title="所属账号">' + escapeHtml(it.account_name) + '</span>' : '') +
      escapeHtml(it.summary || it.desc || (notesState.searchMode ? (it.content || '').slice(0, 60) : '点击阅读')) +
      '</div>' +
      '</button>';
  });
  if (!notesState.searchMode && !notesState.isEnd) {
    html += '<div style="padding:10px 4px"><button class="btn sm" id="notes-more" style="width:100%">加载更多</button></div>';
  }
  box.innerHTML = html;
  if (countEl) {
    countEl.textContent = notesState.searchMode
      ? '搜索结果 ' + list.length + ' 条'
      : (notesState.notebook ? (notesState.notebook.name || notesState.notebook.title || '笔记本') : '全部笔记') + ' · ' + list.length + ' 条';
  }
  $$('.note-item', box).forEach(function (b) {
    b.addEventListener('click', function () {
      const it = list[Number(b.getAttribute('data-idx'))];
      openNote(noteIdOf(it), noteTitleOf(it), it.ia);
    });
  });
  const more = $('#notes-more', box);
  if (more) more.addEventListener('click', function () { loadNotes(false); });
}

/* ============ 阅读 ============ */
async function openNote(noteId, title, ia) {
  notesState.currentNote = { id: String(noteId), title: title || '笔记', content: '', ia: ia || '' };
  renderNoteViewer();
  renderNotesList();
  try {
    const data = await apiGet(qs('/api/ima/note', { note_id: noteId, ia: ia || '' }));
    notesState.currentNote.content = data && data.content !== undefined ? data.content : '';
    renderNoteViewer();
  } catch (e) {
    notesState.currentNote.content = '（内容加载失败：' + (e.message || '未知错误') + '）';
    renderNoteViewer();
  }
}

function renderNoteViewer() {
  const head = $('#note-viewer-head');
  const content = $('#note-content');
  if (!head || !content) return;
  if (!notesState.currentNote) {
    head.classList.add('hidden');
    content.innerHTML = emptyStateHtml('note', '选择一篇笔记开始阅读', '左侧点击笔记标题，或新建一篇笔记');
    mountIcons(content);
    return;
  }
  head.classList.remove('hidden');
  $('#note-viewer-title').textContent = notesState.currentNote.title;
  const src = notesState.currentNote.content;
  if (!src) {
    content.innerHTML = '<div class="loading-center"><div class="spinner"></div><div>加载笔记内容…</div></div>';
    return;
  }
  content.innerHTML = '<div class="md">' + renderMarkdown(src) + '</div>';
  content.scrollTop = 0;
}

/* ============ 搜索笔记（选笔记本=单点；未选=按账号聚合） ============ */
async function runNoteSearch(reset) {
  const q = ($('#note-q') && $('#note-q').value || '').trim();
  if (!q) {
    notesState.searchMode = false;
    renderNotesList();
    return;
  }
  notesState.searchMode = true;
  notesState.searchQuery = q;
  notesState.searchLoading = true;
  renderNotesList();
  try {
    if (notesState.notebook) {
      const data = await apiGet(qs('/api/ima/notes/search', {
        q: q, search_type: notesState.searchType, ia: notesState.nbIa,
      }));
      const name = notesState.notebook.account_name || '';
      notesState.searchResults = pickList(data).map(function (t) { return tagIa(t, notesState.nbIa, name); });
    } else {
      await ensureImaAccounts();
      const accounts = imaAccountsState.accounts;
      const results = await Promise.all(accounts.map(function (a) {
        return apiGet(qs('/api/ima/notes/search', {
          q: q, search_type: notesState.searchType, ia: String(a.id),
        }), { silent: true }).catch(function () { return null; });
      }));
      let merged = [];
      results.forEach(function (data, i) {
        if (!data) return;
        const a = accounts[i];
        const name = a.name || a.client_id_preview || String(a.id);
        merged = merged.concat(pickList(data).map(function (t) { return tagIa(t, String(a.id), name); }));
      });
      notesState.searchResults = merged;
    }
  } catch (e) { notesState.searchResults = []; }
  notesState.searchLoading = false;
  renderNotesList();
}

/* ============ 新建笔记 ============ */
function openNewNoteModal() {
  const nbOptions = notesState.notebooks.map(function (nb) {
    return '<option value="' + escapeHtml(notebookId(nb)) + '" data-ia="' + escapeHtml(String(nb.ia || '')) + '">' +
      escapeHtml((nb.name || nb.title || nb.id) + (nb.account_name ? '（' + nb.account_name + '）' : '')) + '</option>';
  }).join('');
  showModal({
    title: '新建 Markdown 笔记',
    wide: true,
    body:
      '<div class="field">' +
      '  <label>笔记内容（Markdown）</label>' +
      '  <textarea class="textarea" id="nn-content" style="min-height:220px" placeholder="# 标题&#10;&#10;正文内容…"></textarea>' +
      '</div>' +
      '<div class="form-row">' +
      '  <div class="field"><label>归属笔记本（可选）</label><select class="select" id="nn-folder"><option value="" data-ia="">不指定（默认笔记本）</option>' + nbOptions + '</select></div>' +
      '  <div class="field"><label>或新建笔记本名（可选）</label><input class="input" id="nn-folder-name" placeholder="留空则使用上方选择"></div>' +
      '</div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="nn-go">' + icon('save', 15) + '创建笔记</button>',
    onMount: function (root, close) {
      $('#nn-go', root).addEventListener('click', async function () {
        const content = $('#nn-content', root).value;
        if (!content.trim()) { toast('请输入笔记内容', 'warn'); return; }
        const sel = $('#nn-folder', root);
        const opt = sel.options[sel.selectedIndex];
        const folderId = sel.value;
        const ia = opt ? (opt.getAttribute('data-ia') || '') : '';
        const folderName = $('#nn-folder-name', root).value.trim();
        const body = { content: content };
        if (folderName) body.folder_name = folderName;
        else if (folderId) body.folder_id = folderId;
        if (ia) body.ia = ia;
        else if (notesState.notebook && notesState.nbIa) body.ia = notesState.nbIa;
        const btn = $('#nn-go', root);
        btn.disabled = true;
        try {
          const data = await apiPost('/api/ima/note', body);
          toast('笔记创建成功', 'success');
          close();
          const nid = data && (data.note_id !== undefined ? data.note_id : data.id);
          if (nid) {
            openNote(nid, (content.split('\n')[0] || '新笔记').replace(/^#+\s*/, ''), body.ia || '');
          }
          loadNotes(true);
        } catch (e) {
          btn.disabled = false;
        }
      });
    },
  });
}

/* ============ 追加内容 ============ */
function openAppendModal() {
  if (!notesState.currentNote) return;
  showModal({
    title: '追加内容到笔记',
    wide: true,
    body:
      '<div class="hint" style="margin:0 0 10px">当前笔记：<strong>' + escapeHtml(notesState.currentNote.title) + '</strong></div>' +
      '<div class="field"><label>追加内容（Markdown）</label>' +
      '<textarea class="textarea" id="ap-content" style="min-height:180px" placeholder="要追加的正文…"></textarea></div>',
    footer: '<button class="btn" data-close>取消</button><button class="btn primary" id="ap-go">' + icon('edit', 15) + '追加</button>',
    onMount: function (root, close) {
      $('#ap-go', root).addEventListener('click', async function () {
        const content = $('#ap-content', root).value;
        if (!content.trim()) { toast('请输入要追加的内容', 'warn'); return; }
        const btn = $('#ap-go', root);
        btn.disabled = true;
        try {
          await apiPost('/api/ima/note/append', {
            note_id: notesState.currentNote.id,
            content: content,
            ia: notesState.currentNote.ia || '',
          });
          toast('已追加内容', 'success');
          close();
          openNote(notesState.currentNote.id, notesState.currentNote.title, notesState.currentNote.ia);
        } catch (e) {
          btn.disabled = false;
        }
      });
    },
  });
}

/* ============ 事件绑定 ============ */
function bindNotesEvents(root) {
  $('#nb-refresh', root).addEventListener('click', function () {
    loadNotebooks(true);
    loadNotes(true);
  });
  $('#note-new', root).addEventListener('click', openNewNoteModal);
  $('#note-append-btn', root).addEventListener('click', openAppendModal);
  $('#note-close-btn', root).addEventListener('click', function () {
    notesState.currentNote = null;
    renderNoteViewer();
    renderNotesList();
  });
  $('#note-q', root).addEventListener('keydown', function (e) {
    if (e.key === 'Enter') runNoteSearch(true);
    if (e.key === 'Escape') {
      e.target.value = '';
      notesState.searchMode = false;
      renderNotesList();
    }
  });
  $('#note-q', root).addEventListener('input', debounce(function (e) {
    if (!e.target.value.trim() && notesState.searchMode) {
      notesState.searchMode = false;
      renderNotesList();
    }
  }, 400));
  $('#note-search-type', root).addEventListener('click', function () {
    notesState.searchType = notesState.searchType === 0 ? 1 : 0;
    const b = $('#note-search-type');
    b.title = '切换搜索范围：' + (notesState.searchType === 0 ? '标题' : '正文');
    toast('搜索范围：' + (notesState.searchType === 0 ? '标题' : '正文'), 'info', 1800);
    if (notesState.searchMode) runNoteSearch(true);
  });
  $('#note-search-back', root).addEventListener('click', function () {
    notesState.searchMode = false;
    const q = $('#note-q'); if (q) q.value = '';
    renderNotesList();
  });

  // 首屏加载笔记列表
  if (!notesState.notes.length && !notesState.loading) loadNotes(true);
}
