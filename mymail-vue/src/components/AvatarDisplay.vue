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
import { computed, ref } from 'vue'

const props = defineProps({
  src: { type: String, default: '' },
  name: { type: String, default: '' },
  size: { type: [String, Number], default: 32 },
})

const error = ref(false)

const effectiveSrc = computed(() => (error.value ? '' : props.src))
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
