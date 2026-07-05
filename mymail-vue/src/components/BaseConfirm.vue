<script setup>
// BaseConfirm.vue 确认弹窗，替代原生 confirm()
// 用法：
//   const { confirm } = useConfirm()
//   if (await confirm('确定删除？', '此操作不可恢复')) { ... }
import { ref, createApp } from 'vue'
import BaseModal from './BaseModal.vue'
import BaseButton from './BaseButton.vue'
import BaseIcon from './BaseIcon.vue'

const show = ref(false)
const title = ref('')
const message = ref('')
const confirmText = ref('确定')
const cancelText = ref('取消')
const variant = ref('danger') // danger | primary
let resolver = null

function open(opts = {}) {
  title.value = opts.title || '请确认'
  message.value = opts.message || ''
  confirmText.value = opts.confirmText || '确定'
  cancelText.value = opts.cancelText || '取消'
  variant.value = opts.variant || 'danger'
  show.value = true
  return new Promise(resolve => { resolver = resolve })
}

function handleConfirm() {
  show.value = false
  resolver?.(true)
}

function handleCancel() {
  show.value = false
  resolver?.(false)
}
</script>

<template>
  <BaseModal v-model="show" :title="title" size="sm" :close-on-backdrop="false">
    <div class="flex gap-3">
      <div class="shrink-0 w-10 h-10 rounded-full flex items-center justify-center"
        :class="variant === 'danger' ? 'bg-red-500/10 text-red-400' : 'bg-primary-500/10 text-primary-400'">
        <BaseIcon :name="variant === 'danger' ? 'exclamation-triangle' : 'information-circle'" class="h-6 w-6" />
      </div>
      <p class="text-sm text-dark-300 pt-1.5">{{ message }}</p>
    </div>
    <template #footer>
      <BaseButton variant="ghost" size="md" @click="handleCancel">{{ cancelText }}</BaseButton>
      <BaseButton :variant="variant === 'danger' ? 'danger' : 'primary'" size="md" @click="handleConfirm">{{ confirmText }}</BaseButton>
    </template>
  </BaseModal>
</template>
