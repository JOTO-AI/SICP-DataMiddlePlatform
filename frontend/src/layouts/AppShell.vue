<script setup lang="ts">
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { session, logout } from '../session'
import { navigation } from '../app/router'
const route = useRoute()
const busy = ref(false), error = ref(''), collapsed = ref(false)
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

      <nav aria-label="主导航"><RouterLink v-for="item in navigation" :key="item.id" :to="item.path" :aria-label="item.title" :title="collapsed ? item.title : undefined" :class="{ selected: route.name === item.id }" :aria-current="route.name === item.id ? 'page' : undefined"><span class="nav-icon" aria-hidden="true">{{ item.icon }}</span><span class="nav-label">{{ item.title }}</span></RouterLink></nav>
      <div class="sidebar-foot"><span class="phase-dot"></span> 管理控制台<small>本地数据中台</small></div>
    </aside>
    <div class="workspace">
      <header class="topbar"><div class="topbar-location"><button class="sidebar-toggle" :aria-label="collapsed ? '展开侧栏' : '折叠侧栏'" :aria-expanded="!collapsed" aria-controls="console-sidebar" :title="collapsed ? '展开侧栏' : '折叠侧栏'" @click="collapsed = !collapsed"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/></svg></button><span>控制台 <span class="muted">/ {{ navigation.find(item => item.id === route.name)?.title }}</span></span></div><div class="account-actions"><span class="operator-name">{{ session.operator }}</span><button @click="signOut" :disabled="busy">{{ busy ? '正在退出…' : '退出登录' }}</button></div></header>
      <main id="main" tabindex="-1">
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <slot />
      </main>
      <footer>SICP 本地数据中台 <span>管理控制台</span></footer>
    </div>
  </div>
</template>
