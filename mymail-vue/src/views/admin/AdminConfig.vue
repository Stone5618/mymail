<template>
  <div class="max-w-4xl mx-auto">
    <div class="card p-4 sm:p-6">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-base sm:text-lg font-medium text-dark-100 flex items-center gap-2">
          <BaseIcon name="cog-6-tooth" class="h-4 w-4 text-primary-400" />
          <span>系统配置</span>
        </h3>
        <button @click="loadConfig" class="btn-ghost text-sm inline-flex items-center gap-1">
          <BaseIcon name="arrow-path" class="h-4 w-4" :class="{ 'animate-spin': configLoading }" />
          <span>刷新</span>
        </button>
      </div>
      <p class="text-xs text-dark-500 mb-3">配置通过环境变量加载，只读展示。修改需重启服务。</p>
      <div v-if="configGroups.length === 0 && !configLoading" class="text-sm text-dark-500 py-4 text-center">点击刷新加载</div>
      <div v-else class="space-y-4">
        <div v-for="g in configGroups" :key="g.key">
          <h4 class="text-xs font-semibold text-dark-400 uppercase tracking-wide mb-2">{{ g.label }}</h4>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <div v-for="item in g.items" :key="item.key" class="flex items-center justify-between bg-dark-900/40 border border-dark-700/40 rounded-lg px-3 py-2">
              <span class="text-xs text-dark-400 truncate mr-2">{{ item.label }}</span>
              <span class="text-xs font-mono text-dark-200 truncate" :class="{ 'text-amber-400': item.secret }">{{ item.value || '-' }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { getSystemConfig } from '@/api'
import { useToast } from '@/composables/useToast'
import BaseIcon from '@/components/BaseIcon.vue'

const { toast } = useToast()

const configGroups = ref([])
const configLoading = ref(false)

async function loadConfig() {
  configLoading.value = true
  try {
    const data = await getSystemConfig()
    configGroups.value = data.groups || []
  } catch (e) {
    toast(e.message || '加载配置失败', 'error')
  } finally {
    configLoading.value = false
  }
}

onMounted(loadConfig)
</script>
