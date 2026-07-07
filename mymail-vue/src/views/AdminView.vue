<template>
  <div class="flex-1 flex min-h-0">
    <!-- 移动端遮罩 -->
    <div
      v-if="drawerOpen"
      class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm lg:hidden"
      @click="drawerOpen = false"
    />

    <!-- 侧边栏（PC 固定显示，移动端抽屉） -->
    <aside
      class="fixed lg:static inset-y-0 left-0 z-50 w-60 bg-dark-900 border-r border-dark-800
             flex flex-col shrink-0 transition-transform duration-300 lg:translate-x-0"
      :class="drawerOpen ? 'translate-x-0' : '-translate-x-full'"
    >
      <!-- 侧边栏头部：标题 + 关闭按钮（移动端） -->
      <div class="px-4 py-3 border-b border-dark-800 flex items-center justify-between">
        <h2 class="text-base font-semibold text-dark-100 flex items-center gap-2">
          <BaseIcon name="shield-check" class="h-5 w-5 text-primary-400" />
          <span>管理后台</span>
        </h2>
        <button @click="drawerOpen = false" class="lg:hidden text-dark-500 hover:text-dark-300" aria-label="关闭菜单">
          <BaseIcon name="x-mark" class="h-5 w-5" />
        </button>
      </div>

      <!-- 导航区（分组） -->
      <nav class="flex-1 px-3 py-4 overflow-y-auto space-y-6">
        <div v-for="group in navGroups" :key="group.title">
          <div class="px-3 mb-2 text-[11px] font-medium uppercase tracking-wider text-dark-500">
            {{ group.title }}
          </div>
          <div class="space-y-1">
            <RouterLink
              v-for="item in group.items"
              :key="item.name"
              :to="item.to"
              class="flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm transition-colors border-l-2"
              :class="isActive(item)
                ? 'bg-primary-600/15 text-primary-300 border-primary-400'
                : 'text-dark-400 hover:bg-dark-800/50 hover:text-dark-200 border-transparent'"
              @click="drawerOpen = false"
            >
              <BaseIcon :name="item.icon" class="h-4 w-4 shrink-0" />
              <span class="flex-1">{{ item.label }}</span>
            </RouterLink>
          </div>
        </div>
      </nav>

      <!-- 侧边栏底部：管理员信息 -->
      <div class="border-t border-dark-800 px-3 py-3">
        <div class="flex items-center gap-2 px-2 py-1.5 rounded-lg">
          <div class="w-7 h-7 rounded-full bg-primary-600 flex items-center justify-center text-white text-xs font-medium shrink-0">
            {{ adminInitial }}
          </div>
          <div class="flex-1 min-w-0">
            <div class="text-xs text-dark-200 truncate">{{ auth.user?.displayName || '管理员' }}</div>
            <div class="text-[10px] text-dark-500 truncate">{{ auth.user?.email }}</div>
          </div>
        </div>
      </div>
    </aside>

    <!-- 主区 -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- 顶部栏：面包屑 + 返回 -->
      <header class="flex items-center justify-between gap-3 px-4 sm:px-6 py-3 border-b border-dark-800 bg-dark-900/50">
        <div class="flex items-center gap-2 min-w-0">
          <button @click="drawerOpen = true" class="lg:hidden text-dark-300 hover:text-dark-100 p-1" aria-label="打开菜单">
            <BaseIcon name="bars-3" class="h-5 w-5" />
          </button>
          <nav class="flex items-center gap-1.5 text-sm min-w-0">
            <span class="text-dark-500 hidden sm:inline">管理后台</span>
            <BaseIcon name="chevron-right" class="h-3.5 w-3.5 text-dark-600 hidden sm:inline" />
            <span class="text-dark-100 font-medium truncate">{{ currentTitle }}</span>
          </nav>
        </div>
        <button @click="router.push('/inbox')" class="btn-ghost text-sm inline-flex items-center gap-1 shrink-0">
          <BaseIcon name="arrow-left" class="h-4 w-4" />
          <span class="hidden sm:inline">返回邮箱</span>
        </button>
      </header>

      <!-- 内容区 -->
      <div class="flex-1 overflow-y-auto p-4 sm:p-6">
        <RouterView v-slot="{ Component }">
          <KeepAlive :max="6">
            <component :is="Component" />
          </KeepAlive>
        </RouterView>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useRouter, useRoute, RouterLink, RouterView } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import BaseIcon from '@/components/BaseIcon.vue'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const drawerOpen = ref(false)

// 导航分组配置（OA 风格：概览/管理/运维/系统）
const navGroups = [
  {
    title: '概览',
    items: [
      { name: 'overview', to: '/admin/overview', icon: 'chart-bar', label: '仪表盘' },
    ],
  },
  {
    title: '管理',
    items: [
      { name: 'users', to: '/admin/users', icon: 'users', label: '用户管理' },
      { name: 'mails', to: '/admin/mails', icon: 'envelope', label: '邮件管理' },
    ],
  },
  {
    title: '运维',
    items: [
      { name: 'audit', to: '/admin/audit', icon: 'clipboard-document-list', label: '审计日志' },
      { name: 'maintenance', to: '/admin/maintenance', icon: 'wrench-screwdriver', label: '维护工具' },
    ],
  },
  {
    title: '系统',
    items: [
      { name: 'config', to: '/admin/config', icon: 'cog-6-tooth', label: '系统配置' },
    ],
  },
]

const flatItems = computed(() => navGroups.flatMap(g => g.items))

const currentTitle = computed(() => {
  const item = flatItems.value.find(i => route.name === `admin-${i.name}`)
  return item?.label || '管理后台'
})

const adminInitial = computed(() => {
  const name = auth.user?.displayName || auth.user?.email || '?'
  return name.charAt(0).toUpperCase()
})

function isActive(item) {
  return route.name === `admin-${item.name}`
}

// 路由切换时关闭抽屉
watch(() => route.path, () => { drawerOpen.value = false })
</script>
