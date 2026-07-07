<template>
  <div class="max-w-4xl mx-auto">
    <div class="card p-4 sm:p-6">
      <h3 class="text-base sm:text-lg font-medium text-dark-100 flex items-center gap-2 mb-4">
        <BaseIcon name="wrench-screwdriver" class="h-4 w-4 text-primary-400" />
        <span>维护工具</span>
      </h3>
      <div class="space-y-3">
        <div class="flex items-center justify-between bg-dark-900/40 border border-dark-700/40 rounded-lg px-3 py-3">
          <div class="min-w-0 flex-1">
            <div class="text-sm text-dark-200">重新净化全部邮件</div>
            <div class="text-xs text-dark-500 mt-0.5">用当前净化策略对全量 body_html_raw 重新净化并更新 body_html。适用于净化策略变更后修复旧邮件。</div>
          </div>
          <button @click="resanitize" :disabled="resanitizeLoading" class="btn-secondary text-sm ml-3 shrink-0 inline-flex items-center gap-1">
            <BaseIcon v-if="resanitizeLoading" name="arrow-path" class="h-4 w-4 animate-spin" />
            <BaseIcon v-else name="arrow-path" class="h-4 w-4" />
            <span>{{ resanitizeLoading ? '执行中...' : '执行' }}</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { resanitizeAllMails } from '@/api'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import BaseIcon from '@/components/BaseIcon.vue'

const { toast } = useToast()
const { confirm } = useConfirm()

const resanitizeLoading = ref(false)

async function resanitize() {
  const ok = await confirm({
    title: '重新净化全部邮件',
    message: '将用当前净化策略对所有邮件的 body_html_raw 重新净化并更新 body_html。此操作可能耗时，确认执行？',
    confirmText: '执行',
    cancelText: '取消',
    variant: 'danger',
  })
  if (!ok) return
  resanitizeLoading.value = true
  try {
    const data = await resanitizeAllMails()
    toast(`已完成：共 ${data.total} 封，更新 ${data.updated} 封，跳过 ${data.skipped} 封`, 'success')
  } catch (e) {
    toast(e.message || '重新净化失败', 'error')
  } finally {
    resanitizeLoading.value = false
  }
}
</script>
