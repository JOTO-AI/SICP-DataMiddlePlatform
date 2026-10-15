<script setup lang="ts">
import { ref, onMounted, onActivated, onDeactivated } from 'vue'
import { request, ApiError, describe, type Envelope } from '../api'
import { readSystemStatus, statusObject, statusCount, statusBoolean, statusConfigured, statusState, statusTime, type SystemStatus } from '../app/systemStatus'
import { useRequestScope } from '../composables/useRequestScope'
import ErrorState from './ErrorState.vue'
import Timestamp from './Timestamp.vue'
import SystemConfiguration from './SystemConfiguration.vue'
const value = ref<SystemStatus | null>(null), busy = ref(false), error = ref(''), scope = useRequestScope()
let active = true
async function read() {
  if (!active || busy.value) return
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', '/admin/v1/system/status', { signal: run.signal })
    if (!run.current()) return
    const data = readSystemStatus(response.body.data)
    if (response.status !== 200 || response.body.code !== 'ok' || !data) throw new ApiError('系统状态响应不符合当前契约。', 'invalid_response')
    value.value = data
  } catch (cause) { if (run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
function activate() { active = true; if (!value.value) void read() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; scope.cancel(); value.value = null; busy.value = false; error.value = '' })
</script>
<template><section class="panel" aria-label="系统运行状态"><div class="section-bar"><h2>系统运行状态</h2><button :disabled="busy" @click="read">刷新系统状态</button></div><ErrorState v-if="error" :message="error" /><p v-if="error && value" class="warning">读取失败，以下保留上次已确认快照。</p><template v-if="value"><p>探测时间：<Timestamp :value="statusTime(value.observed_at)" empty="未提供" /> · {{ statusState(value.probe_state) }}</p><p>日志存储：{{ value.log_storage_mode === 'dual' ? '双源独立存储' : '单库存储（兼容模式）' }} · 保留期读取：{{ statusState(value.retention_state) }}</p><h3>服务依赖</h3><dl class="facts"><div v-for="(label, key) in { dependencies_ready: '基础依赖', query_ready: '业务查询', sync_ready: '更新执行' }" :key="key"><dt>{{ label }}</dt><dd>{{ statusBoolean(statusObject(value.dependencies)[key]) }}</dd></div></dl><div class="sources"><section v-for="(label, source) in { platform: '平台日志', upstream: '上游日志' }" :key="source" :aria-label="label + '存储状态'"><h3>{{ label }}</h3><p>读取配置：{{ statusConfigured(statusObject(value[source]).reader_configured) }} · 维护配置：{{ statusConfigured(statusObject(value[source]).maintenance_configured) }}</p><dl class="facts"><div><dt>缓冲状态</dt><dd>{{ statusState(statusObject(value[source]).buffer_state) }}</dd></div><div><dt>缓冲已用字节 / 文件</dt><dd>{{ statusCount(statusObject(value[source]).buffer_bytes) }} / {{ statusCount(statusObject(value[source]).buffer_files) }}</dd></div><div><dt>缓冲上限字节 / 文件</dt><dd>{{ statusCount(statusObject(value[source]).buffer_max_bytes) }} / {{ statusCount(statusObject(value[source]).buffer_max_files) }}</dd></div><div><dt>实际存储状态</dt><dd>{{ statusState(statusObject(value[source]).storage_state) }}</dd></div><div><dt>事件 / 回执实际字节</dt><dd>{{ statusCount(statusObject(statusObject(value[source]).storage).event_bytes) }} / {{ statusCount(statusObject(statusObject(value[source]).storage).receipt_bytes) }}</dd></div><div><dt>保留下界 / 高水位</dt><dd>{{ statusCount(statusObject(statusObject(value[source]).storage).floor) }} / {{ statusCount(statusObject(statusObject(value[source]).storage).high_water) }}</dd></div></dl></section></div><p class="hint">配置存在与实际可用性分别展示；缺少探测结果或容量保持未确认，不换算为零或健康。</p><SystemConfiguration :value="value" /></template><p v-else role="status">{{ busy ? '正在探测系统状态…' : '尚未取得系统状态。' }}</p></section></template>
<style scoped>.panel { margin-block: 20px; } p { margin-block: 12px; overflow-wrap: anywhere; } h3 { margin-block: 20px 12px; } .sources { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 24px; } .sources > section { border-top: 1px solid var(--line); min-width: 0; } dd { overflow-wrap: anywhere; } @media(max-width: 900px) { .sources { grid-template-columns: minmax(0, 1fr); } }</style>