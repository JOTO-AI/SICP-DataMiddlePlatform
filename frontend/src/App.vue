<script setup lang="ts">
import { session } from './session'
import { useConsoleStartup } from './app/startup'
import LoginPage from './pages/LoginPage.vue'
import AppShell from './layouts/AppShell.vue'
useConsoleStartup()
</script>
<template>
  <main v-if="session.loading" class="login-page" aria-busy="true"><p role="status">正在恢复登录状态…</p></main>
  <LoginPage v-else-if="!session.operator" />
  <main v-else-if="$route.name === 'login'" class="login-page" aria-busy="true"><p role="status">正在打开控制台…</p></main>
  <AppShell v-else :key="session.epoch">
    <RouterView v-slot="{ Component, route }">
      <KeepAlive :max="3"><component :is="Component" :key="route.path" /></KeepAlive>
    </RouterView>
  </AppShell>
</template>
