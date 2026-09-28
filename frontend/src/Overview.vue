<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { request, describe } from './api'
const busy = ref(false)
const checked = ref('')
const services = ref([{ id: 'query' as const, title: '业务查询服务', subtitle: '成绩与课表查询', live: null as boolean | null, ready: null as boolean | null, details: {} as Record<string, boolean>, error: '' }, { id: 'worker' as const, title: '数据更新服务', subtitle: '采集、构建与发布', live: null as boolean | null, ready: null as boolean | null, details: {} as Record<string, boolean>, error: '' }])
async function refresh() {
  busy.value = true
  await Promise.all(services.value.map(async s => {
    s.live = null; s.ready = null; s.error = ''; s.details = {}
    const responses = await Promise.allSettled([request<Record<string, boolean>>(s.id, '/livez', { health: true }), request<Record<string, boolean>>(s.id, '/readyz', { health: true })])
    const [live, ready] = responses
    if (live.status === 'fulfilled') s.live = live.value.status === 200 && live.value.body.live === true
    else s.error = describe(live.reason)
    if (ready.status === 'fulfilled') { s.ready = ready.value.status === 200 && ready.value.body.ready === true; s.details = ready.value.body }
    else s.error = describe(ready.reason)
  }))
  checked.value = new Date().toLocaleTimeString('zh-CN', { hour12: false }); busy.value = false
}
onMounted(refresh)
</script>
<template>
  <section>
    <div class="page-heading"><div><h1>运行总览</h1><p>确认服务就绪，然后开始查询或更新数据。</p></div><button @click="refresh" :disabled="busy">{{ busy ? '检查中…' : '刷新状态' }}</button></div>
    <div class="section-bar"><h2>服务状态</h2><span class="muted" role="status">{{ checked ? `最近检查 ${checked}` : '正在连接服务' }}</span></div>
    <div class="health-grid" :aria-busy="busy">
      <article v-for="service in services" :key="service.id" class="panel health-panel">
        <div class="section-bar"><h3>{{ service.title }}</h3><span class="tag" :class="{ success: service.ready, warning: service.ready === false }">{{ busy ? '检查中' : service.ready === null ? '连接未知' : service.ready ? '✓ 已就绪' : '尚未就绪' }}</span></div>
        <p class="muted">{{ service.subtitle }}</p>
        <dl class="facts"><div><dt>进程存活</dt><dd>{{ service.live === null ? '未确认' : service.live ? '正常' : '异常' }}</dd></div><div><dt>业务就绪</dt><dd>{{ service.ready === null ? '未确认' : service.ready ? '可用' : '未就绪' }}</dd></div></dl>
        <p v-if="service.error" class="error" role="alert">{{ service.error }}</p>
        <details v-if="Object.keys(service.details).length"><summary>查看依赖检查</summary><dl class="facts"><div v-for="(value, name) in service.details" :key="name"><dt>{{ name }}</dt><dd>{{ value ? '通过' : '未通过' }}</dd></div></dl></details>
      </article>
    </div>
    <div class="section-bar next-heading"><h2>常用操作</h2><span class="muted">从数据核验到更新追踪</span></div>
    <div class="link-list"><a href="#queries"><span class="link-icon" aria-hidden="true">⌕</span><div><h3>查询成绩与课表</h3><p>学生成绩、批量成绩、学生课表与教师课表</p></div><span aria-hidden="true">→</span></a><a href="#tasks"><span class="link-icon" aria-hidden="true">↻</span><div><h3>管理数据更新</h3><p>初始化、增量同步、强制全量与失败任务重跑</p></div><span aria-hidden="true">→</span></a><a href="#settings"><span class="link-icon" aria-hidden="true">⚙</span><div><h3>配置访问凭据</h3><p>分别设置业务密钥与管理令牌</p></div><span aria-hidden="true">→</span></a></div>
    <div class="section-note"><h2>首次使用</h2><p>启动后端服务后，先填写连接凭据；空数据环境需在更新任务中执行首次初始化，任务完成后再查询。进程存活不代表数据已就绪。</p></div>
  </section>
</template>
