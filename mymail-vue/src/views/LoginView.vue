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
    <div class="relative z-10 flex rounded-2xl shadow-2xl overflow-hidden max-w-4xl w-full mx-4 bg-dark-900/80 backdrop-blur-xl border border-dark-700/50 login-card">
      <!-- 左侧品牌 -->
      <div class="hidden md:flex flex-col justify-center items-center p-12 bg-gradient-to-br from-primary-600 to-violet-600 w-1/2 relative overflow-hidden">
        <div class="absolute inset-0 bg-[radial-gradient(circle_at_30%_50%,rgba(255,255,255,0.1),transparent_70%)]" />
        <div class="relative mb-6 drop-shadow-lg">
          <LogoIcon :size="64" class="text-white" />
        </div>
        <h1 class="relative text-3xl font-bold text-white mb-2">MyMail</h1>
        <p class="relative text-white/70 text-center text-sm">安全、高效的邮件管理</p>
        <div class="relative mt-8 flex gap-2 text-white/50 text-xs">
          <span class="px-2 py-1 rounded-full bg-white/10">自托管</span>
          <span class="px-2 py-1 rounded-full bg-white/10">加密传输</span>
          <span class="px-2 py-1 rounded-full bg-white/10">快速搜索</span>
        </div>
      </div>

      <!-- 右侧表单 -->
      <div class="w-full md:w-1/2 p-6 sm:p-8 flex flex-col justify-center">
        <!-- 移动端 logo -->
        <div class="md:hidden flex items-center gap-2 mb-6">
          <LogoIcon :size="32" class="text-primary-400" />
          <span class="text-xl font-bold text-dark-100">MyMail</span>
        </div>

        <div class="mb-6 sm:mb-8">
          <h2 class="text-xl sm:text-2xl font-bold text-dark-100 mb-1">{{ isLogin ? '登录' : '注册' }}</h2>
          <p class="text-sm text-dark-400">{{ isLogin ? '欢迎回来' : '创建新账户' }}</p>
        </div>

        <!-- 错误提示 -->
        <div v-if="error" class="mb-4 rounded-lg bg-red-500/10 border border-red-500/30 px-4 py-3 text-red-400 text-sm flex items-start gap-2">
          <BaseIcon name="exclamation-triangle" class="h-5 w-5 shrink-0 mt-0.5" />
          <span>{{ error }}</span>
        </div>

        <!-- 登录表单 -->
        <form v-if="isLogin" @submit.prevent="handleLogin" class="space-y-3 sm:space-y-4">
          <div>
            <input type="email" v-model="loginForm.email" placeholder="邮箱地址" class="input-field" required autofocus />
          </div>
          <div class="relative">
            <input :type="showPw ? 'text' : 'password'" v-model="loginForm.password" placeholder="密码" class="input-field pr-10" required />
            <button type="button" @click="showPw = !showPw" class="absolute right-3 top-1/2 -translate-y-1/2 text-dark-500 hover:text-dark-300 transition-colors" :aria-label="showPw ? '隐藏密码' : '显示密码'">
              <BaseIcon :name="showPw ? 'eye-slash' : 'eye'" class="h-5 w-5" />
            </button>
          </div>
          <label class="flex items-center gap-2 text-sm text-dark-400 cursor-pointer">
            <input type="checkbox" v-model="loginForm.remember" class="rounded border-dark-600 bg-dark-800" />
            记住我（30天）
          </label>
          <button type="submit" class="btn-primary w-full flex items-center justify-center gap-2" :disabled="loading">
            <BaseIcon v-if="loading" name="arrow-path" class="h-5 w-5 animate-spin" />
            {{ loading ? '登录中...' : '登录' }}
          </button>
        </form>

        <!-- 注册表单 -->
        <form v-else @submit.prevent="handleRegister" class="space-y-3 sm:space-y-4">
          <input type="text" v-model="registerForm.username" placeholder="用户名（3-20字符）" class="input-field" required autofocus />
          <input type="text" v-model="registerForm.displayName" placeholder="显示名称" class="input-field" required />
          <div class="relative">
            <input :type="showPw ? 'text' : 'password'" v-model="registerForm.password" placeholder="密码（至少6位）" class="input-field" required @input="calcStrength" />
          </div>
          <div class="h-1 rounded-full bg-dark-800 overflow-hidden">
            <div class="h-full rounded-full transition-all duration-500"
              :class="pwdStrength < 40 ? 'bg-red-500' : pwdStrength < 70 ? 'bg-amber-500' : 'bg-emerald-500'"
              :style="{ width: pwdStrength + '%' }"
            />
          </div>
          <div class="flex gap-2 text-xs text-dark-500">
            <span :class="{ 'text-red-400': registerForm.password.length > 0 && registerForm.password.length < 6 }">≥6位</span>
            <span :class="{ 'text-emerald-400': /[A-Z]/.test(registerForm.password) }">大写</span>
            <span :class="{ 'text-emerald-400': /[0-9]/.test(registerForm.password) }">数字</span>
            <span :class="{ 'text-emerald-400': /[^a-zA-Z0-9]/.test(registerForm.password) }">符号</span>
          </div>
          <input :type="showPw ? 'text' : 'password'" v-model="registerForm.password2" placeholder="确认密码" class="input-field" required />
          <button type="submit" class="btn-primary w-full flex items-center justify-center gap-2" :disabled="loading">
            <BaseIcon v-if="loading" name="arrow-path" class="h-5 w-5 animate-spin" />
            {{ loading ? '注册中...' : '注册' }}
          </button>
        </form>

        <div class="mt-6 text-center">
          <button @click="isLogin = !isLogin; error = ''" class="text-sm text-primary-400 hover:text-primary-300 transition-colors">
            {{ isLogin ? '没有账户？注册' : '已有账户？登录' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import * as api from '@/api'
import BaseIcon from '@/components/BaseIcon.vue'
import LogoIcon from '@/components/LogoIcon.vue'

const auth = useAuthStore()
const router = useRouter()

const isLogin = ref(true)
const loading = ref(false)
const error = ref('')
const showPw = ref(false)
const pwdStrength = ref(0)

const loginForm = reactive({ email: '', password: '', remember: false })
const registerForm = reactive({ username: '', password: '', password2: '', displayName: '' })

/** 生成随机星空位置 */
function starStyle(i) {
  const x = ((i * 37 + 13) % 100)
  const y = ((i * 23 + 7) % 100)
  const size = 1 + (i % 3)
  const delay = (i * 0.3) % 5
  const duration = 3 + (i % 4)
  return {
    left: x + '%',
    top: y + '%',
    width: size + 'px',
    height: size + 'px',
    animationDelay: delay + 's',
    animationDuration: duration + 's',
  }
}

async function handleLogin() {
  loading.value = true
  error.value = ''
  try {
    const data = await api.login(loginForm.email, loginForm.password, loginForm.remember)
    auth.token = data.token
    auth.user = data.user
    api.setToken(data.token)
    if (data.requirePasswordChange) {
      router.push({ name: 'settings', query: { changePw: '1' } })
    } else {
      router.push('/inbox')
    }
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function handleRegister() {
  if (registerForm.password !== registerForm.password2) {
    error.value = '两次密码不一致'
    return
  }
  loading.value = true
  error.value = ''
  try {
    const data = await api.register(registerForm.username, registerForm.password, registerForm.displayName)
    auth.token = data.token
    auth.user = data.user
    api.setToken(data.token)
    router.push('/inbox')
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function calcStrength() {
  const v = registerForm.password
  let score = 0
  if (v.length >= 6) score++
  if (v.length >= 10) score++
  if (/[A-Z]/.test(v)) score++
  if (/[0-9]/.test(v)) score++
  if (/[^a-zA-Z0-9]/.test(v)) score++
  pwdStrength.value = Math.min(100, score * 20)
}
</script>

<style scoped>
.star {
  position: absolute;
  border-radius: 50%;
  background: white;
  opacity: 0;
  animation: twinkle ease-in-out infinite;
}
@keyframes twinkle {
  0%, 100% { opacity: 0; }
  50% { opacity: 0.6; }
}
.login-card {
  animation: card-in 0.5s ease-out;
}
@keyframes card-in {
  from { opacity: 0; transform: translateY(20px) scale(0.98); }
  to { opacity: 1; transform: translateY(0) scale(1); }
}
</style>
