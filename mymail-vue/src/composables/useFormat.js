export function useFormat() {
  function formatDate(str) {
    if (!str) return ''
    const d = new Date(str)
    const now = new Date()
    const diff = now - d
    if (diff < 86400000 && d.getDate() === now.getDate()) {
      return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    }
    if (d.getFullYear() === now.getFullYear()) {
      return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
    }
    return d.toLocaleDateString('zh-CN')
  }

  function escapeHtml(str) {
    if (!str) return ''
    return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  }

  return { formatDate, escapeHtml }
}
