<template>
  <div class="flex-1 overflow-y-auto">
    <!-- Header -->
    <div class="px-4 sm:px-6 py-3.5 border-b border-dark-800 flex items-center justify-between">
      <h2 class="text-lg sm:text-xl font-semibold text-dark-100 flex items-center gap-2">
        <BaseIcon name="information-circle" class="h-5 w-5 text-primary-400" />
        <span>关于</span>
      </h2>
      <BaseButton variant="ghost" size="md" icon="arrow-left" @click="router.push('/settings')">返回</BaseButton>
    </div>

    <div class="max-w-2xl mx-auto p-6 space-y-6">
      <!-- 品牌区 -->
      <div class="card p-8 text-center">
        <div class="w-20 h-20 mx-auto rounded-2xl bg-primary-600 flex items-center justify-center mb-4">
          <BaseIcon name="envelope" class="h-10 w-10 text-white" />
        </div>
        <h1 class="text-2xl font-bold text-dark-100">MyMail</h1>
        <p class="text-sm text-dark-400 mt-1">安全、高效的邮件管理平台</p>
        <div class="mt-3 flex items-center justify-center gap-2">
          <span class="px-2.5 py-1 rounded-full text-xs font-medium bg-primary-600/15 text-primary-400">v{{ frontendVersion }}</span>
          <span class="px-2.5 py-1 rounded-full text-xs font-medium bg-dark-800 text-dark-400">自托管</span>
        </div>
      </div>

      <!-- 版本信息 -->
      <div class="card p-6">
        <h3 class="text-sm font-semibold text-dark-100 mb-4 flex items-center gap-2">
          <BaseIcon name="code-bracket" class="h-4 w-4 text-primary-400" />
          <span>版本信息</span>
        </h3>
        <div class="space-y-2.5 text-sm">
          <div class="flex justify-between">
            <span class="text-dark-400">前端版本</span>
            <span class="text-dark-200 font-mono">{{ frontendVersion }}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-dark-400">后端版本</span>
            <span class="text-dark-200 font-mono">{{ versionInfo.backend_version || '—' }}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-dark-400">Go 版本</span>
            <span class="text-dark-200 font-mono">{{ versionInfo.go_version || '—' }}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-dark-400">构建时间</span>
            <span class="text-dark-200 font-mono">{{ formatTime(versionInfo.build_time) }}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-dark-400">Commit</span>
            <span class="text-dark-200 font-mono">{{ versionInfo.commit_sha || '—' }}</span>
          </div>
        </div>
      </div>

      <!-- 技术栈 -->
      <div class="card p-6">
        <h3 class="text-sm font-semibold text-dark-100 mb-4 flex items-center gap-2">
          <BaseIcon name="cube" class="h-4 w-4 text-primary-400" />
          <span>技术栈</span>
        </h3>
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
          <div v-for="tech in techStack" :key="tech.name" class="bg-dark-800/60 rounded-lg p-3">
            <div class="text-sm font-medium text-dark-200">{{ tech.name }}</div>
            <div class="text-xs text-dark-500 mt-0.5">{{ tech.desc }}</div>
          </div>
        </div>
      </div>

      <!-- 开源信息 -->
      <div class="card p-6">
        <h3 class="text-sm font-semibold text-dark-100 mb-3 flex items-center gap-2">
          <BaseIcon name="document-text" class="h-4 w-4 text-primary-400" />
          <span>开源协议</span>
        </h3>
        <p class="text-sm text-dark-400 leading-relaxed">
          本项目基于 MIT 协议开源，您可以自由使用、修改和分发。
        </p>
        <div class="mt-4 pt-4 border-t border-dark-800 text-xs text-dark-500">
          <span>&copy; 2026 MyMail. All rights reserved.</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import BaseIcon from '@/components/BaseIcon.vue'
import BaseButton from '@/components/BaseButton.vue'
import { version } from '../../package.json'

const router = useRouter()
const frontendVersion = ref(version || '1.0.0')
const versionInfo = ref({})

const techStack = [
  { name: 'Vue 3', desc: '前端框架' },
  { name: 'Vite', desc: '构建工具' },
  { name: 'Tailwind CSS v4', desc: '样式框架' },
  { name: 'Pinia', desc: '状态管理' },
  { name: 'Go', desc: '后端语言' },
  { name: 'SQLite', desc: '数据库' },
  { name: 'Dovecot', desc: 'IMAP 服务' },
  { name: 'Postfix', desc: 'SMTP 服务' },
  { name: 'Docker', desc: '容器部署' },
]

function formatTime(t) {
  if (!t || t === '0001-01-01T00:00:00Z') return '—'
  try {
    const d = new Date(t)
    return d.toLocaleString('zh-CN', { dateStyle: 'short', timeStyle: 'short' })
  } catch {
    return t
  }
}

onMounted(async () => {
  try {
    const res = await fetch('/api/version')
    if (res.ok) {
      versionInfo.value = await res.json()
    }
  } catch (e) {
    console.warn('[About] 获取版本信息失败:', e)
  }
})
</script>
