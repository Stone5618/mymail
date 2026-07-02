const BASE = '/api'

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
    window.location.href = '/login'
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

export const moveToFolder = (id, folder) =>
  request('/mail/batch/move', { method: 'POST', body: { ids: [id], folder } })

// Star & Attachments
export const toggleStar = (id) =>
  request(`/mail/${id}/star`, { method: 'PUT' })

export const getAttachmentUrl = (msgId, attId) =>
  `/api/mail/${msgId}/attachments/${attId}/download`

export const getDownloadAllUrl = (msgId) =>
  `/api/mail/${msgId}/attachments/download-all`

// Upload (独立附件上传)
export const uploadFiles = (formData) => {
  return request('/mail/upload', { method: 'POST', body: formData })
}

export const deleteUpload = (id) =>
  request(`/mail/upload/${id}`, { method: 'DELETE' })

export const getUploadPreviewUrl = (id) => `/api/mail/upload/${id}/preview`

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
export const checkDns = () => request('/admin/dns-status')
export const getStats = () => request('/admin/stats')
