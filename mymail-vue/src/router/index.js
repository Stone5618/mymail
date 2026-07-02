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
      { path: 'admin', name: 'admin', component: () => import('@/views/AdminView.vue') },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫：未登录跳转登录页
router.beforeEach((to) => {
  const token = localStorage.getItem('token')
  if (!token && to.name !== 'login') return { name: 'login' }
})

export default router
