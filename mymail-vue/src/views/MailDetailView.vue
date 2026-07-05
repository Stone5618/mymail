<template>
  <div class="flex-1 flex flex-col min-h-0">
    <!-- Header -->
    <div class="flex items-center justify-between px-4 sm:px-6 py-3 sm:py-4 border-b border-dark-800">
      <button @click="router.back()" class="btn-ghost text-sm sm:text-base inline-flex items-center gap-1">
        <BaseIcon name="arrow-left" class="h-4 w-4" />
        <span>返回</span>
      </button>
      <div v-if="mail" class="flex items-center gap-1 sm:gap-2">
        <!-- 头部操作区：星标 + 功能按钮 -->
        <div class="flex items-center gap-1 sm:gap-2">
          <button
            @click="toggleStarred"
            class="transition-transform hover:scale-125 inline-flex items-center"
            :class="mail.is_starred ? 'text-yellow-400' : 'text-dark-500'"
          >
            <BaseIcon name="star" :solid="!!mail.is_starred" class="h-5 w-5" />
          </button>
          <button @click="reply" class="btn-ghost text-sm inline-flex items-center gap-1">
            <BaseIcon name="arrow-uturn-left" class="h-4 w-4" />
            <span>回复</span>
          </button>
          <button @click="forward" class="btn-ghost text-sm inline-flex items-center gap-1">
            <BaseIcon name="arrow-uturn-right" class="h-4 w-4" />
            <span>转发</span>
          </button>
          <button @click="del" class="btn-ghost text-sm text-red-400 inline-flex items-center">
            <BaseIcon name="trash" class="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>

    <!-- Loading skeleton -->
    <div v-if="loading" class="flex-1 overflow-y-auto p-4 sm:p-6">
      <div class="max-w-3xl mx-auto space-y-4">
        <div class="skel h-7 rounded w-3/4" />
        <div class="flex items-center gap-3">
          <div class="skel w-10 h-10 rounded-full" />
          <div class="space-y-2 flex-1">
            <div class="skel h-4 rounded w-32" />
            <div class="skel h-3 rounded w-48" />
          </div>
        </div>
        <div class="border-t border-dark-700 pt-6 space-y-3">
          <div class="skel h-4 rounded w-full" />
          <div class="skel h-4 rounded w-5/6" />
          <div class="skel h-4 rounded w-4/6" />
          <div class="skel h-4 rounded w-full" />
          <div class="skel h-4 rounded w-3/6" />
        </div>
      </div>
    </div>

    <!-- Content -->
    <div v-else-if="mail" class="flex-1 overflow-y-auto p-4 sm:p-6">
      <div class="max-w-3xl mx-auto">
        <h1 class="text-xl sm:text-2xl font-bold text-dark-100 mb-4">{{ mail.subject || '(无主题)' }}</h1>

        <!-- Sender info -->
        <div class="flex items-start gap-3 mb-6">
          <div class="w-10 h-10 rounded-full bg-gradient-to-br from-primary-500 to-violet-500 flex items-center justify-center text-white text-sm font-bold shrink-0">
            {{ (mail.from_name || mail.from_addr || '?').charAt(0).toUpperCase() }}
          </div>
          <div class="flex-1 min-w-0">
            <div class="flex items-center justify-between flex-wrap gap-2">
              <div class="text-dark-100 font-medium">{{ mail.from_name || mail.from_addr }}</div>
              <div class="text-xs text-dark-500 shrink-0">{{ formatDate(mail.received_at) }}</div>
            </div>
            <div class="text-sm text-dark-500 truncate">
              &lt;{{ mail.from_addr }}&gt;
              <span v-if="mail.to_addr"> → {{ mail.to_addr }}</span>
            </div>
            <div v-if="mail.cc_addr" class="text-sm text-dark-500 truncate">抄送: {{ mail.cc_addr }}</div>
          </div>
        </div>

        <!-- Body -->
        <div class="border-t border-dark-700 pt-6 mb-6">
          <div class="prose prose-invert max-w-none text-dark-200 leading-relaxed" v-html="sanitize(mail.body_html || escapeHtml(mail.body_text))"></div>
        </div>

        <!-- Attachments -->
        <div v-if="attachments.length" class="border-t border-dark-700 pt-4">
          <div class="flex items-center justify-between mb-3">
            <div class="text-sm text-dark-400 inline-flex items-center gap-1">
              <BaseIcon name="paper-clip" class="h-4 w-4" />
              <span>附件 ({{ attachments.length }})</span>
            </div>
            <a v-if="attachments.length > 1" :href="'/api/mail/' + mail.id + '/attachments/download-all'"
              class="text-xs text-primary-400 hover:text-primary-300 inline-flex items-center gap-1">
              <BaseIcon name="arrow-down-tray" class="h-3.5 w-3.5" />
              <span>下载全部</span>
            </a>
          </div>
          <div class="flex flex-wrap gap-2">
            <a
              v-for="att in attachments" :key="att.id"
              :href="`/api/mail/${mail.id}/attachments/${att.id}/download`"
              target="_blank"
              class="flex items-center gap-2 px-3 py-2 rounded-lg bg-dark-800 border border-dark-700 hover:border-primary-500/50 hover:bg-dark-700 transition-all group"
            >
              <BaseIcon :name="fileIcon(att.filename)" class="h-5 w-5 text-dark-300 group-hover:scale-110 transition-transform" />
              <div>
                <div class="text-sm text-dark-200">{{ att.filename }}</div>
                <div class="text-xs text-dark-500">{{ formatSize(att.size_bytes) }}</div>
              </div>
            </a>
          </div>
        </div>
      </div>
    </div>

    <!-- Error state -->
    <div v-else class="flex-1 flex flex-col items-center justify-center text-dark-500">
      <div class="mb-3">
        <BaseIcon name="face-frown" class="h-16 w-16" />
      </div>
      <p>邮件不存在或加载失败</p>
      <button @click="router.back()" class="btn-ghost mt-3 inline-flex items-center gap-1">
        <BaseIcon name="arrow-left" class="h-4 w-4" />
        <span>返回</span>
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import DOMPurify from 'dompurify'
import { getMail, deleteMail, toggleStar } from '@/api'
import { useFormat } from '@/composables/useFormat'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import BaseIcon from '@/components/BaseIcon.vue'

const props = defineProps({ id: { type: [String, Number], required: true } })
const router = useRouter()
const { formatDate, escapeHtml } = useFormat()
const { toast } = useToast()
const { confirm } = useConfirm()

// P0-4：HTML 净化，防止 XSS（script/iframe/event handler 等被移除）
function sanitize(html) {
  if (!html) return ''
  return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } })
}

const mail = ref(null)
const attachments = ref([])
const loading = ref(true)

function formatSize(bytes) {
  if (!bytes) return ''
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1048576).toFixed(1) + ' MB'
}

function fileIcon(name) {
  const ext = (name || '').split('.').pop().toLowerCase()
  const icons = {
    pdf: 'document-text',
    doc: 'document-text',
    docx: 'document-text',
    xls: 'table-cells',
    xlsx: 'table-cells',
    png: 'photo',
    jpg: 'photo',
    jpeg: 'photo',
    gif: 'photo',
    zip: 'archive-box',
    rar: 'archive-box'
  }
  return icons[ext] || 'document'
}

async function loadMail() {
  loading.value = true
  try {
    const data = await getMail(props.id)
    const { attachments: att, ...msg } = data
    mail.value = msg
    attachments.value = att || []
  } catch {
    mail.value = null
  } finally {
    loading.value = false
  }
}

function reply() {
  router.push({ name: 'compose', query: { replyId: props.id } })
}

async function toggleStarred() {
  if (!mail.value) return
  mail.value.is_starred = mail.value.is_starred ? 0 : 1
  await toggleStar(props.id).catch(() => {
    if (!mail.value) return
    mail.value.is_starred = mail.value.is_starred ? 0 : 1
  })
}

function forward() {
  router.push({ name: 'compose', query: { forwardId: props.id } })
}

async function del() {
  const ok = await confirm({
    title: '删除邮件',
    message: '确定要删除这封邮件吗？',
    confirmText: '删除',
    cancelText: '取消',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await deleteMail(props.id)
    toast('已删除', 'success')
    router.back()
  } catch {
    toast('删除失败', 'error')
  }
}

onMounted(loadMail)
</script>

<style scoped>
.skel {
  background: linear-gradient(90deg, #1e293b 25%, #334155 50%, #1e293b 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
}
@keyframes shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
</style>
