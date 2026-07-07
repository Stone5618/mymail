<template>
  <div class="min-h-screen flex items-center justify-center relative overflow-hidden" style="background-color: var(--c-body-bg)">
    <!-- 动态星空背景 -->
    <div class="absolute inset-0 overflow-hidden">
      <div class="absolute inset-0 bg-gradient-to-br from-primary-600/8 via-transparent to-violet-600/5" />
      <div v-for="i in 30" :key="i" class="star" :style="starStyle(i)" />
    </div>
    <!-- 光晕 -->
    <div class="absolute top-1/4 left-1/4 w-96 h-96 bg-primary-600/5 rounded-full blur-3xl animate-pulse" />
    <div class="absolute bottom-1/4 right-1/4 w-80 h-80 bg-violet-600/5 rounded-full blur-3xl animate-pulse" style="animation-delay: 1s" />

    <!-- 卡片 -->
    <div class="relative z-10 w-full max-w-md mx-4 p-6 sm:p-8 rounded-2xl shadow-2xl bg-dark-900/80 backdrop-blur-xl border border-dark-700/50">
      <!-- Header -->
      <div class="flex items-center gap-3 mb-6">
        <div class="w-10 h-10 rounded-full bg-amber-500/15 flex items-center justify-center">
          <BaseIcon name="shield-exclamation" class="h-5 w-5 text-amber-400" />
        </div>
        <div>
          <h2 class="text-lg font-bold text-dark-100">修改默认密码</h2>
          <p class="text-xs text-dark-400">检测到您仍在使用初始密码，请立即修改</p>
        </div>
      </div>

      <!-- Form -->
      <form @submit.prevent="handleSubmit" class="space-y-4">
        <div>
          <label class="block text-sm text-dark-300 mb-1.5">新密码</label>
          <input
            v-model="newPassword"
            type="password"
            autocomplete="new-password"
            required
            minlength="8"
            class="input-field w-full"
            placeholder="至少 8 位"
          />
        </div>

        <div>
          <label class="block text-sm text-dark-300 mb-1.5">确认新密码</label>
          <input
            v-model="confirmPassword"
            type="password"
            autocomplete="new-password"
            required
            minlength="8"
            class="input-field w-full"
            placeholder="再次输入新密码"
          />
          <p v-if="confirmPassword && newPassword !== confirmPassword" class="text-xs text-red-400 mt-1">两次密码不一致</p>
        </div>

        <div v-if="errorMsg" class="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">{{ errorMsg }}</div>

        <button
          type="submit"
          :disabled="loading || !newPassword || newPassword !== confirmPassword"
          class="btn-primary w-full justify-center"
        >
          <BaseSpinner v-if="loading" :size="16" class="mr-2" />
          {{ loading ? '提交中...' : '修改密码并继续' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { changeDefaultPassword } from '@/api'
import { useToast } from '@/composables/useToast'
import BaseIcon from '@/components/BaseIcon.vue'
import BaseSpinner from '@/components/BaseSpinner.vue'

const router = useRouter()
const auth = useAuthStore()
const { toast } = useToast()

const newPassword = ref('')
const confirmPassword = ref('')
const loading = ref(false)
const errorMsg = ref('')

function starStyle(i) {
  const seed = (i * 9301 + 49297) % 233280
  const rnd = seed / 233280
  const size = 1 + rnd * 2
  return {
    width: `${size}px`,
    height: `${size}px`,
    top: `${(i * 37) % 100}%`,
    left: `${(i * 61) % 100}%`,
    animationDelay: `${rnd * 3}s`,
    animationDuration: `${2 + rnd * 3}s`,
  }
}

async function handleSubmit() {
  if (newPassword.value !== confirmPassword.value) {
    errorMsg.value = '两次密码不一致'
    return
  }
  if (newPassword.value.length < 8) {
    errorMsg.value = '密码至少 8 位'
    return
  }
  loading.value = true
  errorMsg.value = ''
  try {
    await changeDefaultPassword(newPassword.value)
    auth.clearRequirePasswordChange()
    toast('密码已修改', 'success')
    router.push({ name: 'home' })
  } catch (e) {
    errorMsg.value = e.message || '修改失败，请重试'
  } finally {
    loading.value = false
  }
}
</script>
