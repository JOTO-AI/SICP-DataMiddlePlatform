import { createRouter, createWebHistory } from 'vue-router'
import { session } from '../session'

export const navigation = [
  { id: 'overview', path: '/overview', title: '运行总览', icon: '◫' },
  { id: 'queries', path: '/apis', title: '接口中心', icon: '⌕' },
  { id: 'tasks', path: '/updates', title: '更新事件', icon: '↻' },
  { id: 'sources', path: '/sources', title: '数据源', icon: '▤' },
  { id: 'logs', path: '/logs', title: '日志中心', icon: '≡' },
  { id: 'notifications', path: '/notifications', title: '通知中心', icon: '♧' },
  { id: 'settings', path: '/settings', title: '系统设置', icon: '⚙' },
] as const
const pending = () => import('../pages/ModulePending.vue')
export const router = createRouter({
  history: createWebHistory('/console/'),
  routes: [
    { path: '/', redirect: '/overview' },
    { path: '/login', name: 'login', component: () => import('../pages/LoginPage.vue') },
    { path: '/overview', name: 'overview', meta: { navigation: 'overview' }, component: () => import('../pages/Overview.vue') },
    { path: '/apis', name: 'queries', meta: { navigation: 'queries' }, component: () => import('../pages/Queries.vue') },
    { path: '/apis/:apiId', name: 'api-detail', meta: { navigation: 'queries' }, component: pending, props: { title: '接口详情' } },
    { path: '/updates', name: 'tasks', meta: { navigation: 'tasks' }, component: () => import('../pages/Tasks.vue') },
    { path: '/updates/:id', name: 'update-detail', meta: { navigation: 'tasks' }, component: pending, props: { title: '更新详情' } },
    { path: '/sources', name: 'sources', meta: { navigation: 'sources' }, component: () => import('../pages/Sources.vue') },
    ...navigation.filter(item => ['logs', 'notifications', 'settings'].includes(item.id)).map(item => ({ path: item.path, name: item.id, meta: { navigation: item.id }, component: pending, props: { title: item.title } })),
    { path: '/logs/:source(platform|upstream)/events/:eventId([a-f0-9]{64})', name: 'log-detail', meta: { navigation: 'logs' }, component: pending, props: { title: '日志详情' } },
    { path: '/logs/:source', name: 'log-source', meta: { navigation: 'logs' }, component: pending, props: { title: '日志中心' } },
    { path: '/settings/:section', name: 'settings-section', meta: { navigation: 'settings' }, component: pending, props: { title: '系统设置' } },
    { path: '/queries', redirect: to => ({ path: '/apis', query: to.query, hash: to.hash }) },
    { path: '/sync/tasks/:taskId', redirect: to => ({ name: 'update-detail', params: { id: to.params.taskId }, query: to.query }) },
    { path: '/tasks/:taskId', redirect: to => ({ name: 'update-detail', params: { id: to.params.taskId }, query: to.query }) },
    { path: '/tasks', redirect: to => ({ path: '/updates', query: to.query, hash: to.hash }) },
    { path: '/403', name: 'forbidden', component: () => import('../pages/RouteError.vue'), props: { status: 403 } },
    { path: '/404', name: 'not-found', component: () => import('../pages/RouteError.vue'), props: { status: 404 } },
    { path: '/:pathMatch(.*)*', name: 'unknown-route', component: () => import('../pages/RouteError.vue'), props: { status: 404 } },
  ],
})

let returnTarget = '/overview'
export function validReturnTarget(value: unknown): string | null {
  if (typeof value !== 'string' || value.length > 2048 || !value.startsWith('/') || value.startsWith('//') || /[\\\x00-\x20]/.test(value)) return null
  const resolved = router.resolve(value)
  if (!resolved.matched.length || ['login', 'unknown-route'].includes(String(resolved.name))) return null
  return resolved.fullPath
}
export function rememberReturnTarget(value: string) { returnTarget = validReturnTarget(value) ?? '/overview' }
export function consumeReturnTarget(explicit?: unknown) {
  const target = explicit === undefined ? returnTarget : validReturnTarget(explicit) ?? '/overview'
  return target
}
router.beforeEach(to => {
  if (session.loading) return true
  if (!session.operator && to.name !== 'login') { rememberReturnTarget(to.fullPath); return { name: 'login' } }
  if (session.operator && to.name === 'login') return consumeReturnTarget(to.query.returnTo)
  return true
})
router.afterEach((to, from, failure) => { if (!failure && from.name === 'login' && to.name !== 'login') returnTarget = '/overview' })
// Retain published hash links without executing their destination's actions.
export function followLegacyHash() {
  const target = navigation.find(item => `#${item.id}` === window.location.hash)
  if (target) return router.replace(target.path)
}
