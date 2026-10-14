<script setup lang="ts">
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { session, logout } from '../session'
import { navigation } from '../app/router'
const route = useRoute()
const busy = ref(false), error = ref('')
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
  <div class="app-shell">
    <aside class="sidebar">
      <RouterLink class="brand" to="/overview"><span class="brand-mark" aria-hidden="true">S</span><span>SICP<small>本地数据中台</small></span></RouterLink>
      <div class="workspace-label">管理工作台</div>
      <nav aria-label="主导航"><RouterLink v-for="item in navigation" :key="item.id" :to="item.path" :class="{ selected: route.name === item.id }" :aria-current="route.name === item.id ? 'page' : undefined"><span aria-hidden="true">{{ item.icon }}</span>{{ item.title }}</RouterLink></nav>
      <div class="sidebar-foot"><span class="phase-dot"></span> 管理控制台<small>本地数据中台</small></div>
    </aside>
    <div class="workspace">
      <header class="topbar"><span>控制台 <span class="muted">/ {{ navigation.find(item => item.id === route.name)?.title }}</span></span><div class="account-actions"><span class="operator-name">{{ session.operator }}</span><button @click="signOut" :disabled="busy">{{ busy ? '正在退出…' : '退出登录' }}</button></div></header>
      <main id="main" tabindex="-1">
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <slot />
      </main>
      <footer>SICP 本地数据中台 <span>管理控制台</span></footer>
    </div>
  </div>
</template>
