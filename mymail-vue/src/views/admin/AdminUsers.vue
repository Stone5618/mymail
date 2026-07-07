<template>
  <div class="max-w-4xl mx-auto">
    <div class="card overflow-hidden">
      <div class="px-4 sm:px-6 py-4 border-b border-dark-700 flex items-center justify-between">
        <h3 class="text-base sm:text-lg font-medium text-dark-100">用户管理</h3>
        <div class="flex items-center gap-2">
          <button @click="loadUsers" class="btn-ghost text-sm inline-flex items-center gap-1" title="刷新">
            <BaseIcon name="arrow-path" class="h-4 w-4" />
          </button>
          <button @click="openCreateUserModal" class="btn-primary text-sm inline-flex items-center gap-1">
            <BaseIcon name="plus" class="h-4 w-4" />
            <span>创建用户</span>
          </button>
        </div>
      </div>

      <!-- Desktop table -->
      <div class="hidden sm:block overflow-x-auto">
        <table class="w-full">
          <thead>
            <tr class="border-b border-dark-700 text-left text-sm text-dark-400">
              <th class="px-6 py-3">邮箱</th>
              <th class="px-6 py-3">名称</th>
              <th class="px-6 py-3">角色</th>
              <th class="px-6 py-3">配额</th>
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
              <td class="px-6 py-3 text-sm text-dark-400">{{ formatQuota(user.storage_limit) }}</td>
              <td class="px-6 py-3 text-sm">
                <span :class="user.is_active ? 'text-emerald-400' : 'text-red-400'">
                  {{ user.is_active ? '正常' : '禁用' }}
                </span>
              </td>
              <td class="px-6 py-3">
                <div class="flex items-center gap-1">
                  <button @click="openEditUserModal(user)" class="btn-ghost text-xs" title="编辑" aria-label="编辑">
                    <BaseIcon name="pencil" class="h-4 w-4" />
                  </button>
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
          <div class="flex items-center gap-2 text-xs">
            <span class="text-dark-400 inline-flex items-center gap-1">
              <BaseIcon v-if="user.role === 'admin'" name="shield-check" class="h-3.5 w-3.5" />
              <BaseIcon v-else name="user" class="h-3.5 w-3.5" />
              {{ user.role === 'admin' ? '管理员' : '用户' }}
            </span>
            <span class="text-dark-500">{{ formatQuota(user.storage_limit) }}</span>
          </div>
          <div class="flex items-center gap-1 flex-wrap pt-1">
            <button @click="openEditUserModal(user)" class="btn-ghost text-xs p-1.5" title="编辑" aria-label="编辑">
              <BaseIcon name="pencil" class="h-4 w-4" />
            </button>
            <button v-if="user.is_active" @click="disableUser(user.id)" class="btn-ghost text-xs p-1.5" title="禁用" aria-label="禁用">
              <BaseIcon name="no-symbol" class="h-4 w-4" />
            </button>
            <button v-else @click="enableUser(user.id)" class="btn-ghost text-xs p-1.5" title="启用" aria-label="启用">
              <BaseIcon name="lock-open" class="h-4 w-4" />
            </button>
            <button @click="resetPw(user.id)" class="btn-ghost text-xs p-1.5" title="重置密码" aria-label="重置密码">
              <BaseIcon name="key" class="h-4 w-4" />
            </button>
            <button v-if="user.role !== 'admin'" @click="deleteUser(user.id)" class="btn-ghost text-xs text-red-400 p-1.5" title="删除" aria-label="删除">
              <BaseIcon name="trash" class="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- 重置密码弹窗 -->
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

    <!-- 创建/编辑用户弹窗 -->
    <div v-if="userModal" class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" @click.self="userModal = null">
      <div class="bg-dark-800 border border-dark-700 rounded-xl shadow-2xl max-w-md w-full p-6 space-y-4">
        <div class="flex items-center justify-between">
          <h3 class="text-lg font-semibold text-dark-100 flex items-center gap-2">
            <BaseIcon :name="userModal.mode === 'create' ? 'user-plus' : 'pencil'" class="h-5 w-5" />
            {{ userModal.mode === 'create' ? '创建用户' : '编辑用户' }}
          </h3>
          <button @click="userModal = null" class="text-dark-500 hover:text-dark-300" aria-label="关闭">
            <BaseIcon name="x-mark" class="h-5 w-5" />
          </button>
        </div>
        <form @submit.prevent="submitUserModal" class="space-y-3">
          <div v-if="userModal.mode === 'create'">
            <label class="block text-sm text-dark-300 mb-1">用户名 <span class="text-red-400">*</span></label>
            <input v-model="userModal.form.username" type="text" required class="input-field w-full" placeholder="登录用户名" />
          </div>
          <div v-else>
            <label class="block text-sm text-dark-300 mb-1">用户名</label>
            <input :value="userModal.form.username" type="text" readonly class="input-field w-full opacity-60 cursor-not-allowed" />
          </div>
          <div v-if="userModal.mode === 'create'">
            <label class="block text-sm text-dark-300 mb-1">邮箱 <span class="text-red-400">*</span></label>
            <input v-model="userModal.form.email" type="email" required class="input-field w-full" placeholder="user@example.com" />
          </div>
          <div v-else>
            <label class="block text-sm text-dark-300 mb-1">邮箱</label>
            <input :value="userModal.form.email" type="email" readonly class="input-field w-full opacity-60 cursor-not-allowed" />
          </div>
          <div v-if="userModal.mode === 'create'">
            <label class="block text-sm text-dark-300 mb-1">初始密码 <span class="text-red-400">*</span></label>
            <input v-model="userModal.form.password" type="text" required class="input-field w-full" placeholder="初始密码（用户首次登录需修改）" />
          </div>
          <div>
            <label class="block text-sm text-dark-300 mb-1">显示名称</label>
            <input v-model="userModal.form.displayName" type="text" class="input-field w-full" placeholder="显示名称" />
          </div>
          <div>
            <label class="block text-sm text-dark-300 mb-1">角色</label>
            <select v-model="userModal.form.role" class="input-field w-full">
              <option value="user">普通用户</option>
              <option value="admin">管理员</option>
            </select>
          </div>
          <div>
            <label class="block text-sm text-dark-300 mb-1">存储配额 (MB)</label>
            <input v-model.number="userModal.form.storageLimit" type="number" min="0" class="input-field w-full" placeholder="0 表示无限" />
            <p class="text-xs text-dark-500 mt-1">0 表示无限，1024 = 1GB</p>
          </div>
          <div v-if="userModal.error" class="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">{{ userModal.error }}</div>
          <div class="flex justify-end gap-2 pt-2">
            <button type="button" @click="userModal = null" class="btn-secondary text-sm">取消</button>
            <button type="submit" :disabled="userModal.loading" class="btn-primary text-sm">
              <BaseSpinner v-if="userModal.loading" :size="14" class="mr-1.5" />
              {{ userModal.mode === 'create' ? '创建' : '保存' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { getUsers, createUser, updateUser, deleteUser as apiDeleteUser, disableUser as apiDisableUser, enableUser as apiEnableUser, resetPassword } from '@/api'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import BaseIcon from '@/components/BaseIcon.vue'
import BaseSpinner from '@/components/BaseSpinner.vue'

const { toast } = useToast()
const { confirm } = useConfirm()

const users = ref([])
const resetPwModal = ref(null)
const userModal = ref(null)

const MB = 1024 * 1024

async function loadUsers() {
  try {
    const u = await getUsers()
    users.value = Array.isArray(u) ? u : (u.users || [])
  } catch (e) { console.error('[AdminUsers] load users failed:', e) }
}

function formatQuota(bytes) {
  if (!bytes || bytes <= 0) return '无限'
  const gb = bytes / (1024 * MB)
  if (gb >= 1) return gb.toFixed(gb >= 10 ? 0 : 1) + ' GB'
  const mb = bytes / MB
  return Math.round(mb) + ' MB'
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

function openCreateUserModal() {
  userModal.value = {
    mode: 'create',
    form: {
      username: '',
      email: '',
      password: '',
      displayName: '',
      role: 'user',
      storageLimit: 100,
    },
    error: '',
    loading: false,
  }
}

function openEditUserModal(user) {
  userModal.value = {
    mode: 'edit',
    userId: user.id,
    form: {
      username: user.username,
      email: user.email,
      password: '',
      displayName: user.display_name || '',
      role: user.role || 'user',
      storageLimit: user.storage_limit > 0 ? Math.round(user.storage_limit / MB) : 0,
    },
    error: '',
    loading: false,
  }
}

async function submitUserModal() {
  const m = userModal.value
  if (!m) return
  m.error = ''
  if (m.mode === 'create') {
    if (!m.form.username.trim()) { m.error = '请输入用户名'; return }
    if (!m.form.email.trim()) { m.error = '请输入邮箱'; return }
    if (!m.form.password) { m.error = '请输入初始密码'; return }
  } else if (m.form.password && m.form.password.length < 6) {
    m.error = '新密码至少 6 位'; return
  }
  m.loading = true
  try {
    if (m.mode === 'create') {
      await createUser({
        username: m.form.username.trim(),
        email: m.form.email.trim(),
        password: m.form.password,
        display_name: m.form.displayName.trim(),
        role: m.form.role,
        storage_limit: m.form.storageLimit > 0 ? m.form.storageLimit * MB : 0,
      })
      toast('用户创建成功', 'success')
    } else {
      const payload = {
        role: m.form.role,
        storage_limit: m.form.storageLimit > 0 ? m.form.storageLimit * MB : 0,
      }
      if (m.form.password) payload.password = m.form.password
      await updateUser(m.userId, payload)
      toast('用户已更新', 'success')
    }
    userModal.value = null
    await loadUsers()
  } catch (e) {
    m.error = e.message || '操作失败'
  } finally {
    m.loading = false
  }
}

onMounted(loadUsers)
</script>
