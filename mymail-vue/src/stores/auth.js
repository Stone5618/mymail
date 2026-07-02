import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as api from '@/api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(null)
  const token = ref(localStorage.getItem('token') || null)
  const isAuthenticated = computed(() => !!token.value)

  async function login(email, password, remember = false) {
    const data = await api.login(email, password, remember)
    token.value = data.token
    user.value = data.user
    api.setToken(data.token)
  }

  async function register(username, password, displayName) {
    const data = await api.register(username, password, displayName)
    token.value = data.token
    user.value = data.user
    api.setToken(data.token)
  }

  async function fetchMe() {
    if (!token.value) return false
    try {
      user.value = await api.getMe()
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
  }

  return { user, token, isAuthenticated, login, register, fetchMe, logout }
})
