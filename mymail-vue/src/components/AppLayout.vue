<template>
  <div class="flex h-screen overflow-hidden">
    <!-- Mobile overlay -->
    <div v-if="sidebarOpen" class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm lg:hidden" @click="sidebarOpen = false" />

    <!-- Sidebar -->
    <aside
      class="fixed lg:static inset-y-0 left-0 z-50 w-60 bg-dark-900 border-r border-dark-800 flex flex-col shrink-0
             transition-transform duration-300 lg:translate-x-0"
      :class="sidebarOpen ? 'translate-x-0' : '-translate-x-full'"
    >
      <!-- Header -->
      <div class="p-4 border-b border-dark-800 flex items-center justify-between">
        <h1 class="text-lg font-bold text-dark-100 flex items-center gap-2">
          <LogoIcon :size="24" class="text-primary-400" />
          <span>MyMail</span>
        </h1>
        <button @click="sidebarOpen = false" class="lg:hidden text-dark-500 hover:text-dark-300" aria-label="关闭侧边栏">
          <BaseIcon name="x-mark" class="h-5 w-5" />
        </button>
      </div>

      <!-- Compose -->
      <div class="p-3 border-b border-dark-800">
        <button @click="router.push('/compose'); sidebarOpen = false" class="btn-primary w-full flex items-center justify-center gap-2">
          <BaseIcon name="pencil-square" class="h-5 w-5" />
          <span>写邮件</span>
        </button>
      </div>

      <!-- Nav -->
      <nav class="flex-1 p-3 space-y-1 overflow-y-auto">
        <router-link
          v-for="item in navItems" :key="item.to"
          :to="item.to"
          class="sidebar-item group"
          :class="{ active: $route.path === item.to }"
          @click="sidebarOpen = false"
        >
          <BaseIcon :name="item.icon" class="h-5 w-5 shrink-0" :solid="$route.path === item.to" />
          <span class="flex-1">{{ item.label }}</span>
          <span
            v-if="item.badge"
            class="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-primary-600 px-1.5 text-xs font-medium text-white"
          >
            {{ item.badge }}
          </span>
        </router-link>
      </nav>

      <!-- User section -->
      <div class="border-t border-dark-800 p-3">
        <div class="flex items-center gap-3 rounded-lg px-3 py-2 cursor-pointer hover:bg-dark-800 transition-colors group">
          <div class="w-8 h-8 rounded-full bg-primary-600 flex items-center justify-center text-white text-sm font-medium overflow-hidden shrink-0">
            <img v-if="auth.user?.avatarUrl" :src="auth.user.avatarUrl" class="w-full h-full object-cover" alt="" />
            <span v-else>{{ userInitial }}</span>
          </div>
          <div class="flex-1 min-w-0">
            <div class="text-sm text-dark-200 truncate">{{ auth.user?.displayName }}</div>
            <div class="text-xs text-dark-500 truncate">{{ auth.user?.email }}</div>
          </div>
          <button
            @click="handleLogout"
            class="text-dark-400 hover:text-red-400 transition-colors p-1.5 rounded-md hover:bg-dark-700"
            aria-label="退出登录"
            title="退出"
          >
            <BaseIcon name="arrow-left-start-on-rectangle" class="h-5 w-5" />
          </button>
        </div>
      </div>
    </aside>

    <!-- Main -->
    <main class="flex-1 flex flex-col overflow-hidden">
      <!-- Mobile top bar -->
      <div class="lg:hidden flex items-center gap-3 px-4 py-3 border-b border-dark-800 bg-dark-900/80 backdrop-blur-md">
        <button @click="sidebarOpen = true" class="text-dark-300 hover:text-dark-100 p-1" aria-label="打开侧边栏">
          <BaseIcon name="bars-3" class="h-6 w-6" />
        </button>
        <span class="text-lg font-semibold text-dark-100 flex items-center gap-2">
          <LogoIcon :size="20" class="text-primary-400" />
          <span>MyMail</span>
        </span>
        <div class="flex-1" />
        <button @click="router.push('/compose')" class="btn-ghost p-1" aria-label="写邮件">
          <BaseIcon name="pencil-square" class="h-5 w-5" />
        </button>
      </div>

      <router-view v-slot="{ Component }">
        <transition name="page" mode="out-in">
          <component :is="Component" />
        </transition>
      </router-view>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useWsStore } from '@/stores/ws'
import BaseIcon from '@/components/BaseIcon.vue'
import LogoIcon from '@/components/LogoIcon.vue'

const auth = useAuthStore()
const ws = useWsStore()
const router = useRouter()
const route = useRoute()

const sidebarOpen = ref(false)

// 路由切换时关闭侧边栏
watch(() => route.path, () => { sidebarOpen.value = false })

const userInitial = computed(() => {
  const name = auth.user?.displayName || auth.user?.email || '?'
  return name.charAt(0).toUpperCase()
})

const navItems = computed(() => [
  { to: '/inbox', icon: 'inbox-arrow-down', label: '收件箱', badge: ws.unreadCount || 0 },
  { to: '/sent', icon: 'paper-airplane', label: '已发送' },
  { to: '/drafts', icon: 'document-text', label: '草稿箱' },
  { to: '/trash', icon: 'trash', label: '回收站' },
  { to: '/junk', icon: 'folder', label: '垃圾邮件' },
  { to: '/settings', icon: 'cog-6-tooth', label: '设置' },
  { to: '/about', icon: 'information-circle', label: '关于' },
  ...(auth.user?.role === 'admin' ? [{ to: '/admin', icon: 'user', label: '管理' }] : []),
])

function handleLogout() {
  ws.disconnect()
  auth.logout()
  router.push('/login')
}

function onNewMail() {
  ws.startPolling()
}

function onVisibilityChange() {
  if (!document.hidden) {
    ws.connect()
  }
}

onMounted(() => {
  ws.connect()
  ws.startPolling()
  window.addEventListener('mymail:new-mail', onNewMail)
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onUnmounted(() => {
  ws.disconnect()
  ws.stopPolling()
  window.removeEventListener('mymail:new-mail', onNewMail)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>
