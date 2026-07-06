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
import { updateProfile, changePassword, uploadAvatar } from '@/api'
import { useToast } from '@/composables/useToast'
import BaseIcon from '@/components/BaseIcon.vue'
import BaseSpinner from '@/components/BaseSpinner.vue'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const theme = useThemeStore()
const userPrefs = useUserPreferencesStore()
const { toast } = useToast()

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
</script>
