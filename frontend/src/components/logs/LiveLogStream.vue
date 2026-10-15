<script setup lang="ts">
import { ref, onMounted, onDeactivated } from 'vue'
import { createLogStreamState } from '../../app/logSSE'
import { readLogStream } from '../../api/logStream'
import type { LogPage, LogEvent } from '../../app/logHistory'
import { useRequestScope } from '../../composables/useRequestScope'
import Timestamp from '../Timestamp.vue'
const props = defineProps<{ initial: LogPage }>(), scope = useRequestScope()
const state = createLogStreamState(props.initial), rows = ref<LogEvent[]>([]), status = ref('尚未连接'), busy = ref(false)
async function connect() {
  if (busy.value) return
  const run = scope.begin(); busy.value = true; status.value = '正在接收实时日志'
  try {
    await readLogStream(props.initial.source, state.cursor, run.signal, frame => {
      if (!run.current()) return
      const result = state.accept(frame)
      if (result.control) { status.value = result.control.rebuild_required ? '日志范围已变化，存在缺口，请重新查询首批。' : '服务端结束本次连接，可从已确认位置继续连接。'; scope.cancel(); busy.value = false }
      if (result.item) { rows.value.unshift(result.item); if (rows.value.length > 950) { rows.value.length = 950; status.value = '实时列表已裁剪，请到历史日志补查。' } }
    })
    if (run.current()) status.value = '连接已结束，保留完整事件及最后确认位置。'
  } catch { if (run.current()) status.value = '实时连接中断，保留已确认记录；可继续连接或重查历史。' }
  finally { if (run.current()) busy.value = false }
}
onMounted(() => void connect())
onDeactivated(() => { scope.cancel(); busy.value = false; rows.value = [] })
</script>
<template><section aria-label="实时日志接收"><h3>实时接收</h3><p role="status">{{ status }}</p><button :disabled="busy" @click="connect">继续实时连接</button><p class="hint">已接收 {{ rows.length }} 条新增事件；恢复位置只取服务端完整消息，历史列表不随新事件重排。</p><ol><li v-for="item in rows" :key="`${item.source}/${item.epoch}/${item.event_id}`"><Timestamp :value="item.occurred_at" /> · {{ item.kind }} / {{ item.phase }} <RouterLink :to="{ name: 'log-detail', params: { source: item.source, eventId: item.event_id } }">查看新增事件详情</RouterLink></li></ol></section></template>
<style scoped>section { margin-block: 16px; } p { margin-block: 10px; } ol { max-height: 360px; overflow: auto; padding-left: 24px; } li { padding-block: 8px; overflow-wrap: anywhere; }</style>