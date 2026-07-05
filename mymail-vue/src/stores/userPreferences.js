// userPreferences.js 用户偏好管理
// 登录时拉取后端 preferences，修改时同步到后端
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useThemeStore } from './theme'
import * as api from '@/api'

export const useUserPreferencesStore = defineStore('userPreferences', () => {
  const theme = useThemeStore()

  const preferences = ref({
    theme: 'dark',
    language: 'zh-CN',
    notify_email: true,
    notify_web: true,
  })

  const loaded = ref(false)

  // 从后端加载 preferences
  async function load() {
    try {
      const data = await api.getMe()
      if (data.preferences) {
        const parsed = typeof data.preferences === 'string'
          ? JSON.parse(data.preferences)
          : data.preferences
        preferences.value = { ...preferences.value, ...parsed }
        // 同步主题
        if (parsed.theme) theme.setMode(parsed.theme)
      }
      loaded.value = true
    } catch (e) {
      console.warn('[userPreferences] 加载失败:', e)
    }
  }

  // 保存单个偏好项
  async function set(key, value) {
    preferences.value[key] = value
    // 主题变化时本地立即生效
    if (key === 'theme') theme.setMode(value)
    // 同步到后端
    try {
      await api.updateProfile({ preferences: JSON.stringify(preferences.value) })
    } catch (e) {
      console.warn('[userPreferences] 保存失败:', e)
    }
  }

  // 批量保存
  async function saveAll() {
    try {
      await api.updateProfile({ preferences: JSON.stringify(preferences.value) })
    } catch (e) {
      console.warn('[userPreferences] 保存失败:', e)
    }
  }

  return { preferences, loaded, load, set, saveAll }
})
