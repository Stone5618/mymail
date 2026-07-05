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
          <LogoIcon :size="40" class="text-white" />
        </div>
        <h1 class="text-2xl font-bold text-dark-100">MyMail</h1>
        <p class="text-sm text-dark-400 mt-1">安全、高效的邮件管理平台</p>
        <div class="mt-3 flex items-center justify-center gap-2">
          <span class="px-2.5 py-1 rounded-full text-xs font-medium bg-primary-600/15 text-primary-400">v{{ frontendVersion }}</span>
          <span class="px-2.5 py-1 rounded-full text-xs font-medium bg-dark-800 text-dark-400">自托管</span>
        </div>
        <a
          href="https://github.com/Stone5618/mymail"
          target="_blank"
          rel="noopener noreferrer"
          class="mt-4 inline-flex items-center justify-center gap-1.5 text-sm text-primary-400 hover:text-primary-300 transition-colors"
        >
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" />
          </svg>
          <span>GitHub 仓库</span>
        </a>
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
import LogoIcon from '@/components/LogoIcon.vue'
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
