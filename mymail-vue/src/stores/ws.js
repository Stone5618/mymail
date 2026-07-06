import { defineStore } from 'pinia'
import { ref, onUnmounted } from 'vue'
import { useAuthStore } from './auth'

export const useWsStore = defineStore('ws', () => {
  const ws = ref(null)
  const unreadCount = ref(0)
  const connected = ref(false)
  let reconnectTimer = null
  let pollTimer = null
  let reconnectAttempts = 0
  const maxReconnectAttempts = 12 // 达到后改为纯轮询，避免性能损耗

  function connect() {
    const auth = useAuthStore()
    if (!auth.token) return
    if (document.hidden) return // 页面不可见时暂停连接
    if (ws.value) ws.value.close()

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    ws.value = new WebSocket(`${proto}//${location.host}/ws?token=${encodeURIComponent(auth.token)}`)

    ws.value.onopen = () => {
      connected.value = true
      reconnectAttempts = 0
    }
    ws.value.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data)
        if (msg.type === 'new_mail') {
          window.dispatchEvent(new CustomEvent('mymail:new-mail', { detail: msg.data }))
        } else if (msg.type === 'queue_complete') {
          window.dispatchEvent(new CustomEvent('mymail:queue-complete', { detail: msg.data }))
        }
      } catch { /* ignore non-JSON messages (ping/pong, empty) */ }
    }
    ws.value.onclose = () => {
      connected.value = false
      scheduleReconnect()
    }
    ws.value.onerror = () => {
      // 错误处理统一在 onclose 中执行重连
      connected.value = false
    }
  }

  function scheduleReconnect() {
    const auth = useAuthStore()
    if (!auth.token) return
    clearTimeout(reconnectTimer)
    if (document.hidden) return
    if (reconnectAttempts >= maxReconnectAttempts) {
      // 长期连不上时放弃 WebSocket，依赖轮询获取未读数
      return
    }
    reconnectAttempts++
    const delay = Math.min(5000 * Math.pow(2, reconnectAttempts - 1), 60000)
    reconnectTimer = setTimeout(connect, delay)
  }

  function disconnect() {
    clearTimeout(reconnectTimer)
    clearInterval(pollTimer)
    if (ws.value) { ws.value.close(); ws.value = null }
    connected.value = false
    reconnectAttempts = 0
  }

  // 未读轮询（兜底 WebSocket）
  async function startPolling() {
    const { getMailList } = await import('@/api')
    stopPolling()
    await refreshUnread(getMailList)
    pollTimer = setInterval(() => refreshUnread(getMailList), 10000)
  }

  function stopPolling() {
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  }

  async function refreshUnread(getMailList) {
    try {
      const data = await getMailList({ folder: 'INBOX', limit: 1 })
      unreadCount.value = data.unreadCount || 0
    } catch {}
  }

  return { ws, unreadCount, connected, connect, disconnect, startPolling, stopPolling }
})
