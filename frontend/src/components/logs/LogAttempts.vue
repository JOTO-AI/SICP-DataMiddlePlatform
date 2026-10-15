<script setup lang="ts">
import { milliseconds } from "../../shared/duration"
import { computed, ref, watch, onDeactivated } from 'vue'
import { request, ApiError, describe, type Envelope } from '../../api'
import { attemptFilters, belongsToCall } from '../../app/logAttempts'
import { logHistoryPath, readLogPage, type LogEvent, type LogPage } from '../../app/logHistory'
import { logOutcomes, logPhases } from '../../app/logDetail'
import { useRequestScope } from '../../composables/useRequestScope'
import Timestamp from '../Timestamp.vue'
import ErrorState from '../ErrorState.vue'
const props = defineProps<{ call: LogEvent }>()
const filters = computed(() => attemptFilters(props.call)), scope = useRequestScope()
const pages = ref<LogPage[]>([]), busy = ref(false), error = ref('')
const rows = computed(() => pages.value.flatMap(p => p.items).filter(item => belongsToCall(item, props.call)))
const last = computed(() => pages.value.at(-1))
function reset() { scope.cancel(); pages.value = []; busy.value = false; error.value = '' }
watch(() => props.call, reset); onDeactivated(reset)
async function read(next = false) {
  if (busy.value || !filters.value || next && (!last.value?.next_cursor || pages.value.length >= 20)) return
  const previous = next ? last.value : undefined, cursor = previous?.next_cursor ?? '', submitted = { ...filters.value }
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const result = await request<Envelope<unknown>>('worker', logHistoryPath(submitted, cursor), { signal: run.signal, domain: 'logs' })
    if (!run.current()) return
    const p = readLogPage(result.body.data, submitted, previous)
    if (result.status !== 200 || result.body.code !== 'ok' || !p || p.epoch !== props.call.epoch || next && (p.next_cursor === cursor || p.next_cursor && pages.value.some(old => old.next_cursor === p.next_cursor) || p.items.some(item => pages.value.some(old => old.items.some(row => row.event_id === item.event_id))))) throw new ApiError('尝试日志的世代或分页范围已变化，请重新读取。', 'invalid_response')
    if (next) pages.value.push(p); else pages.value = [p]
  } catch (cause) { if (run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
</script>
<template><section aria-label="逻辑调用逐次尝试"><h3>逐次尝试</h3><p class="hint">仅按相同日志源、世代和逻辑调用关联号归属。开始、结束和传输记录分别保留，不合计为尝试次数或调用总耗时。</p><template v-if="filters"><p class="hint">读取发生时间前后各一天内、同一请求/任务/测试关联号的尝试事件，再核对调用关联号。范围外、迟到或已过保留期的记录不在本次结果内。</p><p>查询范围：<Timestamp :value="filters.from" /> 至 <Timestamp :value="filters.to" /></p><button :disabled="busy" @click="read()">{{ pages.length ? '重新读取调用尝试' : '读取调用尝试' }}</button><ErrorState v-if="error" :message="error" /><p v-if="error && pages.length" class="warning">保留已确认的部分记录，本次读取失败。</p><template v-if="pages.length"><p class="hint">已读 {{ pages.length }} 批，匹配 {{ rows.length }} 条事件。{{ last?.next_cursor ? '还有未读取的事件，当前为部分结果。' : '本次范围已读完，仍不代表完整调用生命周期。' }}</p><p v-if="!rows.length">本次已读范围没有可靠关联的尝试，不能据此判断未执行或调用成功。</p><div v-else class="attempt-table" role="region" aria-label="调用尝试事件表" tabindex="0"><table><thead><tr><th>尝试号 / 阶段</th><th>发生时间</th><th>结果 / HTTP</th><th>耗时（ms）</th><th>记录</th></tr></thead><tbody><tr v-for="item in rows" :key="item.event_id"><td>{{ item.attempt_no }} / {{ logPhases[item.phase] }}</td><td><Timestamp :value="item.occurred_at" /></td><td>{{ item.outcome ? logOutcomes[item.outcome] : '未记录' }} / {{ item.http_status ?? '未记录' }}</td><td>{{ milliseconds(item.duration_ns) }}</td><td><RouterLink :to="{ name: 'log-detail', params: { source: item.source, eventId: item.event_id } }">查看尝试详情</RouterLink></td></tr></tbody></table></div><button v-if="last?.next_cursor" :disabled="busy || pages.length >= 20" @click="read(true)">继续读取尝试</button><p v-if="last?.next_cursor && pages.length >= 20" class="warning">已达 20 批上限，当前仅为部分记录；可返回日志中心按时间分段查询。</p></template></template><p v-else class="warning">缺少调用关联号、外围关联标识或发生时间，无法可靠读取逐次尝试；不按相近时间拼接。</p></section></template>
<style scoped>section { margin-block: 24px; } p { margin-block: 12px; } .attempt-table { max-width: 100%; overflow: auto; margin-block: 12px; } table { min-width: 650px; width: 100%; }</style>