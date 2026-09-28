<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { session, login, logout, restoreSession } from './session'
import Overview from './Overview.vue'
import Queries from './Queries.vue'
import Tasks from './Tasks.vue'
const pages = [{ id: 'overview', title: '运行总览', icon: '◫' }, { id: 'queries', title: '业务查询', icon: '⌕' }, { id: 'tasks', title: '更新任务', icon: '↻' }]
const page = ref('overview')
const username = ref(''), password = ref(''), busy = ref(false), error = ref('')
function route() { page.value = pages.some(p => p.id === location.hash.slice(1)) ? location.hash.slice(1) : 'overview' }
onMounted(() => { route(); window.addEventListener('hashchange', route); void restoreSession() })
onUnmounted(() => window.removeEventListener('hashchange', route))
async function submit() {
  busy.value = true; error.value = ''; session.notice = ''
  try { await login(username.value, password.value) }
  catch (e) { error.value = e instanceof Error ? e.message : '无法连接服务，请稍后重试。' }
  finally { password.value = ''; busy.value = false }
}
async function signOut() {
  busy.value = true; error.value = ''
  try { await logout() }
  catch { error.value = '退出未完成，请检查连接后重试。' }
  finally { busy.value = false }
}
</script>

<template>
  <main v-if="session.loading" class="login-page" aria-busy="true"><p role="status">正在恢复登录状态…</p></main>
  <main v-else-if="!session.operator" class="login-page">
    <section class="login-panel" aria-labelledby="login-title">
      <div class="brand"><span class="brand-mark" aria-hidden="true">S</span><span>SICP<small>本地数据中台</small></span></div>
      <div class="login-heading"><h1 id="login-title">登录管理控制台</h1><p class="muted">查询业务数据，管理同步与更新任务。</p></div>
      <form @submit.prevent="submit" :aria-busy="busy">
        <fieldset :disabled="busy">
          <label>管理员账号<input v-model="username" name="username" autocomplete="username" required maxlength="128" autofocus></label>
          <label>密码<input v-model="password" name="password" type="password" autocomplete="current-password" required maxlength="1024" :aria-invalid="!!error" :aria-describedby="error ? 'login-error' : undefined"></label>
          <p v-if="error" id="login-error" class="error" role="alert">{{ error }}</p>
          <p v-else-if="session.notice" role="status">{{ session.notice }}</p>
          <button class="primary" :disabled="busy">{{ busy ? '正在登录…' : '登录' }}</button>
        </fieldset>
      </form>
      <p class="hint">使用部署时设置的管理员账号。登录后可直接查询和管理数据。</p>
    </section>
  </main>
  <template v-else>
    <a class="skip" href="#main">跳到主要内容</a>
    <div class="app-shell">
      <aside class="sidebar">
        <a class="brand" href="#overview"><span class="brand-mark">S</span><span>SICP<small>本地数据中台</small></span></a>
        <div class="workspace-label">管理工作台</div>
        <nav aria-label="主导航"><a v-for="item in pages" :key="item.id" :href="`#${item.id}`" :class="{ selected: page === item.id }" :aria-current="page === item.id ? 'page' : undefined"><span aria-hidden="true">{{ item.icon }}</span>{{ item.title }}</a></nav>
        <div class="sidebar-foot"><span class="phase-dot"></span> 管理控制台<small>本地数据中台</small></div>
      </aside>
      <div class="workspace">
        <header class="topbar"><span>控制台 <span class="muted">/ {{ pages.find(p => p.id === page)?.title }}</span></span><div class="account-actions"><span class="operator-name">{{ session.operator }}</span><button @click="signOut" :disabled="busy">{{ busy ? '正在退出…' : '退出登录' }}</button></div></header>
        <main id="main" tabindex="-1">
          <p v-if="error" class="error" role="alert">{{ error }}</p>
          <KeepAlive :key="session.epoch"><Overview v-if="page === 'overview'"/><Queries v-else-if="page === 'queries'"/><Tasks v-else-if="page === 'tasks'"/></KeepAlive>
        </main>
        <footer>SICP 本地数据中台 <span>管理控制台</span></footer>
      </div>
    </div>
  </template>
</template>
