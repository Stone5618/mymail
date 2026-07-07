import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { version } from './package.json'

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    {
      name: 'html-transform',
      transformIndexHtml(html) {
        return html.replace(/%APP_VERSION%/g, version)
      },
    },
  ],
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'https://pymt.qzz.io',
        changeOrigin: true,
      },
      '/ws': {
        target: 'wss://pymt.qzz.io',
        ws: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        // 将 vue-router 拆到独立 chunk，打破 main.js ↔ router/index.js 的循环依赖
        // 否则 Vite 会把 vue-router 并入主 chunk，导致 router chunk 在主 chunk
        // 完成初始化前调用 createWebHistory()，引发 "$m is not a function"
        manualChunks(id) {
          if (id.includes('node_modules/vue-router')) {
            return 'vue-router'
          }
        },
      },
    },
  },
})
