// useConfirm.js 全局确认弹窗 composable
// 用法：
//   const { confirm } = useConfirm()
//   if (await confirm({ message: '确定删除？' })) { ... }
import { ref, readonly } from 'vue'

const visible = ref(false)
const opts = ref({})
let resolver = null

function confirm(options = {}) {
  opts.value = {
    title: options.title || '请确认',
    message: options.message || '',
    confirmText: options.confirmText || '确定',
    cancelText: options.cancelText || '取消',
    variant: options.variant || 'danger',
  }
  visible.value = true
  return new Promise(resolve => { resolver = resolve })
}

function resolve(value) {
  visible.value = false
  resolver?.(value)
  resolver = null
}

export function useConfirm() {
  return {
    confirm,
    visible: readonly(visible),
    opts: readonly(opts),
    resolve,
  }
}
