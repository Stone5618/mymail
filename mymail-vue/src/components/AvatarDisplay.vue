<template>
  <div
    class="rounded-full overflow-hidden shrink-0 flex items-center justify-center bg-primary-600 text-white font-medium"
    :style="containerStyle"
  >
    <img v-if="effectiveSrc" :src="effectiveSrc" class="w-full h-full object-cover" alt="" @error="onError" />
    <span v-else>{{ initial }}</span>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'

const props = defineProps({
  src: { type: String, default: '' },
  name: { type: String, default: '' },
  size: { type: [String, Number], default: 32 },
})

const error = ref(false)

// src 变化时重置错误状态
watch(() => props.src, () => {
  error.value = false
})

// 直接使用 src，不加 cacheBust 参数
// 浏览器会基于 URL 缓存头像，避免每次组件挂载都重复加载
// 上传新头像后由 SettingsView 负责添加 ?t= 参数刷新缓存
const effectiveSrc = computed(() => {
  if (error.value) return ''
  return props.src
})
const initial = computed(() => {
  const n = props.name || '?'
  return n.charAt(0).toUpperCase()
})
const pxSize = computed(() => (typeof props.size === 'number' ? props.size + 'px' : props.size))
const containerStyle = computed(() => ({
  width: pxSize.value,
  height: pxSize.value,
  fontSize: `calc(${pxSize.value} * 0.4)`,
}))

function onError() {
  error.value = true
}
</script>
