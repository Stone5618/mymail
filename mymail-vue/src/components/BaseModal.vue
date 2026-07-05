<script setup>
// BaseModal.vue 模态弹窗
// 用法：<BaseModal v-model="show" title="标题">内容</BaseModal>
import { watch } from 'vue'
import BaseIcon from './BaseIcon.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  title: { type: String, default: '' },
  size: { type: String, default: 'md' }, // sm | md | lg
  closeOnBackdrop: { type: Boolean, default: true },
})

const emit = defineEmits(['update:modelValue'])

const sizeCls = {
  sm: 'max-w-sm',
  md: 'max-w-md',
  lg: 'max-w-2xl',
}[props.size] || 'max-w-md'

function close() {
  emit('update:modelValue', false)
}

// ESC 关闭 + body scroll lock
watch(() => props.modelValue, (v) => {
  if (v) {
    document.body.style.overflow = 'hidden'
    const onEsc = (e) => { if (e.key === 'Escape') close() }
    document.addEventListener('keydown', onEsc, { once: true })
  } else {
    document.body.style.overflow = ''
  }
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="modelValue" class="fixed inset-0 z-[100] flex items-center justify-center p-4">
        <div class="absolute inset-0 bg-black/60 backdrop-blur-sm" @click="closeOnBackdrop && close()" />
        <div class="relative w-full bg-dark-900 border border-dark-700/80 rounded-2xl shadow-2xl overflow-hidden" :class="sizeCls">
          <div v-if="title || $slots.header" class="flex items-center justify-between px-5 py-3.5 border-b border-dark-800">
            <h3 class="text-sm font-semibold text-dark-100">{{ title }}</h3>
            <button @click="close" class="text-dark-500 hover:text-dark-300 transition-colors p-1 rounded-md" aria-label="关闭弹窗">
              <BaseIcon name="x-mark" class="h-5 w-5" />
            </button>
          </div>
          <div class="p-5">
            <slot />
          </div>
          <div v-if="$slots.footer" class="px-5 py-3 border-t border-dark-800 flex items-center justify-end gap-2">
            <slot name="footer" />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-enter-active, .modal-leave-active {
  transition: opacity 0.2s ease;
}
.modal-enter-from, .modal-leave-to {
  opacity: 0;
}
</style>
