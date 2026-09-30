<script setup lang="ts">
import { computed, ref, onActivated, onDeactivated, onUnmounted } from 'vue'
import { request, describe, time, type Envelope } from './api'
const busy = ref(false)
const checked = ref('')
interface Task { task_id: string; mode: string; status: string; accepted_at: string; started_at?: string }
const currentTask = ref<Task | null>(null), taskError = ref(''), now = ref(Date.now())
const activityTitle = computed(() => currentTask.value?.status === 'accepted' ? '更新任务等待执行' : currentTask.value?.mode === 'initialize' ? '正在初始化' : currentTask.value?.mode === 'force_full' ? '正在全量更新' : '正在同步')
const elapsed = computed(() => {
  const minutes = Math.max(0, Math.floor((now.value - Date.parse(currentTask.value?.started_at || '')) / 60000))
  return minutes < 1 ? '不足 1 分钟' : `${minutes} 分钟`
})
let active = false
let timer: ReturnType<typeof setTimeout> | undefined
const services = ref([{ id: 'query' as const, title: '业务查询服务', subtitle: '成绩与课表查询', live: null as boolean | null, ready: null as boolean | null, details: {} as Record<string, boolean>, error: '' }, { id: 'worker' as const, title: '数据更新服务', subtitle: '采集、构建与发布', live: null as boolean | null, ready: null as boolean | null, details: {} as Record<string, boolean>, error: '' }])
const workerExecuting = computed(() => !!currentTask.value && services.value[1]!.live === true && !services.value[1]!.error && services.value[1]!.details.dependencies_ready !== false)
async function refreshTask() {
  taskError.value = ''
  try {
    const overview = (await request<Envelope<{ items: Task[] }>>('worker', '/admin/v1/tasks/')).body.data
    const task = overview.items.find(item => ['accepted', 'running'].includes(item.status))
    if (!task) { currentTask.value = null; return }
    const detail = (await request<Envelope<Task>>('worker', `/admin/v1/tasks/${encodeURIComponent(task.task_id)}`)).body.data
    currentTask.value = ['accepted', 'running'].includes(detail.status) ? detail : null
  } catch (e) { currentTask.value = null; taskError.value = `无法确认更新任务状态：${describe(e)}` }
}
async function refresh() {
  if (busy.value) return
  clearTimeout(timer)
  busy.value = true
  await Promise.all([refreshTask(), ...services.value.map(async s => {
    s.error = ''
    const responses = await Promise.allSettled([request<Record<string, boolean>>(s.id, '/livez', { health: true }), request<Record<string, boolean>>(s.id, '/readyz', { health: true })])
    const [live, ready] = responses
    if (live.status === 'fulfilled') s.live = live.value.status === 200 && live.value.body.live === true
    else { s.live = null; s.error = describe(live.reason) }
    if (ready.status === 'fulfilled') { s.ready = ready.value.status === 200 && ready.value.body.ready === true; s.details = ready.value.body }
    else { s.ready = null; s.details = {}; s.error = describe(ready.reason) }
  })])
  now.value = Date.now()
  checked.value = new Date().toLocaleTimeString('zh-CN', { hour12: false }); busy.value = false
  if (active) timer = setTimeout(refresh, 10000)
}
onActivated(() => { active = true; void refresh() })
function stop() { active = false; clearTimeout(timer) }
onDeactivated(stop)
onUnmounted(stop)
</script>
<template>
  <section>
    <div class="page-heading"><div><h1>运行总览</h1><p>查看服务可用状态与后台更新进度，每 10 秒自动刷新。</p></div><button @click="refresh" :disabled="busy">{{ busy ? '检查中…' : '刷新状态' }}</button></div>
    <section v-if="currentTask" class="update-progress" aria-labelledby="update-progress-title">
      <div class="section-bar"><h2 id="update-progress-title" aria-live="polite">{{ activityTitle }}</h2><a href="#tasks">查看更新任务 →</a></div>
      <p>{{ currentTask.status === 'accepted' ? '任务已受理，等待后台开始执行。' : '任务正在后台执行，包括上游采集、构建与发布。完成后将自动刷新状态。' }}</p>
      <p v-if="currentTask.mode === 'initialize' && !services[0]!.ready">首次初始化完成前，成绩与课表暂不可查询。</p>
      <p v-else-if="services[0]!.ready">当前已发布数据仍可查询。</p>
      <p v-if="!workerExecuting" class="hint">更新服务状态异常或尚未确认，请结合下方检查结果判断任务是否仍在推进。</p>
      <div class="result-meta"><span v-if="currentTask.started_at">开始时间：{{ time(currentTask.started_at) }} · 已运行 {{ elapsed }}</span><span v-else>受理时间：{{ time(currentTask.accepted_at) }}</span><span>任务号：<code>{{ currentTask.task_id }}</code></span></div>
    </section>
    <p v-if="taskError" class="error" role="alert">{{ taskError }}</p>
    <div class="section-bar"><h2>服务状态</h2><span class="muted" role="status">{{ checked ? `最近检查 ${checked}` : '正在连接服务' }}</span></div>
    <div class="health-grid" :aria-busy="busy">
      <article v-for="service in services" :key="service.id" class="panel health-panel">
        <div class="section-bar"><h3>{{ service.title }}</h3><span class="tag" :class="{ progress: service.id === 'worker' && workerExecuting, success: service.ready && !(service.id === 'worker' && workerExecuting), warning: service.ready === false && !(service.id === 'worker' && workerExecuting) }">{{ service.id === 'worker' && workerExecuting ? activityTitle : service.ready === null ? busy ? '检查中' : '连接未知' : service.ready ? '✓ 已就绪' : '尚未就绪' }}</span></div>
        <p class="muted">{{ service.subtitle }}</p>
        <dl class="facts"><div><dt>进程存活</dt><dd>{{ service.live === null ? '未确认' : service.live ? '正常' : '异常' }}</dd></div><div><dt>业务就绪</dt><dd>{{ service.ready === null ? '未确认' : service.ready ? '可用' : '未就绪' }}</dd></div></dl>
        <p v-if="service.error" class="error" role="alert">{{ service.error }}</p>
        <details v-if="Object.keys(service.details).length"><summary>查看依赖检查</summary><dl class="facts"><div v-for="(value, name) in service.details" :key="name"><dt>{{ name }}</dt><dd>{{ value ? '通过' : '未通过' }}</dd></div></dl></details>
      </article>
    </div>
    <div class="section-bar next-heading"><h2>常用操作</h2><span class="muted">从数据核验到更新追踪</span></div>
    <div class="link-list"><a href="#queries"><span class="link-icon" aria-hidden="true">⌕</span><div><h3>查询成绩与课表</h3><p>学生成绩、批量成绩、学生课表与教师课表</p></div><span aria-hidden="true">→</span></a><a href="#tasks"><span class="link-icon" aria-hidden="true">↻</span><div><h3>管理数据更新</h3><p>自动更新记录、强制全量与失败任务重跑</p></div><span aria-hidden="true">→</span></a></div>
    <div class="section-note"><h2>首次使用</h2><p>系统会自动初始化全新环境，并持续进行增量同步。可在更新任务中查看进度；数据就绪后即可查询。</p></div>
  </section>
</template>
