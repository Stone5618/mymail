<template>
  <router-view v-slot="{ Component }">
    <transition name="page" mode="out-in">
      <component :is="Component" />
    </transition>
  </router-view>
  <ConfirmContainer />
</template>

<script setup>
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { useUserPreferencesStore } from '@/stores/userPreferences'
import ConfirmContainer from '@/components/ConfirmContainer.vue'

const auth = useAuthStore()
const theme = useThemeStore()
const userPrefs = useUserPreferencesStore()

onMounted(async () => {
  // 初始化主题
  theme.applyTheme()
  if (auth.token) {
    await auth.fetchMe()
    // 登录用户加载偏好设置
    await userPrefs.load()
  }
})
</script>
