<template>
  <div class="max-w-4xl mx-auto space-y-6">
    <!-- 统计卡片 -->
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 sm:gap-4">
      <div v-for="s in statCards" :key="s.label" class="card p-4">
        <div class="text-sm text-dark-400 flex items-center gap-1.5">
          <BaseIcon :name="s.icon" class="h-4 w-4" />
          <span>{{ s.label }}</span>
        </div>
        <div class="text-2xl font-bold text-dark-100 mt-1">{{ s.value }}</div>
      </div>
    </div>

    <!-- DNS 检测 -->
    <div class="card p-4 sm:p-6">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-base sm:text-lg font-medium text-dark-100">DNS 检测</h3>
        <button @click="loadDns" class="btn-ghost text-sm inline-flex items-center gap-1">
          <BaseIcon name="arrow-path" class="h-4 w-4" />
          刷新
        </button>
      </div>
      <div v-if="dns.length" class="space-y-2">
        <div v-for="d in dns" :key="d.type" class="flex items-center justify-between py-2 border-b border-dark-800/50 last:border-0">
          <span class="text-sm font-mono text-dark-300">{{ d.type }}</span>
          <span class="flex items-center gap-1">
            <BaseIcon
              :name="d.status === 'ok' ? 'check-circle' : 'x-circle'"
              solid
              :class="['h-4 w-4', d.status === 'ok' ? 'text-emerald-400' : 'text-red-400']"
            />
            <span class="text-xs text-dark-500">{{ d.value }}</span>
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { getStats, checkDns } from '@/api'
import { useToast } from '@/composables/useToast'
import BaseIcon from '@/components/BaseIcon.vue'

const { toast } = useToast()

const stats = ref({})
const dns = ref([])

const statCards = computed(() => [
  { icon: 'users', label: '用户总数', value: stats.value.users || 0 },
  { icon: 'envelope', label: '今日邮件', value: stats.value.messages || 0 },
  { icon: 'paper-airplane', label: '今日发送', value: stats.value.sent || 0 },
])

async function loadStats() {
  try {
    const s = await getStats()
    stats.value = { users: s.total_users || 0, messages: (s.today_received || 0), sent: (s.today_sent || 0) }
  } catch (e) { console.error('[AdminOverview] load stats failed:', e) }
}

async function loadDns() {
  try {
    const d = await checkDns()
    dns.value = formatDns(d)
    toast('DNS 检测完成', 'success')
  } catch (e) {
    console.error('[AdminOverview] load dns failed:', e)
  }
}

function formatDns(d) {
  return [
    { type: 'MX', status: d.mx, value: d.mx === 'ok' ? '已配置' : '未配置' },
    { type: 'SPF', status: d.spf, value: d.spf === 'ok' ? '已配置' : '未配置' },
    { type: 'DMARC', status: d.dmarc, value: d.dmarc === 'ok' ? '已配置' : '未配置' },
  ]
}

onMounted(() => {
  loadStats()
  loadDns()
})
</script>
