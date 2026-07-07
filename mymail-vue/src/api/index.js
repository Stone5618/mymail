const BASE = '/api'

// P1-15：延迟导入 router，避免循环依赖（api ← router ← views ← api）
let _router = null
async function getRouter() {
  if (!_router) {
    const mod = await import('@/router')
    _router = mod.default
  }
  return _router
}

export function setToken(t) {
  if (t) localStorage.setItem('token', t)
  else localStorage.removeItem('token')
}

export function getToken() {
  return localStorage.getItem('token')
}

async function request(path, options = {}) {
  const headers = { ...options.headers }
  const token = getToken()
  if (token) headers['Authorization'] = `Bearer ${token}`

  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
    if (options.body && typeof options.body === 'object') {
      options.body = JSON.stringify(options.body)
    }
  }

  const res = await fetch(BASE + path, { ...options, headers })

  if (res.status === 401) {
    setToken(null)
    localStorage.removeItem('role')
    // P1-15：用 router.replace 跳转（无页面刷新），延迟导入避免循环依赖
    const router = await getRouter()
    router.replace({ name: 'login' })
    throw new Error('请重新登录')
  }

  const raw = await res.text()
  let data = {}
  if (raw) {
    try { data = JSON.parse(raw) }
    catch (e) {
      throw new Error('服务器响应异常: ' + e.message)
    }
  }
  if (!res.ok) throw new Error(data.error || '请求失败')
  return data
}

// ===== Auth =====
export const login = (email, password, remember) =>
  request('/auth/login', { method: 'POST', body: { email, password, remember } })

export const register = (username, password, displayName) =>
  request('/auth/register', { method: 'POST', body: { username, password, displayName } })

export const getMe = () => request('/auth/me')

export const updateProfile = (data) =>
  request('/auth/profile', { method: 'PUT', body: data })

export const changePassword = (currentPw, newPw) =>
  request('/auth/password', { method: 'PUT', body: { currentPassword: currentPw, newPassword: newPw } })

export const changeDefaultPassword = (newPw) =>
  request('/auth/change-default-password', { method: 'POST', body: { newPassword: newPw } })

export const uploadAvatar = (formData) =>
  request('/auth/avatar', { method: 'POST', body: formData })

// Mail
export const getMailList = (params = {}) => {
  // 过滤掉 undefined/null 值，避免 URLSearchParams 将其转为 "undefined" 字符串
  const filtered = Object.fromEntries(Object.entries(params).filter(([, v]) => v != null && v !== ''))
  const qs = new URLSearchParams(filtered).toString()
  return request(`/mail/list?${qs}`)
}

export const getMail = (id) => request(`/mail/${id}`)

export const sendMail = (formData) =>
  request('/mail/send', { method: 'POST', body: formData })

export const saveDraft = (data) =>
  request('/mail/save-draft', { method: 'POST', body: data })

export const deleteMail = (id) =>
  request(`/mail/${id}`, { method: 'DELETE' })

export const markRead = (id) =>
  request(`/mail/${id}/read`, { method: 'PUT' })

export const markUnread = (id) =>
  request(`/mail/${id}/unread`, { method: 'PUT' })

export const getUnreadCount = () =>
  request('/mail/unread-count')

export const restoreMail = (id) =>
  request(`/mail/${id}/restore`, { method: 'PUT' })

export const emptyTrash = () =>
  request('/mail/empty-trash', { method: 'POST' })

export const moveToFolder = (id, folder) =>
  request('/mail/batch/move', { method: 'POST', body: { ids: [id], folder } })

// Star & Attachments
export const toggleStar = (id) =>
  request(`/mail/${id}/star`, { method: 'PUT' })

export const getAttachmentUrl = (msgId, attId) =>
  `/api/mail/${msgId}/attachments/${attId}/download`

export const getDownloadAllUrl = (msgId) =>
  `/api/mail/${msgId}/attachments/download-all`

// 带 JWT 的附件下载：fetch → blob → 触发浏览器下载
// 原生 <a href> 无法携带 Authorization 头，会被后端 401 拦截并跳登录，故改用此方式
async function downloadBlob(path, fallbackName) {
  const token = getToken()
  const res = await fetch(BASE + path, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (res.status === 401) {
    setToken(null)
    localStorage.removeItem('role')
    const router = await getRouter()
    router.replace({ name: 'login' })
    throw new Error('请重新登录')
  }
  if (!res.ok) throw new Error('下载失败')
  // 优先使用响应头中的文件名
  let filename = fallbackName || 'download'
  const disp = res.headers.get('Content-Disposition')
  if (disp) {
    const m = /filename="?([^"]+)"?/.exec(disp)
    if (m && m[1]) filename = decodeURIComponent(m[1])
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export const downloadAttachment = (msgId, attId, filename) =>
  downloadBlob(`/mail/${msgId}/attachments/${attId}/download`, filename)

export const downloadAllAttachments = (msgId) =>
  downloadBlob(`/mail/${msgId}/attachments/download-all`, 'attachments.zip')

// Batch
export const batchDelete = (ids) =>
  request('/mail/batch/delete', { method: 'POST', body: { ids } })

export const batchMarkRead = (ids) =>
  request('/mail/batch/mark-read', { method: 'POST', body: { ids } })

export const batchMove = (ids, folder) =>
  request('/mail/batch/move', { method: 'POST', body: { ids, folder } })

// Admin
export const getUsers = () => request('/admin/users')
export const getUser = (id) => request(`/admin/users/${id}`)
export const deleteUser = (id) => request(`/admin/users/${id}`, { method: 'DELETE' })
export const disableUser = (id) => request(`/admin/users/${id}`, { method: 'PUT', body: { isActive: false } })
export const enableUser = (id) => request(`/admin/users/${id}`, { method: 'PUT', body: { isActive: true } })
export const resetPassword = (id, password) => request(`/admin/users/${id}`, { method: 'PUT', body: { password } })
export const createUser = (data) => request('/admin/users', { method: 'POST', body: data })
export const updateUser = (id, data) => request(`/admin/users/${id}`, { method: 'PUT', body: data })
export const checkDns = () => request('/admin/dns-status')
export const getStats = () => request('/admin/stats')
export const getSystemConfig = () => request('/admin/config')
export const resanitizeAllMails = () => request('/admin/maintenance/resanitize', { method: 'POST', body: { confirm: true } })

// ===== 审计日志（AC-6） =====
export const getAuditLogs = (params) => {
  const query = new URLSearchParams()
  if (params?.page) query.set('page', params.page)
  if (params?.page_size) query.set('page_size', params.page_size)
  if (params?.actor_type) query.set('actor_type', params.actor_type)
  if (params?.action) query.set('action', params.action)
  if (params?.result) query.set('result', params.result)
  if (params?.start) query.set('start', params.start)
  if (params?.end) query.set('end', params.end)
  const qs = query.toString()
  return request(qs ? `/admin/audit-logs?${qs}` : '/admin/audit-logs')
}

// ===== 邮件列表管理（AC-9） =====
export const getAdminMails = (params) => {
  const query = new URLSearchParams()
  if (params?.page) query.set('page', params.page)
  if (params?.page_size) query.set('page_size', params.page_size)
  if (params?.user_id) query.set('user_id', params.user_id)
  if (params?.folder) query.set('folder', params.folder)
  if (params?.is_read !== undefined && params?.is_read !== '') query.set('is_read', params.is_read)
  if (params?.start) query.set('start', params.start)
  if (params?.end) query.set('end', params.end)
  const qs = query.toString()
  return request(qs ? `/admin/mails?${qs}` : '/admin/mails')
}

// ===== API Key 管理（AC-11） =====
export const listAPIKeys = () => request('/auth/api-keys')
export const createAPIKey = (data) => request('/auth/api-keys', { method: 'POST', body: data })
export const deleteAPIKey = (id) => request(`/auth/api-keys/${id}`, { method: 'DELETE' })

// ===== 邮件规则管理（AC-12） =====
export const listRules = () => request('/rules')
export const createRule = (data) => request('/rules', { method: 'POST', body: data })
export const updateRule = (id, data) => request(`/rules/${id}`, { method: 'PUT', body: data })
export const deleteRule = (id) => request(`/rules/${id}`, { method: 'DELETE' })
