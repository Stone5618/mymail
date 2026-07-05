// theme.js 主题管理 store
// 支持 dark / light / system 三种模式
import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'

const STORAGE_KEY = 'mymail-theme'

export const useThemeStore = defineStore('theme', () => {
  const mode = ref(localStorage.getItem(STORAGE_KEY) || 'dark') // dark | light | system
  const systemDark = ref(window.matchMedia('(prefers-color-scheme: dark)').matches)

  // 系统主题变化监听
  const mql = window.matchMedia('(prefers-color-scheme: dark)')
  mql.addEventListener('change', (e) => { systemDark.value = e.matches })

  const isDark = computed(() => {
    if (mode.value === 'system') return systemDark.value
    return mode.value === 'dark'
  })

  const actualTheme = computed(() => isDark.value ? 'dark' : 'light')

  // 应用主题到 document
  function applyTheme() {
    document.documentElement.setAttribute('data-theme', actualTheme.value)
  }

  function setMode(m) {
    mode.value = m
    localStorage.setItem(STORAGE_KEY, m)
  }

  // 初始化
  applyTheme()
  watch(actualTheme, applyTheme)

  return { mode, isDark, actualTheme, setMode, applyTheme }
})
