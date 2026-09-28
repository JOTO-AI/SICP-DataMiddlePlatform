<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { credentials } from './api'
import Overview from './Overview.vue'
import Queries from './Queries.vue'
import Tasks from './Tasks.vue'
const pages = [{ id: 'overview', title: '运行总览', icon: '◫' }, { id: 'queries', title: '业务查询', icon: '⌕' }, { id: 'tasks', title: '更新任务', icon: '↻' }, { id: 'settings', title: '连接设置', icon: '⚙' }]
const page = ref('overview')
const notice = ref('')
function route() { page.value = pages.some(p => p.id === location.hash.slice(1)) ? location.hash.slice(1) : 'overview' }
onMounted(() => { route(); window.addEventListener('hashchange', route) })
onUnmounted(() => window.removeEventListener('hashchange', route))
function clear() { credentials.query = ''; credentials.worker = ''; notice.value = '凭据已清除，页面中的查询与任务数据已释放。'; session.value++ }
const session = ref(0)
</script>

<template>
  <a class="skip" href="#main">跳到主要内容</a>
  <div class="app-shell">
    <aside class="sidebar">
      <a class="brand" href="#overview"><span class="brand-mark">S</span><span>SICP<small>本地数据中台</small></span></a>
      <div class="workspace-label">管理工作台</div>
      <nav aria-label="主导航"><a v-for="item in pages" :key="item.id" :href="`#${item.id}`" :class="{ selected: page === item.id }" :aria-current="page === item.id ? 'page' : undefined"><span aria-hidden="true">{{ item.icon }}</span>{{ item.title }}</a></nav>
      <div class="sidebar-foot"><span class="phase-dot"></span> Phase 1<small>本地运行 · 独立认证</small></div>
    </aside>
    <div class="workspace">
      <header class="topbar"><span>控制台 <span class="muted">/ {{ pages.find(p => p.id === page)?.title }}</span></span><a href="#settings" class="connection">{{ credentials.query || credentials.worker ? '已填写连接凭据' : '待配置连接凭据' }} <span aria-hidden="true">↗</span></a></header>
      <main id="main" tabindex="-1">
        <KeepAlive :key="session"><Overview v-if="page === 'overview'"/><Queries v-else-if="page === 'queries'"/><Tasks v-else-if="page === 'tasks'"/></KeepAlive>
        <section v-if="page === 'settings'">
          <div class="page-heading"><div><h1>连接设置</h1><p>分别配置业务查询与管理操作的访问凭据。</p></div><span class="tag">仅当前页面有效</span></div>
          <form class="panel settings" @submit.prevent="notice = '凭据已在当前页面内存中应用，可前往业务查询或更新任务。'">
            <h2>访问凭据</h2><p class="muted">从部署配置中获取。刷新或关闭页面后需要重新填写。</p>
            <label>业务 API Key<input v-model="credentials.query" type="password" autocomplete="off" spellcheck="false" placeholder="对应 QUERY_API_KEY"></label>
            <label>管理 Bearer Token<input v-model="credentials.worker" type="password" autocomplete="off" spellcheck="false" placeholder="填写令牌本身，不含 Bearer 前缀"></label>
            <p class="hint">管理权限由后端校验。业务密钥不会授予更新权限。</p>
            <div class="actions"><button class="primary">应用凭据</button><button type="button" @click="clear">清除凭据与数据</button></div><p role="status">{{ notice }}</p>
          </form>
          <div class="section-note"><h2>服务连接</h2><p>所有请求通过同源代理分别连接 query 和 worker。开发环境默认连接本机 18080 / 18081 端口；部署时由网关配置目标服务。</p></div>
        </section>
      </main>
      <footer>SICP 本地数据中台 <span>Phase 1 控制台</span></footer>
    </div>
  </div>
</template>
