<template>
  <div class="flex-1 flex flex-col min-h-0">
    <div class="flex items-center justify-between px-4 sm:px-6 py-3 sm:py-4 border-b border-dark-800">
      <h2 class="text-lg sm:text-xl font-semibold text-dark-100">⚙️ 设置</h2>
      <button @click="router.push('/inbox')" class="btn-ghost text-sm">← 返回</button>
    </div>
    <div class="flex-1 overflow-y-auto p-4 sm:p-6">
      <div class="max-w-2xl mx-auto space-y-6">

        <!-- 个人信息 -->
        <div class="card p-4 sm:p-6">
          <h3 class="text-base sm:text-lg font-medium text-dark-100 mb-4">个人信息</h3>
          <div class="space-y-4">
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
import { ref, onMounted, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { updateProfile, changePassword } from '@/api'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const { toast } = useToast()

const displayName = ref('')
const signature = ref('')
const oldPw = ref('')
const newPw = ref('')
const newPw2 = ref('')

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
