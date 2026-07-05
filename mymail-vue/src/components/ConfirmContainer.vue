<script setup>
// ConfirmContainer.vue 全局确认弹窗容器
// 放在 App.vue 中，配合 useConfirm 使用
import { useConfirm } from '@/composables/useConfirm'
import BaseModal from './BaseModal.vue'
import BaseButton from './BaseButton.vue'
import BaseIcon from './BaseIcon.vue'

const { visible, opts, resolve } = useConfirm()
</script>

<template>
  <BaseModal v-model="visible.value" :title="opts.value.title" size="sm" :close-on-backdrop="false">
    <div class="flex gap-3">
      <div class="shrink-0 w-10 h-10 rounded-full flex items-center justify-center"
        :class="opts.value.variant === 'danger' ? 'bg-red-500/10 text-red-400' : 'bg-primary-500/10 text-primary-400'">
        <BaseIcon :name="opts.value.variant === 'danger' ? 'exclamation-triangle' : 'information-circle'" class="h-6 w-6" />
      </div>
      <p class="text-sm text-dark-300 pt-1.5">{{ opts.value.message }}</p>
    </div>
    <template #footer>
      <BaseButton variant="ghost" size="md" @click="resolve(false)">{{ opts.value.cancelText }}</BaseButton>
      <BaseButton :variant="opts.value.variant === 'danger' ? 'danger' : 'primary'" size="md" @click="resolve(true)">{{ opts.value.confirmText }}</BaseButton>
    </template>
  </BaseModal>
</template>
