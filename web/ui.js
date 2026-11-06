/* ImaHub — ui.js：图标、toast、模态框、Markdown 渲染、通用工具 */
'use strict';

/* ============ DOM 速查 ============ */
const $ = (sel, root) => (root || document).querySelector(sel);
const $$ = (sel, root) => Array.prototype.slice.call((root || document).querySelectorAll(sel));

function el(tag, cls, html) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (html !== undefined) n.innerHTML = html;
  return n;
}

function escapeHtml(s) {
  return String(s === undefined || s === null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

/* ============ 图标（feather 风格手写内联 SVG） ============ */
const ICON_PATHS = {
  folder: '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>',
  folderPlus: '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/><line x1="12" y1="11" x2="12" y2="17"/><line x1="9" y1="14" x2="15" y2="14"/>',
  file: '<path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/>',
  fileText: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><line x1="10" y1="9" x2="8" y2="9"/>',
  image: '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/>',
  music: '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
  film: '<rect x="2" y="2" width="20" height="20" rx="2.2"/><line x1="7" y1="2" x2="7" y2="22"/><line x1="17" y1="2" x2="17" y2="22"/><line x1="2" y1="12" x2="22" y2="12"/><line x1="2" y1="7" x2="7" y2="7"/><line x1="2" y1="17" x2="7" y2="17"/><line x1="17" y1="17" x2="22" y2="17"/><line x1="17" y1="7" x2="22" y2="7"/>',
  archive: '<rect x="2" y="3" width="20" height="5" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8"/><line x1="10" y1="12" x2="14" y2="12"/>',
  book: '<path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>',
  code: '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>',
  globe: '<circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>',
  download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>',
  upload: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/>',
  search: '<circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>',
  plus: '<line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>',
  x: '<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>',
  chevronRight: '<polyline points="9 18 15 12 9 6"/>',
  chevronLeft: '<polyline points="15 18 9 12 15 6"/>',
  chevronDown: '<polyline points="6 9 12 15 18 9"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h0a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h0a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v0a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
  home: '<path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/>',
  users: '<path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
  user: '<path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>',
  logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/>',
  sun: '<circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>',
  moon: '<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>',
  copy: '<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
  refresh: '<polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/>',
  trash: '<polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/>',
  edit: '<path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.12 2.12 0 0 1 3 3L12 15l-4 1 1-4z"/>',
  eye: '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>',
  eyeOff: '<path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/>',
  link: '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>',
  database: '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/>',
  alert: '<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>',
  check: '<path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/>',
  checkCircle: '<path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/>',
  info: '<circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>',
  loader: '<line x1="12" y1="2" x2="12" y2="6"/><line x1="12" y1="18" x2="12" y2="22"/><line x1="4.93" y1="4.93" x2="7.76" y2="7.76"/><line x1="16.24" y1="16.24" x2="19.07" y2="19.07"/><line x1="2" y1="12" x2="6" y2="12"/><line x1="18" y1="12" x2="22" y2="12"/><line x1="4.93" y1="19.07" x2="7.76" y2="16.24"/><line x1="16.24" y1="7.76" x2="19.07" y2="4.93"/>',
  lock: '<rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
  key: '<path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/>',
  note: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>',
  notebook: '<path d="M2 6h4"/><path d="M2 10h4"/><path d="M2 14h4"/><path d="M2 18h4"/><rect x="4" y="2" width="16" height="20" rx="2"/><path d="M15 2v20"/><path d="M15 7h5"/><path d="M15 12h5"/><path d="M15 17h5"/>',
  menu: '<line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/>',
  wifi: '<path d="M5 12.55a11 11 0 0 1 14.08 0"/><path d="M1.42 9a16 16 0 0 1 21.16 0"/><path d="M8.53 16.11a6 6 0 0 1 6.95 0"/><line x1="12" y1="20" x2="12.01" y2="20"/>',
  shield: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
  inbox: '<polyline points="22 12 16 12 14 15 10 15 8 12 2 12"/><path d="M5.45 5.11L2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>',
  save: '<path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/>',
};

function icon(name, size) {
  const p = ICON_PATHS[name] || ICON_PATHS.file;
  const s = size || 18;
  return '<svg class="icon-svg" width="' + s + '" height="' + s + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + p + '</svg>';
}

/** 把 <span data-icon="xxx"> 占位替换成 SVG */
function mountIcons(root) {
  $$('[data-icon]', root || document).forEach(function (n) {
    n.innerHTML = icon(n.getAttribute('data-icon'), n.getAttribute('data-size') || 18);
    n.removeAttribute('data-icon');
  });
}

/* ============ Toast ============ */
function toast(msg, type, ttl) {
  const root = $('#toasts');
  if (!root) return;
  const iconName = type === 'error' ? 'alert' : type === 'warn' ? 'alert' : type === 'success' ? 'checkCircle' : 'info';
  const t = el('div', 'toast ' + (type || 'info'), icon(iconName, 17) + '<div>' + escapeHtml(msg) + '</div>');
  root.appendChild(t);
  const life = ttl || (type === 'error' ? 5200 : 3200);
  setTimeout(function () {
    t.classList.add('out');
    setTimeout(function () { t.remove(); }, 240);
  }, life);
}

/* ============ 模态框 ============ */
/**
 * @param {{title:string, body:string, footer?:string, wide?:boolean, xwide?:boolean, onMount?:(root:HTMLElement, close:()=>void)=>void, onClose?:()=>void}} opts
 */
function showModal(opts) {
  const root = $('#modal-root');
  const mask = el('div', 'modal-mask');
  const sizeCls = opts.xwide ? ' xwide' : opts.wide ? ' wide' : '';
  mask.innerHTML =
    '<div class="modal' + sizeCls + '">' +
    '<div class="modal-head"><h3>' + escapeHtml(opts.title || '') + '</h3>' +
    '<button class="modal-close" data-close>' + icon('x', 18) + '</button></div>' +
    '<div class="modal-body">' + (opts.body || '') + '</div>' +
    (opts.footer ? '<div class="modal-foot">' + opts.footer + '</div>' : '') +
    '</div>';
  root.appendChild(mask);

  let closed = false;
  function close() {
    if (closed) return;
    closed = true;
    mask.remove();
    if (opts.onClose) opts.onClose();
  }
  mask.addEventListener('click', function (e) {
    if (e.target === mask) close();
  });
  $$('[data-close]', mask).forEach(function (b) { b.addEventListener('click', close); });
  document.addEventListener('keydown', function esc(e) {
    if (e.key === 'Escape' && !closed) { close(); }
    if (closed) document.removeEventListener('keydown', esc);
  });
  if (opts.onMount) opts.onMount(mask, close);
  return { close: close, root: mask };
}

/** 确认弹窗 → Promise<boolean> */
function confirmDialog(message, opts) {
  opts = opts || {};
  return new Promise(function (resolve) {
    let settled = false;
    const m = showModal({
      title: opts.title || '确认操作',
      body: '<div style="font-size:13.5px;line-height:1.8;color:var(--text-2)">' + message + '</div>',
      footer:
        '<button class="btn" data-no>取消</button>' +
        '<button class="btn ' + (opts.danger ? 'danger' : 'primary') + '" data-yes>' + escapeHtml(opts.okText || '确认') + '</button>',
      onMount: function (root, close) {
        $('[data-yes]', root).addEventListener('click', function () { settled = true; resolve(true); close(); });
        $('[data-no]', root).addEventListener('click', function () { settled = true; resolve(false); close(); });
      },
      onClose: function () { if (!settled) resolve(false); },
    });
  });
}

/* ============ 时间 / 文本工具 ============ */
function formatTime(ts) {
  if (!ts && ts !== 0) return '—';
  let n = Number(ts);
  if (!isFinite(n) || n <= 0) return '—';
  if (n < 1e12) n = n * 1000; // 秒 → 毫秒
  const d = new Date(n);
  if (isNaN(d.getTime())) return '—';
  const p = (x) => (x < 10 ? '0' + x : '' + x);
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
}

function extOf(name) {
  const m = /\.(.[^.]*)$/.exec(String(name || ''));
  return m ? m[1].toLowerCase() : '';
}

/* 扩展名 → 图标名 + 主题色（返回 CSS color） */
const EXT_VISUALS = {
  pdf: { i: 'fileText', c: '#e5484d', t: 'PDF' },
  doc: { i: 'fileText', c: '#2b6de4', t: 'Word' },
  docx: { i: 'fileText', c: '#2b6de4', t: 'Word' },
  xls: { i: 'file', c: '#12a672', t: 'Excel' },
  xlsx: { i: 'file', c: '#12a672', t: 'Excel' },
  csv: { i: 'file', c: '#12a672', t: '表格' },
  ppt: { i: 'file', c: '#e8710a', t: 'PPT' },
  pptx: { i: 'file', c: '#e8710a', t: 'PPT' },
  txt: { i: 'fileText', c: '#6b7280', t: '文本' },
  md: { i: 'fileText', c: '#6366f1', t: 'Markdown' },
  markdown: { i: 'fileText', c: '#6366f1', t: 'Markdown' },
  html: { i: 'code', c: '#0ea5e9', t: '网页' },
  htm: { i: 'code', c: '#0ea5e9', t: '网页' },
  xmind: { i: 'notebook', c: '#0d9488', t: 'Xmind' },
  epub: { i: 'book', c: '#d97706', t: 'EPUB' },
  mobi: { i: 'book', c: '#d97706', t: '电子书' },
  jpg: { i: 'image', c: '#db2777', t: '图片' },
  jpeg: { i: 'image', c: '#db2777', t: '图片' },
  png: { i: 'image', c: '#db2777', t: '图片' },
  gif: { i: 'image', c: '#db2777', t: '图片' },
  webp: { i: 'image', c: '#db2777', t: '图片' },
  bmp: { i: 'image', c: '#db2777', t: '图片' },
  svg: { i: 'image', c: '#db2777', t: '图片' },
  mp3: { i: 'music', c: '#8b5cf6', t: '音频' },
  wav: { i: 'music', c: '#8b5cf6', t: '音频' },
  m4a: { i: 'music', c: '#8b5cf6', t: '音频' },
  flac: { i: 'music', c: '#8b5cf6', t: '音频' },
  mp4: { i: 'film', c: '#7c3aed', t: '视频' },
  mov: { i: 'film', c: '#7c3aed', t: '视频' },
  mkv: { i: 'film', c: '#7c3aed', t: '视频' },
  avi: { i: 'film', c: '#7c3aed', t: '视频' },
  zip: { i: 'archive', c: '#64748b', t: '压缩包' },
  rar: { i: 'archive', c: '#64748b', t: '压缩包' },
  '7z': { i: 'archive', c: '#64748b', t: '压缩包' },
};

function fileVisual(name, isFolder) {
  if (isFolder) return { i: 'folder', c: '#f59e0b', t: '文件夹' };
  const v = EXT_VISUALS[extOf(name)];
  return v || { i: 'file', c: '#8b93a7', t: extOf(name) ? extOf(name).toUpperCase() : '文件' };
}

function fileIconHtml(item) {
  const v = fileVisual(item.title || item.name, item.is_folder);
  return '<div class="file-icon" style="background:' + v.c + '1f;color:' + v.c + '">' + icon(v.i, 19) + '</div>';
}

/* ============ 数据适配（IMA 返回结构不完全固定，做容错提取） ============ */
function pickList(data) {
  if (!data) return [];
  if (Array.isArray(data)) return data;
  const keys = ['info_list', 'knowledge_list', 'note_list', 'notebook_list', 'note_folder_infos', 'list', 'items', 'results', 'rows'];
  for (let i = 0; i < keys.length; i++) {
    if (Array.isArray(data[keys[i]])) return data[keys[i]];
  }
  for (const k in data) {
    if (k === 'errors') continue; // 账号错误数组不是条目列表
    if (Array.isArray(data[k])) return data[k];
  }
  return [];
}

function pickPage(data) {
  return {
    items: pickList(data),
    isEnd: data ? (data.is_end === undefined ? true : !!data.is_end) : true,
    nextCursor: data ? (data.next_cursor !== undefined ? data.next_cursor : (data.cursor !== undefined ? data.cursor : null)) : null,
    raw: data || {},
  };
}

function itemId(it) {
  return it.media_id !== undefined ? it.media_id
    : it.note_id !== undefined ? it.note_id
    : it.folder_id !== undefined ? it.folder_id
    : it.id !== undefined ? it.id
    : it.doc_id !== undefined ? it.doc_id : '';
}

function itemTitle(it) {
  return it.title || it.name || it.file_name || it.doc_title || '(未命名)';
}

/* ============ Markdown 渲染（先转义再加工，防 XSS） ============ */
function renderMarkdown(src) {
  const text = String(src === undefined || src === null ? '' : src);
  const esc = escapeHtml(text);

  // 代码块抽出来放占位符（剥离 ``` 后的语言标记）
  const blocks = [];
  let work = esc.replace(/```[ \t]*[\w-]*[ \t]*\n([\s\S]*?)```/g, function (m, code) {
    blocks.push('<pre class="md-code"><code>' + code.replace(/\n$/, '') + '</code></pre>');
    return '\u0000CODE' + (blocks.length - 1) + '\u0000';
  });

  function inline(s) {
    return s
      .replace(/`([^`]+)`/g, '<code class="md-ic">$1</code>')
      .replace(/\*\*([^*]+)\*\*/g, '<strong class="md-strong">$1</strong>')
      .replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em class="md-em">$2</em>')
      .replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a class="md-link" href="$2" target="_blank" rel="noopener noreferrer">$1</a>');
  }

  const lines = work.split('\n');
  const out = [];
  let inUl = false, inOl = false, inQuote = false;
  function closeLists() {
    if (inUl) { out.push('</ul>'); inUl = false; }
    if (inOl) { out.push('</ol>'); inOl = false; }
  }
  function closeQuote() {
    if (inQuote) { out.push('</blockquote>'); inQuote = false; }
  }
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const line = raw.trimEnd();
    if (/^\u0000CODE\d+\u0000$/.test(line.trim())) {
      closeLists(); closeQuote();
      out.push(line.trim());
      continue;
    }
    if (line.trim() === '') { closeLists(); closeQuote(); continue; }
    let m;
    if ((m = line.match(/^(#{1,3})\s+(.*)$/))) {
      closeLists(); closeQuote();
      const lv = m[1].length;
      out.push('<div class="md-h' + lv + '">' + inline(m[2]) + '</div>');
    } else if (/^(-{3,}|\*{3,})$/.test(line.trim())) {
      closeLists(); closeQuote();
      out.push('<hr class="md-hr">');
    } else if ((m = line.match(/^\s*[-*]\s+(.*)$/))) {
      closeQuote();
      if (!inUl) { closeLists(); out.push('<ul class="md-ul">'); inUl = true; }
      out.push('<li>' + inline(m[1]) + '</li>');
    } else if ((m = line.match(/^\s*\d+[.)]\s+(.*)$/))) {
      closeQuote();
      if (!inOl) { closeLists(); out.push('<ol class="md-ol">'); inOl = true; }
      out.push('<li>' + inline(m[1]) + '</li>');
    } else if ((m = line.match(/^&gt;\s?(.*)$/))) {
      closeLists();
      if (!inQuote) { out.push('<blockquote class="md-quote">'); inQuote = true; }
      out.push('<div>' + inline(m[1]) + '</div>');
    } else {
      closeLists(); closeQuote();
      out.push('<div class="md-p">' + inline(line) + '</div>');
    }
  }
  closeLists(); closeQuote();
  let html = out.join('\n');
  html = html.replace(/\u0000CODE(\d+)\u0000/g, function (m, i) { return blocks[Number(i)]; });
  return html;
}

/* ============ 剪贴板 ============ */
async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = el('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      ta.remove();
    }
    toast('已复制到剪贴板', 'success');
    return true;
  } catch (e) {
    toast('复制失败，请手动选择复制', 'error');
    return false;
  }
}

/* ============ 杂项 ============ */
function debounce(fn, ms) {
  let t = null;
  return function () {
    const args = arguments, self = this;
    clearTimeout(t);
    t = setTimeout(function () { fn.apply(self, args); }, ms || 300);
  };
}

function emptyStateHtml(iconName, title, desc, actionHtml) {
  return '<div class="empty">' +
    '<span data-icon="' + iconName + '" data-size="46"></span>' +
    '<div class="t">' + escapeHtml(title) + '</div>' +
    (desc ? '<div class="d">' + escapeHtml(desc) + '</div>' : '') +
    (actionHtml ? '<div style="margin-top:14px">' + actionHtml + '</div>' : '') +
    '</div>';
}

function skeletonRows(n, cols) {
  let html = '';
  for (let i = 0; i < (n || 5); i++) {
    html += '<div class="sk-row">';
    html += '<div class="skeleton" style="width:36px;height:36px;border-radius:9px"></div>';
    html += '<div style="flex:1"><div class="skeleton sk-line" style="width:' + (35 + ((i * 17) % 45)) + '%"></div></div>';
    html += '<div class="skeleton sk-line" style="width:64px"></div>';
    html += '<div class="skeleton sk-line" style="width:52px"></div>';
    html += '</div>';
  }
  return html;
}
