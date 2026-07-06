import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { title: '登录', public: true } },
  {
    path: '/',
    component: () => import('@/components/AppLayout.vue'),
    meta: { title: '邮件' },
    children: [
      { path: '', redirect: '/inbox' },
      { path: 'inbox', name: 'inbox', component: () => import('@/views/MailView.vue'), props: { folder: 'INBOX' }, meta: { title: '收件箱' } },
      { path: 'sent', name: 'sent', component: () => import('@/views/MailView.vue'), props: { folder: 'SENT' }, meta: { title: '已发送' } },
      { path: 'drafts', name: 'drafts', component: () => import('@/views/MailView.vue'), props: { folder: 'DRAFTS' }, meta: { title: '草稿箱' } },
      { path: 'trash', name: 'trash', component: () => import('@/views/MailView.vue'), props: { folder: 'TRASH' }, meta: { title: '回收站' } },
      { path: 'junk', name: 'junk', component: () => import('@/views/MailView.vue'), props: { folder: 'JUNK' }, meta: { title: '垃圾邮件' } },
      { path: 'mail/:id', name: 'mail-detail', component: () => import('@/views/MailDetailView.vue'), props: true, meta: { title: '邮件详情' } },
      { path: 'compose', name: 'compose', component: () => import('@/views/ComposeView.vue'), meta: { title: '写邮件' } },
      { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { title: '设置' } },
      { path: 'about', name: 'about', component: () => import('@/views/AboutView.vue'), meta: { title: '关于' } },
      { path: 'admin', name: 'admin', component: () => import('@/views/AdminView.vue'), meta: { title: '管理', requiresAdmin: true } },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫：未登录跳转登录页，非管理员访问管理页跳转收件箱
router.beforeEach((to) => {
  const token = localStorage.getItem('token')
  if (!token && to.name !== 'login') return { name: 'login' }
  if (to.meta.requiresAdmin) {
    const role = localStorage.getItem('role')
    if (role !== 'admin') return { name: 'inbox' }
  }
})

// 根据路由 meta.title 更新页面标题
router.afterEach((to) => {
  const title = to.meta?.title
  document.title = title ? `${title} - MyMail` : 'MyMail - 邮件平台'
})

export default router
