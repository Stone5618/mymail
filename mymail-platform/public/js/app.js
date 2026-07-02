// ============================================
// MyMail - Client-side Application
// ============================================

const API = '/api';
let currentUser = null;
let token = localStorage.getItem('token');
let ws = null;
const _cleanups = [];  // F1: page cleanup registry

// F4: dynamic domain from user email
function getDomain() {
  return currentUser?.email?.split('@')[1] || 'localhost';
}

// ---- Utilities ----
function $(sel) { return document.querySelector(sel); }
function $$(sel) { return document.querySelectorAll(sel); }

function toast(msg, type = 'info') {
  const container = $('#toast-container');
  const el = document.createElement('div');
  el.className = 'toast';
  const icons = { info: '💡', success: '✅', error: '❌', warning: '⚠️' };
  el.innerHTML = `<span>${icons[type] || '💡'}</span><span>${msg}</span>`;
  container.appendChild(el);
  requestAnimationFrame(() => el.classList.add('show'));
  setTimeout(() => {
    el.classList.remove('show');
    setTimeout(() => el.remove(), 300);
  }, 3000);
}

async function api(path, options = {}) {
  const headers = { ...options.headers };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json';
    if (options.body && typeof options.body === 'object') {
      options.body = JSON.stringify(options.body);
    }
  }
  const res = await fetch(API + path, { ...options, headers });
  if (res.status === 401) {
    logout();
    throw new Error('请重新登录');
  }
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || '请求失败');
  return data;
}

function formatDate(str) {
  if (!str) return '';
  const d = new Date(str);
  const now = new Date();
  const diff = now - d;
  if (diff < 86400000 && d.getDate() === now.getDate()) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
  }
  if (d.getFullYear() === now.getFullYear()) {
    return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' });
  }
  return d.toLocaleDateString('zh-CN');
}

function escapeHtml(str) {
  if (!str) return '';
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// ---- Auth ----
async function login(email, password, remember) {
  const data = await api('/auth/login', {
    method: 'POST',
    body: { email, password, remember },
  });
  token = data.token;
  localStorage.setItem('token', token);
  currentUser = data.user;
  connectWebSocket();
  navigate('/inbox');
}

async function register(username, password, displayName) {
  const data = await api('/auth/register', {
    method: 'POST',
    body: { username, password, displayName },
  });
  token = data.token;
  localStorage.setItem('token', token);
  currentUser = data.user;
  connectWebSocket();
  navigate('/inbox');
}

function logout() {
  token = null;
  currentUser = null;
  localStorage.removeItem('token');
  if (ws) ws.close();
  navigate('/login');
}

async function checkAuth() {
  if (!token) return false;
  try {
    currentUser = await api('/auth/me');
    connectWebSocket();
    return true;
  } catch {
    return false;
  }
}

// ---- WebSocket ----
function connectWebSocket() {
  if (ws) ws.close();
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${proto}//${location.host}/ws`, [`auth.${token}`]);
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    if (msg.type === 'new_mail') {
      toast(`新邮件: ${msg.data.subject}`, 'info');
      playNotifSound();
      if (typeof refreshInbox === 'function') refreshInbox();
    }
  };
  ws.onclose = () => {
    if (token) setTimeout(connectWebSocket, 5000);
  };
}

function playNotifSound() {
  try {
    const ctx = new AudioContext();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.frequency.value = 800;
    gain.gain.value = 0.1;
    osc.start();
    gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.3);
    osc.stop(ctx.currentTime + 0.3);
  } catch {}
}

// ---- Router ----
const routes = {
  '/login': renderLogin,
  '/register': renderRegister,
  '/inbox': renderInbox,
  '/sent': renderFolder,
  '/drafts': renderFolder,
  '/trash': renderFolder,
  '/compose': renderCompose,
  '/mail/:id': renderMailDetail,
  '/settings': renderSettings,
  '/admin': renderAdmin,
};

function navigate(path) {
  // F1: run page cleanups before navigating away
  while (_cleanups.length) { try { _cleanups.pop()(); } catch {} }
  history.pushState(null, '', path);
  render();
}

async function render() {
  const path = location.pathname;
  const app = $('#app');

  if (path === '/login' || path === '/register') {
    routes[path](app);
    return;
  }

  if (!currentUser && !(await checkAuth())) {
    navigate('/login');
    return;
  }

  // Match route
  for (const [pattern, handler] of Object.entries(routes)) {
    const regex = new RegExp('^' + pattern.replace(/:(\w+)/g, '(?<$1>[^/]+)') + '$');
    const match = path.match(regex);
    if (match) {
      handler(app, match.groups || {});
      return;
    }
  }

  navigate('/inbox');
}

window.addEventListener('popstate', render);

// ---- Login Page ----
function renderLogin(app) {
  app.innerHTML = `
    <div class="min-h-screen flex items-center justify-center p-4">
      <div class="card p-8 w-full max-w-md">
        <div class="text-center mb-8">
          <div class="text-5xl mb-3">📧</div>
          <h1 class="text-2xl font-bold text-dark-100">MyMail</h1>
          <p class="text-dark-500 text-sm mt-1">自建邮件服务平台</p>
        </div>
        <form id="login-form" class="space-y-4">
          <div>
            <label class="block text-sm text-dark-400 mb-1.5">邮箱地址</label>
            <input type="email" name="email" class="input-field" placeholder="user@${getDomain()}" required>
          </div>
          <div>
            <label class="block text-sm text-dark-400 mb-1.5">密码</label>
            <div class="relative">
              <input type="password" name="password" class="input-field pr-10" placeholder="••••••••" required>
              <button type="button" class="absolute right-3 top-1/2 -translate-y-1/2 text-dark-500 hover:text-dark-300" onclick="togglePassword(this)">👁</button>
            </div>
          </div>
          <div class="flex items-center gap-2">
            <input type="checkbox" name="remember" id="remember" class="rounded border-dark-600 bg-dark-800 text-primary-500">
            <label for="remember" class="text-sm text-dark-400">记住我</label>
          </div>
          <div id="login-error" class="hidden text-red-400 text-sm bg-red-400/10 px-3 py-2 rounded-lg"></div>
          <button type="submit" class="btn-primary w-full">登 录</button>
          <p class="text-center text-sm text-dark-500">
            还没有账号？<a href="/register" onclick="event.preventDefault();navigate('/register')" class="text-primary-400 hover:text-primary-300">立即注册</a>
          </p>
        </form>
      </div>
    </div>`;

  $('#login-form').onsubmit = async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    try {
      await login(fd.get('email'), fd.get('password'), fd.get('remember') === 'on');
    } catch (err) {
      const el = $('#login-error');
      el.textContent = err.message;
      el.classList.remove('hidden');
    }
  };
}

// ---- Register Page ----
function renderRegister(app) {
  app.innerHTML = `
    <div class="min-h-screen flex items-center justify-center p-4">
      <div class="card p-8 w-full max-w-md">
        <div class="text-center mb-8">
          <div class="text-5xl mb-3">📧</div>
          <h1 class="text-2xl font-bold text-dark-100">创建账号</h1>
          <p class="text-dark-500 text-sm mt-1">注册你的邮箱</p>
        </div>
        <form id="register-form" class="space-y-4">
          <div>
            <label class="block text-sm text-dark-400 mb-1.5">用户名</label>
            <div class="flex items-center gap-0">
              <input type="text" name="username" class="input-field rounded-r-none" placeholder="zhangsan" pattern="[a-zA-Z0-9._]{3,20}" required>
              <span class="bg-dark-800 border border-l-0 border-dark-700 text-dark-500 px-3 py-2.5 rounded-r-lg text-sm whitespace-nowrap">@${getDomain()}</span>
            </div>
          </div>
          <div>
            <label class="block text-sm text-dark-400 mb-1.5">显示名称</label>
            <input type="text" name="displayName" class="input-field" placeholder="张三">
          </div>
          <div>
            <label class="block text-sm text-dark-400 mb-1.5">密码</label>
            <input type="password" name="password" class="input-field" placeholder="至少6位" minlength="6" required>
            <div id="pwd-strength" class="h-1 mt-1.5 rounded-full bg-dark-800 overflow-hidden"><div class="h-full transition-all duration-300 rounded-full" style="width:0%"></div></div>
          </div>
          <div>
            <label class="block text-sm text-dark-400 mb-1.5">确认密码</label>
            <input type="password" name="password2" class="input-field" placeholder="再次输入密码" required>
          </div>
          <div id="reg-error" class="hidden text-red-400 text-sm bg-red-400/10 px-3 py-2 rounded-lg"></div>
          <button type="submit" class="btn-primary w-full">注 册</button>
          <p class="text-center text-sm text-dark-500">
            已有账号？<a href="/login" onclick="event.preventDefault();navigate('/login')" class="text-primary-400 hover:text-primary-300">立即登录</a>
          </p>
        </form>
      </div>
    </div>`;

  // Password strength
  const pwdInput = $('input[name="password"]');
  pwdInput.addEventListener('input', () => {
    const v = pwdInput.value;
    let score = 0;
    if (v.length >= 6) score++;
    if (v.length >= 10) score++;
    if (/[A-Z]/.test(v)) score++;
    if (/[0-9]/.test(v)) score++;
    if (/[^a-zA-Z0-9]/.test(v)) score++;
    const pct = Math.min(100, score * 20);
    const bar = $('#pwd-strength div');
    bar.style.width = pct + '%';
    bar.className = `h-full transition-all duration-300 rounded-full ${pct < 40 ? 'bg-red-500' : pct < 70 ? 'bg-yellow-500' : 'bg-emerald-500'}`;
  });

  $('#register-form').onsubmit = async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    if (fd.get('password') !== fd.get('password2')) {
      const el = $('#reg-error');
      el.textContent = '两次密码不一致';
      el.classList.remove('hidden');
      return;
    }
    try {
      await register(fd.get('username'), fd.get('password'), fd.get('displayName'));
    } catch (err) {
      const el = $('#reg-error');
      el.textContent = err.message;
      el.classList.remove('hidden');
    }
  };
}

// ---- Sidebar Layout ----
function layoutWithSidebar(contentHtml, activeItem = 'inbox') {
  const unreadBadge = currentUser ? `<span id="sidebar-unread" class="badge badge-unread ml-auto hidden">0</span>` : '';
  return `
    <div class="flex h-screen overflow-hidden">
      <!-- Sidebar -->
      <aside class="w-60 bg-dark-900 border-r border-dark-800 flex flex-col shrink-0">
        <div class="p-4 border-b border-dark-800">
          <h1 class="text-lg font-bold text-dark-100 flex items-center gap-2">📧 MyMail</h1>
        </div>
        <nav class="flex-1 p-3 space-y-1">
          <button onclick="navigate('/compose')" class="btn-primary w-full mb-3 flex items-center justify-center gap-2">
            <span>✏️</span> 写邮件
          </button>
          <div class="sidebar-item ${activeItem === 'inbox' ? 'active' : ''}" onclick="navigate('/inbox')">
            <span>📥</span> 收件箱 ${unreadBadge}
          </div>
          <div class="sidebar-item ${activeItem === 'sent' ? 'active' : ''}" onclick="navigate('/sent')">
            <span>📤</span> 已发送
          </div>
          <div class="sidebar-item ${activeItem === 'drafts' ? 'active' : ''}" onclick="navigate('/drafts')">
            <span>📝</span> 草稿箱
          </div>
          <div class="sidebar-item ${activeItem === 'trash' ? 'active' : ''}" onclick="navigate('/trash')">
            <span>🗑️</span> 垃圾箱
          </div>
          <div class="border-t border-dark-800 my-2"></div>
          <div class="sidebar-item ${activeItem === 'settings' ? 'active' : ''}" onclick="navigate('/settings')">
            <span>⚙️</span> 设置
          </div>
          ${currentUser && currentUser.role === 'admin' ? `
          <div class="sidebar-item ${activeItem === 'admin' ? 'active' : ''}" onclick="navigate('/admin')">
            <span>🔧</span> 管理后台
          </div>` : ''}
        </nav>
        <div class="p-3 border-t border-dark-800">
          <div class="flex items-center gap-3 px-2">
            <div class="w-8 h-8 rounded-full bg-primary-600/30 flex items-center justify-center text-primary-400 text-sm font-bold">
              ${(currentUser?.displayName || currentUser?.username || 'U')[0].toUpperCase()}
            </div>
            <div class="flex-1 min-w-0">
              <div class="text-sm text-dark-200 truncate">${escapeHtml(currentUser?.displayName || currentUser?.username)}</div>
              <div class="text-xs text-dark-500 truncate">${escapeHtml(currentUser?.email)}</div>
            </div>
            <button onclick="logout()" class="text-dark-500 hover:text-red-400 text-sm" title="退出">🚪</button>
          </div>
        </div>
      </aside>

      <!-- Main -->
      <main class="flex-1 flex flex-col overflow-hidden">
        ${contentHtml}
      </main>
    </div>`;
}

// ---- Inbox / Folder Pages ----
let refreshInbox = null;

function renderInbox(app) {
  renderMailList(app, 'INBOX');
}

function renderFolder(app) {
  const folderMap = { '/sent': 'SENT', '/drafts': 'DRAFTS', '/trash': 'TRASH' };
  const folder = folderMap[location.pathname] || 'INBOX';
  renderMailList(app, folder);
}

function renderMailList(app, folder) {
  const folderNames = { INBOX: '收件箱', SENT: '已发送', DRAFTS: '草稿箱', TRASH: '垃圾箱' };
  const folderIcons = { INBOX: '📥', SENT: '📤', DRAFTS: '📝', TRASH: '🗑️' };
  const sidebarKey = { INBOX: 'inbox', SENT: 'sent', DRAFTS: 'drafts', TRASH: 'trash' };

  app.innerHTML = layoutWithSidebar(`
    <div class="flex items-center justify-between px-6 py-4 border-b border-dark-800">
      <div class="flex items-center gap-3">
        <h2 class="text-xl font-semibold text-dark-100">${folderIcons[folder]} ${folderNames[folder]}</h2>
        <span id="folder-total" class="text-sm text-dark-500"></span>
      </div>
      <div class="flex items-center gap-3">
        <input type="text" id="mail-search" class="input-field w-64" placeholder="🔍 搜索邮件...">
        ${folder === 'TRASH' ? '<button onclick="emptyTrash()" class="btn-danger text-sm">清空垃圾箱</button>' : ''}
      </div>
    </div>
    <div id="mail-list" class="flex-1 overflow-y-auto">
      <div class="flex items-center justify-center h-full text-dark-500">加载中...</div>
    </div>
    <div id="mail-pagination" class="flex items-center justify-between px-6 py-3 border-t border-dark-800 text-sm text-dark-400"></div>
  `, sidebarKey[folder]);

  let currentPage = 1;

  async function loadMailList(page = 1) {
    try {
      const search = $('#mail-search')?.value || '';
      const data = await api(`/mail/list?folder=${folder}&page=${page}&limit=20${search ? '&search=' + encodeURIComponent(search) : ''}`);
      currentPage = page;

      const list = $('#mail-list');
      if (!data.messages.length) {
        list.innerHTML = `<div class="flex flex-col items-center justify-center h-full text-dark-500"><div class="text-4xl mb-3">📭</div><div>暂无邮件</div></div>`;
        return;
      }

      list.innerHTML = data.messages.map(msg => `
        <div class="mail-row ${msg.is_read ? '' : 'unread'}" onclick="navigate('/mail/${msg.id}')">
          <input type="checkbox" class="rounded border-dark-600 bg-dark-800 text-primary-500 shrink-0" onclick="event.stopPropagation()">
          <button class="text-lg shrink-0 ${msg.is_starred ? 'text-yellow-400' : 'text-dark-600'}" onclick="event.stopPropagation();toggleStar(${msg.id},this)">⭐</button>
          ${msg.has_attach ? '<span class="text-dark-500 shrink-0">📎</span>' : ''}
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-3">
              <span class="mail-sender text-sm ${msg.is_read ? 'text-dark-400' : 'text-dark-100 font-semibold'} truncate w-32">
                ${folder === 'SENT' || folder === 'DRAFTS' ? escapeHtml(msg.to_addr) : escapeHtml(msg.from_name || msg.from_addr)}
              </span>
              <span class="text-sm ${msg.is_read ? 'text-dark-400' : 'text-dark-200'} truncate flex-1">${escapeHtml(msg.subject || '(无主题)')}</span>
            </div>
          </div>
          <span class="text-xs text-dark-500 shrink-0">${formatDate(msg.received_at)}</span>
        </div>
      `).join('');

      // Pagination
      const totalPages = Math.ceil(data.total / data.limit);
      const pag = $('#mail-pagination');
      pag.innerHTML = `
        <span>共 ${data.total} 封</span>
        <div class="flex gap-1">
          ${page > 1 ? `<button onclick="loadMailList(${page - 1})" class="btn-ghost text-sm">◀ 上一页</button>` : ''}
          <span class="px-3 py-1 text-dark-300">${page} / ${totalPages}</span>
          ${page < totalPages ? `<button onclick="loadMailList(${page + 1})" class="btn-ghost text-sm">下一页 ▶</button>` : ''}
        </div>`;

      // Update unread badge
      if (folder === 'INBOX') {
        const badge = $('#sidebar-unread');
        if (badge) {
          const unread = data.unreadCount;
          if (unread > 0) {
            badge.textContent = unread;
            badge.classList.remove('hidden');
          } else {
            badge.classList.add('hidden');
          }
        }
      }
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  // Expose for WebSocket refresh
  refreshInbox = () => loadMailList(currentPage);
  // Expose loadMailList for pagination
  window.loadMailList = loadMailList;

  loadMailList();

  // Search
  let searchTimer;
  const searchInput = $('#mail-search');
  if (searchInput) {
    searchInput.addEventListener('input', () => {
      clearTimeout(searchTimer);
      searchTimer = setTimeout(() => loadMailList(1), 300);
    });
  }
}

async function toggleStar(id, btn) {
  try {
    await api(`/mail/${id}/star`, { method: 'PUT' });
    btn.classList.toggle('text-yellow-400');
    btn.classList.toggle('text-dark-600');
  } catch {}
}

async function emptyTrash() {
  if (!confirm('确定清空垃圾箱？此操作不可恢复')) return;
  try {
    await api('/mail/empty-trash', { method: 'POST' });
    toast('垃圾箱已清空', 'success');
    render();
  } catch (err) {
    toast(err.message, 'error');
  }
}

// ---- Compose Page ----
function renderCompose(app, params) {
  app.innerHTML = layoutWithSidebar(`
    <div class="flex items-center justify-between px-6 py-4 border-b border-dark-800">
      <h2 class="text-xl font-semibold text-dark-100">✏️ 写邮件</h2>
      <div class="flex gap-2">
        <button onclick="navigate('/inbox')" class="btn-ghost text-sm">← 返回</button>
      </div>
    </div>
    <div class="flex-1 overflow-y-auto p-6">
      <form id="compose-form" class="max-w-3xl mx-auto space-y-4">
        <div>
          <label class="block text-sm text-dark-400 mb-1.5">收件人</label>
          <div id="to-tags" class="flex flex-wrap gap-2 input-field min-h-[42px] items-center">
            <input type="text" id="to-input" class="bg-transparent outline-none flex-1 min-w-[120px] text-dark-100" placeholder="输入邮箱后回车">
          </div>
        </div>
        <div>
          <label class="block text-sm text-dark-400 mb-1.5 cursor-pointer" onclick="$('#cc-bcc').classList.toggle('hidden')">抄送/密送 ▾</label>
          <div id="cc-bcc" class="hidden space-y-3">
            <input type="text" name="cc" class="input-field" placeholder="抄送 (多个用逗号分隔)">
            <input type="text" name="bcc" class="input-field" placeholder="密送 (多个用逗号分隔)">
          </div>
        </div>
        <div>
          <label class="block text-sm text-dark-400 mb-1.5">主题</label>
          <input type="text" name="subject" class="input-field" placeholder="邮件主题">
        </div>
        <div>
          <label class="block text-sm text-dark-400 mb-1.5">正文</label>
          <div id="editor-container" class="bg-dark-900 border border-dark-700 rounded-lg overflow-hidden">
            <div id="quill-toolbar"></div>
            <div id="quill-editor" style="min-height:200px"></div>
          </div>
        </div>
        <div>
          <label class="block text-sm text-dark-400 mb-1.5">📎 附件</label>
          <div id="drop-zone" class="border-2 border-dashed border-dark-700 rounded-lg p-6 text-center text-dark-500 hover:border-primary-500/50 transition-colors cursor-pointer">
            <div class="text-2xl mb-2">📁</div>
            <div>拖拽文件到此处，或 <span class="text-primary-400">点击选择</span></div>
            <div class="text-xs mt-1">支持: zip, rar, pdf, doc, xls, jpg, png (最大 25MB)</div>
            <input type="file" id="file-input" multiple class="hidden">
          </div>
          <div id="file-list" class="flex flex-wrap gap-2 mt-2"></div>
        </div>
        <div class="flex items-center justify-between pt-2">
          <div class="flex items-center gap-2">
            <label class="flex items-center gap-2 text-sm text-dark-400">
              <input type="checkbox" name="useSignature" checked class="rounded border-dark-600 bg-dark-800 text-primary-500">
              附加签名
            </label>
          </div>
          <div class="flex gap-2">
            <button type="button" onclick="saveDraft()" class="btn-secondary text-sm">💾 存草稿</button>
            <button type="submit" class="btn-primary">📤 发送</button>
          </div>
        </div>
      </form>
    </div>
  `, '');

  // Initialize Quill editor
  const quill = new Quill('#quill-editor', {
    theme: 'snow',
    placeholder: '写点什么...',
    modules: {
      toolbar: [
        ['bold', 'italic', 'underline', 'strike'],
        [{ 'size': ['small', false, 'large', 'huge'] }],
        [{ 'color': [] }, { 'background': [] }],
        [{ 'align': [] }],
        ['blockquote', 'code-block'],
        [{ 'list': 'ordered' }, { 'list': 'bullet' }],
        ['link', 'clean'],
      ],
    },
  });

  // File handling
  const files = [];
  const dropZone = $('#drop-zone');
  const fileInput = $('#file-input');
  const fileList = $('#file-list');

  dropZone.onclick = () => fileInput.click();
  fileInput.onchange = () => { for (const f of fileInput.files) addFile(f); };
  dropZone.ondragover = (e) => { e.preventDefault(); dropZone.classList.add('border-primary-500'); };
  dropZone.ondragleave = () => dropZone.classList.remove('border-primary-500');
  dropZone.ondrop = (e) => {
    e.preventDefault();
    dropZone.classList.remove('border-primary-500');
    for (const f of e.dataTransfer.files) addFile(f);
  };

  function addFile(file) {
    if (file.size > 25 * 1024 * 1024) {
      toast(`${file.name} 超过 25MB 限制`, 'error');
      return;
    }
    files.push(file);
    renderFiles();
  }

  function renderFiles() {
    fileList.innerHTML = files.map((f, i) => `
      <div class="flex items-center gap-2 bg-dark-800 border border-dark-700 rounded-lg px-3 py-2 text-sm">
        <span>📎</span>
        <span class="text-dark-200 truncate max-w-[150px]">${escapeHtml(f.name)}</span>
        <span class="text-dark-500">${(f.size / 1024 / 1024).toFixed(1)}MB</span>
        <button onclick="removeFile(${i})" class="text-dark-500 hover:text-red-400">✕</button>
      </div>
    `).join('');
  }

  window.removeFile = (i) => { files.splice(i, 1); renderFiles(); };

  // To recipients
  const toRecipients = [];
  const toInput = $('#to-input');
  toInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      const val = toInput.value.trim().replace(/,$/, '');
      if (val && /^[^\s@]+@[^\s@]+$/.test(val)) {
        toRecipients.push(val);
        toInput.value = '';
        renderToTags();
      } else if (val) {
        toast('邮箱格式不正确', 'error');
      }
    }
  });

  function renderToTags() {
    const container = $('#to-tags');
    const tags = container.querySelectorAll('.tag').forEach(t => t.remove());
    toRecipients.forEach((addr, i) => {
      const tag = document.createElement('span');
      tag.className = 'tag flex items-center gap-1 bg-primary-600/20 text-primary-300 px-2 py-0.5 rounded text-sm';
      tag.innerHTML = `${escapeHtml(addr)} <button onclick="removeTo(${i})" class="hover:text-red-400">✕</button>`;
      container.insertBefore(tag, toInput);
    });
  }

  window.removeTo = (i) => { toRecipients.splice(i, 1); renderToTags(); };

  // Auto-save draft
  let autoSaveTimer;
  autoSaveTimer = setInterval(() => {
    if (quill.getLength() > 1 || toRecipients.length > 0) {
      saveDraft(true);
    }
  }, 30000);
  // Keyboard shortcuts (named for cleanup)
  function _composeKeyHandler(e) {
    if (e.ctrlKey && e.key === 'Enter') {
      const form = $('#compose-form');
      if (form) form.dispatchEvent(new Event('submit'));
    }
    if (e.ctrlKey && e.key === 's') {
      e.preventDefault();
      saveDraft();
    }
  }
  document.addEventListener('keydown', _composeKeyHandler);

  // F1: register cleanup for timers + listeners
  _cleanups.push(() => { clearInterval(autoSaveTimer); document.removeEventListener('keydown', _composeKeyHandler); });

  // Save draft
  window.saveDraft = async (silent = false) => {
    try {
      await api('/mail/save-draft', {
        method: 'POST',
        body: {
          to: toRecipients.join(', '),
          cc: $('input[name="cc"]')?.value,
          bcc: $('input[name="bcc"]')?.value,
          subject: $('input[name="subject"]')?.value,
          bodyHtml: quill.root.innerHTML,
          bodyText: quill.getText(),
        },
      });
      if (!silent) toast('草稿已保存', 'success');
    } catch {}
  };

  // Send
  $('#compose-form').onsubmit = async (e) => {
    e.preventDefault();
    if (toRecipients.length === 0) {
      toast('请添加收件人', 'error');
      return;
    }

    const fd = new FormData();
    fd.append('to', toRecipients.join(', '));
    fd.append('cc', $('input[name="cc"]')?.value || '');
    fd.append('bcc', $('input[name="bcc"]')?.value || '');
    fd.append('subject', $('input[name="subject"]')?.value || '');
    fd.append('bodyHtml', quill.root.innerHTML);
    fd.append('bodyText', quill.getText());
    for (const f of files) fd.append('attachments', f);

    try {
      await api('/mail/send', { method: 'POST', body: fd });
      toast('发送成功！', 'success');
      clearInterval(autoSaveTimer);
      navigate('/sent');
    } catch (err) {
      toast(err.message, 'error');
    }
  };

}

// ---- Mail Detail Page ----
function renderMailDetail(app, { id }) {
  app.innerHTML = layoutWithSidebar(`
    <div class="flex items-center justify-between px-6 py-4 border-b border-dark-800">
      <button onclick="history.back()" class="btn-ghost text-sm">← 返回</button>
      <div class="flex gap-2">
        <button onclick="replyMail(${id})" class="btn-ghost text-sm">↩ 回复</button>
        <button onclick="forwardMail(${id})" class="btn-ghost text-sm">↪ 转发</button>
        <button onclick="deleteMail(${id})" class="btn-danger text-sm">🗑 删除</button>
      </div>
    </div>
    <div id="mail-content" class="flex-1 overflow-y-auto p-6">
      <div class="flex items-center justify-center h-full text-dark-500">加载中...</div>
    </div>
  `, '');

  loadMailDetail(id);
}

async function loadMailDetail(id) {
  try {
    const msg = await api(`/mail/${id}`);
    const container = $('#mail-content');

    container.innerHTML = `
      <div class="max-w-3xl mx-auto">
        <h1 class="text-xl font-bold text-dark-100 mb-6">${escapeHtml(msg.subject || '(无主题)')}</h1>
        <div class="card p-4 mb-6">
          <div class="flex items-center gap-3 mb-2">
            <div class="w-10 h-10 rounded-full bg-primary-600/30 flex items-center justify-center text-primary-400 font-bold">
              ${(msg.from_name || msg.from_addr || '?')[0].toUpperCase()}
            </div>
            <div>
              <div class="text-dark-100 font-medium">${escapeHtml(msg.from_name || msg.from_addr)}</div>
              <div class="text-dark-500 text-sm">&lt;${escapeHtml(msg.from_addr)}&gt;</div>
            </div>
            <div class="ml-auto text-dark-500 text-sm">${new Date(msg.received_at).toLocaleString('zh-CN')}</div>
          </div>
          <div class="text-sm text-dark-400">
            致: ${escapeHtml(msg.to_addr)}${msg.cc_addr ? ' | 抄送: ' + escapeHtml(msg.cc_addr) : ''}
          </div>
        </div>

        <div class="prose prose-invert max-w-none mb-6" id="mail-body">
          ${msg.body_html || '<pre class="text-dark-300 whitespace-pre-wrap">' + escapeHtml(msg.body_text) + '</pre>'}
        </div>

        ${msg.attachments && msg.attachments.length > 0 ? `
          <div class="card p-4">
            <div class="flex items-center justify-between mb-3">
              <h3 class="text-dark-200 font-medium">📎 附件 (${msg.attachments.length})</h3>
              <a href="/api/mail/${id}/attachments/download-all" class="text-primary-400 hover:text-primary-300 text-sm">下载全部</a>
            </div>
            <div class="space-y-2">
              ${msg.attachments.map(att => `
                <div class="flex items-center gap-3 p-2 rounded-lg hover:bg-dark-800">
                  <span class="text-xl">${getFileIcon(att.mime_type)}</span>
                  <div class="flex-1 min-w-0">
                    <div class="text-dark-200 text-sm truncate">${escapeHtml(att.filename)}</div>
                    <div class="text-dark-500 text-xs">${(att.size_bytes / 1024 / 1024).toFixed(2)} MB</div>
                  </div>
                  <a href="/api/mail/${id}/attachments/${att.id}/download" class="btn-ghost text-sm">下载</a>
                </div>
              `).join('')}
            </div>
          </div>
        ` : ''}
      </div>`;
  } catch (err) {
    toast(err.message, 'error');
  }
}

function getFileIcon(mime) {
  if (!mime) return '📄';
  if (mime.includes('zip') || mime.includes('rar') || mime.includes('7z')) return '📦';
  if (mime.includes('pdf')) return '📕';
  if (mime.includes('image')) return '🖼️';
  if (mime.includes('word') || mime.includes('document')) return '📘';
  if (mime.includes('excel') || mime.includes('sheet')) return '📗';
  return '📄';
}

async function deleteMail(id) {
  if (!confirm('确定删除？')) return;
  try {
    await api(`/mail/${id}`, { method: 'DELETE' });
    toast('已删除', 'success');
    navigate('/inbox');
  } catch (err) {
    toast(err.message, 'error');
  }
}

async function replyMail(id) {
  try {
    const msg = await api(`/mail/${id}`);
    navigate('/compose');
    setTimeout(() => {
      const toInput = $('#to-input');
      if (toInput) {
        toInput.value = msg.from_addr;
        toInput.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
      }
      const subject = $('input[name="subject"]');
      if (subject) subject.value = 'Re: ' + (msg.subject || '');
      // F2: insert quoted original
      const quill = Quill.find($('#quill-editor'));
      if (quill) {
        const quoteDate = msg.received_at ? new Date(msg.received_at).toLocaleString('zh-CN') : '';
        const quoted = `<br><br><blockquote style="border-left:3px solid #555;padding-left:10px;color:#888"><p>在 ${escapeHtml(quoteDate)}，${escapeHtml(msg.from_name || msg.from_addr)} &lt;${escapeHtml(msg.from_addr)}&gt; 写道：</p>${msg.body_html || escapeHtml(msg.body_text || '')}</blockquote>`;
        quill.root.innerHTML = quoted;
      }
    }, 150);
  } catch {}
}

async function forwardMail(id) {
  try {
    const msg = await api(`/mail/${id}`);
    navigate('/compose');
    setTimeout(() => {
      const subject = $('input[name="subject"]');
      if (subject) subject.value = 'Fwd: ' + (msg.subject || '');
      // F2: insert forwarded content
      const quill = Quill.find($('#quill-editor'));
      if (quill) {
        const quoteDate = msg.received_at ? new Date(msg.received_at).toLocaleString('zh-CN') : '';
        const quoted = `<br><br><p>---------- 转发邮件 ----------</p><p>发件人: ${escapeHtml(msg.from_name || msg.from_addr)} &lt;${escapeHtml(msg.from_addr)}&gt;</p><p>日期: ${escapeHtml(quoteDate)}</p><p>收件人: ${escapeHtml(msg.to_addr)}</p><p>主题: ${escapeHtml(msg.subject || '')}</p><br>${msg.body_html || escapeHtml(msg.body_text || '')}`;
        quill.root.innerHTML = quoted;
      }
    }, 150);
  } catch {}
}

// ---- Settings Page ----
function renderSettings(app) {
  app.innerHTML = layoutWithSidebar(`
    <div class="flex items-center justify-between px-6 py-4 border-b border-dark-800">
      <h2 class="text-xl font-semibold text-dark-100">⚙️ 设置</h2>
      <button onclick="navigate('/inbox')" class="btn-ghost text-sm">← 返回</button>
    </div>
    <div class="flex-1 overflow-y-auto p-6">
      <div class="max-w-2xl mx-auto space-y-6">
        <!-- Profile -->
        <div class="card p-6">
          <h3 class="text-dark-100 font-medium mb-4">👤 个人信息</h3>
          <form id="profile-form" class="space-y-4">
            <div>
              <label class="block text-sm text-dark-400 mb-1.5">显示名称</label>
              <input type="text" name="displayName" class="input-field" value="${escapeHtml(currentUser?.displayName || '')}">
            </div>
            <div>
              <label class="block text-sm text-dark-400 mb-1.5">邮箱</label>
              <input type="text" class="input-field bg-dark-800" value="${escapeHtml(currentUser?.email)}" disabled>
            </div>
            <button type="submit" class="btn-primary">保存</button>
          </form>
        </div>

        <!-- Password -->
        <div class="card p-6">
          <h3 class="text-dark-100 font-medium mb-4">🔑 修改密码</h3>
          <form id="password-form" class="space-y-4">
            <div>
              <label class="block text-sm text-dark-400 mb-1.5">当前密码</label>
              <input type="password" name="currentPassword" class="input-field" required>
            </div>
            <div>
              <label class="block text-sm text-dark-400 mb-1.5">新密码</label>
              <input type="password" name="newPassword" class="input-field" minlength="6" required>
            </div>
            <button type="submit" class="btn-primary">修改密码</button>
          </form>
        </div>

        <!-- IMAP Config -->
        <div class="card p-6">
          <h3 class="text-dark-100 font-medium mb-4">📧 客户端配置 (IMAP/SMTP)</h3>
          <div class="space-y-3 text-sm">
            <div class="flex justify-between p-3 bg-dark-800 rounded-lg">
              <span class="text-dark-400">收件服务器 (IMAP)</span>
              <span class="text-dark-200 font-mono">mail.${getDomain()}:993 (SSL)</span>
            </div>
            <div class="flex justify-between p-3 bg-dark-800 rounded-lg">
              <span class="text-dark-400">发件服务器 (SMTP)</span>
              <span class="text-dark-200 font-mono">mail.${getDomain()}:465 (SSL)</span>
            </div>
            <div class="flex justify-between p-3 bg-dark-800 rounded-lg">
              <span class="text-dark-400">用户名</span>
              <span class="text-dark-200 font-mono">${escapeHtml(currentUser?.email)}</span>
            </div>
            <div class="flex justify-between p-3 bg-dark-800 rounded-lg">
              <span class="text-dark-400">密码</span>
              <span class="text-dark-200">与网页登录密码相同</span>
            </div>
          </div>
        </div>

        <!-- Storage -->
        <div class="card p-6">
          <h3 class="text-dark-100 font-medium mb-4">💾 存储空间</h3>
          <div class="flex items-center gap-4 mb-2">
            <div class="flex-1 h-3 bg-dark-800 rounded-full overflow-hidden">
              <div class="h-full bg-primary-500 rounded-full transition-all" style="width:${Math.min(100, (currentUser?.storageUsed || 0) / (currentUser?.storageLimit || 1) * 100)}%"></div>
            </div>
            <span class="text-dark-400 text-sm">${((currentUser?.storageUsed || 0) / 1024 / 1024).toFixed(1)} MB / ${((currentUser?.storageLimit || 0) / 1024 / 1024).toFixed(0)} MB</span>
          </div>
        </div>
      </div>
    </div>
  `, 'settings');

  $('#profile-form').onsubmit = async (e) => {
    e.preventDefault();
    try {
      await api('/auth/profile', { method: 'PUT', body: { displayName: $('input[name="displayName"]').value } });
      currentUser.displayName = $('input[name="displayName"]').value;
      toast('已保存', 'success');
    } catch (err) { toast(err.message, 'error'); }
  };

  $('#password-form').onsubmit = async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    try {
      await api('/auth/password', { method: 'PUT', body: { currentPassword: fd.get('currentPassword'), newPassword: fd.get('newPassword') } });
      toast('密码已修改', 'success');
      e.target.reset();
    } catch (err) { toast(err.message, 'error'); }
  };
}

// ---- Admin Page ----
function renderAdmin(app) {
  app.innerHTML = layoutWithSidebar(`
    <div class="flex items-center justify-between px-6 py-4 border-b border-dark-800">
      <h2 class="text-xl font-semibold text-dark-100">🔧 管理后台</h2>
      <button onclick="navigate('/inbox')" class="btn-ghost text-sm">← 返回</button>
    </div>
    <div class="flex-1 overflow-y-auto p-6">
      <div class="max-w-4xl mx-auto space-y-6">
        <!-- Stats -->
        <div id="admin-stats" class="grid grid-cols-4 gap-4"></div>

        <!-- Users -->
        <div class="card p-6">
          <div class="flex items-center justify-between mb-4">
            <h3 class="text-dark-100 font-medium">👥 用户管理</h3>
            <button onclick="showCreateUser()" class="btn-primary text-sm">+ 新建用户</button>
          </div>
          <div id="user-list"></div>
        </div>

        <!-- DNS Status -->
        <div class="card p-6">
          <h3 class="text-dark-100 font-medium mb-4">🌐 DNS 配置状态</h3>
          <div id="dns-status" class="space-y-2"></div>
        </div>
      </div>
    </div>
  `, 'admin');

  loadAdminData();
}

async function loadAdminData() {
  try {
    const stats = await api('/admin/stats');
    $('#admin-stats').innerHTML = [
      { label: '用户总数', value: stats.totalUsers, icon: '👥' },
      { label: '活跃用户', value: stats.activeUsers, icon: '✅' },
      { label: '今日发信', value: stats.todaySent, icon: '📤' },
      { label: '今日收信', value: stats.todayReceived, icon: '📥' },
    ].map(s => `
      <div class="card p-4 text-center">
        <div class="text-2xl mb-1">${s.icon}</div>
        <div class="text-2xl font-bold text-dark-100">${s.value}</div>
        <div class="text-sm text-dark-500">${s.label}</div>
      </div>
    `).join('');

    const users = await api('/admin/users');
    $('#user-list').innerHTML = `
      <table class="w-full text-sm">
        <thead><tr class="text-dark-400 border-b border-dark-800">
          <th class="text-left py-2">用户名</th><th class="text-left py-2">邮箱</th><th class="text-left py-2">状态</th><th class="text-left py-2">存储</th>
        </tr></thead>
        <tbody>${users.users.map(u => `
          <tr class="border-b border-dark-800/50">
            <td class="py-2 text-dark-200">${escapeHtml(u.username)}</td>
            <td class="py-2 text-dark-400">${escapeHtml(u.email)}</td>
            <td class="py-2">${u.is_active ? '<span class="text-emerald-400">✅ 启用</span>' : '<span class="text-red-400">❌ 禁用</span>'}</td>
            <td class="py-2 text-dark-400">${(u.storage_used / 1024 / 1024).toFixed(1)}MB</td>
          </tr>
        `).join('')}</tbody>
      </table>`;

    // DNS status
    try {
      const dns = await api('/admin/dns-status');
      $('#dns-status').innerHTML = [
        { label: 'MX 记录', ok: dns.mx === 'ok' },
        { label: 'SPF 记录', ok: dns.spf === 'ok' },
      ].map(d => `
        <div class="flex items-center gap-2 p-2">
          <span>${d.ok ? '✅' : '⚠️'}</span>
          <span class="text-dark-300">${d.label}</span>
          <span class="text-dark-500 text-sm">${d.ok ? '已配置' : '未配置'}</span>
        </div>
      `).join('');
    } catch {}
  } catch (err) {
    toast(err.message, 'error');
  }
}

function showCreateUser() {
  const modal = document.createElement('div');
  modal.className = 'fixed inset-0 bg-black/60 flex items-center justify-center z-50';
  modal.innerHTML = `
    <div class="card p-6 w-full max-w-md">
      <h3 class="text-dark-100 font-medium mb-4">新建用户</h3>
      <form id="create-user-form" class="space-y-4">
        <input type="text" name="username" class="input-field" placeholder="用户名" required>
        <input type="password" name="password" class="input-field" placeholder="密码" required>
        <input type="text" name="displayName" class="input-field" placeholder="显示名称">
        <div class="flex gap-2 justify-end">
          <button type="button" onclick="this.closest('.fixed').remove()" class="btn-secondary">取消</button>
          <button type="submit" class="btn-primary">创建</button>
        </div>
      </form>
    </div>`;
  document.body.appendChild(modal);
  modal.querySelector('form').onsubmit = async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    try {
      await api('/admin/users', { method: 'POST', body: { username: fd.get('username'), password: fd.get('password'), displayName: fd.get('displayName') } });
      toast('用户创建成功', 'success');
      modal.remove();
      loadAdminData();
    } catch (err) { toast(err.message, 'error'); }
  };
}

// ---- Password toggle ----
function togglePassword(btn) {
  const input = btn.previousElementSibling;
  input.type = input.type === 'password' ? 'text' : 'password';
}

// ---- Init ----
render();
