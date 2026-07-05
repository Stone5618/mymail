<template>
  <div class="space-y-0">
    <div v-for="i in count" :key="i" class="flex items-center gap-3 px-4 sm:px-6 py-3 border-b border-dark-800/50">
      <div class="skel w-4 h-4 rounded" />
      <div class="skel w-1.5 h-1.5 rounded-full" />
      <div class="flex-1 space-y-2">
        <div class="flex items-center justify-between">
          <div class="skel h-4 rounded" :style="{ width: rw(100, 200) + 'px' }" />
          <div class="skel h-3 rounded w-12" />
        </div>
        <div class="skel h-3.5 rounded" :style="{ width: rw(200, 400) + 'px' }" />
      </div>
    </div>
  </div>
</template>

<script setup>
defineProps({ count: { type: Number, default: 6 } })

function rw(min, max) {
  return Math.floor(Math.random() * (max - min) + min)
}
</script>

<style scoped>
.skel {
  background: linear-gradient(90deg, var(--c-bg-elevated) 25%, var(--c-bg-hover) 50%, var(--c-bg-elevated) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
}
@keyframes shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
</style>
