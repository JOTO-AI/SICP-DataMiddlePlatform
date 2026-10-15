<script setup lang="ts">
import { ref, watch, onMounted, onDeactivated, onUnmounted } from 'vue'
import { createLogStreamState } from '../../app/logSSE'
import { reconnectDelay, streamFailureAction } from '../../app/logLive'
import { readLogStream, LogStreamError } from '../../api/logStream'
import type { LogPage, LogEvent } from '../../app/logHistory'
import { useRequestScope } from '../../composables/useRequestScope'
import { session } from '../../session'
import Timestamp from '../Timestamp.vue'
const props = defineProps<{ initial: LogPage; enabled: boolean }>(), emit = defineEmits<{ rebuild: [] }>(), scope = useRequestScope()
const state = createLogStreamState(props.initial), rows = ref<LogEvent[]>([]), status = ref('尚未连接'), busy = ref(false), waiting = ref(false), rebuild = ref(false)
let retries = 0, active = true, timer: ReturnType<typeof setTimeout> | undefined
function stop() { clearTimeout(timer); timer = undefined; waiting.value = false; scope.cancel(); busy.value = false }
function gap() { stop(); rebuild.value = true; status.value = '日志恢复位置或范围已变化，可能存在缺口。请重新读取实时起点，不能静默拼接新旧范围。' }
function retry(after = 0) {
  if (!active || !props.enabled || !session.operator || rebuild.value) return
  const delay = reconnectDelay(retries++, after)
  waiting.value = true; status.value = `实时连接已断开，${Math.ceil(delay / 1000)} 秒后从最后完整位置重连。`
  timer = setTimeout(() => { timer = undefined; waiting.value = false; void connect() }, delay)
}
async function connect() {
  if (!active || !props.enabled || !session.operator || busy.value || waiting.value || rebuild.value) return
  const run = scope.begin(); busy.value = true; status.value = '正在接收实时日志'
  try {
    await readLogStream(props.initial.source, state.cursor, run.signal, frame => {
      if (!run.current()) return
      const result = state.accept(frame)
      if (frame.event === 'event' || frame.event === 'checkpoint') retries = 0
      if (result.control) {
        if (result.control.rebuild_required) gap()
        else { stop(); if (result.control.retryable) retry(); else status.value = '服务端停止本次连接，请核对服务状态后重试。' }
      }
      if (result.item) { rows.value.unshift(result.item); if (rows.value.length > 950) { rows.value.length = 950; status.value = '实时列表已裁剪，请到历史日志补查。' } }
    })
    if (run.current()) { busy.value = false; retry() }
  } catch (cause) {
    if (!run.current()) return
    busy.value = false
    const action = streamFailureAction(cause instanceof LogStreamError ? cause.status : 0)
    if (action === 'rebuild') gap()
    else if (action === 'retry') retry(cause instanceof LogStreamError ? cause.retryAfter : 0)
    else status.value = '实时连接被拒绝或响应类型异常，已停止自动重连，请核对服务状态。'
  } finally { if (run.current()) busy.value = false }
}
watch(() => props.enabled, enabled => { if (enabled) void connect(); else { stop(); status.value = '已暂停实时连接，保留最后完整位置。' } }, { flush: 'sync' })
watch(() => session.epoch, () => { active = false; stop(); rows.value = [] }, { flush: 'sync' })
defineExpose({ pause: stop })
onMounted(() => void connect())
function leave() { active = false; stop(); rows.value = [] }
onDeactivated(leave); onUnmounted(leave)
</script>
<template><section aria-label="实时日志接收"><h3>实时接收</h3><p role="status">{{ status }}</p><button v-if="rebuild" @click="emit('rebuild')">重新读取实时起点</button><button v-else :disabled="busy || waiting || !enabled" @click="connect">继续实时连接</button><p class="hint">已接收 {{ rows.length }} 条新增事件；恢复位置只取服务端完整消息，历史列表不随新事件重排。</p><ol><li v-for="item in rows" :key="`${item.source}/${item.epoch}/${item.event_id}`"><Timestamp :value="item.occurred_at" /> · {{ item.kind }} / {{ item.phase }} <RouterLink :to="{ name: 'log-detail', params: { source: item.source, eventId: item.event_id } }">查看新增事件详情</RouterLink></li></ol></section></template>
<style scoped>section { margin-block: 16px; } p { margin-block: 10px; } ol { max-height: 360px; overflow: auto; padding-left: 24px; } li { padding-block: 8px; overflow-wrap: anywhere; }</style>