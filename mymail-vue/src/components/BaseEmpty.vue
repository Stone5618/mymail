<script setup>
// BaseEmpty.vue 空状态组件
// 用法：<BaseEmpty icon="inbox" title="收件箱为空" description="还没有邮件" />
import BaseIcon from './BaseIcon.vue'

defineProps({
  icon: { type: String, default: 'inbox' },
  title: { type: String, default: '暂无数据' },
  description: { type: String, default: '' },
})
</script>

<template>
  <div class="flex flex-col items-center justify-center py-16 text-center">
    <div class="w-16 h-16 rounded-2xl bg-dark-800 flex items-center justify-center mb-4">
      <BaseIcon :name="icon" class="h-8 w-8 text-dark-500" />
    </div>
    <p class="text-sm font-medium text-dark-300">{{ title }}</p>
    <p v-if="description" class="text-xs text-dark-500 mt-1">{{ description }}</p>
    <div class="mt-4">
      <slot />
    </div>
  </div>
</template>
