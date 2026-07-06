<template>
  <div class="flex-1 flex flex-col min-h-0">
    <div class="flex items-center justify-between px-4 sm:px-6 py-3 sm:py-4 border-b border-dark-800">
      <h2 class="text-lg sm:text-xl font-semibold text-dark-100 flex items-center gap-2">
        <BaseIcon :name="titleIcon" class="h-5 w-5" />
        {{ title }}
      </h2>
      <button @click="handleBack" class="btn-ghost text-sm inline-flex items-center gap-1">
        <BaseIcon name="arrow-left" class="h-4 w-4" />
        返回
      </button>
    </div>

    <form @submit.prevent="handleSend" class="flex-1 flex flex-col min-h-0">
      <div class="flex-1 overflow-y-auto p-4 sm:p-6">
        <div class="max-w-3xl mx-auto space-y-3 sm:space-y-4">
          <!-- 收件人 -->
          <div>
            <label class="block text-xs sm:text-sm text-dark-400 mb-1.5">收件人</label>
            <div class="input-field flex flex-wrap gap-2 min-h-[42px] items-center">
              <span v-for="(r, i) in toRecipients" :key="i" class="flex items-center gap-1 bg-dark-700 text-dark-200 rounded-full px-2.5 py-0.5 text-xs sm:text-sm">
                {{ r }}
                <button type="button" @click="toRecipients.splice(i, 1)" class="text-dark-500 hover:text-dark-300 inline-flex items-center">
                  <BaseIcon name="x-mark" class="h-3.5 w-3.5" />
                </button>
              </span>
              <input
                ref="toInput"
                :value="toInputValue"
                @input="onToInput"
                @blur="addToRecipient"
                @paste="onToPaste"
                @keydown.enter.prevent="addToRecipient"
                @keydown.tab.prevent="addToRecipient"
                @keydown.backspace="!toInputValue && toRecipients.length && toRecipients.pop()"
                enterkeyhint="next"
                inputmode="email"
                type="email"
                autocomplete="email"
                class="bg-transparent outline-none flex-1 min-w-[100px] text-dark-100 text-sm"
                placeholder="输入邮箱，逗号或空格分隔"
              />
              <button type="submit" class="hidden" @click.prevent="addToRecipient" />
            </div>
          </div>

          <!-- 抄送/密送 -->
          <button type="button" @click="showCcBcc = !showCcBcc" class="text-sm text-dark-500 hover:text-dark-300 transition-colors inline-flex items-center gap-1">
            抄送/密送
            <BaseIcon :name="showCcBcc ? 'chevron-up' : 'chevron-down'" class="h-4 w-4" />
          </button>
          <div v-if="showCcBcc" class="space-y-3">
            <input v-model="cc" type="text" class="input-field" placeholder="抄送 (多个用逗号分隔)" />
            <input v-model="bcc" type="text" class="input-field" placeholder="密送 (多个用逗号分隔)" />
          </div>

          <!-- 主题 -->
          <div>
            <label class="block text-xs sm:text-sm text-dark-400 mb-1.5">主题</label>
            <input v-model="subject" type="text" class="input-field" placeholder="邮件主题" />
          </div>

          <!-- 编辑器 -->
          <div>
            <div ref="editorRef" class="min-h-[250px] sm:min-h-[300px] bg-dark-900 rounded-lg border border-dark-700 overflow-hidden"
                 :class="{ hidden: !quillReady }"></div>
            <div v-if="!quillReady && !quillFailed" class="min-h-[250px] sm:min-h-[300px] flex flex-col items-center justify-center bg-dark-900 rounded-lg border border-dark-700 text-dark-500">
              <BaseIcon name="arrow-path" class="h-6 w-6 animate-spin mb-2" />
              <span class="text-sm">加载编辑器...</span>
            </div>
            <textarea v-if="quillFailed" v-model="fallbackBody"
              class="input-field min-h-[250px] sm:min-h-[300px]" placeholder="写点什么..."></textarea>
          </div>

          <!-- 附件上传区 -->
          <div>
            <label class="block text-xs sm:text-sm text-dark-400 mb-1.5">附件</label>
            <UploadZone ref="uploadZone" @update:attachments="onAttachmentsUpdate" />
          </div>

          <!-- 签名 -->
          <label v-if="auth.user?.signature" class="flex items-center gap-2 text-sm text-dark-400 cursor-pointer">
            <input type="checkbox" v-model="useSignature" class="rounded border-dark-600 bg-dark-800" />
            签名
          </label>
        </div>
      </div>

      <!-- 底部发送栏 -->
      <div class="border-t border-dark-800 px-4 sm:px-6 py-3 sm:py-4 bg-dark-900/50">
        <div class="max-w-3xl mx-auto flex items-center justify-between">
          <button type="button" @click="handleSaveDraft" class="btn-secondary text-sm inline-flex items-center" :disabled="saving">
            <BaseIcon :name="saving ? 'arrow-path' : 'archive-box'" :class="['h-4 w-4', saving ? 'animate-spin' : '']" />
            <span class="hidden sm:inline ml-1">{{ saving ? '保存中...' : '保存草稿' }}</span>
          </button>
          <button type="submit" class="btn-primary text-sm sm:text-base inline-flex items-center gap-1" :disabled="sending || (uploadZone && uploadZone.isUploading())">
            <BaseIcon :name="sending ? 'arrow-path' : 'paper-airplane'" :class="['h-4 w-4', sending ? 'animate-spin' : '']" />
            <span class="hidden sm:inline">{{ sending ? '发送中...' : '发送' }}</span>
          </button>
        </div>
      </div>
    </form>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, onBeforeUnmount, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DOMPurify from 'dompurify'
import Quill from 'quill'
import 'quill/dist/quill.snow.css'
import { useAuthStore } from '@/stores/auth'
import { sendMail, saveDraft, getMail, deleteMail } from '@/api'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import UploadZone from '@/components/UploadZone.vue'
import BaseIcon from '@/components/BaseIcon.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { confirm } = useConfirm()
const { toast } = useToast()

// P0-4：HTML 净化，防止回复/转发时 XSS（原邮件 body_html 可能含恶意脚本）
// 保留 style 属性，确保原邮件的颜色、背景、字号等样式在回复/转发时可见。
function sanitize(html) {
  if (!html) return ''
  return DOMPurify.sanitize(html, {
    USE_PROFILES: { html: true },
    ADD_ATTR: ['style'],
  })
}

const toRecipients = ref([])
const toInputValue = ref('')
const toInput = ref(null)
const showCcBcc = ref(false)
const cc = ref('')
const bcc = ref('')
const subject = ref('')
const useSignature = ref(true)
const sending = ref(false)
const saving = ref(false)
const attachmentIds = ref([])

const editorRef = ref(null)
const uploadZone = ref(null)
const quillReady = ref(false)
const quillFailed = ref(false)
const fallbackBody = ref('')
let quill = null
function hasContent() {
  return toRecipients.value.length > 0 || subject.value || (quill && quill.getLength() > 1) || fallbackBody.value
}

async function handleBack() {
  if (!hasContent()) { router.back(); return }
  const ok = await confirm({
    title: '保存草稿？',
    message: '是否在退出前保存草稿？',
    confirmText: '保存并退出',
    cancelText: '直接退出',
    variant: 'primary',
  })
  if (ok) {
    handleSaveDraft().then(() => router.back())
  } else {
    router.back()
  }
}

function onBeforeUnload(e) {
  if (hasContent()) { e.preventDefault(); e.returnValue = '' }
}

onMounted(async () => {
  await loadQuill()
  await loadDraft()
  await loadReplyOrForward()
  window.addEventListener('beforeunload', onBeforeUnload)
})

onUnmounted(() => { window.removeEventListener('beforeunload', onBeforeUnload); quill = null })
let currentDraftId = null

const draftId = route.query.draftId ? Number(route.query.draftId) : null
const replyId = route.query.replyId ? Number(route.query.replyId) : null
const forwardId = route.query.forwardId ? Number(route.query.forwardId) : null

const title = computed(() => {
  if (draftId) return '编辑草稿'
  if (replyId) return '回复'
  if (forwardId) return '转发'
  return '新邮件'
})

const titleIcon = computed(() => {
  if (draftId) return 'pencil-square'
  if (replyId) return 'arrow-uturn-left'
  if (forwardId) return 'arrow-uturn-right'
  return 'pencil-square'
})

function onToInput(e) {
  const val = e.target.value
  // 移动端：空格或逗号触发添加收件人
  if (val.endsWith(' ') || val.endsWith(',') || val.endsWith('\n') || val.endsWith('\r')) {
    toInputValue.value = val.replace(/[\s,]+$/g, '').trim()
    addToRecipient()
  } else {
    toInputValue.value = val
  }
}

function addToRecipient() {
  const raw = toInputValue.value.trim()
  if (!raw) return
  const emails = raw.split(/[,;\s]+/).map(s => s.trim()).filter(s => s.includes('@'))
  if (emails.length) {
    toRecipients.value.push(...emails)
    toInputValue.value = ''
  }
}

function onToPaste(e) {
  const pasted = (e.clipboardData || window.clipboardData).getData('text')
  if (!pasted) return
  const emails = pasted.split(/[,;\s]+/).map(s => s.trim()).filter(s => s.includes('@'))
  if (emails.length) {
    e.preventDefault()
    toRecipients.value.push(...emails)
    toInputValue.value = ''
  }
}

function onAttachmentsUpdate(ids) {
  attachmentIds.value = ids
}

function getBodyHtml() {
  if (quill) return quill.root.innerHTML
  if (quillFailed.value) return fallbackBody.value.replace(/\n/g, '<br>')
  return ''
}

async function setBodyHtml(html) {
  await nextTick()
  if (quill) quill.root.innerHTML = html
  else fallbackBody.value = html
}

async function loadQuill() {
  try {
    await nextTick()
    if (editorRef.value) {
      quill = new Quill(editorRef.value, {
        theme: 'snow',
        placeholder: '写点什么...',
        modules: {
          toolbar: [
            [{ header: [1, 2, 3, false] }],
            ['bold', 'italic', 'underline', 'strike'],
            [{ color: [] }, { background: [] }],
            [{ list: 'ordered' }, { list: 'bullet' }],
            [{ align: [] }],
            ['blockquote', 'code-block'],
            ['link'],
            ['clean'],
          ],
        },
      })
      quillReady.value = true
    }
  } catch {
    console.error('[ComposeView] Quill init failed')
    quillFailed.value = true
  }
}

async function loadDraft() {
  if (!draftId) return
  try {
    const data = await getMail(draftId)
    toRecipients.value = data.to_addr ? data.to_addr.split(',').map(s => s.trim()).filter(Boolean) : []
    cc.value = data.cc_addr || ''
    bcc.value = data.bcc_addr || ''
    subject.value = data.subject === '(无主题)' ? '' : (data.subject || '')
    await setBodyHtml(data.body_html || data.body_text || '')
    currentDraftId = draftId
  } catch {}
}

async function loadReplyOrForward() {
  const id = replyId || forwardId
  if (!id) return
  try {
    const data = await getMail(id)
    const { attachments: att, ...msg } = data
    if (replyId) {
      toRecipients.value = [msg.from_addr]
      subject.value = 'Re: ' + (msg.subject || '')
      const date = msg.received_at ? new Date(msg.received_at).toLocaleString('zh-CN') : ''
      await setBodyHtml(`<br><br><p>---------- 回复内容 ----------</p><p>发件人: ${msg.from_name || msg.from_addr} &lt;${msg.from_addr}&gt;</p><p>日期: ${date}</p><p>主题: ${msg.subject || ''}</p><br>${sanitize(msg.body_html || msg.body_text || '')}`)
    } else {
      subject.value = 'Fwd: ' + (msg.subject || '')
      const date = msg.received_at ? new Date(msg.received_at).toLocaleString('zh-CN') : ''
      await setBodyHtml(`<br><br><p>---------- 转发邮件 ----------</p><p>发件人: ${msg.from_name || msg.from_addr} &lt;${msg.from_addr}&gt;</p><p>日期: ${date}</p><p>收件人: ${msg.to_addr}</p><p>主题: ${msg.subject || ''}</p><br>${sanitize(msg.body_html || msg.body_text || '')}`)
    }
  } catch {}
}

async function handleSend() {
  addToRecipient()
  if (toRecipients.value.length === 0) { toast('请添加收件人', 'error'); return }
  if (uploadZone.value && uploadZone.value.isUploading()) { toast('请等待附件上传完成', 'error'); return }
  sending.value = true
  try {
    const fd = new FormData()
    fd.append('to', toRecipients.value.join(', '))
    fd.append('cc', cc.value)
    fd.append('bcc', bcc.value)
    fd.append('subject', subject.value)
    fd.append('bodyHtml', getBodyHtml())
    fd.append('bodyText', quill ? quill.getText() : fallbackBody.value)
    // Attach files directly in send request
    if (uploadZone.value) {
      const files = uploadZone.value.getFiles()
      for (const file of files) fd.append('attachments', file)
    }
    await sendMail(fd)
    toast('📤 正在发送...', 'info')
    if (currentDraftId) deleteMail(currentDraftId).catch(() => {})
    router.push('/sent')
  } catch (e) { toast('发送失败: ' + e.message, 'error') }
  finally { sending.value = false }
}

async function handleSaveDraft() {
  saving.value = true
  try {
    const res = await saveDraft({
      to: toRecipients.value.join(', '), cc: cc.value, bcc: bcc.value,
      subject: subject.value || '(无主题)', bodyHtml: getBodyHtml(),
      bodyText: quill ? quill.getText() : fallbackBody.value, draftId: currentDraftId || undefined,
    })
    currentDraftId = res.id
    toast('草稿已保存', 'success')
  } catch { toast('保存失败', 'error') }
  finally { saving.value = false }
}


</script>
