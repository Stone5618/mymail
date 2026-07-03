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

  // P1-19：用 textContent 实现完整转义（& < > " ' 全部转义）
  // 浏览器 textContent 赋值后 innerHTML 返回完整转义字符串，比正则替换更安全。
  function escapeHtml(str) {
    if (!str) return ''
    const div = document.createElement('div')
    div.textContent = String(str)
    return div.innerHTML
  }

  return { formatDate, escapeHtml }
}
