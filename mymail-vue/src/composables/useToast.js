/** Toast 通知 composable */
export function useToast() {
  const icons = {
    info: '💡',
    success: '✅',
    error: '❌',
    warning: '⚠️',
  }

  /**
   * 显示 toast
   * @param {string} message - 消息文本
   * @param {'info'|'success'|'error'|'warning'} type - 类型
   */
  function toast(message, type = 'info') {
    let container = document.getElementById('toast-container')
    if (!container) {
      container = document.createElement('div')
      container.id = 'toast-container'
      container.className = 'fixed bottom-4 right-4 z-[9999] flex flex-col-reverse gap-2'
      document.body.appendChild(container)
    }

    const el = document.createElement('div')
    el.className = 'flex items-center gap-3 rounded-xl border border-dark-700/80 bg-dark-800/95 backdrop-blur-md px-4 py-3 text-dark-100 shadow-2xl max-w-sm transition-all duration-300 ease-out opacity-0 translate-y-4 scale-95'

    const colors = {
      info: 'border-l-4 border-l-blue-500',
      success: 'border-l-4 border-l-emerald-500',
      error: 'border-l-4 border-l-red-500',
      warning: 'border-l-4 border-l-amber-500',
    }

    el.innerHTML = `
      <span class="text-lg shrink-0">${icons[type] || '💡'}</span>
      <span class="text-sm">${escapeHtml(message)}</span>
      <button class="ml-auto text-dark-500 hover:text-dark-300 shrink-0" onclick="this.parentElement.remove()">✕</button>
    `

    container.appendChild(el)

    // 触发动画
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        el.classList.remove('opacity-0', 'translate-y-4', 'scale-95')
        const extraClasses = (colors[type] || '').split(' ').filter(Boolean)
        if (extraClasses.length) el.classList.add(...extraClasses)
      })
    })

    // 自动消失
    setTimeout(() => {
      el.classList.add('opacity-0', 'translate-y-4', 'scale-95')
      setTimeout(() => el.remove(), 300)
    }, 3500)
  }

  function escapeHtml(str) {
    const div = document.createElement('div')
    div.textContent = str
    return div.innerHTML
  }

  return { toast }
}
