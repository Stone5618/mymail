/** Toast 通知 composable */
// 使用内联 SVG 图标替代 emoji，确保跨平台一致性。
const ICONS = {
  info: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="h-5 w-5"><path stroke-linecap="round" stroke-linejoin="round" d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" /></svg>',
  success: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="h-5 w-5"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>',
  error: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="h-5 w-5"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" /></svg>',
  warning: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="h-5 w-5"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.008v.008H12v-.008z" /></svg>',
}

const X_ICON = '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="h-4 w-4"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>'

export function useToast() {
  const colors = {
    info: 'border-l-4 border-l-blue-500',
    success: 'border-l-4 border-l-emerald-500',
    error: 'border-l-4 border-l-red-500',
    warning: 'border-l-4 border-l-amber-500',
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

    el.innerHTML = `
      <span class="shrink-0 text-${type === 'info' ? 'blue' : type === 'success' ? 'emerald' : type === 'error' ? 'red' : 'amber'}-400">${ICONS[type] || ICONS.info}</span>
      <span class="text-sm">${escapeHtml(message)}</span>
      <button class="ml-auto text-dark-500 hover:text-dark-300 shrink-0" aria-label="关闭">${X_ICON}</button>
    `

    // 关闭按钮
    const closeBtn = el.querySelector('button')
    closeBtn.addEventListener('click', () => {
      el.classList.add('opacity-0', 'translate-y-4', 'scale-95')
      setTimeout(() => el.remove(), 300)
    })

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
