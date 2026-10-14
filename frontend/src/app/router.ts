import { createRouter, createWebHistory } from 'vue-router'

export const navigation = [
  { id: 'overview', path: '/overview', title: '运行总览', icon: '◫' },
  { id: 'queries', path: '/queries', title: '接口中心', icon: '⌕' },
  { id: 'tasks', path: '/tasks', title: '更新事件', icon: '↻' },
  { id: 'sources', path: '/sources', title: '数据源', icon: '▤' },
  { id: 'logs', path: '/logs', title: '日志中心', icon: '≡' },
  { id: 'notifications', path: '/notifications', title: '通知中心', icon: '♧' },
  { id: 'settings', path: '/settings', title: '系统设置', icon: '⚙' },
] as const

export const router = createRouter({
  history: createWebHistory('/console/'),
  routes: [
    { path: '/', redirect: '/overview' },
    { path: '/overview', name: 'overview', component: () => import('../pages/Overview.vue') },
    { path: '/queries', name: 'queries', component: () => import('../pages/Queries.vue') },
    { path: '/tasks', name: 'tasks', component: () => import('../pages/Tasks.vue') },
    ...navigation.filter(item => ['sources', 'logs', 'notifications', 'settings'].includes(item.id)).map(item => ({ path: item.path, name: item.id, component: () => import('../pages/ModulePending.vue'), props: { title: item.title } })),
    { path: '/:pathMatch(.*)*', redirect: '/overview' },
  ],
})

// Preserve published hash links while existing pages migrate to RouterLink.
export function followLegacyHash() {
  const target = navigation.find(item => `#${item.id}` === window.location.hash)
  if (target) void router.replace(target.path)
}
