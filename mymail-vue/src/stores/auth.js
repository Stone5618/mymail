import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as api from '@/api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(null)
  const token = ref(localStorage.getItem('token') || null)
  const isAuthenticated = computed(() => !!token.value)

  // P0-5：将 role 同步到 localStorage，供 router 守卫读取（无需访问 pinia 实例）
  function persistRole(role) {
    if (role) localStorage.setItem('role', role)
    else localStorage.removeItem('role')
  }

  async function login(email, password, remember = false) {
    const data = await api.login(email, password, remember)
    token.value = data.token
    user.value = data.user
    api.setToken(data.token)
    persistRole(data.user?.role)
  }

  async function register(username, password, displayName) {
    const data = await api.register(username, password, displayName)
    token.value = data.token
    user.value = data.user
    api.setToken(data.token)
    persistRole(data.user?.role)
  }

  async function fetchMe() {
    if (!token.value) return false
    try {
      user.value = await api.getMe()
      persistRole(user.value?.role)
      return true
    } catch {
      logout()
      return false
    }
  }

  function logout() {
    user.value = null
    token.value = null
    api.setToken(null)
    persistRole(null)
  }

  return { user, token, isAuthenticated, login, register, fetchMe, logout }
})
