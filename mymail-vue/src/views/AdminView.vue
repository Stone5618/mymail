<template>
  <div class="flex-1 flex flex-col min-h-0">
    <div class="flex items-center justify-between px-4 sm:px-6 py-3 sm:py-4 border-b border-dark-800">
      <h2 class="text-lg sm:text-xl font-semibold text-dark-100">👤 管理后台</h2>
      <button @click="router.push('/inbox')" class="btn-ghost text-sm">← 返回</button>
    </div>
    <div class="flex-1 overflow-y-auto p-4 sm:p-6">
      <div class="max-w-4xl mx-auto space-y-6">

        <!-- 统计卡片 -->
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 sm:gap-4">
          <div v-for="s in statCards" :key="s.label" class="card p-4">
            <div class="text-sm text-dark-400">{{ s.icon }} {{ s.label }}</div>
            <div class="text-2xl font-bold text-dark-100 mt-1">{{ s.value }}</div>
          </div>
        </div>

        <!-- DNS 检测 -->
        <div class="card p-4 sm:p-6">
          <div class="flex items-center justify-between mb-4">
            <h3 class="text-base sm:text-lg font-medium text-dark-100">DNS 检测</h3>
            <button @click="loadDns" class="btn-ghost text-sm">🔄 刷新</button>
          </div>
          <div v-if="dns.length" class="space-y-2">
            <div v-for="d in dns" :key="d.type" class="flex items-center justify-between py-2 border-b border-dark-800/50 last:border-0">
              <span class="text-sm font-mono text-dark-300">{{ d.type }}</span>
              <span class="flex items-center gap-1">
                <span :class="d.status === 'ok' ? 'text-emerald-400' : 'text-red-400'">
                  {{ d.status === 'ok' ? '✅' : '❌' }}
                </span>
                <span class="text-xs text-dark-500">{{ d.value }}</span>
              </span>
            </div>
          </div>
        </div>

        <!-- 用户管理 -->
        <div class="card overflow-hidden">
          <div class="px-4 sm:px-6 py-4 border-b border-dark-700">
            <h3 class="text-base sm:text-lg font-medium text-dark-100">用户管理</h3>
          </div>

          <!-- Desktop table -->
          <div class="hidden sm:block overflow-x-auto">
            <table class="w-full">
              <thead>
                <tr class="border-b border-dark-700 text-left text-sm text-dark-400">
                  <th class="px-6 py-3">邮箱</th>
                  <th class="px-6 py-3">名称</th>
                  <th class="px-6 py-3">角色</th>
                  <th class="px-6 py-3">状态</th>
                  <th class="px-6 py-3">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="user in users" :key="user.id" class="border-b border-dark-800/50 hover:bg-dark-800/30 transition-colors">
                  <td class="px-6 py-3 text-sm text-dark-200">{{ user.email }}</td>
                  <td class="px-6 py-3 text-sm text-dark-300">{{ user.display_name }}</td>
                  <td class="px-6 py-3 text-sm">
                    <span :class="user.role === 'admin' ? 'text-primary-400' : 'text-dark-500'">
                      {{ user.role === 'admin' ? '管理员' : '用户' }}
                    </span>
                  </td>
                  <td class="px-6 py-3 text-sm">
                    <span :class="user.is_active ? 'text-emerald-400' : 'text-red-400'">
                      {{ user.is_active ? '正常' : '禁用' }}
                    </span>
                  </td>
                  <td class="px-6 py-3">
                    <div class="flex items-center gap-1">
                      <button v-if="user.is_active" @click="disableUser(user.id)" class="btn-ghost text-xs" title="禁用">🚫</button>
                      <button v-else @click="enableUser(user.id)" class="btn-ghost text-xs" title="启用">✅</button>
                      <button @click="resetPw(user.id)" class="btn-ghost text-xs" title="重置密码">🔑</button>
                      <button v-if="user.role !== 'admin'" @click="deleteUser(user.id)" class="btn-ghost text-xs text-red-400" title="删除">🗑</button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Mobile cards -->
          <div class="sm:hidden divide-y divide-dark-800/50">
            <div v-for="user in users" :key="user.id" class="p-4 space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm text-dark-200 font-medium">{{ user.display_name }}</span>
                <span class="text-xs px-2 py-0.5 rounded-full" :class="user.is_active ? 'bg-emerald-500/20 text-emerald-400' : 'bg-red-500/20 text-red-400'">
                  {{ user.is_active ? '正常' : '禁用' }}
                </span>
              </div>
              <div class="text-xs text-dark-500">{{ user.email }}</div>
              <div class="flex items-center gap-2 pt-1">
                <span class="text-xs text-dark-400">{{ user.role === 'admin' ? '👑 管理员' : '👤 用户' }}</span>
                <div class="flex-1" />
                <button v-if="user.is_active" @click="disableUser(user.id)" class="btn-ghost text-xs">🚫</button>
                <button v-else @click="enableUser(user.id)" class="btn-ghost text-xs">✅</button>
                <button @click="resetPw(user.id)" class="btn-ghost text-xs">🔑</button>
                <button v-if="user.role !== 'admin'" @click="deleteUser(user.id)" class="btn-ghost text-xs text-red-400">🗑</button>
              </div>
            </div>
          </div>
        </div>

      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getUsers, getStats, checkDns, deleteUser as apiDeleteUser, disableUser as apiDisableUser, enableUser as apiEnableUser, resetPassword } from '@/api'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const { toast } = useToast()

const users = ref([])
const stats = ref({})
const dns = ref([])

const statCards = computed(() => [
  { icon: '👥', label: '用户总数', value: stats.value.users || 0 },
  { icon: '📧', label: '今日邮件', value: stats.value.messages || 0 },
  { icon: '📤', label: '今日发送', value: stats.value.sent || 0 },
])

async function loadAll() {
  try {
    const [u, s, d] = await Promise.all([getUsers(), getStats(), checkDns()])
    users.value = u.users || []
    stats.value = { users: s.totalUsers || 0, messages: (s.todayReceived || 0), sent: (s.todaySent || 0) }
    dns.value = formatDns(d)
  } catch {}
}

async function loadDns() {
  try {
    const d = await checkDns()
    dns.value = formatDns(d)
    toast('DNS 检测完成', 'success')
  } catch {}
}

function formatDns(d) {
  return [
    { type: 'MX', status: d.mx, value: d.mx === 'ok' ? '已配置' : '未配置' },
    { type: 'SPF', status: d.spf, value: d.spf === 'ok' ? '已配置' : '未配置' },
    { type: 'DMARC', status: d.dmarc, value: d.dmarc === 'ok' ? '已配置' : '未配置' },
  ]
}

async function deleteUser(id) {
  try { await apiDeleteUser(id); users.value = users.value.filter(u => u.id !== id); toast('已删除', 'success') }
  catch (e) { toast(e.message, 'error') }
}

async function disableUser(id) {
  try { await apiDisableUser(id); const u = users.value.find(u => u.id === id); if (u) u.is_active = 0; toast('已禁用', 'success') }
  catch (e) { toast(e.message, 'error') }
}

async function enableUser(id) {
  try { await apiEnableUser(id); const u = users.value.find(u => u.id === id); if (u) u.is_active = 1; toast('已启用', 'success') }
  catch (e) { toast(e.message, 'error') }
}

function generatePw() {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789!@#$%'
  return Array.from({ length: 12 }, () => chars[Math.floor(Math.random() * chars.length)]).join('')
}

async function resetPw(id) {
  const pw = generatePw()
  try { await resetPassword(id, pw); toast(`新密码: ${pw}`, 'success') }
  catch (e) { toast(e.message, 'error') }
}

onMounted(loadAll)
</script>
