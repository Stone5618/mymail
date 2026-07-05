<template>
  <div class="flex-1 flex flex-col min-h-0">
    <div class="flex items-center justify-between px-4 sm:px-6 py-3 sm:py-4 border-b border-dark-800">
      <h2 class="text-lg sm:text-xl font-semibold text-dark-100 flex items-center gap-2">
        <BaseIcon name="user" class="h-5 w-5" />
        管理后台
      </h2>
      <button @click="router.push('/inbox')" class="btn-ghost text-sm inline-flex items-center gap-1">
        <BaseIcon name="arrow-left" class="h-4 w-4" />
        返回
      </button>
    </div>
    <div class="flex-1 overflow-y-auto p-4 sm:p-6">
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
                      <button v-if="user.is_active" @click="disableUser(user.id)" class="btn-ghost text-xs" title="禁用" aria-label="禁用">
                        <BaseIcon name="no-symbol" class="h-4 w-4" />
                      </button>
                      <button v-else @click="enableUser(user.id)" class="btn-ghost text-xs" title="启用" aria-label="启用">
                        <BaseIcon name="lock-open" class="h-4 w-4" />
                      </button>
                      <button @click="resetPw(user.id)" class="btn-ghost text-xs" title="重置密码" aria-label="重置密码">
                        <BaseIcon name="key" class="h-4 w-4" />
                      </button>
                      <button v-if="user.role !== 'admin'" @click="deleteUser(user.id)" class="btn-ghost text-xs text-red-400" title="删除" aria-label="删除">
                        <BaseIcon name="trash" class="h-4 w-4" />
                      </button>
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
                <span class="text-xs text-dark-400 inline-flex items-center gap-1">
                  <BaseIcon v-if="user.role === 'admin'" name="shield-check" class="h-3.5 w-3.5" />
                  <BaseIcon v-else name="user" class="h-3.5 w-3.5" />
                  {{ user.role === 'admin' ? '管理员' : '用户' }}
                </span>
                <div class="flex-1" />
                <button v-if="user.is_active" @click="disableUser(user.id)" class="btn-ghost text-xs" title="禁用" aria-label="禁用">
                  <BaseIcon name="no-symbol" class="h-4 w-4" />
                </button>
                <button v-else @click="enableUser(user.id)" class="btn-ghost text-xs" title="启用" aria-label="启用">
                  <BaseIcon name="lock-open" class="h-4 w-4" />
                </button>
                <button @click="resetPw(user.id)" class="btn-ghost text-xs" title="重置密码" aria-label="重置密码">
                  <BaseIcon name="key" class="h-4 w-4" />
                </button>
                <button v-if="user.role !== 'admin'" @click="deleteUser(user.id)" class="btn-ghost text-xs text-red-400" title="删除" aria-label="删除">
                  <BaseIcon name="trash" class="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>
        </div>

      </div>
    </div>

    <!-- P1-16：重置密码弹窗（替代原明文 toast） -->
    <div v-if="resetPwModal" class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" @click.self="resetPwModal = null">
      <div class="bg-dark-800 border border-dark-700 rounded-xl shadow-2xl max-w-md w-full p-6 space-y-4">
        <div class="flex items-center justify-between">
          <h3 class="text-lg font-semibold text-dark-100 flex items-center gap-2">
            <BaseIcon name="key" class="h-5 w-5" />
            新密码
          </h3>
          <button @click="resetPwModal = null" class="text-dark-500 hover:text-dark-300" aria-label="关闭">
            <BaseIcon name="x-mark" class="h-5 w-5" />
          </button>
        </div>
        <p class="text-sm text-dark-400">已为该用户重置密码，请复制后安全地告知用户。此密码仅显示一次。</p>
        <div class="flex items-center gap-2 bg-dark-900 border border-dark-700 rounded-lg px-3 py-2.5">
          <code class="flex-1 font-mono text-sm text-primary-300 break-all">{{ resetPwModal.password }}</code>
          <button
            @click="copyPassword"
            class="shrink-0 px-3 py-1.5 rounded-md bg-primary-600 hover:bg-primary-500 text-white text-xs font-medium transition-colors inline-flex items-center gap-1"
          >
            <BaseIcon name="clipboard" class="h-4 w-4" />
            复制
          </button>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button @click="resetPwModal = null" class="btn-secondary text-sm">关闭</button>
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
import { useConfirm } from '@/composables/useConfirm'
import BaseIcon from '@/components/BaseIcon.vue'

const router = useRouter()
const { toast } = useToast()
const { confirm } = useConfirm()

const users = ref([])
const stats = ref({})
const dns = ref([])
// P1-16：重置密码弹窗状态（替代原明文 toast）
const resetPwModal = ref(null)

const statCards = computed(() => [
  { icon: 'users', label: '用户总数', value: stats.value.users || 0 },
  { icon: 'envelope', label: '今日邮件', value: stats.value.messages || 0 },
  { icon: 'paper-airplane', label: '今日发送', value: stats.value.sent || 0 },
])

async function loadAll() {
  // 分别处理，避免 DNS 检测接口异常影响用户列表和统计
  try {
    const u = await getUsers()
    users.value = Array.isArray(u) ? u : (u.users || [])
  } catch (e) { console.error('[AdminView] load users failed:', e) }

  try {
    const s = await getStats()
    stats.value = { users: s.total_users || 0, messages: (s.today_received || 0), sent: (s.today_sent || 0) }
  } catch (e) { console.error('[AdminView] load stats failed:', e) }

  try {
    const d = await checkDns()
    dns.value = formatDns(d)
  } catch (e) { console.error('[AdminView] load dns failed:', e) }
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
  const ok = await confirm({
    title: '删除用户',
    message: '确定要删除该用户吗？此操作不可恢复。',
    confirmText: '删除',
    cancelText: '取消',
    variant: 'danger',
  })
  if (!ok) return
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

// P1-16：重置密码改弹窗显示 + 复制按钮（不再用明文 toast）
async function resetPw(id) {
  const pw = generatePw()
  try {
    await resetPassword(id, pw)
    resetPwModal.value = { userId: id, password: pw }
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function copyPassword() {
  if (!resetPwModal.value) return
  try {
    await navigator.clipboard.writeText(resetPwModal.value.password)
    toast('已复制到剪贴板', 'success')
  } catch {
    toast('复制失败，请手动选择', 'error')
  }
}

onMounted(loadAll)
</script>
