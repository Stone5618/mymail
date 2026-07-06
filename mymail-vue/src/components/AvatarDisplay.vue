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
const cacheBust = ref(Date.now())

// 当头像 URL 变化时更新缓存破坏参数，避免浏览器仍显示旧图
watch(() => props.src, () => {
  error.value = false
  if (props.src && props.src.startsWith('/api/avatars/')) {
    cacheBust.value = Date.now()
  }
}, { immediate: true })

const effectiveSrc = computed(() => {
  if (error.value) return ''
  const src = props.src
  if (!src || !src.startsWith('/api/avatars/')) return src
  const sep = src.includes('?') ? '&' : '?'
  return `${src}${sep}t=${cacheBust.value}`
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
