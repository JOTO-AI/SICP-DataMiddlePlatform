import { createRouter, createWebHistory } from 'vue-router'

export const navigation = [
  { id: 'overview', path: '/overview', title: '运行总览', icon: '◫' },
  { id: 'queries', path: '/queries', title: '业务查询', icon: '⌕' },
  { id: 'tasks', path: '/tasks', title: '更新任务', icon: '↻' },
] as const

export const router = createRouter({
  history: createWebHistory('/console/'),
  routes: [
    { path: '/', redirect: '/overview' },
    { path: '/overview', name: 'overview', component: () => import('../pages/Overview.vue') },
    { path: '/queries', name: 'queries', component: () => import('../pages/Queries.vue') },
    { path: '/tasks', name: 'tasks', component: () => import('../pages/Tasks.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/overview' },
  ],
})

// Preserve published hash links while existing pages migrate to RouterLink.
export function followLegacyHash() {
  const target = navigation.find(item => `#${item.id}` === window.location.hash)
  if (target) void router.replace(target.path)
}
