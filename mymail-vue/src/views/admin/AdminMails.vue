<template>
  <div class="max-w-5xl mx-auto">
    <div class="card overflow-hidden">
      <div class="px-4 sm:px-6 py-4 border-b border-dark-700 flex items-center justify-between">
        <h3 class="text-base sm:text-lg font-medium text-dark-100 flex items-center gap-2">
          <BaseIcon name="envelope" class="h-4 w-4 text-primary-400" />
          <span>邮件管理</span>
        </h3>
        <button @click="loadAdminMails" :disabled="mailLoading" class="btn-secondary text-sm inline-flex items-center gap-1">
          <BaseIcon v-if="mailLoading" name="arrow-path" class="h-4 w-4 animate-spin" />
          <BaseIcon v-else name="arrow-path" class="h-4 w-4" />
          <span>刷新</span>
        </button>
      </div>

      <!-- 筛选栏：移动端垂直堆叠，桌面端横向排列 -->
      <div class="px-4 sm:px-6 py-3 border-b border-dark-800/50 bg-dark-900/30">
        <div class="flex flex-col sm:flex-row sm:flex-wrap sm:items-center gap-2">
          <select v-model="mailFilter.user_id" @change="onMailFilterChange" class="input-filter text-sm w-full sm:w-auto">
            <option value="">全部用户</option>
            <option v-for="u in users" :key="u.id" :value="u.id">{{ u.email }}</option>
          </select>
          <select v-model="mailFilter.folder" @change="onMailFilterChange" class="input-filter text-sm w-full sm:w-auto">
            <option value="">全部文件夹</option>
            <option value="INBOX">收件箱</option>
            <option value="SENT">已发送</option>
            <option value="DRAFTS">草稿箱</option>
            <option value="TRASH">回收站</option>
            <option value="JUNK">垃圾邮件</option>
          </select>
          <select v-model="mailFilter.is_read" @change="onMailFilterChange" class="input-filter text-sm w-full sm:w-auto">
            <option value="">全部状态</option>
            <option value="true">已读</option>
            <option value="false">未读</option>
          </select>
          <div class="flex items-center gap-2">
            <input v-model="mailFilter.start" @change="onMailFilterChange" type="date" class="input-filter text-sm flex-1 sm:flex-none" />
            <span class="text-dark-500 text-xs">至</span>
            <input v-model="mailFilter.end" @change="onMailFilterChange" type="date" class="input-filter text-sm flex-1 sm:flex-none" />
          </div>
          <button @click="resetMailFilter" class="btn-ghost text-xs self-start sm:self-auto">重置</button>
        </div>
      </div>

      <!-- 桌面端表格 -->
      <div class="hidden sm:block overflow-x-auto">
        <table v-if="adminMails.length" class="w-full">
          <thead>
            <tr class="border-b border-dark-700 text-left text-xs text-dark-400">
              <th class="px-4 py-2">用户</th>
              <th class="px-4 py-2">发件人</th>
              <th class="px-4 py-2">主题</th>
              <th class="px-4 py-2">文件夹</th>
              <th class="px-4 py-2">状态</th>
              <th class="px-4 py-2">评分</th>
              <th class="px-4 py-2">大小</th>
              <th class="px-4 py-2">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in adminMails" :key="m.id" class="border-b border-dark-800/50 hover:bg-dark-800/30">
              <td class="px-4 py-2 text-xs text-dark-300 whitespace-nowrap">{{ m.user_email || ('#' + m.user_id) }}</td>
              <td class="px-4 py-2 text-xs text-dark-200 max-w-[160px] truncate" :title="m.from_addr">{{ m.from_addr }}</td>
              <td class="px-4 py-2 text-xs text-dark-200 max-w-[200px] truncate" :title="m.subject">{{ m.subject || '(无主题)' }}</td>
              <td class="px-4 py-2 text-xs">
                <span class="px-1.5 py-0.5 rounded text-[10px]" :class="folderBadgeClass(m.folder)">{{ formatFolder(m.folder) }}</span>
              </td>
              <td class="px-4 py-2 text-xs">
                <div class="flex items-center gap-1">
                  <span v-if="m.is_read" class="text-dark-500 text-[10px]">已读</span>
                  <span v-else class="text-primary-400 text-[10px]">未读</span>
                  <span v-if="m.is_starred" class="text-amber-400 text-[10px]">★</span>
                  <span v-if="m.is_deleted" class="text-red-400 text-[10px]">删</span>
                  <span v-if="m.has_attach" class="text-dark-400 text-[10px]" title="含附件">📎</span>
                </div>
              </td>
              <td class="px-4 py-2 text-xs">
                <span :class="spamScoreClass(m.spam_score)">{{ m.spam_score }}</span>
              </td>
              <td class="px-4 py-2 text-xs text-dark-400 whitespace-nowrap">{{ formatSize(m.size_bytes) }}</td>
              <td class="px-4 py-2 text-xs text-dark-400 whitespace-nowrap">{{ formatMailTime(m.received_at) }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="px-4 py-8 text-center text-sm text-dark-500">
          {{ mailLoading ? '加载中...' : '暂无邮件' }}
        </div>
      </div>

      <!-- 移动端卡片 -->
      <div class="sm:hidden divide-y divide-dark-800/50">
        <div v-for="m in adminMails" :key="m.id" class="p-4 space-y-2">
          <div class="flex items-start justify-between gap-2">
            <span class="text-sm text-dark-100 font-medium flex-1 line-clamp-2 break-all">{{ m.subject || '(无主题)' }}</span>
            <span class="shrink-0 px-1.5 py-0.5 rounded text-[10px]" :class="folderBadgeClass(m.folder)">{{ formatFolder(m.folder) }}</span>
          </div>
          <div class="text-xs text-dark-400 space-y-0.5">
            <div class="flex items-center gap-1">
              <span class="text-dark-500 shrink-0">发</span>
              <span class="truncate">{{ m.from_addr }}</span>
            </div>
            <div class="flex items-center gap-1">
              <span class="text-dark-500 shrink-0">收</span>
              <span class="truncate">{{ m.user_email || ('#' + m.user_id) }}</span>
            </div>
          </div>
          <div class="flex items-center gap-2 flex-wrap text-[10px]">
            <span v-if="m.is_read" class="text-dark-500">已读</span>
            <span v-else class="text-primary-400">未读</span>
            <span v-if="m.is_starred" class="text-amber-400">★星标</span>
            <span v-if="m.is_deleted" class="text-red-400">已删除</span>
            <span v-if="m.has_attach" class="text-dark-400">📎附件</span>
            <span :class="spamScoreClass(m.spam_score)">评分 {{ m.spam_score }}</span>
            <span class="text-dark-500">{{ formatSize(m.size_bytes) }}</span>
          </div>
          <div class="text-[10px] text-dark-500">{{ formatMailTime(m.received_at) }}</div>
        </div>
        <div v-if="!adminMails.length" class="px-4 py-8 text-center text-sm text-dark-500">
          {{ mailLoading ? '加载中...' : '暂无邮件' }}
        </div>
      </div>

      <!-- 分页 -->
      <div v-if="mailTotal > mailPageSize" class="px-4 sm:px-6 py-3 border-t border-dark-800/50 flex items-center justify-between text-xs text-dark-400">
        <span>共 {{ mailTotal }} 条，第 {{ mailPage }} / {{ mailTotalPages }} 页</span>
        <div class="flex items-center gap-2">
          <button @click="goMailPage(mailPage - 1)" :disabled="mailPage <= 1 || mailLoading" class="btn-ghost text-xs">上一页</button>
          <button @click="goMailPage(mailPage + 1)" :disabled="mailPage >= mailTotalPages || mailLoading" class="btn-ghost text-xs">下一页</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { getAdminMails, getUsers } from '@/api'
import BaseIcon from '@/components/BaseIcon.vue'

const adminMails = ref([])
const mailLoading = ref(false)
const mailTotal = ref(0)
const mailPage = ref(1)
const mailPageSize = 50
const mailFilter = ref({ user_id: '', folder: '', is_read: '', start: '', end: '' })
const mailTotalPages = computed(() => Math.max(1, Math.ceil(mailTotal.value / mailPageSize)))
const users = ref([])

async function loadUsers() {
  try {
    const u = await getUsers()
    users.value = Array.isArray(u) ? u : (u.users || [])
  } catch (e) { console.error('[AdminMails] load users failed:', e) }
}

async function loadAdminMails() {
  mailLoading.value = true
  try {
    const data = await getAdminMails({
      page: mailPage.value,
      page_size: mailPageSize,
      user_id: mailFilter.value.user_id || undefined,
      folder: mailFilter.value.folder,
      is_read: mailFilter.value.is_read,
      start: mailFilter.value.start,
      end: mailFilter.value.end,
    })
    adminMails.value = data.items || []
    mailTotal.value = data.total || 0
  } catch (e) {
    console.error('[AdminMails] load admin mails failed:', e)
    adminMails.value = []
    mailTotal.value = 0
  } finally {
    mailLoading.value = false
  }
}

function onMailFilterChange() {
  mailPage.value = 1
  loadAdminMails()
}

function resetMailFilter() {
  mailFilter.value = { user_id: '', folder: '', is_read: '', start: '', end: '' }
  mailPage.value = 1
  loadAdminMails()
}

function goMailPage(p) {
  if (p < 1 || p > mailTotalPages.value || mailLoading.value) return
  mailPage.value = p
  loadAdminMails()
}

function formatFolder(f) {
  const m = { INBOX: '收件箱', SENT: '已发送', DRAFTS: '草稿箱', TRASH: '回收站', JUNK: '垃圾邮件' }
  return m[f] || f
}

function folderBadgeClass(f) {
  const m = {
    INBOX: 'bg-primary-500/20 text-primary-400',
    SENT: 'bg-emerald-500/20 text-emerald-400',
    DRAFTS: 'bg-amber-500/20 text-amber-400',
    TRASH: 'bg-red-500/20 text-red-400',
    JUNK: 'bg-orange-500/20 text-orange-400',
  }
  return m[f] || 'bg-dark-700 text-dark-400'
}

function spamScoreClass(s) {
  if (s >= 10) return 'text-red-400 font-medium'
  if (s >= 5) return 'text-amber-400'
  if (s > 0) return 'text-dark-300'
  return 'text-dark-600'
}

function formatSize(bytes) {
  if (!bytes || bytes <= 0) return '—'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

function formatMailTime(t) {
  if (!t) return '—'
  return t.length >= 16 ? t.slice(0, 16) : t
}

onMounted(() => {
  loadUsers()
  loadAdminMails()
})
</script>
