<script setup>
defineProps({
  size: { type: [String, Number], default: 24 },
})
</script>

<template>
  <svg
    xmlns="http://www.w3.org/2000/svg"
    :width="size"
    :height="size"
    viewBox="0 0 64 64"
    fill="none"
    aria-hidden="true"
  >
    <rect width="64" height="64" rx="14" fill="#4f46e5" />
    <path d="M12 20h40l-20 16z" fill="#fff" opacity=".9" />
    <path d="M12 44l16-12h4l4 3 4-3h4l16 12z" fill="#fff" opacity=".6" />
  </svg>
</template>
