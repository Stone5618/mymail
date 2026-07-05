<template>
  <div class="spinner" :style="{ width: pxSize, height: pxSize }" role="status">
    <span class="sr-only">加载中...</span>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  size: { type: [String, Number], default: 24 },
})

const pxSize = computed(() => (typeof props.size === 'number' ? props.size + 'px' : props.size))
</script>

<style scoped>
.spinner {
  border: 3px solid var(--c-bg-hover);
  border-top-color: var(--color-primary-500);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
