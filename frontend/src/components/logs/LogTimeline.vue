<script setup lang="ts">
import { computed, ref, watch, onDeactivated, useId } from 'vue'
import { request, ApiError, describe, type Envelope } from '../../api'
import { emptyLogFilters, logHistoryPath, readLogPage, type LogEvent, type LogPage, type LogFilters } from '../../app/logHistory'
import { logActivity } from '../../app/logActivity'
import { timestampMicros } from '../../shared/timestamp'
import { milliseconds } from '../../shared/duration'
import { useRequestScope } from '../../composables/useRequestScope'
import Timestamp from '../Timestamp.vue'
import ErrorState from '../ErrorState.vue'
const props = defineProps<{ event: LogEvent }>(), scope = useRequestScope()
const pages = ref<LogPage[]>([]), busy = ref(false), error = ref('')
const expanded = ref(false), previewCount = 3, listId = useId()
const visibleRows = computed(() => expanded.value ? rows.value : rows.value.slice(0, previewCount))
const last = computed(() => pages.value.at(-1))
const filters = computed<LogFilters | null>(() => {
  const event = props.event, at = event.occurred_at ? Date.parse(event.occurred_at) : NaN
  const field = event.request_id ? 'request_id' : event.task_id ? 'task_id' : event.test?.test_run_id ? 'test_run_id' : null
  if (!field || !Number.isFinite(at)) return null
  return { ...emptyLogFilters(event.source), identityField: field, identity: field === 'test_run_id' ? event.test!.test_run_id : event[field]!, format: 'correlation', from: new Date(Math.max(Date.UTC(2000, 0, 1), at - 86400000)).toISOString(), to: new Date(Math.min(Date.UTC(9999, 11, 31), at + 86400000)).toISOString() }
})
const rows = computed(() => {
  const items = new Map<string, LogEvent>()
  for (const page of pages.value) for (const item of page.items) items.set(item.event_id, item)
  items.set(props.event.event_id, props.event)
  return [...items.values()].sort((a, b) => {
    const at = timestampMicros(a.occurred_at), bt = timestampMicros(b.occurred_at)
    if (at === null || bt === null) return at === bt ? 0 : at === null ? 1 : -1
    return at === bt ? 0 : at < bt ? -1 : 1
  })
})
function reset() { expanded.value = false; scope.cancel(); pages.value = []; busy.value = false; error.value = '' }
watch(() => props.event, reset); onDeactivated(reset)
async function read(next = false) {
  if (busy.value || !filters.value || next && (!last.value?.next_cursor || pages.value.length >= 20)) return
  const previous = next ? last.value : undefined, cursor = previous?.next_cursor ?? '', submitted = { ...filters.value }
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const result = await request<Envelope<unknown>>('worker', logHistoryPath(submitted, cursor), { signal: run.signal, domain: 'logs' })
    if (!run.current()) return
    const page = readLogPage(result.body.data, submitted, previous)
    if (result.status !== 200 || result.body.code !== 'ok' || !page || page.epoch !== props.event.epoch || next && (page.next_cursor === cursor || page.next_cursor && pages.value.some(old => old.next_cursor === page.next_cursor) || page.items.some(item => pages.value.some(old => old.items.some(row => row.event_id === item.event_id))))) throw new ApiError('关联时序的世代或分页范围已变化，请重新读取。', 'invalid_response')
    if (next) pages.value.push(page); else { pages.value = [page]; expanded.value = false }
  } catch (cause) { if (run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
</script>
<template><section class="execution-timeline panel" aria-label="执行时序"><div class="section-bar"><h2>执行时序</h2><button v-if="filters" :disabled="busy" @click="read()">{{ pages.length ? '重新读取关联时序' : '读取关联时序' }}</button></div><ErrorState v-if="error" :message="error" /><p v-if="error && pages.length" class="warning">本次读取失败，保留已确认的部分记录。</p><ol :id="listId"><li v-for="item in visibleRows" :key="item.event_id" :class="item.outcome || 'unconfirmed'"><Timestamp precise :value="item.occurred_at" empty="发生时间未提供" /><div><strong>{{ logActivity(item).title }}</strong><p v-if="logActivity(item).details">{{ logActivity(item).details }}</p><p>{{ logActivity(item).result }}<template v-if="item.http_status !== null"> · HTTP {{ item.http_status }}</template><template v-if="item.duration_ns !== null"> · {{ milliseconds(item.duration_ns) }} ms</template></p><RouterLink v-if="item.event_id !== event.event_id" :to="{ name: 'log-detail', params: { source: item.source, eventId: item.event_id }, query: { epoch: item.epoch, sequence: item.sequence } }">查看日志详情</RouterLink><small v-else>当前事件</small></div></li></ol><div v-if="rows.length > previewCount" class="timeline-toggle"><p class="hint">{{ expanded ? '已显示全部已读取的' : '显示前 ' + previewCount + ' 条，共已读取' }} {{ rows.length }} 条记录</p><button type="button" :aria-expanded="expanded" :aria-controls="listId" @click="expanded = !expanded">{{ expanded ? '收起' : '展开全部已读取记录（' + rows.length + ' 条）' }}</button></div><p class="hint">{{ pages.length ? '按发生时间排列已读取的关联事件；相同时间不代表先后或因果。' : '当前仅显示所选事件；其它执行阶段需读取关联记录。' }}</p><p v-if="filters" class="hint">读取本事件前后各一天、同源同世代且关联号相同的事件；入库时间不作为执行阶段。</p><p v-else class="hint">缺少关联号或发生时间，无法可靠读取其它阶段。</p><template v-if="pages.length"><p class="hint">已读 {{ pages.length }} 批。{{ last?.next_cursor ? '仍有未读事件，当前为部分时序。' : '本次范围已读完，不能据此确认完整执行链。' }}</p><button v-if="last?.next_cursor" :disabled="busy || pages.length >= 20" @click="read(true)">继续读取时序</button><p v-if="last?.next_cursor && pages.length >= 20" class="warning">已达 20 批上限，请在日志中心按时间分段查询。</p></template></section></template>
<style scoped>
.execution-timeline { min-width: 0; overflow-wrap: anywhere; } .section-bar { flex-wrap: wrap; gap: 12px; } .section-bar button { font-size: 12px; padding: 6px 10px; }
ol { list-style: none; padding: 8px 0 0; margin: 0; } li { position: relative; display: grid; grid-template-columns: minmax(110px, .85fr) minmax(0, 1fr); gap: 16px; padding: 0 0 28px 22px; min-height: 85px; font-size: 13px; } li::before { content: ''; position: absolute; width: 1px; background: var(--line); top: 10px; bottom: -10px; left: 4px; } li:last-child::before { display: none; } li::after { content: ''; width: 9px; height: 9px; border-radius: 50%; position: absolute; left: 0; top: 5px; background: var(--muted); } li.succeeded::after { background: var(--green); } li.failed::after, li.interrupted::after { background: var(--red); } li p, li small { color: var(--muted); margin-top: 6px; } li strong { font-size: 14px; } .hint { margin-top: 12px; }
.timeline-toggle { display: flex; flex-wrap: wrap; justify-content: center; align-items: center; gap: 8px 16px; padding-bottom: 12px; } .timeline-toggle .hint { margin: 0; } .timeline-toggle button { font-size: 13px; }
@media(max-width: 500px) { li { grid-template-columns: minmax(0,1fr); gap: 8px; padding-bottom: 24px; } }
</style>
