import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import { ElMessage } from 'element-plus'
import { errorMessage } from '@/api'
import AppLayout from '@/layouts/AppLayout.vue'
import LoginView from '@/views/LoginView.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    /** 不需要登录即可访问 */
    public?: boolean
    /** 仅管理员可访问 */
    admin?: boolean
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView, meta: { title: '登录', public: true } },
    {
      path: '/',
      component: AppLayout,
      children: [
        { path: '', redirect: '/dashboard' },
        {
          path: 'dashboard',
          name: 'dashboard',
          component: () => import('@/views/DashboardView.vue'),
          meta: { title: '概览' },
        },
        {
          path: 'resources',
          name: 'resources',
          component: () => import('@/views/ResourcesView.vue'),
          meta: { title: '资源中心' },
        },
        {
          path: 'monitor',
          name: 'monitor',
          component: () => import('@/views/MonitorView.vue'),
          meta: { title: '监控中心' },
        },
        {
          path: 'accounts',
          name: 'accounts',
          component: () => import('@/views/AccountsView.vue'),
          meta: { title: '云账号' },
        },
        {
          path: 'system/users',
          name: 'users',
          component: () => import('@/views/UsersView.vue'),
          meta: { title: '用户管理', admin: true },
        },
        {
          path: 'system/audit',
          name: 'audit',
          component: () => import('@/views/AuditView.vue'),
          meta: { title: '审计日志', admin: true },
        },
        {
          path: ':pathMatch(.*)*',
          name: 'not-found',
          component: () => import('@/views/NotFoundView.vue'),
          meta: { title: '页面不存在' },
        },
      ],
    },
  ],
  scrollBehavior(to, from, saved) {
    if (saved) return saved
    // 同一页面内只改查询参数（筛选、分页、打开抽屉）时保持滚动位置。
    if (to.path === from.path) return false
    return { top: 0 }
  },
})

function loginRoute(to: RouteLocationNormalized) {
  return to.fullPath === '/' || to.name === 'dashboard'
    ? { name: 'login' }
    : { name: 'login', query: { redirect: to.fullPath } }
}

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.meta.public) {
    // 已登录用户访问登录页时直接进入系统。
    if (to.name === 'login' && auth.token) {
      const user = await auth.ensureUser().catch(() => null)
      if (user) return { path: '/dashboard' }
    }
    return true
  }
  if (!auth.token) return loginRoute(to)
  try {
    const user = await auth.ensureUser()
    if (!user) return loginRoute(to)
    if (to.meta.admin && user.role !== 'admin') {
      ElMessage.warning('该页面仅管理员可访问')
      return { path: '/dashboard' }
    }
  } catch (err) {
    ElMessage.error(errorMessage(err))
    return loginRoute(to)
  }
  return true
})

router.afterEach((to) => {
  const name = useAppStore().meta.name || '云枢'
  document.title = to.meta.title ? `${to.meta.title} · ${name}` : name
})
