<script setup>
// BaseButton.vue 统一按钮组件
// 用法：<BaseButton variant="primary" size="md" :loading="saving">保存</BaseButton>
import BaseIcon from './BaseIcon.vue'

const props = defineProps({
  variant: { type: String, default: 'ghost' }, // primary | ghost | danger | subtle
  size: { type: String, default: 'md' },       // sm | md | lg
  icon: { type: String, default: '' },          // Heroicon name
  iconRight: { type: String, default: '' },
  loading: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  type: { type: String, default: 'button' },
  block: { type: Boolean, default: false },
})

const sizeCls = {
  sm: 'px-2.5 py-1.5 text-xs gap-1',
  md: 'px-3.5 py-2 text-sm gap-1.5',
  lg: 'px-5 py-2.5 text-sm gap-2',
}[props.size] || 'px-3.5 py-2 text-sm gap-1.5'

const iconSize = props.size === 'sm' ? 'h-3.5 w-3.5' : props.size === 'lg' ? 'h-5 w-5' : 'h-4 w-4'

const variantCls = {
  primary: 'bg-primary-600 hover:bg-primary-500 text-white font-medium shadow-sm',
  ghost: 'text-dark-300 hover:bg-dark-800 hover:text-dark-100',
  danger: 'text-red-400 hover:bg-red-500/10 hover:text-red-300',
  subtle: 'bg-dark-800 text-dark-200 hover:bg-dark-700',
}[props.variant] || 'text-dark-300 hover:bg-dark-800'
</script>

<template>
  <button
    :type="type"
    :disabled="disabled || loading"
    class="inline-flex items-center justify-center rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50"
    :class="[variantCls, sizeCls, { 'w-full': block }]"
  >
    <BaseIcon v-if="loading" name="arrow-path" :class="iconSize" class="animate-spin" />
    <BaseIcon v-else-if="icon" :name="icon" :class="iconSize" />
    <slot />
    <BaseIcon v-if="iconRight && !loading" :name="iconRight" :class="iconSize" />
  </button>
</template>
