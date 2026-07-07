<template>
  <div class="flex-1 flex flex-col min-h-0">
    <div class="flex items-center justify-between px-4 sm:px-6 py-3 sm:py-4 border-b border-dark-800">
      <h2 class="text-lg sm:text-xl font-semibold text-dark-100 flex items-center gap-2">
        <BaseIcon name="cog-6-tooth" class="h-5 w-5 text-primary-400" />
        <span>设置</span>
      </h2>
      <button @click="router.push('/inbox')" class="btn-ghost text-sm flex items-center gap-1">
        <BaseIcon name="arrow-left" class="h-4 w-4" />
        <span>返回</span>
      </button>
    </div>
    <div class="flex-1 overflow-y-auto p-4 sm:p-6">
      <div class="max-w-2xl mx-auto space-y-6">

        <!-- 外观 -->
        <div class="card p-4 sm:p-6">
          <h3 class="text-base sm:text-lg font-medium text-dark-100 mb-4 flex items-center gap-2">
            <BaseIcon name="swatch" class="h-4 w-4 text-primary-400" />
            <span>外观</span>
          </h3>
          <div class="space-y-3">
            <label class="block text-sm text-dark-400">主题</label>
            <div class="grid grid-cols-3 gap-2">
              <button
                v-for="opt in themeOptions" :key="opt.value"
                @click="setTheme(opt.value)"
                class="rounded-lg border p-3 text-center transition-all"
                :class="theme.mode === opt.value
                  ? 'border-primary-500 bg-primary-600/10 text-primary-400'
                  : 'border-dark-700 text-dark-400 hover:border-dark-600'"
              >
                <BaseIcon :name="opt.icon" class="h-5 w-5 mx-auto mb-1.5" />
                <span class="text-xs font-medium">{{ opt.label }}</span>
              </button>
            </div>
          </div>
        </div>

        <!-- 个人信息 -->
        <div class="card p-4 sm:p-6">
          <h3 class="text-base sm:text-lg font-medium text-dark-100 mb-4">个人信息</h3>
          <div class="space-y-4">
            <div class="flex items-center gap-4">
              <div
                class="relative w-16 h-16 rounded-full bg-primary-600 flex items-center justify-center text-white text-xl font-medium overflow-hidden shrink-0 cursor-pointer group"
                @click="triggerAvatarUpload"
              >
                <img v-if="auth.user?.avatarUrl" :src="auth.user.avatarUrl" class="w-full h-full object-cover" alt="" />
                <span v-else>{{ userInitial }}</span>
                <div class="absolute inset-0 bg-black/40 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
                  <BaseIcon name="camera" class="h-5 w-5" />
                </div>
                <div v-if="avatarLoading" class="absolute inset-0 bg-black/50 flex items-center justify-center">
                  <BaseSpinner :size="20" />
                </div>
              </div>
              <div>
                <button @click="triggerAvatarUpload" class="btn-secondary text-sm">更换头像</button>
                <p class="text-xs text-dark-500 mt-1.5">支持 JPG/PNG/GIF/WebP，最大 2MB</p>
              </div>
              <input ref="avatarInput" type="file" accept="image/*" class="hidden" @change="onAvatarSelected" />
            </div>
            <div>
              <label class="block text-sm text-dark-400 mb-1.5">显示名称</label>
              <input v-model="displayName" type="text" class="input-field" />
            </div>
            <div>
              <label class="block text-sm text-dark-400 mb-1.5">签名</label>
              <textarea v-model="signature" class="input-field min-h-[100px] resize-y" placeholder="邮件签名（不超过2000字）" maxlength="2000"></textarea>
              <div class="text-xs text-dark-500 mt-1">{{ signature.length }} / 2000</div>
            </div>
            <button @click="saveProfile" class="btn-primary w-full sm:w-auto">保存</button>
          </div>
        </div>

        <!-- 修改密码 -->
        <div id="pw-section" class="card p-4 sm:p-6">
          <h3 class="text-base sm:text-lg font-medium text-dark-100 mb-4">修改密码</h3>
          <div class="space-y-4">
            <input v-model="oldPw" type="password" class="input-field" placeholder="当前密码" />
            <input v-model="newPw" type="password" class="input-field" placeholder="新密码" />
            <input v-model="newPw2" type="password" class="input-field" placeholder="确认新密码" />
            <button @click="changePw" class="btn-primary w-full sm:w-auto">修改密码</button>
          </div>
        </div>

        <!-- 邮件配置 -->
        <div class="card p-4 sm:p-6">
          <h3 class="text-base sm:text-lg font-medium text-dark-100 mb-4">邮件配置</h3>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 sm:gap-4 text-sm">
            <div class="flex justify-between sm:block">
              <span class="text-dark-400">IMAP 服务器</span>
              <span class="text-dark-200 sm:mt-1 sm:block">{{ auth.user?.email ? `imap.${auth.user.email.split('@')[1]}` : '-' }}</span>
            </div>
            <div class="flex justify-between sm:block">
              <span class="text-dark-400">IMAP 端口</span>
              <span class="text-dark-200 sm:mt-1 sm:block">993 (SSL)</span>
            </div>
            <div class="flex justify-between sm:block">
              <span class="text-dark-400">SMTP 服务器</span>
              <span class="text-dark-200 sm:mt-1 sm:block">{{ auth.user?.email ? `smtp.${auth.user.email.split('@')[1]}` : '-' }}</span>
            </div>
            <div class="flex justify-between sm:block">
              <span class="text-dark-400">SMTP 端口</span>
              <span class="text-dark-200 sm:mt-1 sm:block">587 (STARTTLS)</span>
            </div>
          </div>
        </div>

        <!-- API Key 管理（AC-11） -->
        <div class="card p-4 sm:p-6">
          <div class="flex items-center justify-between mb-4">
            <h3 class="text-base sm:text-lg font-medium text-dark-100 flex items-center gap-2">
              <BaseIcon name="key" class="h-4 w-4 text-primary-400" />
              <span>API Key</span>
            </h3>
            <button @click="openCreateAPIKeyModal" class="btn-primary text-sm inline-flex items-center gap-1">
              <BaseIcon name="plus" class="h-4 w-4" />
              <span>新建</span>
            </button>
          </div>
          <p class="text-xs text-dark-500 mb-3">用于通过 <code class="text-primary-300">/api/v1/send</code> 接口程序化发送邮件。明文 Key 仅在创建时显示一次，请妥善保存。</p>

          <!-- 列表 -->
          <div v-if="apiKeys.length === 0" class="text-sm text-dark-500 py-4 text-center">暂无 API Key</div>
          <div v-else class="space-y-2">
            <div v-for="k in apiKeys" :key="k.id" class="flex items-center justify-between bg-dark-900/50 border border-dark-700/50 rounded-lg px-3 py-2.5">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="text-sm font-medium text-dark-200 truncate">{{ k.name }}</span>
                  <span v-if="!k.is_active" class="text-xs px-1.5 py-0.5 rounded bg-red-500/20 text-red-400">已禁用</span>
                </div>
                <div class="text-xs text-dark-500 mt-0.5 flex items-center gap-2 flex-wrap">
                  <code class="font-mono">{{ k.key_prefix }}****</code>
                  <span>·</span>
                  <span>{{ (k.scopes || []).join(', ') || '无权限' }}</span>
                  <span>·</span>
                  <span>{{ k.rate_limit }}/分钟</span>
                  <span v-if="k.last_used_at">·</span>
                  <span v-if="k.last_used_at">最近使用 {{ formatDate(k.last_used_at) }}</span>
                </div>
              </div>
              <button @click="removeAPIKey(k.id, k.name)" class="btn-ghost text-xs text-red-400 ml-2 shrink-0" title="删除" aria-label="删除">
                <BaseIcon name="trash" class="h-4 w-4" />
              </button>
            </div>
          </div>
        </div>

        <!-- 邮件规则管理（AC-12） -->
        <div class="card p-4 sm:p-6">
          <div class="flex items-center justify-between mb-4">
            <h3 class="text-base sm:text-lg font-medium text-dark-100 flex items-center gap-2">
              <BaseIcon name="funnel" class="h-4 w-4 text-primary-400" />
              <span>邮件规则</span>
            </h3>
            <button @click="openCreateRuleModal" class="btn-primary text-sm inline-flex items-center gap-1">
              <BaseIcon name="plus" class="h-4 w-4" />
              <span>新建</span>
            </button>
          </div>
          <p class="text-xs text-dark-500 mb-3">第一版支持单条件 + 单动作。规则按优先级升序执行（数字越小越先执行）。</p>

          <div v-if="rules.length === 0" class="text-sm text-dark-500 py-4 text-center">暂无规则</div>
          <div v-else class="space-y-2">
            <div v-for="r in rules" :key="r.id" class="flex items-center justify-between bg-dark-900/50 border border-dark-700/50 rounded-lg px-3 py-2.5">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="text-xs text-dark-500 font-mono">#{{ r.priority }}</span>
                  <span class="text-sm font-medium text-dark-200 truncate">{{ r.name }}</span>
                  <span v-if="!r.is_active" class="text-xs px-1.5 py-0.5 rounded bg-red-500/20 text-red-400">已禁用</span>
                </div>
                <div class="text-xs text-dark-500 mt-0.5">
                  {{ summarizeCondition(r.conditions[0]) }} → {{ summarizeAction(r.actions[0]) }}
                </div>
              </div>
              <div class="flex items-center gap-1 ml-2 shrink-0">
                <button @click="toggleRuleActive(r)" class="btn-ghost text-xs" :title="r.is_active ? '禁用' : '启用'" :aria-label="r.is_active ? '禁用' : '启用'">
                  <BaseIcon :name="r.is_active ? 'eye-slash' : 'eye'" class="h-4 w-4" />
                </button>
                <button @click="openEditRuleModal(r)" class="btn-ghost text-xs" title="编辑" aria-label="编辑">
                  <BaseIcon name="pencil" class="h-4 w-4" />
                </button>
                <button @click="removeRule(r.id, r.name)" class="btn-ghost text-xs text-red-400" title="删除" aria-label="删除">
                  <BaseIcon name="trash" class="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>
        </div>

      </div>
    </div>

    <!-- 创建 API Key 弹窗 -->
    <div v-if="apiKeyModal" class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" @click.self="apiKeyModal = null">
      <div class="bg-dark-800 border border-dark-700 rounded-xl shadow-2xl max-w-md w-full p-6 space-y-4">
        <div class="flex items-center justify-between">
          <h3 class="text-lg font-semibold text-dark-100 flex items-center gap-2">
            <BaseIcon name="key" class="h-5 w-5" />
            新建 API Key
          </h3>
          <button @click="apiKeyModal = null" class="text-dark-500 hover:text-dark-300" aria-label="关闭">
            <BaseIcon name="x-mark" class="h-5 w-5" />
          </button>
        </div>

        <!-- 创建成功后显示明文 Key（仅一次） -->
        <div v-if="apiKeyModal.plainText" class="space-y-3">
          <div class="rounded-lg bg-emerald-500/10 border border-emerald-500/30 px-3 py-2 text-sm text-emerald-400">
            <BaseIcon name="check-circle" solid class="h-4 w-4 inline mr-1" />
            Key 已创建，<strong>明文仅显示一次</strong>，请立即复制保存。
          </div>
          <div class="flex items-center gap-2 bg-dark-900 border border-dark-700 rounded-lg px-3 py-2.5">
            <code class="flex-1 font-mono text-sm text-primary-300 break-all">{{ apiKeyModal.plainText }}</code>
            <button @click="copyAPIKey(apiKeyModal.plainText)" class="shrink-0 px-3 py-1.5 rounded-md bg-primary-600 hover:bg-primary-500 text-white text-xs font-medium transition-colors inline-flex items-center gap-1">
              <BaseIcon name="clipboard" class="h-4 w-4" />
              复制
            </button>
          </div>
          <div class="flex justify-end pt-2">
            <button @click="apiKeyModal = null" class="btn-primary text-sm">我已保存</button>
          </div>
        </div>

        <!-- 创建表单 -->
        <form v-else @submit.prevent="submitAPIKeyModal" class="space-y-3">
          <div>
            <label class="block text-sm text-dark-300 mb-1">名称 <span class="text-red-400">*</span></label>
            <input v-model="apiKeyModal.form.name" type="text" required class="input-field w-full" placeholder="如：发送脚本" />
          </div>
          <div>
            <label class="block text-sm text-dark-300 mb-1">权限</label>
            <div class="space-y-1.5">
              <label v-for="s in scopeOptions" :key="s.value" class="flex items-center gap-2 text-sm text-dark-300 cursor-pointer">
                <input type="checkbox" :value="s.value" :checked="apiKeyModal.form.scopes.includes(s.value)" @change="toggleScope(s.value)" class="rounded border-dark-600 bg-dark-800" />
                <span>{{ s.label }}</span>
              </label>
            </div>
          </div>
          <div>
            <label class="block text-sm text-dark-300 mb-1">限速（次/分钟）</label>
            <input v-model.number="apiKeyModal.form.rateLimit" type="number" min="1" max="600" class="input-field w-full" />
            <p class="text-xs text-dark-500 mt-1">默认 60，最大 600</p>
          </div>
          <div v-if="apiKeyModal.error" class="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">{{ apiKeyModal.error }}</div>
          <div class="flex justify-end gap-2 pt-2">
            <button type="button" @click="apiKeyModal = null" class="btn-secondary text-sm">取消</button>
            <button type="submit" :disabled="apiKeyModal.loading" class="btn-primary text-sm">
              <BaseSpinner v-if="apiKeyModal.loading" :size="14" class="mr-1.5" />
              创建
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- 创建/编辑规则弹窗（AC-12） -->
    <div v-if="ruleModal" class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" @click.self="ruleModal = null">
      <div class="bg-dark-800 border border-dark-700 rounded-xl shadow-2xl max-w-md w-full p-6 space-y-4">
        <div class="flex items-center justify-between">
          <h3 class="text-lg font-semibold text-dark-100 flex items-center gap-2">
            <BaseIcon :name="ruleModal.mode === 'create' ? 'plus' : 'pencil'" class="h-5 w-5" />
            {{ ruleModal.mode === 'create' ? '新建规则' : '编辑规则' }}
          </h3>
          <button @click="ruleModal = null" class="text-dark-500 hover:text-dark-300" aria-label="关闭">
            <BaseIcon name="x-mark" class="h-5 w-5" />
          </button>
        </div>
        <form @submit.prevent="submitRuleModal" class="space-y-3">
          <div>
            <label class="block text-sm text-dark-300 mb-1">名称 <span class="text-red-400">*</span></label>
            <input v-model="ruleModal.form.name" type="text" required class="input-field w-full" placeholder="如：广告过滤" />
          </div>
          <div>
            <label class="block text-sm text-dark-300 mb-1">优先级</label>
            <input v-model.number="ruleModal.form.priority" type="number" min="0" class="input-field w-full" />
            <p class="text-xs text-dark-500 mt-1">数字越小越先执行（默认 0）</p>
          </div>
          <div class="border-t border-dark-700 pt-3">
            <label class="block text-sm text-dark-300 mb-2">条件</label>
            <div class="grid grid-cols-2 gap-2 mb-2">
              <select v-model="ruleModal.form.condField" class="input-field">
                <option v-for="f in ruleFieldOptions" :key="f.value" :value="f.value">{{ f.label }}</option>
              </select>
              <select v-model="ruleModal.form.condOp" class="input-field">
                <option v-for="o in ruleOpOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
              </select>
            </div>
            <input v-model="ruleModal.form.condValue" type="text" required class="input-field w-full" placeholder="匹配值" />
          </div>
          <div class="border-t border-dark-700 pt-3">
            <label class="block text-sm text-dark-300 mb-2">动作</label>
            <select v-model="ruleModal.form.actionType" class="input-field w-full mb-2">
              <option v-for="a in ruleActionOptions" :key="a.value" :value="a.value">{{ a.label }}</option>
            </select>
            <select v-if="ruleModal.form.actionType === 'move'" v-model="ruleModal.form.actionFolder" class="input-field w-full">
              <option value="INBOX">收件箱</option>
              <option value="SENT">已发送</option>
              <option value="DRAFTS">草稿箱</option>
              <option value="TRASH">回收站</option>
              <option value="JUNK">垃圾邮件</option>
            </select>
          </div>
          <div v-if="ruleModal.error" class="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">{{ ruleModal.error }}</div>
          <div class="flex justify-end gap-2 pt-2">
            <button type="button" @click="ruleModal = null" class="btn-secondary text-sm">取消</button>
            <button type="submit" :disabled="ruleModal.loading" class="btn-primary text-sm">
              <BaseSpinner v-if="ruleModal.loading" :size="14" class="mr-1.5" />
              {{ ruleModal.mode === 'create' ? '创建' : '保存' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { useUserPreferencesStore } from '@/stores/userPreferences'
import { updateProfile, changePassword, uploadAvatar, listAPIKeys, createAPIKey, deleteAPIKey, listRules, createRule, updateRule, deleteRule } from '@/api'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import BaseIcon from '@/components/BaseIcon.vue'
import BaseSpinner from '@/components/BaseSpinner.vue'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const theme = useThemeStore()
const userPrefs = useUserPreferencesStore()
const { toast } = useToast()
const { confirm } = useConfirm()

const displayName = ref('')
const signature = ref('')
const oldPw = ref('')
const newPw = ref('')
const newPw2 = ref('')
const avatarLoading = ref(false)
const avatarInput = ref(null)

const userInitial = computed(() => {
  const name = auth.user?.displayName || auth.user?.email || '?'
  return name.charAt(0).toUpperCase()
})

function triggerAvatarUpload() {
  avatarInput.value?.click()
}

async function onAvatarSelected(e) {
  const file = e.target.files?.[0]
  if (!file) return
  if (!file.type.startsWith('image/')) {
    toast('请选择图片文件', 'error')
    return
  }
  if (file.size > 2 * 1024 * 1024) {
    toast('头像大小不能超过 2MB', 'error')
    return
  }
  avatarLoading.value = true
  try {
    const formData = new FormData()
    formData.append('avatar', file)
    const data = await uploadAvatar(formData)
    if (auth.user) {
      // 追加时间戳，避免浏览器缓存导致上传后仍显示旧头像
      const sep = data.avatarUrl.includes('?') ? '&' : '?'
      auth.user.avatarUrl = `${data.avatarUrl}${sep}t=${Date.now()}`
    }
    toast('头像已更新', 'success')
  } catch (err) {
    toast(err.message || '上传失败', 'error')
  } finally {
    avatarLoading.value = false
    e.target.value = ''
  }
}

const themeOptions = [
  { value: 'dark', label: '深色', icon: 'moon' },
  { value: 'light', label: '浅色', icon: 'sun' },
  { value: 'system', label: '跟随系统', icon: 'computer-desktop' },
]

async function setTheme(value) {
  theme.setMode(value)
  // 同步到后端
  await userPrefs.set('theme', value)
}

onMounted(async () => {
  if (auth.user) {
    displayName.value = auth.user?.displayName || ''
    signature.value = auth.user?.signature || ''
  }
  await nextTick()
  if (route.query.changePw) {
    document.getElementById('pw-section')?.scrollIntoView({ behavior: 'smooth' })
  }
  // 模块 H：加载 API Key 列表（FeatureAPIKey 未启用时接口 404，静默处理）
  loadAPIKeys()
  // 模块 I：加载邮件规则列表（FeatureRules 未启用时接口 404，静默处理）
  loadRules()
})

async function saveProfile() {
  try {
    await updateProfile({ displayName: displayName.value, signature: signature.value })
    await auth.fetchMe()
    toast('已保存', 'success')
  } catch { toast('保存失败', 'error') }
}

async function changePw() {
  if (newPw.value !== newPw2.value) {
    toast('两次密码不一致', 'error')
    return
  }
  try {
    await changePassword(oldPw.value, newPw.value)
    toast('密码已修改', 'success')
    oldPw.value = newPw.value = newPw2.value = ''
  } catch (e) { toast(e.message, 'error') }
}

// ===== 模块 H：API Key 管理（AC-11） =====
const apiKeys = ref([])
const apiKeyModal = ref(null)

const scopeOptions = [
  { value: 'send', label: 'send（程序化发信）' },
]

function formatDate(s) {
  if (!s) return ''
  try {
    const d = new Date(s)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  } catch { return s }
}

async function loadAPIKeys() {
  try {
    const data = await listAPIKeys()
    apiKeys.value = Array.isArray(data) ? data : []
  } catch (e) {
    // FeatureAPIKey 未启用时接口 404，静默处理
    if (!String(e.message).includes('404')) {
      console.error('[SettingsView] load api keys failed:', e)
    }
    apiKeys.value = []
  }
}

function openCreateAPIKeyModal() {
  apiKeyModal.value = {
    plainText: '',
    form: {
      name: '',
      scopes: ['send'],
      rateLimit: 60,
    },
    error: '',
    loading: false,
  }
}

function toggleScope(value) {
  const m = apiKeyModal.value
  if (!m) return
  const idx = m.form.scopes.indexOf(value)
  if (idx >= 0) m.form.scopes.splice(idx, 1)
  else m.form.scopes.push(value)
}

async function submitAPIKeyModal() {
  const m = apiKeyModal.value
  if (!m) return
  m.error = ''
  if (!m.form.name.trim()) { m.error = '请输入名称'; return }
  if (m.form.scopes.length === 0) { m.error = '至少选择一个权限'; return }
  m.loading = true
  try {
    const data = await createAPIKey({
      name: m.form.name.trim(),
      scopes: m.form.scopes,
      rate_limit: m.form.rateLimit || 60,
    })
    // 创建成功：切换到明文展示态（仅此一次）
    m.plainText = data.plain_text
    await loadAPIKeys()
    toast('API Key 已创建', 'success')
  } catch (e) {
    m.error = e.message || '创建失败'
  } finally {
    m.loading = false
  }
}

async function removeAPIKey(id, name) {
  const ok = await confirm({
    title: '删除 API Key',
    message: `确定删除 "${name}" 吗？使用该 Key 的程序将立即无法发信。`,
    confirmText: '删除',
    cancelText: '取消',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await deleteAPIKey(id)
    apiKeys.value = apiKeys.value.filter(k => k.id !== id)
    toast('已删除', 'success')
  } catch (e) { toast(e.message || '删除失败', 'error') }
}

async function copyAPIKey(text) {
  try {
    await navigator.clipboard.writeText(text)
    toast('已复制到剪贴板', 'success')
  } catch {
    toast('复制失败，请手动选择', 'error')
  }
}

// ===== 模块 I：邮件规则管理（AC-12） =====
const rules = ref([])
const ruleModal = ref(null)

const ruleFieldOptions = [
  { value: 'from', label: '发件人' },
  { value: 'to', label: '收件人' },
  { value: 'subject', label: '主题' },
]
const ruleOpOptions = [
  { value: 'contains', label: '包含' },
  { value: 'equals', label: '等于' },
  { value: 'regex', label: '正则匹配' },
]
const ruleActionOptions = [
  { value: 'move', label: '移动到文件夹' },
  { value: 'mark_read', label: '标记已读' },
  { value: 'star', label: '加星' },
  { value: 'delete', label: '删除到回收站' },
]
const folderLabels = { INBOX: '收件箱', SENT: '已发送', DRAFTS: '草稿箱', TRASH: '回收站', JUNK: '垃圾邮件' }

function summarizeCondition(c) {
  if (!c) return '无条件'
  const fieldLabel = (ruleFieldOptions.find(f => f.value === c.field) || {}).label || c.field
  const opLabel = (ruleOpOptions.find(o => o.value === c.op) || {}).label || c.op
  return `${fieldLabel} ${opLabel} "${c.value}"`
}

function summarizeAction(a) {
  if (!a) return '无动作'
  const typeLabel = (ruleActionOptions.find(o => o.value === a.type) || {}).label || a.type
  if (a.type === 'move') return `${typeLabel}（${folderLabels[a.folder] || a.folder}）`
  return typeLabel
}

async function loadRules() {
  try {
    const data = await listRules()
    rules.value = Array.isArray(data) ? data : []
  } catch (e) {
    if (!String(e.message).includes('404')) {
      console.error('[SettingsView] load rules failed:', e)
    }
    rules.value = []
  }
}

function openCreateRuleModal() {
  ruleModal.value = {
    mode: 'create',
    form: {
      name: '',
      priority: 0,
      condField: 'from',
      condOp: 'contains',
      condValue: '',
      actionType: 'move',
      actionFolder: 'JUNK',
    },
    error: '',
    loading: false,
  }
}

function openEditRuleModal(r) {
  const c = r.conditions && r.conditions[0] ? r.conditions[0] : {}
  const a = r.actions && r.actions[0] ? r.actions[0] : {}
  ruleModal.value = {
    mode: 'edit',
    ruleId: r.id,
    form: {
      name: r.name || '',
      priority: r.priority ?? 0,
      condField: c.field || 'from',
      condOp: c.op || 'contains',
      condValue: typeof c.value === 'string' ? c.value : '',
      actionType: a.type || 'move',
      actionFolder: a.folder || 'JUNK',
    },
    error: '',
    loading: false,
  }
}

async function submitRuleModal() {
  const m = ruleModal.value
  if (!m) return
  m.error = ''
  if (!m.form.name.trim()) { m.error = '请输入规则名称'; return }
  if (!m.form.condValue.trim()) { m.error = '请输入匹配值'; return }
  m.loading = true
  try {
    // 组装 conditions / actions JSON（单条件 + 单动作）
    const conditions = [{ field: m.form.condField, op: m.form.condOp, value: m.form.condValue.trim() }]
    const actions = [{ type: m.form.actionType }]
    if (m.form.actionType === 'move') actions[0].folder = m.form.actionFolder

    if (m.mode === 'create') {
      await createRule({
        name: m.form.name.trim(),
        priority: m.form.priority || 0,
        conditions,
        actions,
      })
      toast('规则已创建', 'success')
    } else {
      await updateRule(m.ruleId, {
        name: m.form.name.trim(),
        priority: m.form.priority,
        conditions,
        actions,
      })
      toast('规则已更新', 'success')
    }
    ruleModal.value = null
    await loadRules()
  } catch (e) {
    m.error = e.message || '操作失败'
  } finally {
    m.loading = false
  }
}

async function toggleRuleActive(r) {
  try {
    await updateRule(r.id, { is_active: !r.is_active })
    r.is_active = !r.is_active
    toast(r.is_active ? '已启用' : '已禁用', 'success')
  } catch (e) { toast(e.message || '操作失败', 'error') }
}

async function removeRule(id, name) {
  const ok = await confirm({
    title: '删除规则',
    message: `确定删除规则 "${name}" 吗？`,
    confirmText: '删除',
    cancelText: '取消',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await deleteRule(id)
    rules.value = rules.value.filter(r => r.id !== id)
    toast('已删除', 'success')
  } catch (e) { toast(e.message || '删除失败', 'error') }
}
</script>
