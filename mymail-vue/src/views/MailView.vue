<template>
  <div class="flex-1 flex flex-col min-h-0">
    <!-- Header -->
    <div class="flex items-center justify-between px-4 sm:px-6 py-3 sm:py-4 border-b border-dark-800">
      <h2 class="text-lg sm:text-xl font-semibold text-dark-100">{{ folderIcon }} {{ folderName }}</h2>
      <div class="flex items-center gap-2">
        <!-- 星标筛选 -->
        <button
          @click="starredOnly = !starredOnly; loadMails(1)"
          class="text-sm transition-colors"
          :class="starredOnly ? 'text-yellow-400' : 'text-dark-500 hover:text-dark-300'"
          title="只看星标"
        >⭐</button>
        <input
          v-model="search"
          @keyup.enter="loadMails(1)"
          type="text"
          placeholder="🔍 搜索..."
          class="input-field w-32 sm:w-56 text-sm"
        />
        <button v-if="mails.length > 0" @click="toggleSelectAll" class="btn-ghost text-sm" :title="allSelected ? '取消全选' : '全选'">{{ allSelected ? '☑' : '☐' }}</button>
        <button @click="loadMails(1)" class="btn-ghost text-sm">⟳</button>
      </div>
    </div>

    <!-- Batch bar -->
    <transition name="fade">
      <div v-if="selected.length > 0" class="flex items-center gap-2 sm:gap-3 px-4 sm:px-6 py-2 bg-primary-600/10 border-b border-primary-500/20">
        <span class="text-sm text-primary-400">{{ selected.length }} 已选</span>
        <button @click="batchRead" class="btn-ghost text-xs sm:text-sm">📖 已读</button>
        <button @click="batchUnread" class="btn-ghost text-xs sm:text-sm">📩 未读</button>
        <button @click="batchDelete" class="btn-ghost text-xs sm:text-sm text-red-400">🗑 删除</button>
        <button @click="selected = []" class="btn-ghost text-xs sm:text-sm ml-auto">取消</button>
      </div>
    </transition>

    <!-- Mail list -->
    <div class="flex-1 overflow-y-auto">
      <SkeletonList v-if="loading" :count="8" />
      <div v-else-if="mails.length === 0" class="flex flex-col items-center justify-center py-20 text-dark-500">
        <div class="text-5xl mb-3">📭</div>
        <p class="text-sm">{{ search ? '没有找到匹配的邮件' : '这里空空如也' }}</p>
        <button v-if="search" @click="search = ''; loadMails(1)" class="btn-ghost mt-2 text-sm">清除搜索</button>
      </div>
      <div v-else>
        <div
          v-for="mail in mails" :key="mail.id"
          class="flex items-center gap-3 px-4 sm:px-6 py-3 border-b border-dark-800/50 cursor-pointer transition-colors hover:bg-white/[0.03] group"
          :class="{ 'bg-dark-900/80': !mail.is_read }"
          @click="openMail(mail)"
        >
          <input
            type="checkbox"
            :checked="selected.includes(mail.id)"
            @click.stop="toggleSelect(mail.id)"
            class="rounded border-dark-600 bg-dark-800 shrink-0"
          />
          <!-- 星标 -->
          <button
            @click.stop="starMail(mail)"
            class="text-base shrink-0 transition-transform hover:scale-125"
            :class="mail.is_starred ? 'text-yellow-400' : 'text-dark-600 opacity-0 group-hover:opacity-100'"
          >{{ mail.is_starred ? '★' : '☆' }}</button>
          <!-- 未读蓝点 -->
          <div class="w-1.5 h-1.5 rounded-full shrink-0" :class="mail.is_read ? 'opacity-0' : 'bg-primary-500'" />
          <div class="flex-1 min-w-0">
            <div class="flex items-center justify-between mb-0.5">
              <span class="text-sm truncate" :class="mail.is_read ? 'text-dark-300' : 'text-dark-100 font-semibold'">
                {{ displayName(mail) }}
              </span>
              <div class="flex items-center gap-1.5 shrink-0 ml-2">
                <!-- 附件图标 -->
                <span v-if="mail.has_attach" class="text-xs text-dark-500" title="有附件">📎</span>
                <span class="text-xs text-dark-500">{{ formatDate(mail.received_at) }}</span>
              </div>
            </div>
            <div class="text-sm truncate" :class="mail.is_read ? 'text-dark-500' : 'text-dark-200'">
              {{ mail.subject || '(无主题)' }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Pagination -->
    <div v-if="totalPages > 1" class="flex items-center justify-center gap-3 px-4 sm:px-6 py-3 border-t border-dark-800">
      <button :disabled="page <= 1" @click="loadMails(page - 1)" class="btn-ghost text-sm" :class="{ 'opacity-30': page <= 1 }">← 上一页</button>
      <span class="text-sm text-dark-400">{{ page }} / {{ totalPages }}</span>
      <button :disabled="page >= totalPages" @click="loadMails(page + 1)" class="btn-ghost text-sm" :class="{ 'opacity-30': page >= totalPages }">下一页 →</button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getMailList, markRead, markUnread, toggleStar, batchMarkRead, batchDelete as apiBatchDelete } from '@/api'
import { useFormat } from '@/composables/useFormat'
import SkeletonList from '@/components/SkeletonList.vue'

const props = defineProps({ folder: { type: String, default: 'INBOX' } })
const router = useRouter()
const { formatDate } = useFormat()

const mails = ref([])
const page = ref(1)
const total = ref(0)
const search = ref('')
const loading = ref(true)
const selected = ref([])
const starredOnly = ref(false)

const folderMap = { INBOX: '收件箱', SENT: '已发送', DRAFTS: '草稿箱', TRASH: '回收站', JUNK: '垃圾邮件' }
const folderIcons = { INBOX: '📥', SENT: '📤', DRAFTS: '📝', TRASH: '🗑️', JUNK: '📁' }
const folderName = computed(() => folderMap[props.folder] || props.folder)
const folderIcon = computed(() => folderIcons[props.folder] || '📧')
const totalPages = computed(() => Math.ceil(total.value / 20))
const allSelected = computed(() => mails.value.length > 0 && selected.value.length === mails.value.length)

function toggleSelectAll() {
  if (allSelected.value) selected.value = []
  else selected.value = mails.value.map(m => m.id)
}

function displayName(mail) {
  if (props.folder === 'SENT' || props.folder === 'DRAFTS') return mail.to_addr
  return mail.from_name || mail.from_addr
}

async function loadMails(p = 1) {
  loading.value = true
  page.value = p
  selected.value = []
  try {
    const data = await getMailList({
      folder: props.folder, page: p, limit: 20,
      search: search.value || undefined,
      starred: starredOnly.value ? '1' : undefined,
    })
    mails.value = data.messages
    total.value = data.total
  } catch (e) {
    console.error('[MailView] error:', e)
  } finally {
    loading.value = false
  }
}

function toggleSelect(id) {
  const idx = selected.value.indexOf(id)
  if (idx >= 0) selected.value.splice(idx, 1)
  else selected.value.push(id)
}

async function starMail(mail) {
  mail.is_starred = mail.is_starred ? 0 : 1
  await toggleStar(mail.id).catch(() => {
    mail.is_starred = mail.is_starred ? 0 : 1
  })
}

async function openMail(mail) {
  if (props.folder === 'DRAFTS') {
    router.push({ name: 'compose', query: { draftId: mail.id } })
  } else {
    if (!mail.is_read) await markRead(mail.id).catch(() => {})
    router.push({ name: 'mail-detail', params: { id: mail.id } })
  }
}

async function batchRead() {
  await batchMarkRead(selected.value).catch(() => {})
  selected.value.forEach(id => { const m = mails.value.find(m => m.id === id); if (m) m.is_read = 1 })
  selected.value = []
}

async function batchUnread() {
  for (const id of selected.value) {
    await markUnread(id).catch(() => {})
    const m = mails.value.find(m => m.id === id)
    if (m) m.is_read = 0
  }
  selected.value = []
}

async function batchDelete() {
  await apiBatchDelete(selected.value).catch(e => toast('删除失败', 'error'))
  loadMails(page.value)
}

watch(() => props.folder, () => { starredOnly.value = false; loadMails(1) })
onMounted(() => loadMails())
</script>
