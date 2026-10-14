<script setup lang="ts">
import { computed, reactive, ref, watch, onActivated, onDeactivated } from 'vue'
import { useRoute } from 'vue-router'
import { request, ApiError, describe, type Envelope } from '../api'
import { emptyLogFilters, logHistoryPath, readLogPage, logID, type LogPage, type LogEvent } from '../app/logHistory'
import { consumeTestLogs } from '../app/testLogNavigation'
import { consumeSourceLogs } from '../app/logNavigation'
import { operations } from '../queryOperations'
import { useRequestScope } from '../composables/useRequestScope'
import PageHeader from '../components/PageHeader.vue'
import ErrorState from '../components/ErrorState.vue'
import EmptyState from '../components/EmptyState.vue'
import Timestamp from '../components/Timestamp.vue'
import CopyId from '../components/CopyId.vue'
import JsonText from '../components/JsonText.vue'
const routeSource = String(useRoute().params.source ?? 'platform')
const filters = reactive(emptyLogFilters(routeSource === 'upstream' ? 'upstream' : 'platform'))
const validSource = ['platform', 'upstream'].includes(routeSource)
const pages = ref<LogPage[]>([]), index = ref(0), busy = ref(false), error = ref(''), expired = ref(false)
const current = computed(() => pages.value[index.value]), scope = useRequestScope(), detailScope = useRequestScope()
const detail = ref(''), detailBusy = ref(false), detailError = ref(''), detailID = ref('')
let active = false
function clearDetail() { detailScope.cancel(); detail.value = ''; detailError.value = ''; detailID.value = ''; detailBusy.value = false }
function reset() { scope.cancel(); pages.value = []; index.value = 0; busy.value = false; error.value = ''; expired.value = false; clearDetail() }
watch(filters, reset, { flush: 'sync' })
watch(() => filters.source, source => { filters.operation = ''; filters.identity = ''; filters.identityField = source === 'upstream' ? 'test_run_id' : 'request_id'; filters.format = 'raw' }, { flush: 'sync' })
async function read(next = false) {
  if (!active || !validSource || busy.value || next && (!current.value?.next_cursor || expired.value)) return
  if (next && index.value + 1 < pages.value.length) { index.value++; clearDetail(); return }
  if (next && pages.value.length >= 20) { error.value = '本次最多保留 20 批，请缩小时间范围后重新查询。'; return }
  let path: string
  const cursor = next ? current.value!.next_cursor! : '', previous = next ? current.value : undefined, submitted = { ...filters }
  try { path = logHistoryPath(submitted, cursor) } catch (cause) { error.value = describe(cause); return }
  const run = scope.begin(); busy.value = true; error.value = ''; clearDetail()
  try {
    const result = await request<Envelope<unknown>>('worker', path, { signal: run.signal, domain: 'logs' })
    if (!active || !run.current()) return
    const value = readLogPage(result.body.data, submitted, previous)
    if (result.status !== 200 || result.body.code !== 'ok' || !value || next && (value.next_cursor === cursor || value.next_cursor && pages.value.some(p => p.next_cursor === value.next_cursor) || value.items.some(item => pages.value.some(p => p.items.some(old => old.event_id === item.event_id))))) throw new ApiError('日志响应范围、标识或分页顺序不符合当前查询，未接纳本批。', 'invalid_response')
    if (next) { pages.value.push(value); index.value++ } else { pages.value = [value]; index.value = 0 }
    expired.value = false
  } catch (cause) {
    if (active && run.current()) {
      expired.value = cause instanceof ApiError && cause.code === 'cursor_expired'
      error.value = expired.value ? '查询游标已过期或日志保留范围已变化。请重新查询首批；不会自动拼接新旧范围。' : describe(cause)
    }
  } finally { if (run.current()) busy.value = false }
}
function previous() { if (!busy.value && index.value > 0) { index.value--; clearDetail() } }
async function showDetail(item: LogEvent) {
  clearDetail(); const run = detailScope.begin(); detailBusy.value = true; detailID.value = item.event_id
  try {
    const result = await request<Envelope<unknown>>('worker', `/admin/v1/logs/${item.source}/events/${item.event_id}`, { signal: run.signal, domain: 'logs' })
    if (!active || !run.current()) return
    const value = result.body.data as Record<string, unknown> | null
    if (result.status !== 200 || result.body.code !== 'ok' || !value || value.source !== item.source || value.epoch !== item.epoch || value.event_id !== item.event_id || value.sequence !== item.sequence || !logID(value.event_id)) throw new ApiError('日志详情与所选事件不一致。', 'invalid_response')
    detail.value = JSON.stringify(value, null, 2)
  } catch (cause) { if (active && run.current()) detailError.value = cause instanceof ApiError && cause.status === 404 ? '该事件暂不可查询，可能已过保留期；不能据此判断从未执行。' : describe(cause) }
  finally { if (run.current()) detailBusy.value = false }
}
onActivated(() => {
  active = true
  const test = consumeTestLogs(), source = consumeSourceLogs()
  if (test && test.source === routeSource) { Object.assign(filters, test); void read() }
  else if (source && source.source === routeSource) { Object.assign(filters, emptyLogFilters('upstream'), { identity: source.task_id, identityField: 'task_id', format: source.id_format }); void read() }
})
onDeactivated(() => { active = false; reset(); Object.assign(filters, emptyLogFilters(routeSource === 'upstream' ? 'upstream' : 'platform')) })
</script>
<template><section class="log-history"><PageHeader title="日志中心" description="查询服务端持久调用日志，筛选在后端执行；开始、结束与逐次尝试可能各占一条事件。" /><template v-if="validSource">
<form class="panel log-filters" aria-label="调用日志筛选" @submit.prevent="read()">
<label>日志来源<select v-model="filters.source"><option value="platform">本地日志库</option><option value="upstream">上游日志库</option></select></label>
<label>业务接口<select v-model="filters.operation" :disabled="filters.source === 'upstream'"><option value="">全部业务接口</option><option v-for="op in operations" :key="op.operation" :value="op.operation">{{ op.title }}</option></select></label>
<label>关联标识类型<select v-model="filters.identityField"><option value="request_id">请求号</option><option value="test_run_id">源测试作业号</option><option value="task_id">更新任务号</option></select></label>
<label>关联标识<input v-model="filters.identity" autocomplete="off" spellcheck="false" maxlength="128" /></label>
<label>标识格式<select v-model="filters.format"><option value="raw">原始 ID</option><option value="correlation">日志关联 ID</option></select></label>
<label>调用结果<select v-model="filters.outcome"><option value="">全部结果</option><option value="succeeded">成功</option><option value="failed">失败</option><option value="interrupted">中断</option></select></label>
<label>开始时间<input v-model="filters.from" type="datetime-local" /></label><label>结束时间<input v-model="filters.to" type="datetime-local" /></label>
<p class="hint filter-note">时间留空使用服务端最近 24 小时；手动时间为当前浏览器时区，最多查询 7 天。源测试使用作业号，源表全历史筛选尚未开放。筛选仅保留于当前页面内存。</p><button class="primary" type="submit" :disabled="busy">{{ busy ? '读取中…' : '查询日志首批' }}</button></form>
<ErrorState v-if="error" :message="error" /><p v-if="error && current" class="warning">本次读取未成功，以下保留上次已确认批次。</p>
<section v-if="current" class="panel" aria-label="持久调用日志"><h2>{{ current.source === 'platform' ? '本地日志库' : '上游日志库' }}</h2><p class="hint">范围 <Timestamp :value="String(current.filter.from)" /> 至 <Timestamp :value="String(current.filter.to)" /> · 第 {{ index + 1 }} 批 · 每批最多 50 条</p><p class="hint">后续新增记录不会改变本次翻批范围；查看新日志请重新查询首批。日志有保留期限，空结果不证明从未调用。</p>
<div v-if="current.items.length" class="log-scroll" role="region" aria-label="持久调用日志表格" tabindex="0"><table><thead><tr><th>发生时间</th><th>类型 / 阶段</th><th>结果 / HTTP</th><th>耗时（ns）</th><th>关联标识</th><th>事件详情</th></tr></thead><tbody><tr v-for="item in current.items" :key="item.event_id"><td><Timestamp :value="item.occurred_at" /></td><td>{{ item.kind }} / {{ item.phase }}</td><td>{{ item.outcome ?? '尚未记录' }} / {{ item.http_status ?? '未记录' }}</td><td>{{ item.duration_ns ?? '未记录' }}</td><td><CopyId v-if="item.request_id" :value="item.request_id" label="日志请求关联号" /><CopyId v-if="item.test?.test_run_id" :value="item.test.test_run_id" label="日志测试关联号" /><CopyId v-if="item.task_id" :value="item.task_id" label="日志任务关联号" /></td><td><button type="button" @click="showDetail(item)">查看日志详情</button></td></tr></tbody></table></div>
<EmptyState v-else title="本批没有匹配的日志事件" description="请核对时间与标识，也可能尚未入库或已过保留期。" />
<div class="log-paging"><button :disabled="busy || index === 0" @click="previous">上一批日志</button><button :disabled="busy || expired || !current.next_cursor" @click="read(true)">下一批日志</button><span v-if="!current.next_cursor" class="hint">已到本次范围末批</span></div></section>
<EmptyState v-else :loading="busy" title="尚未查询持久日志" description="选择条件后查询，支持按请求号或源测试作业号定位。" />
<section v-if="detailID" class="panel" aria-label="所选日志详情"><div class="section-bar"><h2>日志详情</h2><button @click="clearDetail">关闭日志详情</button></div><CopyId :value="detailID" label="日志事件号" /><p class="hint">仅展示后端脱敏详情；内容可能截断，不保证保存完整测试响应。</p><ErrorState v-if="detailError" :message="detailError" /><p v-if="detailBusy" role="status">正在读取日志详情…</p><JsonText v-if="detail" :text="detail" label="日志脱敏详情" /></section>
</template><ErrorState v-else message="日志来源无效，仅支持 platform 或 upstream。" /></section></template>
<style scoped>
.log-history { min-width: 0; } .log-filters { display: flex; flex-wrap: wrap; gap: 16px; align-items: end; }
.log-filters label { flex: 1 1 220px; min-width: 0; } .filter-note { flex-basis: 100%; } .panel { margin-bottom: 20px; } .panel > p { margin-block: 12px; } .log-scroll { overflow: auto; } table { width: 100%; min-width: 840px; } .log-paging { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-top: 16px; }
</style>
