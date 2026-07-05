import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue') },
  {
    path: '/',
    component: () => import('@/components/AppLayout.vue'),
    children: [
      { path: '', redirect: '/inbox' },
      { path: 'inbox', name: 'inbox', component: () => import('@/views/MailView.vue'), props: { folder: 'INBOX' } },
      { path: 'sent', name: 'sent', component: () => import('@/views/MailView.vue'), props: { folder: 'SENT' } },
      { path: 'drafts', name: 'drafts', component: () => import('@/views/MailView.vue'), props: { folder: 'DRAFTS' } },
      { path: 'trash', name: 'trash', component: () => import('@/views/MailView.vue'), props: { folder: 'TRASH' } },
      { path: 'junk', name: 'junk', component: () => import('@/views/MailView.vue'), props: { folder: 'JUNK' } },
      { path: 'mail/:id', name: 'mail-detail', component: () => import('@/views/MailDetailView.vue'), props: true },
      { path: 'compose', name: 'compose', component: () => import('@/views/ComposeView.vue') },
      { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue') },
      { path: 'about', name: 'about', component: () => import('@/views/AboutView.vue') },
      // P0-5：admin 路由加 meta.requiresAdmin 守卫
      { path: 'admin', name: 'admin', component: () => import('@/views/AdminView.vue'), meta: { requiresAdmin: true } },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫：
//   1. 未登录 → 跳转登录页
//   2. P0-5：非管理员访问 requiresAdmin 路由 → 重定向到 /inbox
router.beforeEach((to) => {
  const token = localStorage.getItem('token')
  if (!token && to.name !== 'login') return { name: 'login' }
  // P0-5：管理员路由守卫（role 由 auth store 在登录/拉取用户时写入 localStorage）
  if (to.meta.requiresAdmin) {
    const role = localStorage.getItem('role')
    if (role !== 'admin') return { name: 'inbox' }
  }
})

export default router
