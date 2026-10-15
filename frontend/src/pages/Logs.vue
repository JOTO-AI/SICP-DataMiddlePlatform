<script setup lang="ts">
import { milliseconds } from "../shared/duration"
import { computed, reactive, ref, watch, onActivated, onDeactivated } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import { request, ApiError, describe, type Envelope } from '../api'
import { emptyLogFilters, logHistoryPath, readLogPage, type LogPage, type LogEvent } from '../app/logHistory'
import { consumeTestLogs } from '../app/testLogNavigation'
import { consumeSourceLogs } from '../app/logNavigation'
import { logViews, logViewFilters, selectedLogView } from '../app/logViews'
import { operations } from '../queryOperations'
import { useRequestScope } from '../composables/useRequestScope'
import PageHeader from '../components/PageHeader.vue'
import ErrorState from '../components/ErrorState.vue'
import EmptyState from '../components/EmptyState.vue'
import Timestamp from '../components/Timestamp.vue'
import CopyId from '../components/CopyId.vue'
import LiveLogStream from '../components/logs/LiveLogStream.vue'
import { logKinds, logPhases, logOutcomes } from '../app/logDetail'
const route = useRoute(), router = useRouter(), listPath = route.path
const routeSource = String(route.params.source ?? 'platform')
const filters = reactive(emptyLogFilters(routeSource === 'upstream' ? 'upstream' : 'platform'))
const validSource = ['platform', 'upstream'].includes(routeSource)
const view = computed(() => selectedLogView(filters))
function chooseView(id: string) { const next = logViewFilters(id); if (next) Object.assign(filters, next) }
const pages = ref<LogPage[]>([]), index = ref(0), busy = ref(false), error = ref(''), expired = ref(false)
const liveBudget = computed(() => Math.max(0, 1000 - pages.value.reduce((sum, page) => sum + page.items.length, 0)))
const current = computed(() => pages.value[index.value]), scope = useRequestScope()
const liveRef = ref<InstanceType<typeof LiveLogStream> | null>(null)
const liveGeneration = ref(0), liveGap = ref(false)
let active = false, keepBatch = false
onBeforeRouteLeave(to => { keepBatch = to.name === 'log-detail' })
function reset() { liveRef.value?.pause(); liveGeneration.value++; liveGap.value = false; scope.cancel(); pages.value = []; index.value = 0; busy.value = false; error.value = ''; expired.value = false }
watch(filters, reset, { flush: 'sync' })
watch(() => filters.mode, mode => { if (mode === 'live') filters.to = '' }, { flush: 'sync' })
watch(() => filters.source, source => { filters.kind = ''; filters.phase = ''; filters.operation = ''; filters.identity = ''; filters.identityField = source === 'upstream' ? 'test_run_id' : 'request_id'; filters.format = 'raw' }, { flush: 'sync' })
async function read(next = false) {
  if (!active || !validSource || busy.value || next && (!current.value?.next_cursor || expired.value)) return
  if (next && index.value + 1 < pages.value.length) { liveRef.value?.pause(); liveRef.value?.clearRecords(); index.value++; return }
  if (next && pages.value.length >= 20) { error.value = '本次最多保留 20 批，请缩小时间范围后重新查询。'; return }
  let path: string
  const cursor = next ? current.value!.next_cursor! : '', previous = next ? current.value : undefined, submitted = { ...filters }
  try { path = logHistoryPath(submitted, cursor) } catch (cause) { error.value = cause instanceof Error ? cause.message : '日志筛选无效。'; return }
  liveRef.value?.pause()
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const result = await request<Envelope<unknown>>('worker', path, { signal: run.signal, domain: 'logs' })
    if (!active || !run.current()) return
    const value = readLogPage(result.body.data, submitted, previous)
    if (result.status !== 200 || result.body.code !== 'ok' || !value || next && (value.next_cursor === cursor || value.next_cursor && pages.value.some(p => p.next_cursor === value.next_cursor) || value.items.some(item => pages.value.some(p => p.items.some(old => old.event_id === item.event_id))))) throw new ApiError('日志响应范围、标识或分页顺序不符合当前查询，未接纳本批。', 'invalid_response')
    liveRef.value?.clearRecords()
    if (next) { pages.value.push(value); index.value++ } else { pages.value = [value]; index.value = 0; liveGeneration.value++ }
    expired.value = false
  } catch (cause) {
    if (active && run.current()) {
      expired.value = cause instanceof ApiError && cause.code === 'cursor_expired'
      error.value = expired.value ? '查询游标已过期或日志保留范围已变化。请重新查询首批；不会自动拼接新旧范围。' : describe(cause)
    }
  } finally { if (run.current()) busy.value = false }
}
function rebuildLive() { liveGap.value = true; void read() }
function previous() { if (!busy.value && index.value > 0) { index.value-- } }
function showDetail(item: LogEvent) { void router.push({ name: 'log-detail', params: { source: item.source, eventId: item.event_id }, query: { epoch: item.epoch, sequence: item.sequence, from: listPath } }) }
onActivated(() => {
  active = true
  const test = consumeTestLogs(), source = consumeSourceLogs()
  if (test && test.source === routeSource) { Object.assign(filters, test); void read() }
  else if (source && source.source === routeSource) { Object.assign(filters, emptyLogFilters('upstream'), { identity: source.task_id, identityField: 'task_id', format: source.id_format }); void read() }
})
function discard() { reset(); Object.assign(filters, emptyLogFilters(routeSource === 'upstream' ? 'upstream' : 'platform')) }
onDeactivated(() => { active = false; liveRef.value?.pause(); scope.cancel(); busy.value = false; if (!keepBatch) discard() })
watch(() => route.path, path => { if (!active && path !== listPath && route.name !== 'log-detail') discard() })
</script>
<template><section class="log-history"><PageHeader title="日志中心" description="查询服务端持久调用日志，筛选在后端执行；开始、结束与逐次尝试可能各占一条事件。" /><template v-if="validSource">
<nav class="log-view-tabs" aria-label="日志视图"><button v-for="item in logViews" :key="item.id" type="button" :class="{ active: view === item.id }" :aria-pressed="view === item.id" @click="chooseView(item.id)">{{ item.title }}</button></nav><p class="hint">当前视图：{{ logViews.find(item => item.id === view)?.title ?? '当前源全部事件' }} · 日志来源：{{ filters.source === 'platform' ? '本地日志库（platform）' : '上游日志库（upstream）' }}。选择视图后点击查询，切换会清除原筛选和已读批次。</p><form class="panel log-filters" aria-label="调用日志筛选" @submit.prevent="read()">
<label>查询模式<select v-model="filters.mode"><option value="history">历史日志</option><option value="live">实时日志</option></select></label><label>日志来源<select v-model="filters.source"><option value="platform">本地日志库</option><option value="upstream">上游日志库</option></select></label>
<label>事件类型<select v-model="filters.kind"><option value="">当前源全部事件</option><template v-if="filters.source === 'platform'"><option value="request">接口请求</option><option value="task">更新任务</option><option value="step">更新步骤</option><option value="audit">管理审计</option><option value="call">逻辑调用</option><option value="attempt">逐次尝试</option></template><template v-else><option value="call">逻辑调用</option><option value="attempt">逐次尝试</option></template></select></label><label>业务接口<select v-model="filters.operation" :disabled="filters.source === 'upstream'"><option value="">全部业务接口</option><option v-for="op in operations" :key="op.operation" :value="op.operation">{{ op.title }}</option></select></label>
<label>关联标识类型<select v-model="filters.identityField"><option value="request_id">请求号</option><option value="test_run_id">源测试作业号</option><option value="task_id">更新任务号</option></select></label>
<label>关联标识<input v-model="filters.identity" autocomplete="off" spellcheck="false" maxlength="128" /></label>
<label>标识格式<select v-model="filters.format"><option value="raw">原始 ID</option><option value="correlation">日志关联 ID</option></select></label>
<label>事件阶段<select v-model="filters.phase"><option value="">全部阶段</option><option value="started">开始</option><option value="finished">结束</option><option value="transport">传输记录</option></select></label><label>调用结果<select v-model="filters.outcome"><option value="">全部结果</option><option value="succeeded">成功</option><option value="failed">失败</option><option value="interrupted">中断</option></select></label>
<label>开始时间<input v-model="filters.from" type="datetime-local" /></label><label v-if="filters.mode !== 'live'">结束时间<input v-model="filters.to" type="datetime-local" /></label>
<p class="hint filter-note">原始请求号、任务号或测试作业号选择“原始 ID”；从日志复制的关联号选择“日志关联 ID”。日志事件号用于详情定位，不作为任务号查询。</p><p class="hint filter-note">按事件发生时间筛选，入库时间单独展示。历史时间留空使用服务端最近 24 小时，手动范围最多 7 天；实时模式只设开始时间，留空由服务端确定起点。输入时间为当前浏览器时区。源测试使用作业号，源表全历史筛选尚未开放。筛选仅保留于当前页面内存。</p><button class="primary" type="submit" :disabled="busy">{{ busy ? '读取中…' : '查询日志首批' }}</button></form>
<ErrorState v-if="error" :message="error" /><p v-if="error && current" class="warning">本次读取未成功，以下保留上次已确认批次。</p>
<section v-if="current" class="panel" aria-label="持久调用日志"><h2>{{ current.source === 'platform' ? '本地日志库' : '上游日志库' }}</h2><p class="hint">范围 <Timestamp :value="String(current.filter.from)" /> 至 <Timestamp v-if="current.filter.to" :value="String(current.filter.to)" /><span v-else>实时（无固定结束时间）</span> · 第 {{ index + 1 }} 批 · 每批最多 50 条</p><p class="hint">后续新增记录不会改变本次历史翻批范围；日志有保留期限，空结果不证明从未调用。</p>
<p v-if="liveGap" class="warning">已请求新的实时起点；前后范围之间可能存在缺口，请按时间补查历史。</p><LiveLogStream ref="liveRef" :enabled="!busy" :budget="liveBudget" v-if="current.filter.mode === 'live' && index === 0" :key="liveGeneration" :initial="current" @rebuild="rebuildLive" @history="filters.mode = 'history'" /><div v-if="current.items.length" class="log-scroll" role="region" aria-label="持久调用日志表格" tabindex="0"><table><thead><tr><th>发生 / 入库时间</th><th>类型 / 阶段</th><th>结果 / HTTP</th><th>耗时（ms）</th><th>关联标识</th><th>事件详情</th></tr></thead><tbody><tr v-for="item in current.items" :key="item.event_id"><td><Timestamp :value="item.occurred_at" /><small>入库：<Timestamp :value="item.received_at" /></small></td><td>{{ logKinds[item.kind] || item.kind }} / {{ logPhases[item.phase] || item.phase }}</td><td>{{ item.outcome ? logOutcomes[item.outcome] || item.outcome : '尚未记录' }} / {{ item.http_status ?? '未记录' }}</td><td>{{ milliseconds(item.duration_ns) }}</td><td><CopyId v-if="item.request_id" :value="item.request_id" label="日志请求关联号" /><CopyId v-if="item.test?.test_run_id" :value="item.test.test_run_id" label="日志测试关联号" /><CopyId v-if="item.task_id" :value="item.task_id" label="日志任务关联号" /></td><td><button type="button" @click="showDetail(item)">查看日志详情</button></td></tr></tbody></table></div>
<EmptyState v-else title="本批没有匹配的日志事件" description="请核对时间与标识，也可能尚未入库或已过保留期。" />
<div class="log-paging"><button :disabled="busy || index === 0" @click="previous">上一批日志</button><button :disabled="busy || expired || !current.next_cursor" @click="read(true)">下一批日志</button><span v-if="!current.next_cursor" class="hint">已到本次范围末批</span></div></section>
<EmptyState v-else :loading="busy" title="尚未查询持久日志" description="选择条件后查询，支持按请求号或源测试作业号定位。" />
</template><ErrorState v-else message="日志来源无效，仅支持 platform 或 upstream。" /></section></template>
<style scoped>
.log-view-tabs { display: flex; flex-direction: row; flex-wrap: nowrap; overflow-x: auto; gap: 24px; margin-block: 18px 12px; border-bottom: 1px solid var(--line); }
.log-view-tabs button { flex-shrink: 0; padding: 10px 2px; border: 0; border-bottom: 2px solid transparent; border-radius: 0; background: transparent; color: var(--muted); }
.log-view-tabs button.active { border-bottom-color: var(--brand); color: var(--color-primary-text); } .log-history { min-width: 0; } .log-filters { display: flex; flex-wrap: wrap; gap: 16px; align-items: end; }
.log-filters label { flex: 1 1 220px; min-width: 0; } .filter-note { flex-basis: 100%; } .panel { margin-bottom: 20px; } .panel > p { margin-block: 12px; } .log-scroll { overflow: auto; } table { width: 100%; min-width: 840px; } .log-paging { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-top: 16px; }
</style>
