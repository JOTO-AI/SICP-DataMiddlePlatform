<script setup lang="ts">
import { ref, onActivated, onDeactivated } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { emptyLogFilters, type LogSource } from '../app/logHistory'
import { prepareTestLogs } from '../app/testLogNavigation'
import { request, describe, ApiError, type Envelope, type Result } from '../api'
import { updateDetailPath, readUpdateDetail, updateModes, updateTimes, validUpdateID, type UpdateDetail } from '../app/updateEvents'
import { useRequestScope } from '../composables/useRequestScope'
import { updateMeaning } from '../app/updateMeaning'
import OperationTree from '../components/updates/OperationTree.vue'
import TaskDiagnostics from '../components/updates/TaskDiagnostics.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import ErrorState from '../components/ErrorState.vue'
import Timestamp from '../components/Timestamp.vue'
import CopyId from '../components/CopyId.vue'
import StatusBadge from '../components/StatusBadge.vue'
import ResponseViewer from '../components/ResponseViewer.vue'
const id = String(useRoute().params.id), scope = useRequestScope()
const task = ref<UpdateDetail | null>(null), response = ref<Result<Envelope<unknown>> | null>(null), busy = ref(false), error = ref(''), readAt = ref('')
const router = useRouter()
async function openLogs(source: LogSource) {
  if (task.value && prepareTestLogs({ ...emptyLogFilters(source), identityField: 'task_id', identity: task.value.task_id, format: 'raw' })) await router.push({ name: 'log-source', params: { source } })
}
let active = false
async function read() {
  if (!active || busy.value) return
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const value = await request<Envelope<unknown>>('worker', updateDetailPath(id), { signal: run.signal, captureResponse: true })
    if (!active || !run.current()) return
    const detail = readUpdateDetail(value.body.data, id)
    if (value.status !== 200 || value.body.code !== 'ok' || !detail) throw new ApiError('更新事件详情与当前事件标识或契约不一致。', 'invalid_response')
    task.value = detail; response.value = value; readAt.value = new Date().toISOString()
  } catch (cause) { if (active && run.current()) error.value = cause instanceof ApiError && cause.status === 404 ? '未取得该更新事件：可能不存在或已超出可查询范围；不能据此认定从未执行。' : describe(cause) }
  finally { if (run.current()) busy.value = false }
}
onActivated(() => { active = true; if (!task.value) void read() })
onDeactivated(() => { active = false; scope.cancel(); busy.value = false })
</script>
<template><section><RouterLink to="/updates">← 返回更新事件</RouterLink><PageHeader title="更新详情" description="控制区任务事实与诊断信息分别展示。打开详情不会提交更新操作。"><button type="button" :disabled="busy" @click="read">刷新事件详情</button></PageHeader><ErrorState v-if="error" :message="error" /><p v-if="error && task" class="warning">当前读取失败，保留上次已确认详情，不代表最新状态。</p><template v-if="task"><section class="panel" aria-label="更新事件事实"><div class="section-bar"><h2>{{ updateModes[task.mode] || task.mode }}</h2><StatusBadge :value="task.status" /></div><p class="hint">{{ updateMeaning(task.status, task.mode) }} <RouterLink to="/overview">查看当前数据组合</RouterLink></p><p>更新事件号：<CopyId :value="id" label="更新事件号" /></p><p>关联任务号：<CopyId :value="task.task_id" label="任务号" /></p><p class="hint">当前后端以任务号稳定映射更新事件号；它不是日志 event_id。读取于 <Timestamp :value="readAt" />，此页不会自动重排或刷新。</p><dl class="facts"><div><dt>发起来源</dt><dd>{{ task.operator === 'scheduler' ? '系统调度' : task.operator || '未提供' }}</dd></div><div><dt>进度确认</dt><dd>{{ task.progress_confirmed === true ? '已确认' : task.progress_confirmed === false ? '尚未确认' : '未提供' }}</dd></div><div v-for="[key, title] in updateTimes" :key="key"><dt>{{ title }}</dt><dd><Timestamp :value="task[key]" empty="尚未确认" /></dd></div></dl><p v-if="task.predecessor_id">前序任务：<RouterLink v-if="validUpdateID(task.predecessor_id)" :to="{ name: 'update-detail', params: { id: task.predecessor_id } }">{{ task.predecessor_id }}</RouterLink></p><p v-if="task.failure" class="warning">失败记录：{{ task.failure }}</p><p v-if="task.diagnostics_unavailable" class="warning">诊断日志暂不可获取，以上控制区任务事实仍保留。</p><p class="hint">执行器恢复沿用此任务和事件；主动重跑创建新事件，并通过前序任务号关联。恢复会话和每次尝试在操作树中分别保留。</p><p v-if="task.mode === 'initialize' && task.status === 'failed'" class="warning">初始化失败可按既有规则重跑失败任务或补齐单表，已完成数据由后端决定是否复用。<RouterLink to="/updates">前往更新操作核对条件</RouterLink>；打开页面不会自动提交。</p><p class="hint">未发生或未返回的阶段不补时间，缺少日志不覆盖任务状态。</p></section><nav aria-label="更新事件日志"><button @click="openLogs('platform')">查看本地更新日志</button> <button @click="openLogs('upstream')">查看上游采集日志</button></nav><TaskDiagnostics :task="task" read-only /><OperationTree :task-id="id" /><details v-if="response" class="panel"><summary>实际任务响应</summary><ResponseViewer :text="response.responseText ?? ''" :http-status="response.status" :request-id="response.requestID" :stale="!!error" label="更新事件响应原文" /></details></template><EmptyState v-else :loading="busy" title="尚未取得更新事件详情" description="可重新读取或返回更新事件列表。" /></section></template>
<style scoped>
section > .page-heading { margin-top: 16px; } .panel { margin-block: 20px; overflow-wrap: anywhere; } .panel > p { margin-block: 12px; }
</style>