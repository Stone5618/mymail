<script setup>
// BaseIcon.vue 统一图标入口，基于 @heroicons/vue。
// 用法：<BaseIcon name="inbox" class="h-5 w-5" />
// name 使用小写下划线命名（去 Icon 后缀），如 inbox / paper-airplane / trash
import { computed } from 'vue'
import * as outline from '@heroicons/vue/24/outline'
import * as solid from '@heroicons/vue/24/solid'

const props = defineProps({
  name: { type: String, required: true },
  solid: { type: Boolean, default: false },
})

const icon = computed(() => {
  const lib = props.solid ? solid : outline
  const key = props.name
    .split(/[-_]/)
    .map(s => s.charAt(0).toUpperCase() + s.slice(1))
    .join('') + 'Icon'
  return lib[key] || outline.QuestionMarkCircleIcon
})
</script>

<template>
  <component :is="icon" aria-hidden="true" />
</template>
