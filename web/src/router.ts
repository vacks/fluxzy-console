import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { public: true, title: '登录' },
  },
  {
    path: '/setup',
    name: 'setup',
    component: () => import('@/views/SetupView.vue'),
    meta: { public: true, title: '初始化' },
  },
  {
    path: '/',
    component: () => import('@/components/layout/AppShell.vue'),
    children: [
      {
        path: '',
        name: 'overview',
        component: () => import('@/views/OverviewView.vue'),
        meta: { title: '概览' },
      },
      {
        path: 'live',
        name: 'live',
        component: () => import('@/views/LiveView.vue'),
        meta: { title: '实时流量' },
      },
      {
        path: 'sessions',
        name: 'sessions',
        component: () => import('@/views/SessionsView.vue'),
        meta: { title: '会话历史' },
      },
      {
        path: 'sessions/:id',
        name: 'session',
        component: () => import('@/views/SessionView.vue'),
        meta: { title: '会话详情' },
      },
      {
        path: 'rules',
        name: 'rules',
        component: () => import('@/views/RulesView.vue'),
        meta: { title: '规则管理' },
      },
      {
        path: 'audit',
        name: 'audit',
        component: () => import('@/views/AuditView.vue'),
        meta: { title: '日志', admin: true },
      },
      {
        path: 'users',
        name: 'users',
        component: () => import('@/views/UsersView.vue'),
        meta: { title: '用户管理', admin: true },
      },
      {
        path: 'runtime',
        name: 'runtime',
        component: () => import('@/views/RuntimeView.vue'),
        meta: { title: '运行时' },
      },
      {
        path: 'settings',
        name: 'settings',
        component: () => import('@/views/SettingsView.vue'),
        meta: { title: '设置' },
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    redirect: '/',
  },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(to, from, saved) {
    if (saved) return saved
    if (to.name === 'live') return false
    return { top: 0 }
  },
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.loaded) {
    try {
      await auth.load()
    } catch {
      // Do not leave the SPA with an unresolved guard and a blank page.
      return { name: 'login', query: { redirect: to.fullPath } }
    }
  }

  if (auth.setupRequired && to.name !== 'setup') {
    return { name: 'setup' }
  }
  if (!auth.setupRequired && to.name === 'setup') {
    return { name: auth.user ? 'overview' : 'login' }
  }
  if (to.meta.public) {
    if (auth.user && (to.name === 'login' || to.name === 'setup')) return { name: 'overview' }
    return true
  }
  if (!auth.user) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.meta.admin && !auth.isAdmin) {
    return { name: 'overview' }
  }
  return true
})

router.afterEach((to) => {
  const title = (to.meta.title as string) || '控制台'
  document.title = `${title} · FLUXZY · CONSOLE`
})
