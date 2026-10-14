<script setup lang="ts">
import { ref } from 'vue'
import { session, login } from '../session'
const username = ref(''), password = ref(''), busy = ref(false), error = ref('')
async function submit() {
  if (busy.value) return
  busy.value = true; error.value = ''; session.notice = ''
  try { await login(username.value, password.value) }
  catch (e) { error.value = e instanceof Error ? e.message : '无法连接服务，请稍后重试。' }
  finally { password.value = ''; busy.value = false }
}
</script>
<template>
  <main class="login-page">
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
</template>
