<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import Drawer from '../components/Drawer.vue'
import { useRoute } from 'vue-router'
import { session, logout } from '../session'
import { navigation } from '../app/router'
const route = useRoute()
const busy = ref(false), error = ref(''), collapsed = ref(false), navigationOpen = ref(false)
const mobile = window.matchMedia('(max-width: 767px)')
function viewportChanged() { if (!mobile.matches) navigationOpen.value = false }
onMounted(() => mobile.addEventListener('change', viewportChanged))
onUnmounted(() => mobile.removeEventListener('change', viewportChanged))
watch(() => route.fullPath, async () => {
  if (!navigationOpen.value) return
  navigationOpen.value = false
  await nextTick()
  document.querySelector<HTMLElement>('#main')?.focus({ preventScroll: true })
})
function closeNavigation() { navigationOpen.value = false }
async function signOut() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await logout() }
  catch { error.value = '退出未完成，请检查连接后重试。' }
  finally { busy.value = false }
}
</script>
<template>
  <a class="skip" href="#main">跳到主要内容</a>
  <div class="app-shell" :class="{ 'sidebar-collapsed': collapsed }">
    <aside id="console-sidebar" class="sidebar">
      <RouterLink class="brand" to="/overview" aria-label="SICP 运行总览"><span class="brand-mark" aria-hidden="true">S</span><span class="brand-copy">SICP<small>本地数据中台</small></span></RouterLink>

      <nav aria-label="主导航"><RouterLink v-for="item in navigation" :key="item.id" :to="item.path" :aria-label="item.title" :title="item.title" :class="{ selected: route.name === item.id }" :aria-current="route.name === item.id ? 'page' : undefined"><span class="nav-icon" aria-hidden="true">{{ item.icon }}</span><span class="nav-label">{{ item.title }}</span></RouterLink></nav>
      <div class="sidebar-foot"><span class="phase-dot"></span> 管理控制台<small>本地数据中台</small></div>
    </aside>
    <div class="workspace">
      <header class="topbar"><div class="topbar-location"><button class="mobile-navigation-toggle" aria-label="打开主导航" :aria-expanded="navigationOpen" aria-controls="console-mobile-navigation" @click="navigationOpen = true"><span aria-hidden="true">☰</span></button><button class="sidebar-toggle" :aria-label="collapsed ? '展开侧栏' : '折叠侧栏'" :aria-expanded="!collapsed" aria-controls="console-sidebar" :title="collapsed ? '展开侧栏' : '折叠侧栏'" @click="collapsed = !collapsed"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/></svg></button><span>控制台 <span class="muted">/ {{ navigation.find(item => item.id === route.name)?.title }}</span></span></div><div class="account-actions"><span class="operator-name">{{ session.operator }}</span><button @click="signOut" :disabled="busy">{{ busy ? '正在退出…' : '退出登录' }}</button></div></header>
      <main id="main" tabindex="-1">
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <slot />
      </main>
      <footer>SICP 本地数据中台 <span>管理控制台</span></footer>
    </div>
    <Drawer v-model:open="navigationOpen" title="主导航"><nav id="console-mobile-navigation" class="mobile-navigation" aria-label="主导航"><RouterLink v-for="item in navigation" :key="item.id" :to="item.path" :class="{ selected: route.name === item.id }" :aria-current="route.name === item.id ? 'page' : undefined" @click="route.name === item.id && closeNavigation()"><span class="nav-icon" aria-hidden="true">{{ item.icon }}</span>{{ item.title }}</RouterLink></nav></Drawer>
  </div>
</template>
