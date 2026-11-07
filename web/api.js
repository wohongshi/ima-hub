/* ImaHub — api.js：fetch/XHR 封装，统一解包 {code,msg,data} */
'use strict';

/* ============ ima 多账号（容量池）上下文 ============ */
const imaAccountsState = {
  accounts: [],   // [{id,name,client_id_preview,source?,upload_kb_id?}]
  defaultId: '',  // 默认账号 id
};

/** 请求错误对象 */
class ApiError extends Error {
  constructor(msg, code, unauthorized) {
    super(msg || '请求失败');
    this.code = code;
    this.unauthorized = !!unauthorized;
  }
}

/**
 * 通用请求：统一携带 same-origin Cookie，解包业务信封。
 * @param {string} method
 * @param {string} path
 * @param {object} [body] JSON 请求体（GET 不传）
 * @param {{silent?:boolean}} [opts] silent=true 时错误不弹 toast
 * @returns {Promise<any>} data 字段
 */
async function api(method, path, body, opts) {
  opts = opts || {};
  let res;
  try {
    const init = { method: method, credentials: 'same-origin', headers: {} };
    if (body !== undefined && body !== null) {
      init.headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(body);
    }
    res = await fetch(path, init);
  } catch (e) {
    if (!opts.silent) toast('网络错误，请检查连接', 'error');
    throw new ApiError('网络错误', -1, false);
  }
  let json = null;
  try {
    json = await res.json();
  } catch (e) {
    if (!opts.silent) toast('服务响应异常（非 JSON）', 'error');
    throw new ApiError('服务响应异常', -1, res.status === 401);
  }
  if (!json || json.code !== 0) {
    const code = json ? json.code : -1;
    const msg = (json && json.msg) ? json.msg : ('请求失败（HTTP ' + res.status + '）');
    const err = new ApiError(msg, code, res.status === 401 && path !== '/api/login' && path !== '/api/setup');
    if (err.unauthorized) {
      if (typeof onUnauthorized === 'function') onUnauthorized();
    } else if (!opts.silent) {
      toast(msg, 'error');
    }
    throw err;
  }
  return json.data === undefined ? {} : json.data;
}

const apiGet = (path, opts) => api('GET', path, undefined, opts);
const apiPost = (path, body, opts) => api('POST', path, body, opts);
const apiPatch = (path, body, opts) => api('PATCH', path, body, opts);
const apiPut = (path, body, opts) => api('PUT', path, body, opts);
const apiDel = (path, opts) => api('DELETE', path, undefined, opts);

/** 构造带 query 的 GET 路径（ia 等参数由调用方显式传入） */
function qs(path, params) {
  params = params || {};
  const parts = [];
  for (const k in params) {
    const v = params[k];
    if (v === undefined || v === null || v === '') continue;
    parts.push(encodeURIComponent(k) + '=' + encodeURIComponent(String(v)));
  }
  return parts.length ? path + '?' + parts.join('&') : path;
}

/**
 * multipart 上传（XHR 以获得进度）。
 * @param {File} file
 * @param {{kbId?:string, folderId?:string, force?:boolean, ia?:string}} fields
 *        ia='auto' 服务端自动分配账号（kbId 可省略）；ia=<账号id> 时必须带 kbId
 * @param {(ratio:number)=>void} [onProgress]
 * @returns {Promise<{media_id:string, repeated:boolean, ia?:string, account_name?:string, kb_id?:string}>}
 */
function apiUpload(file, fields, onProgress) {
  return apiUploadTo('/api/ima/upload', file, fields, onProgress);
}

/** multipart 上传到指定端点（离线队列走 /api/queue/upload，字段同 apiUpload） */
function apiUploadTo(url, file, fields, onProgress) {
  return new Promise(function (resolve, reject) {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', url, true);
    xhr.withCredentials = true;
    if (xhr.upload && onProgress) {
      xhr.upload.onprogress = function (e) {
        if (e.lengthComputable) onProgress(Math.min(1, e.loaded / e.total));
      };
    }
    xhr.onload = function () {
      let json = null;
      try { json = JSON.parse(xhr.responseText); } catch (e) { /* fallthrough */ }
      if (!json || json.code !== 0) {
        const msg = (json && json.msg) ? json.msg : ('上传失败（HTTP ' + xhr.status + '）');
        const err = new ApiError(msg, json ? json.code : -1, xhr.status === 401);
        if (err.unauthorized && typeof onUnauthorized === 'function') onUnauthorized();
        reject(err);
        return;
      }
      onProgress && onProgress(1);
      resolve(json.data || {});
    };
    xhr.onerror = function () { reject(new ApiError('网络错误，上传失败', -1, false)); };
    xhr.ontimeout = function () { reject(new ApiError('上传超时', -1, false)); };
    const fd = new FormData();
    fd.append('file', file, file.name);
    if (fields.kbId) fd.append('kb_id', fields.kbId);
    if (fields.folderId) fd.append('folder_id', fields.folderId);
    if (fields.force) fd.append('force', '1');
    if (fields.ia) fd.append('ia', fields.ia);
    xhr.timeout = 1800000; // 30 分钟兜底，配合 ontimeout 避免上传永久挂起
    xhr.send(fd);
  });
}

/** 浏览器触发下载（服务端带 Content-Disposition: attachment）；ia = 该条目所属账号 */
function triggerDownload(mediaId, ia) {
  const a = document.createElement('a');
  a.href = '/api/ima/download?media_id=' + encodeURIComponent(mediaId) + (ia ? '&ia=' + encodeURIComponent(ia) : '');
  a.download = '';
  a.rel = 'noopener';
  a.style.display = 'none';
  document.body.appendChild(a);
  a.click();
  setTimeout(function () { a.remove(); }, 800);
}

/** 给聚合列表条目标记所属账号 */
function tagIa(item, ia, accountName) {
  const it = Object.assign({}, item);
  if (it.ia === undefined || it.ia === null || it.ia === '') it.ia = ia || '';
  if (!it.account_name && accountName) it.account_name = accountName;
  return it;
}

/** 确保 ima 账号清单已加载（聚合视图需要） */
async function ensureImaAccounts() {
  if (imaAccountsState.accounts.length) return imaAccountsState.accounts;
  await loadIaAccounts();
  return imaAccountsState.accounts;
}
