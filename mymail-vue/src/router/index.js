import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { title: '登录', public: true } },
  { path: '/change-default-password', name: 'change-default-password', component: () => import('@/views/ChangeDefaultPasswordView.vue'), meta: { title: '修改默认密码', public: true } },
  {
    path: '/',
    component: () => import('@/components/AppLayout.vue'),
    meta: { title: '邮件' },
    children: [
      { path: '', redirect: '/inbox', meta: { title: '收件箱' } },
      { path: 'inbox', name: 'inbox', component: () => import('@/views/MailView.vue'), props: { folder: 'INBOX' }, meta: { title: '收件箱' } },
      { path: 'sent', name: 'sent', component: () => import('@/views/MailView.vue'), props: { folder: 'SENT' }, meta: { title: '已发送' } },
      { path: 'drafts', name: 'drafts', component: () => import('@/views/MailView.vue'), props: { folder: 'DRAFTS' }, meta: { title: '草稿箱' } },
      { path: 'trash', name: 'trash', component: () => import('@/views/MailView.vue'), props: { folder: 'TRASH' }, meta: { title: '回收站' } },
      { path: 'junk', name: 'junk', component: () => import('@/views/MailView.vue'), props: { folder: 'JUNK' }, meta: { title: '垃圾邮件' } },
      { path: 'mail/:id', name: 'mail-detail', component: () => import('@/views/MailDetailView.vue'), props: true, meta: { title: '邮件详情' } },
      { path: 'compose', name: 'compose', component: () => import('@/views/ComposeView.vue'), meta: { title: '写邮件' } },
      { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { title: '设置' } },
      { path: 'about', name: 'about', component: () => import('@/views/AboutView.vue'), meta: { title: '关于' } },
      {
        path: 'admin',
        component: () => import('@/views/AdminView.vue'),
        meta: { title: '管理', requiresAdmin: true },
        children: [
          { path: '', redirect: { name: 'admin-overview' } },
          { path: 'overview', name: 'admin-overview', component: () => import('@/views/admin/AdminOverview.vue'), meta: { title: '概览' } },
          { path: 'users', name: 'admin-users', component: () => import('@/views/admin/AdminUsers.vue'), meta: { title: '用户管理' } },
          { path: 'mails', name: 'admin-mails', component: () => import('@/views/admin/AdminMails.vue'), meta: { title: '邮件管理' } },
          { path: 'audit', name: 'admin-audit', component: () => import('@/views/admin/AdminAudit.vue'), meta: { title: '审计日志' } },
          { path: 'config', name: 'admin-config', component: () => import('@/views/admin/AdminConfig.vue'), meta: { title: '系统配置' } },
          { path: 'maintenance', name: 'admin-maintenance', component: () => import('@/views/admin/AdminMaintenance.vue'), meta: { title: '维护工具' } },
        ],
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫：未登录跳转登录页，非管理员访问管理页跳转收件箱，强制改密
router.beforeEach(async (to) => {
  const token = localStorage.getItem('token')
  if (!token && !to.meta.public) return { name: 'login' }
  if (token && to.name === 'login') return { name: 'inbox' }

  // 强制改密检查（需登录但非改密页）
  if (token && to.name !== 'change-default-password') {
    const { useAuthStore } = await import('@/stores/auth')
    const auth = useAuthStore()
    // 首次进入或刷新后，fetchMe 尚未执行时，从 localStorage 兜底
    if (auth.requirePasswordChange) return { name: 'change-default-password' }
  }

  if (to.meta.requiresAdmin) {
    const role = localStorage.getItem('role')
    if (role !== 'admin') return { name: 'inbox' }
  }
})

// 根据路由 meta.title 更新页面标题
router.afterEach((to) => {
  if (!to || !to.meta) {
    document.title = 'MyMail - 邮件平台'
    return
  }
  const title = to.meta.title
  document.title = title ? `${title} - MyMail` : 'MyMail - 邮件平台'
})

export default router
