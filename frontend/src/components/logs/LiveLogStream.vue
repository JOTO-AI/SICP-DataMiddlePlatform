<script setup lang="ts">
import { ref, reactive, watch, onMounted, onDeactivated, onUnmounted } from 'vue'
import { createLogStreamState } from '../../app/logSSE'
import { reconnectDelay, streamFailureAction } from '../../app/logLive'
import { readLogStream, LogStreamError } from '../../api/logStream'
import { emptyLogBuffer, appendLog, revealLogs, trimLogBuffer } from '../../app/logBuffer'
import type { LogPage } from '../../app/logHistory'
import { useRequestScope } from '../../composables/useRequestScope'
import { usePageVisibility } from '../../composables/usePageVisibility'
import { session } from '../../session'
import Timestamp from '../Timestamp.vue'
import { logKinds, logPhases } from '../../app/logDetail'
const props = defineProps<{ initial: LogPage; enabled: boolean; budget: number }>(), emit = defineEmits<{ rebuild: []; history: [] }>(), scope = useRequestScope()
const state = createLogStreamState(props.initial), buffer = reactive(emptyLogBuffer()), status = ref('尚未连接'), busy = ref(false), waiting = ref(false), rebuild = ref(false), blocked = ref(false)
let retries = 0, active = true, retryNotBefore = 0, timer: ReturnType<typeof setTimeout> | undefined
function stop() { clearTimeout(timer); timer = undefined; waiting.value = false; scope.cancel(); busy.value = false }
const pageVisible = usePageVisibility(() => { stop(); if (!rebuild.value && !blocked.value) status.value = '页面已隐藏，实时连接暂停；返回后从最后完整位置恢复。' }, () => { if (active) void connect() })
function gap() { stop(); rebuild.value = true; status.value = '日志恢复位置或范围已变化，可能存在缺口。请重新读取实时起点，不能静默拼接新旧范围。' }
function retry(after = 0) {
  if (!active || !pageVisible.value || !props.enabled || !session.operator || rebuild.value) return
  schedule(reconnectDelay(retries++, after))
}
function schedule(delay: number) {
  retryNotBefore = Date.now() + delay
  waiting.value = true; status.value = `实时连接已断开，${Math.ceil(delay / 1000)} 秒后从最后完整位置重连。`
  timer = setTimeout(() => { timer = undefined; waiting.value = false; void connect() }, delay)
}
async function connect() {
  if (!active || !pageVisible.value || !props.enabled || !session.operator || busy.value || waiting.value || rebuild.value || blocked.value) return
  if (retryNotBefore > Date.now()) { schedule(retryNotBefore - Date.now()); return }
  const run = scope.begin(); busy.value = true; status.value = '正在接收实时日志'
  try {
    await readLogStream(props.initial.source, state.cursor, run.signal, frame => {
      if (!run.current()) return
      const result = state.accept(frame)
      if (frame.event === 'event' || frame.event === 'checkpoint') retries = 0
      if (result.control) {
        if (result.control.rebuild_required) gap()
        else { stop(); if (result.control.retryable) retry(); else { blocked.value = true; status.value = '服务端停止本次连接，请核对服务状态后重试。' } }
      }
      if (result.item) appendLog(buffer, result.item, props.budget)
    })
    if (run.current()) { busy.value = false; retry() }
  } catch (cause) {
    if (!run.current()) return
    busy.value = false
    const action = streamFailureAction(cause instanceof LogStreamError ? cause.status : 0)
    if (action === 'rebuild') gap()
    else if (action === 'retry') retry(cause instanceof LogStreamError ? cause.retryAfter : 0)
    else { blocked.value = true; status.value = '实时连接被拒绝或响应类型异常，已停止自动重连，请核对服务状态。' }
  } finally { if (run.current()) busy.value = false }
}
watch(() => props.enabled, enabled => { if (enabled) void connect(); else { stop(); status.value = '已暂停实时连接，保留最后完整位置。' } }, { flush: 'sync' })
watch(() => session.epoch, () => { active = false; stop(); Object.assign(buffer, emptyLogBuffer()) }, { flush: 'sync' })
watch(() => props.budget, limit => trimLogBuffer(buffer, limit), { flush: 'sync' })
function clearRecords() { Object.assign(buffer, emptyLogBuffer()) }
defineExpose({ pause: stop, clearRecords })
function manualConnect() { blocked.value = false; void connect() }
onMounted(() => void connect())
function leave() { active = false; stop(); Object.assign(buffer, emptyLogBuffer()) }
onDeactivated(leave); onUnmounted(leave)
</script>
<template><section aria-label="实时日志接收"><h3>实时接收</h3><p role="status">{{ status }}</p><button v-if="rebuild" @click="emit('rebuild')">重新读取实时起点</button><button v-else :disabled="busy || waiting || !enabled" @click="manualConnect">继续实时连接</button><p class="hint">已接收 {{ buffer.received }} 条新增事件；恢复位置只取服务端完整消息，历史列表不随新事件重排。</p><p>已展示 {{ buffer.visible.length }} 条 · 待展示 {{ buffer.pending.length }} 条 · 实时剩余预算 {{ budget }} 条</p><button :disabled="!buffer.pending.length" @click="revealLogs(buffer, budget)">显示 {{ buffer.pending.length }} 条新日志</button><p v-if="buffer.dropped" class="warning">本次已有 {{ buffer.dropped }} 条摘要被裁剪，不能视为完整历史。<button @click="emit('history')">转到历史补查</button></p><ol aria-label="已展示实时事件"><li v-for="item in buffer.visible" :key="`${item.source}/${item.epoch}/${item.event_id}`"><Timestamp :value="item.occurred_at" /> · {{ logKinds[item.kind] }} / {{ logPhases[item.phase] }} <RouterLink :to="{ name: 'log-detail', params: { source: item.source, eventId: item.event_id } }">查看新增事件详情</RouterLink></li></ol></section></template>
<style scoped>section { margin-block: 16px; } p { margin-block: 10px; } ol { max-height: 360px; overflow: auto; padding-left: 24px; } li { padding-block: 8px; overflow-wrap: anywhere; }</style>