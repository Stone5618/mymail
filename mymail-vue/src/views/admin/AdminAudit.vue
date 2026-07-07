<template>
  <div class="max-w-5xl mx-auto">
    <div class="card overflow-hidden">
      <div class="px-4 sm:px-6 py-4 border-b border-dark-700 flex items-center justify-between">
        <h3 class="text-base sm:text-lg font-medium text-dark-100 flex items-center gap-2">
          <BaseIcon name="clipboard-document-list" class="h-4 w-4 text-primary-400" />
          <span>审计日志</span>
        </h3>
        <button @click="loadAuditLogs" :disabled="auditLoading" class="btn-secondary text-sm inline-flex items-center gap-1">
          <BaseIcon v-if="auditLoading" name="arrow-path" class="h-4 w-4 animate-spin" />
          <BaseIcon v-else name="arrow-path" class="h-4 w-4" />
          <span>刷新</span>
        </button>
      </div>

      <!-- 筛选栏：移动端垂直堆叠，桌面端横向排列 -->
      <div class="px-4 sm:px-6 py-3 border-b border-dark-800/50 bg-dark-900/30">
        <div class="flex flex-col sm:flex-row sm:flex-wrap sm:items-center gap-2">
          <select v-model="auditFilter.actor_type" @change="onAuditFilterChange" class="input-filter text-sm w-full sm:w-auto">
            <option value="">全部操作者</option>
            <option value="admin">管理员</option>
            <option value="user">用户</option>
            <option value="api">API</option>
            <option value="smtp">SMTP</option>
            <option value="system">系统</option>
          </select>
          <input v-model="auditFilter.action" @change="onAuditFilterChange" placeholder="动作前缀（如 admin.user）" class="input-filter text-sm w-full sm:flex-1 sm:min-w-[160px]" />
          <select v-model="auditFilter.result" @change="onAuditFilterChange" class="input-filter text-sm w-full sm:w-auto">
            <option value="">全部结果</option>
            <option value="success">成功</option>
            <option value="failure">失败</option>
            <option value="denied">拒绝</option>
          </select>
          <div class="flex items-center gap-2">
            <input v-model="auditFilter.start" @change="onAuditFilterChange" type="date" class="input-filter text-sm flex-1 sm:flex-none" />
            <span class="text-dark-500 text-xs">至</span>
            <input v-model="auditFilter.end" @change="onAuditFilterChange" type="date" class="input-filter text-sm flex-1 sm:flex-none" />
          </div>
          <button @click="resetAuditFilter" class="btn-ghost text-xs self-start sm:self-auto">重置</button>
        </div>
      </div>

      <!-- 桌面端表格 -->
      <div class="hidden sm:block overflow-x-auto">
        <table v-if="auditLogs.length" class="w-full">
          <thead>
            <tr class="border-b border-dark-700 text-left text-xs text-dark-400">
              <th class="px-4 py-2">时间</th>
              <th class="px-4 py-2">操作者</th>
              <th class="px-4 py-2">动作</th>
              <th class="px-4 py-2">资源</th>
              <th class="px-4 py-2">结果</th>
              <th class="px-4 py-2">详情</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in auditLogs" :key="log.id" class="border-b border-dark-800/50 hover:bg-dark-800/30">
              <td class="px-4 py-2 text-xs text-dark-300 whitespace-nowrap">{{ formatAuditTime(log.timestamp) }}</td>
              <td class="px-4 py-2 text-xs">
                <div class="text-dark-200">{{ formatActor(log.actor_type) }}<span v-if="log.actor_id" class="text-dark-500">#{{ log.actor_id }}</span></div>
                <div v-if="log.actor_ip" class="text-dark-500 text-[10px]">{{ log.actor_ip }}</div>
              </td>
              <td class="px-4 py-2 text-xs text-dark-200 font-mono">{{ log.action }}</td>
              <td class="px-4 py-2 text-xs text-dark-400">
                <span v-if="log.resource_type">{{ log.resource_type }}<span v-if="log.resource_id" class="text-dark-500">#{{ log.resource_id }}</span></span>
                <span v-else class="text-dark-600">—</span>
              </td>
              <td class="px-4 py-2 text-xs">
                <span :class="auditResultClass(log.result)">{{ formatAuditResult(log.result) }}</span>
              </td>
              <td class="px-4 py-2 text-xs text-dark-400 max-w-xs truncate" :title="log.detail">{{ log.detail || '—' }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="px-4 py-8 text-center text-sm text-dark-500">
          {{ auditLoading ? '加载中...' : '暂无审计日志' }}
        </div>
      </div>

      <!-- 移动端卡片 -->
      <div class="sm:hidden divide-y divide-dark-800/50">
        <div v-for="log in auditLogs" :key="log.id" class="p-4 space-y-2">
          <div class="flex items-start justify-between gap-2">
            <code class="text-xs text-dark-200 font-mono break-all flex-1">{{ log.action }}</code>
            <span class="shrink-0 text-[10px] px-1.5 py-0.5 rounded" :class="auditResultBadgeClass(log.result)">
              {{ formatAuditResult(log.result) }}
            </span>
          </div>
          <div class="text-xs text-dark-400">
            <span>{{ formatActor(log.actor_type) }}</span>
            <span v-if="log.actor_id" class="text-dark-500">#{{ log.actor_id }}</span>
            <span v-if="log.actor_ip" class="text-dark-500 ml-2">{{ log.actor_ip }}</span>
          </div>
          <div v-if="log.resource_type" class="text-xs text-dark-500">
            资源: {{ log.resource_type }}<span v-if="log.resource_id">#{{ log.resource_id }}</span>
          </div>
          <div class="text-[10px] text-dark-500">{{ formatAuditTime(log.timestamp) }}</div>
          <details v-if="log.detail" class="text-xs">
            <summary class="text-dark-500 cursor-pointer">详情</summary>
            <pre class="mt-1 p-2 bg-dark-900 rounded text-dark-400 whitespace-pre-wrap break-all">{{ log.detail }}</pre>
          </details>
        </div>
        <div v-if="!auditLogs.length" class="px-4 py-8 text-center text-sm text-dark-500">
          {{ auditLoading ? '加载中...' : '暂无审计日志' }}
        </div>
      </div>

      <!-- 分页 -->
      <div v-if="auditTotal > auditPageSize" class="px-4 sm:px-6 py-3 border-t border-dark-800/50 flex items-center justify-between text-xs text-dark-400">
        <span>共 {{ auditTotal }} 条，第 {{ auditPage }} / {{ auditTotalPages }} 页</span>
        <div class="flex items-center gap-2">
          <button @click="goAuditPage(auditPage - 1)" :disabled="auditPage <= 1 || auditLoading" class="btn-ghost text-xs">上一页</button>
          <button @click="goAuditPage(auditPage + 1)" :disabled="auditPage >= auditTotalPages || auditLoading" class="btn-ghost text-xs">下一页</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { getAuditLogs } from '@/api'
import BaseIcon from '@/components/BaseIcon.vue'

const auditLogs = ref([])
const auditLoading = ref(false)
const auditTotal = ref(0)
const auditPage = ref(1)
const auditPageSize = 50
const auditFilter = ref({ actor_type: '', action: '', result: '', start: '', end: '' })
const auditTotalPages = computed(() => Math.max(1, Math.ceil(auditTotal.value / auditPageSize)))

async function loadAuditLogs() {
  auditLoading.value = true
  try {
    const data = await getAuditLogs({
      page: auditPage.value,
      page_size: auditPageSize,
      actor_type: auditFilter.value.actor_type,
      action: auditFilter.value.action,
      result: auditFilter.value.result,
      start: auditFilter.value.start,
      end: auditFilter.value.end,
    })
    auditLogs.value = data.items || []
    auditTotal.value = data.total || 0
  } catch (e) {
    console.error('[AdminAudit] load audit logs failed:', e)
    auditLogs.value = []
    auditTotal.value = 0
  } finally {
    auditLoading.value = false
  }
}

function onAuditFilterChange() {
  auditPage.value = 1
  loadAuditLogs()
}

function resetAuditFilter() {
  auditFilter.value = { actor_type: '', action: '', result: '', start: '', end: '' }
  auditPage.value = 1
  loadAuditLogs()
}

function goAuditPage(p) {
  if (p < 1 || p > auditTotalPages.value || auditLoading.value) return
  auditPage.value = p
  loadAuditLogs()
}

function formatAuditTime(ts) {
  if (!ts) return '—'
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatActor(t) {
  const m = { admin: '管理员', user: '用户', api: 'API', smtp: 'SMTP', system: '系统' }
  return m[t] || t
}

function formatAuditResult(r) {
  const m = { success: '成功', failure: '失败', denied: '拒绝' }
  return m[r] || r
}

function auditResultClass(r) {
  if (r === 'success') return 'text-emerald-400'
  if (r === 'failure') return 'text-red-400'
  if (r === 'denied') return 'text-amber-400'
  return 'text-dark-400'
}

function auditResultBadgeClass(r) {
  if (r === 'success') return 'bg-emerald-500/20 text-emerald-400'
  if (r === 'failure') return 'bg-red-500/20 text-red-400'
  if (r === 'denied') return 'bg-amber-500/20 text-amber-400'
  return 'bg-dark-700 text-dark-400'
}

onMounted(loadAuditLogs)
</script>
