<script setup lang="ts">
import OverviewStages from '../components/updates/OverviewStages.vue'
import NavigationIcon from '../components/NavigationIcon.vue'
import { readUpdateDetail, updateDetailPath, updateDuration, updateStatuses, type UpdateDetail } from '../app/updateEvents'
import { sourceName } from '../app/sourceNames'
import { sourceItems, sourceFailureLabels, type SourceState } from '../app/sources'
import StatusBadge from '../components/StatusBadge.vue'
import { displayEnum } from '../shared/display'
import Timestamp from '../components/Timestamp.vue'
import { actionReason, initializationState, latestTaskTime, taskModeLabels, observationLabel, type OverviewSnapshot, type SystemSnapshot } from '../app/overview'
import PageHeader from '../components/PageHeader.vue'
import { computed, ref, onActivated, onDeactivated, onUnmounted } from 'vue'
import { request, describe, time, ApiError, type Envelope } from '../api'
import { pollingDelay } from '../app/polling'
import { usePageVisibility } from '../composables/usePageVisibility'
import { useRequestScope } from '../composables/useRequestScope'
import type { RequestRun } from '../app/requestScope'
import AcquisitionProgress from '../components/updates/AcquisitionProgress.vue'
import WorkProgress from '../components/updates/WorkProgress.vue'
import type { WorkProgress as WorkProgressData } from '../workProgress'
import type { Acquisition } from '../acquisition'
const busy = ref(false)
const checked = ref('')
const overviewSnapshot = ref<OverviewSnapshot | null>(null), overviewError = ref('')
const systemSnapshot = ref<SystemSnapshot | null>(null), systemError = ref('')
const taskReadAt = ref('')
const sources = ref<SourceState[] | null>(null), sourcesError = ref(''), sourcesReadAt = ref('')
const recentItems = computed(() => overviewSnapshot.value?.tasks?.items ?? [])
const latestTask = computed(() => recentItems.value[0])
const initialization = computed(() => initializationState(overviewSnapshot.value?.tasks?.initialized, !!overviewError.value, !taskError.value && currentTask.value ? currentTask.value.status : latestTask.value?.status))
const publicationTime = computed(() => latestTaskTime(recentItems.value, 'published_at'))
const successCheckTime = computed(() => latestTaskTime(recentItems.value, 'checked_at'))
const concerns = computed(() => [
  ...(overviewError.value ? ['运行快照读取失败，当前更新状态未确认。'] : []),
  ...(taskError.value ? ['活动任务读取失败，当前进度未确认。'] : []),
  ...(sourcesError.value ? ['数据源读取失败，当前确认进度未确认。'] : []),
  ...services.value.filter(service => service.error || service.ready !== true).map(service => `${service.title}：${service.error ? '当前状态未确认' : service.ready === false ? '尚未就绪' : '未观测'}`),
  ...(!sourcesError.value ? (sources.value ?? []).filter(source => source.failure_state === 'unresolved').map(source => `${sourceName(source.id)}：失败尚未覆盖`) : []),
  ...(systemError.value ? ['日志状态读取失败。'] : []),
])

const recentDetail = ref<UpdateDetail | null>(null), recentDetailError = ref('')
const shownDetail = computed(() => recentDetail.value?.task_id === latestTask.value?.task_id ? recentDetail.value : null)
const sourcePreview = computed(() => [...(sources.value ?? [])].sort((a, b) => Number(b.failure_state === 'unresolved') - Number(a.failure_state === 'unresolved')).slice(0, 7))
const recentDuration = computed(() => latestTask.value ? updateDuration({ status: latestTask.value.status, started_at: latestTask.value.started_at ?? null, finished_at: latestTask.value.finished_at ?? null }) : '未提供')
const summarySignals = computed(() => {
  const query = services.value[0]!
  const queryKnown = !query.error && query.ready !== null
  const logs = [systemSnapshot.value?.platform, systemSnapshot.value?.upstream]
  const logsReadable = !systemError.value && logs.every(log => log?.reader_configured === true && log.storage_state === 'available')
  const logsUnavailable = !systemError.value && logs.some(log => log?.storage_state === 'unavailable')
  const updating = !taskError.value && !!currentTask.value
  const status = latestTask.value?.status
  return [
    { title: queryKnown ? query.ready ? '查询可用' : '查询尚未就绪' : '查询状态未知', note: query.error ? '状态读取失败' : '业务查询服务', tone: queryKnown ? query.ready ? 'good' : 'warn' : 'unknown', icon: queryKnown && query.ready ? '✓' : '!' },
    { title: logsReadable ? '日志存储可读' : logsUnavailable ? '日志存储不可读' : '日志状态未知', note: '平台与上游存储观测', tone: logsReadable ? 'good' : logsUnavailable ? 'warn' : 'unknown', icon: logsReadable ? '✓' : '!' },
    { title: overviewError.value || taskError.value ? '更新状态未确认' : updating ? activityTitle.value : status === 'failed' ? '最近更新失败' : status === 'success_published' ? '最近更新已发布' : status === 'success_unchanged' ? '检查完成，无变化' : status ? displayEnum(status, updateStatuses) : '暂无更新记录', note: queryKnown && query.ready ? '已发布数据仍可查询' : '查询状态见左侧', tone: overviewError.value || taskError.value ? 'unknown' : updating ? 'working' : status === 'failed' ? 'warn' : ['success_published', 'success_unchanged'].includes(status ?? '') ? 'good' : 'unknown', icon: updating ? '↻' : ['success_published', 'success_unchanged'].includes(status ?? '') ? '✓' : '!' },
  ]
})

async function refreshSources(run: RequestRun) {
  try {
    const result = await request<Envelope<{ items: SourceState[] }>>('worker', '/admin/v1/sources', { signal: run.signal })
    if (!current(run)) return
    const items = sourceItems(result.body.data)
    if (!items) throw new ApiError('数据源响应格式异常。', 'invalid_response')
    sources.value = items; sourcesError.value = ''; sourcesReadAt.value = new Date().toISOString()
  } catch (error) { if (current(run)) { recordRetryAfter(error); sourcesError.value = describe(error) } }
}
const logSources = [{ id: 'platform' as const, title: '平台日志' }, { id: 'upstream' as const, title: '上游日志' }]
async function refreshOverview(run: RequestRun) {
  try {
    const result = await request<Envelope<OverviewSnapshot>>('worker', '/admin/v1/overview', { signal: run.signal })
    if (!current(run)) return
    if (!result.body.data || typeof result.body.data !== 'object' || Array.isArray(result.body.data)) throw new ApiError('运行快照响应格式异常。', 'invalid_response')
    overviewSnapshot.value = result.body.data; overviewError.value = ''
    const id = latestTask.value?.task_id
    if (!id) { recentDetail.value = null; recentDetailError.value = ''; return }
    if (recentDetail.value?.task_id !== id) recentDetail.value = null
    try {
      const response = await request<Envelope<unknown>>('worker', updateDetailPath(id), { signal: run.signal })
      if (!current(run)) return
      const detail = readUpdateDetail(response.body.data, id)
      if (!detail) throw new ApiError('更新详情标识或格式不匹配。', 'invalid_response')
      recentDetail.value = detail; recentDetailError.value = ''
    } catch (error) { if (current(run)) { recordRetryAfter(error); recentDetailError.value = describe(error) } }
  } catch (error) { if (current(run)) { recordRetryAfter(error); overviewError.value = describe(error) } }
}
async function refreshSystem(run: RequestRun) {
  try {
    const result = await request<Envelope<SystemSnapshot>>('worker', '/admin/v1/system/status', { signal: run.signal })
    if (!current(run)) return
    if (!result.body.data || typeof result.body.data !== 'object' || Array.isArray(result.body.data)) throw new ApiError('日志观测响应格式异常。', 'invalid_response')
    systemSnapshot.value = result.body.data; systemError.value = ''
  } catch (error) { if (current(run)) { recordRetryAfter(error); systemError.value = describe(error) } }
}
interface Task { task_id: string; mode: string; status: string; accepted_at: string; started_at?: string; work_progress?: WorkProgressData; acquisition?: Acquisition }
const currentTask = ref<Task | null>(null), taskError = ref(''), now = ref(Date.now())
const activityTitle = computed(() => currentTask.value?.status === 'accepted' ? '更新任务等待执行' : currentTask.value?.mode === 'initialize' ? '正在初始化' : currentTask.value?.mode === 'force_full' ? '正在全量更新' : '正在同步')
const elapsed = computed(() => {
  const minutes = Math.max(0, Math.floor((now.value - Date.parse(currentTask.value?.started_at || '')) / 60000))
  if (!Number.isFinite(minutes)) return '未知时长'
  return minutes < 1 ? '不足 1 分钟' : `${minutes} 分钟`
})
let active = false, failures = 0, serverDelay = 0
function recordRetryAfter(error: unknown) { if (error instanceof ApiError) serverDelay = Math.max(serverDelay, pollingDelay(15000, 0, error.headers['Retry-After'])) }
const pageVisible = usePageVisibility(() => { requests.cancel(); busy.value = false; clearTimeout(timer) }, () => { if (active) void refresh() })
const requests = useRequestScope()
function current(run: RequestRun) { return active && run.current() }
let timer: ReturnType<typeof setTimeout> | undefined
const services = ref([{ id: 'query' as const, title: '业务查询服务', subtitle: '九类业务查询', live: null as boolean | null, ready: null as boolean | null, details: {} as Record<string, boolean | null>, error: '', liveAt: '', readyAt: '', liveStale: false, readyStale: false }, { id: 'worker' as const, title: '数据更新服务', subtitle: '采集、构建与发布', live: null as boolean | null, ready: null as boolean | null, details: {} as Record<string, boolean | null>, error: '', liveAt: '', readyAt: '', liveStale: false, readyStale: false }])
const workerExecuting = computed(() => !!currentTask.value && !taskError.value && services.value[1]!.live === true && !services.value[1]!.error && services.value[1]!.details.dependencies_ready !== false)
async function refreshTask(run: RequestRun) {
  try {
    const overview = (await request<Envelope<{ items: Task[] }>>('worker', '/admin/v1/tasks/', { signal: run.signal })).body.data
    if (!current(run)) return
    const task = overview.items.find(item => ['accepted', 'queued', 'running', 'published_pending_progress'].includes(item.status))
    if (!task) { currentTask.value = null; taskError.value = ''; taskReadAt.value = new Date().toISOString(); return }
    const detail = (await request<Envelope<Task>>('worker', `/admin/v1/tasks/${encodeURIComponent(task.task_id)}`, { signal: run.signal })).body.data
    if (!current(run)) return
    taskError.value = ''; taskReadAt.value = new Date().toISOString()
    currentTask.value = ['accepted', 'queued', 'running', 'published_pending_progress'].includes(detail.status) ? detail : null
  } catch (e) { if (current(run)) { recordRetryAfter(e); taskError.value = `无法确认更新任务状态：${describe(e)}` } }
}
async function refresh() {
  if (!active || !pageVisible.value || busy.value) return
  clearTimeout(timer)
  busy.value = true
  const run = requests.begin(); serverDelay = 0
  await Promise.all([refreshOverview(run), refreshSystem(run), refreshSources(run), refreshTask(run), ...services.value.map(async s => {
    const responses = await Promise.allSettled([request<Record<string, boolean>>(s.id, '/livez', { health: true, signal: run.signal }), request<Record<string, boolean>>(s.id, '/readyz', { health: true, signal: run.signal })])
    if (!current(run)) return
    const [live, ready] = responses
    const errors: string[] = []
    const readAt = new Date().toISOString()
    if (live.status === 'fulfilled') { s.live = typeof live.value.body.live === 'boolean' ? live.value.status === 200 && live.value.body.live : null; s.liveAt = readAt; s.liveStale = false }
    else { recordRetryAfter(live.reason); s.liveStale = true; errors.push(`进程状态：${describe(live.reason)}`) }
    if (ready.status === 'fulfilled') {
      s.ready = typeof ready.value.body.ready === 'boolean' ? ready.value.status === 200 && ready.value.body.ready : null
      s.details = Object.fromEntries(Object.entries(ready.value.body).map(([key, value]) => [key, typeof value === 'boolean' ? value : null]))
      s.readyAt = readAt; s.readyStale = false
    } else { recordRetryAfter(ready.reason); s.readyStale = true; errors.push(`就绪状态：${describe(ready.reason)}`) }
    s.error = errors.join('；')
  })])
  if (!current(run)) return
  now.value = Date.now()
  checked.value = new Date().toLocaleTimeString('zh-CN', { hour12: false }); busy.value = false
  failures = overviewError.value || recentDetailError.value || systemError.value || sourcesError.value || taskError.value || services.value.some(service => service.error) ? failures + 1 : 0
  if (active && pageVisible.value) timer = setTimeout(refresh, Math.max(serverDelay, pollingDelay(15000, failures)))
}
onActivated(() => { active = true; void refresh() })
function stop() { active = false; requests.cancel(); busy.value = false; clearTimeout(timer) }
onDeactivated(stop)
onUnmounted(stop)
</script>
<template>
  <section class="overview-page">
    <PageHeader title="运行总览" description="查看查询可用性与最近更新"><button class="primary" @click="refresh" :disabled="busy"><span aria-hidden="true">↻</span> {{ busy ? '检查中…' : '刷新状态' }}</button></PageHeader>
    <section class="overview-summary panel" aria-label="运行状态摘要" :aria-busy="busy">
      <div v-for="signal in summarySignals" :key="signal.note" class="overview-signal" :class="signal.tone"><span class="signal-icon" aria-hidden="true">{{ signal.icon }}</span><div><strong>{{ signal.title }}</strong><small>{{ signal.note }}</small></div></div>
      <dl class="overview-metrics">
        <div><dt>当前组合事件</dt><dd><code :title="overviewSnapshot?.active?.event_id">{{ overviewSnapshot?.active?.event_id ?? (overviewSnapshot?.active_state === 'empty' ? '尚未发布' : '未观测') }}</code></dd></div>
        <div><dt>内容发布</dt><dd><Timestamp :value="publicationTime" empty="最近记录未提供" /></dd></div>
        <div><dt>成功检查</dt><dd><Timestamp :value="successCheckTime" empty="最近记录未提供" /></dd></div>
        <div><dt>下次调度</dt><dd><Timestamp :value="overviewSnapshot?.tasks?.next_due_at" /></dd></div>
      </dl>
      <p v-if="overviewError" class="warning summary-warning">以上为旧快照，当前状态未确认。</p>
    </section>
    <div class="overview-columns">
      <div class="overview-main panel">
        <section class="overview-recent" aria-label="最近更新">
          <div class="section-bar"><h2>最近更新</h2><RouterLink v-if="latestTask" class="button primary" :to="{ name: 'update-detail', params: { id: latestTask.task_id } }">{{ latestTask.status === 'failed' ? '查看失败事件' : '查看事件详情' }}</RouterLink><RouterLink v-else to="/updates">查看更新记录 →</RouterLink></div>
          <p v-if="overviewError" class="warning">当前更新状态未确认；以下记录来自上次成功快照。</p>
          <template v-if="latestTask">
            <div class="recent-task-heading"><RouterLink :to="{ name: 'update-detail', params: { id: latestTask.task_id } }"><code>{{ latestTask.task_id }}</code></RouterLink><StatusBadge :value="latestTask.status" :tone="overviewError ? 'neutral' : undefined" /><strong>{{ displayEnum(latestTask.mode, taskModeLabels) }}</strong></div>
            <div class="recent-times"><span>开始时间：<Timestamp :value="latestTask.started_at" /></span><span>结束时间：<Timestamp :value="latestTask.finished_at" /></span><span>耗时：{{ recentDuration }}</span></div>
            <OverviewStages v-if="shownDetail && !recentDetailError && !overviewError" :task="shownDetail" />
            <p v-else class="hint">{{ recentDetailError ? '阶段详情读取失败，当前阶段未确认。' : '阶段详情尚未取得。' }}</p>
            <div v-if="latestTask.status === 'failed'" class="recent-failure"><span aria-hidden="true">!</span><p>{{ !overviewError && !recentDetailError && shownDetail?.failure ? shownDetail.failure : '最近更新失败，请查看事件中的失败操作。' }}</p><RouterLink :to="{ name: 'update-detail', params: { id: latestTask.task_id } }">查看失败操作 ›</RouterLink></div>
          </template>
          <p v-else class="hint">{{ overviewSnapshot ? '最近更新记录为空。' : '尚未取得更新记录。' }}</p>
        </section>
            <section v-if="currentTask && !taskError" class="update-progress" aria-labelledby="update-progress-title">
      <div class="section-bar"><h2 id="update-progress-title" aria-live="polite">{{ activityTitle }}</h2><RouterLink to="/updates">查看更新任务 →</RouterLink></div>
      <p>{{ currentTask.status === 'accepted' ? '任务已受理，等待后台开始执行。' : '任务正在后台执行，包括上游采集、构建与发布。完成后将自动刷新状态。' }}</p>
      <p v-if="currentTask.mode === 'initialize' && !services[0]!.ready">首次初始化完成前，业务数据暂不可查询。</p>
      <p v-else-if="services[0]!.ready && !services[0]!.error">当前已发布数据仍可查询。</p>
      <p v-if="!workerExecuting" class="hint">更新服务状态异常或尚未确认，请结合下方检查结果判断任务是否仍在推进。</p>
      <div class="result-meta"><span v-if="currentTask.started_at">开始时间：{{ time(currentTask.started_at) }} · 已运行 {{ elapsed }}</span><span v-else>受理时间：{{ time(currentTask.accepted_at) }}</span><span>任务号：<code>{{ currentTask.task_id }}</code></span></div>
      <details class="active-progress-details"><summary>查看当前采集与执行进度</summary><WorkProgress :progress="currentTask.work_progress" :status="currentTask.status" :started-at="currentTask.started_at" />
      <AcquisitionProgress :progress="currentTask.acquisition" :status="currentTask.status" compact /></details>
    </section>
    <p v-if="taskError" class="error" role="alert">{{ taskError }}</p><p v-if="taskError && currentTask" class="warning">上次读取的任务：<code>{{ currentTask.task_id }}</code>，当前进度未确认。读取时间：<Timestamp :value="taskReadAt" /></p>


        <section class="overview-sources" aria-label="数据源状态">
          <div class="section-bar"><h2>数据源状态</h2></div>
          <p v-if="sourcesError" class="error" role="alert">数据源读取失败：{{ sourcesError }}</p>
          <p v-if="sourcesError && sources" class="warning">沿用旧快照，当前状态未确认。读取时间：<Timestamp :value="sourcesReadAt" /></p>
          <div v-if="sources?.length" class="table-wrap" role="region" aria-label="数据源状态表" tabindex="0"><table><thead><tr><th scope="col">数据源</th><th scope="col">最近检查</th><th scope="col">已确认进度</th><th scope="col">状态</th><th scope="col">操作</th></tr></thead><tbody><tr v-for="source in sourcePreview" :key="source.id" :class="{ 'source-attention': !sourcesError && source.failure_state === 'unresolved' }"><th scope="row"><span :title="source.id">{{ sourceName(source.id) }}</span><span class="sr-only">{{ source.id }}</span></th><td><Timestamp :value="source.checked_at" /></td><td><Timestamp :value="source.confirmed_until" /></td><td><StatusBadge :value="source.failure_state" :labels="sourceFailureLabels" :tone="!sourcesError && source.failure_state === 'unresolved' ? 'danger' : 'neutral'" /></td><td><RouterLink :to="{ path: '/sources', query: { source: source.id } }" :aria-label="'查看数据源 ' + sourceName(source.id)">查看 ›</RouterLink></td></tr></tbody></table></div>
          <p v-else class="hint">{{ sources ? '后端返回空列表。' : '尚未取得数据源。' }}</p>
          <RouterLink class="all-sources" to="/sources">{{ sources ? '查看全部 ' + sources.length + ' 类数据源' : '查看数据源' }} ›</RouterLink>
          <p class="hint source-meaning">按本次响应显示，失败源优先；无失败记录不代表健康。</p>
        </section>
      </div>
      <aside class="overview-side panel">
        <section aria-label="需要关注"><h2>需要关注</h2>
          <div v-if="!overviewError && latestTask?.status === 'failed'" class="attention-card danger"><strong><span aria-hidden="true">!</span> 最近数据更新失败</strong><Timestamp :value="latestTask.finished_at" /><p>{{ !recentDetailError && shownDetail?.failure ? shownDetail.failure : '请查看事件详情，定位失败操作及关联日志。' }}</p><RouterLink :to="{ name: 'update-detail', params: { id: latestTask.task_id } }">查看事件 ›</RouterLink></div>
          <ul v-if="concerns.length" class="overview-concerns"><li v-for="concern in concerns.slice(0, 3)" :key="concern">{{ concern }}</li></ul>
          <details v-if="concerns.length > 3" class="more-concerns"><summary>其余 {{ concerns.length - 3 }} 项关注事项</summary><ul class="overview-concerns"><li v-for="concern in concerns.slice(3)" :key="concern">{{ concern }}</li></ul></details>
          <p v-if="!concerns.length && latestTask?.status !== 'failed'" class="hint">已读取的观测中暂无待处理异常；未知状态仍需检查。</p>
        </section>
        <div class="overview-shortcuts"><h2>常用操作</h2><RouterLink to="/apis"><NavigationIcon name="queries" /><span><strong>接口中心</strong><small>查看接口、参数、响应与在线测试</small></span><span aria-hidden="true">›</span></RouterLink><RouterLink to="/updates"><NavigationIcon name="tasks" /><span><strong>查看更新记录</strong><small>查看历史更新事件与执行结果</small></span><span aria-hidden="true">›</span></RouterLink></div>
      </aside>
    </div>
    <details class="overview-diagnostics panel"><summary>服务与存储详情</summary><p class="hint">正常每 15 秒刷新，后台暂停，连接失败时延长间隔。发布与成功检查取自后端最近更新记录，不代表全部历史。日志存储观测不代表日志查询服务就绪。</p>    <div class="section-bar"><h2>服务状态</h2><span class="muted" role="status">{{ checked ? `本页读取 ${checked}` : '正在连接服务' }}</span></div>
    <div class="health-grid" :aria-busy="busy">
      <article v-for="service in services" :key="service.id" class="panel health-panel">
        <div class="section-bar"><h3>{{ service.title }}</h3><span class="tag" :class="{ progress: service.id === 'worker' && workerExecuting, success: !service.error && service.ready && !(service.id === 'worker' && workerExecuting), warning: !!service.error || service.ready === false && !(service.id === 'worker' && workerExecuting) }">{{ service.error ? '当前状态未确认' : service.id === 'worker' && workerExecuting ? activityTitle : service.ready === null ? busy ? '检查中' : '连接未知' : service.ready ? '✓ 已就绪' : '尚未就绪' }}</span></div>
        <details class="service-details"><summary>服务详情 · {{ service.subtitle }}</summary>
        <dl class="facts"><div><dt>进程存活</dt><dd>{{ service.live === null ? '未确认' : service.live ? '正常' : '异常' }}</dd></div><div><dt>业务就绪</dt><dd>{{ service.ready === null ? '未确认' : service.ready ? '可用' : '未就绪' }}</dd></div></dl>
        </details><p v-if="service.error" class="error" role="alert">{{ service.error }}</p>
        <p v-if="service.liveStale && service.liveAt" class="warning">进程状态沿用上次快照；读取时间：<Timestamp :value="service.liveAt" /></p>
        <p v-if="service.readyStale && service.readyAt" class="warning">就绪状态与依赖沿用上次快照；读取时间：<Timestamp :value="service.readyAt" /></p>
        <details v-if="Object.keys(service.details).length"><summary>查看依赖检查</summary><dl class="facts"><div v-for="(value, name) in service.details" :key="name"><dt>{{ name }}</dt><dd>{{ value === null ? '未确认' : value ? '通过' : '未通过' }}</dd></div></dl></details>
      </article>
    </div>
<div class="overview-observations">    <section class="overview-facts panel" aria-label="运行快照"><h2>运行快照</h2><p v-if="overviewError" class="error" role="alert">运行快照读取失败：{{ overviewError }}</p><p v-if="overviewError && overviewSnapshot" class="warning">显示上次成功读取的运行快照，当前状态未确认。</p><p v-if="overviewSnapshot" class="hint">后端观测时间：<Timestamp :value="overviewSnapshot.observed_at" /></p><p v-else class="hint">尚未取得运行快照。</p></section>
    <section class="overview-facts panel" aria-label="日志状态"><h2>日志状态</h2><p class="hint">存储与缓冲分别观测，不代表日志查询服务已经就绪。</p><p v-if="systemError" class="error" role="alert">日志状态读取失败：{{ systemError }}</p><p v-if="systemError && systemSnapshot" class="warning">显示上次成功读取的日志观测，当前状态未确认。</p><p v-if="systemSnapshot" class="hint">后端观测时间：<Timestamp :value="systemSnapshot.observed_at" /></p><dl class="facts"><div v-for="source in logSources" :key="source.id"><dt>{{ source.title }}</dt><dd>存储：{{ observationLabel(systemSnapshot?.[source.id]?.storage_state) }}<small>缓冲：{{ observationLabel(systemSnapshot?.[source.id]?.buffer_state) }}</small></dd></div></dl></section></div></details>
        <section v-if="initialization !== 'complete'" class="panel initialization-note" aria-label="初始化状态">
      <h2>{{ initialization === 'failed' ? '初始化尚未完成，等待补齐' : initialization === 'active' ? '初始化任务正在处理' : initialization === 'unknown' ? '初始化状态未确认' : '等待自动初始化' }}</h2>
      <p v-if="initialization === 'unknown'" class="hint">尚未获得可靠的初始化状态，请重新读取。旧快照不能作为启动维护操作的依据。</p>
      <template v-else>
        <p v-if="initialization === 'failed'">可到更新事件中选择原失败任务重跑，或补齐失败表。已完成的表按后端规则复用；全部补齐并校验后才完成初始化。</p>
        <p v-else-if="initialization === 'active'">初始化由后台执行，包含采集、构建、校验和发布。请查看当前进度，等待完成。</p>
        <p v-else>全新环境由系统自动创建初始化任务。当前尚无可确认的已发布数据，首次初始化完成前不能进行业务查询。</p>
        <RouterLink to="/updates">{{ initialization === 'failed' ? '查看失败任务与补齐入口 →' : '查看初始化进度 →' }}</RouterLink>
        <dl v-if="initialization === 'failed'" class="facts"><div><dt>失败重跑条件</dt><dd>{{ actionReason(overviewSnapshot?.actions?.rerun) }}</dd></div><div><dt>单表补齐条件</dt><dd>{{ actionReason(overviewSnapshot?.actions?.table_force_full) }}</dd></div></dl>
        <p class="hint">页面只提供既有更新操作入口；当前没有手动首次初始化操作。上游冷却和任务冲突以提交时后端判断为准。</p>
      </template>
    </section>

  </section>
</template>
