<script setup lang="ts">
import { ref, computed, nextTick, onActivated, onDeactivated, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { emptyLogFilters, type LogSource } from '../app/logHistory'
import { prepareTestLogs } from '../app/testLogNavigation'
import { request, describe, ApiError, type Envelope, type Result } from '../api'
import { updateDetailPath, readUpdateDetail, updateModes, updateTimes, validUpdateID, type UpdateDetail } from '../app/updateEvents'
import { usePageVisibility } from '../composables/usePageVisibility'
import { useRequestScope } from '../composables/useRequestScope'
import { sourceName } from '../app/sourceNames'
import { updateMeaning } from '../app/updateMeaning'
import OperationTree from '../components/updates/OperationTree.vue'
import TaskDiagnostics from '../components/updates/TaskDiagnostics.vue'
import DetailTabs from '../components/DetailTabs.vue'
import UpdateStages from '../components/updates/UpdateStages.vue'
import EmptyState from '../components/EmptyState.vue'
import ErrorState from '../components/ErrorState.vue'
import Timestamp from '../components/Timestamp.vue'
import CopyId from '../components/CopyId.vue'
import StatusBadge from '../components/StatusBadge.vue'
import LazyResponse from '../components/LazyResponse.vue'
const id = String(useRoute().params.id), scope = useRequestScope()
const task = ref<UpdateDetail | null>(null), response = ref<Result<Envelope<unknown>> | null>(null), busy = ref(false), error = ref(''), readAt = ref('')
const router = useRouter(), tab = ref(0), tree = ref<InstanceType<typeof OperationTree> | null>(null)
const tabs = ['操作树', '转移与发布', '关联日志', '异常与通知']
async function showGroup(group: string) { tab.value = 0; await nextTick(); await tree.value?.showGroup(group) }
async function openLogs(source: LogSource) {
  if (task.value && prepareTestLogs({ ...emptyLogFilters(source), identityField: 'task_id', identity: task.value.task_id, format: 'raw' })) await router.push({ name: 'log-source', params: { source } })
}
let active = false, timer: ReturnType<typeof setTimeout> | undefined
const auto = ref(false)
const terminal = computed(() => !!task.value?.finished_at || ['success_published', 'success_unchanged', 'failed'].includes(task.value?.status || ''))
function pause() { clearTimeout(timer); scope.cancel(); busy.value = false }
function stop() { auto.value = false; clearTimeout(timer) }
function toggleAuto() { if (auto.value) void read(); else pause() }
const visible = usePageVisibility(pause, () => { if (active && auto.value && !terminal.value) void read() })
async function read() {
  if (!active || !visible.value || busy.value) return
  clearTimeout(timer)
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const value = await request<Envelope<unknown>>('worker', updateDetailPath(id), { signal: run.signal, captureResponse: true })
    if (!active || !run.current()) return
    const detail = readUpdateDetail(value.body.data, id)
    if (value.status !== 200 || value.body.code !== 'ok' || !detail) throw new ApiError('更新事件详情与当前事件标识或契约不一致。', 'invalid_response')
    task.value = detail; response.value = value; readAt.value = new Date().toISOString(); if (terminal.value) stop()
  } catch (cause) { if (active && run.current()) { stop(); error.value = cause instanceof ApiError && cause.status === 404 ? '未取得该更新事件：可能不存在或已超出可查询范围；不能据此认定从未执行。' : describe(cause) + ' 自动刷新已停止。' } }
  finally { if (run.current()) { busy.value = false; if (active && visible.value && auto.value && !terminal.value) timer = setTimeout(read, 5000) } }
}
onActivated(() => { active = true; if (!task.value || auto.value && !terminal.value) void read() })
function deactivate() { active = false; pause() }
onDeactivated(deactivate); onUnmounted(deactivate)
</script>
<template>
<section class="update-detail-page">
  <RouterLink class="back-link" to="/updates">← 返回更新事件</RouterLink>
  <header class="event-heading" :role="task ? 'region' : undefined" :aria-label="task ? '更新事件事实' : undefined">
    <div><div class="title-line"><h1>更新详情</h1><StatusBadge v-if="task" :value="task.status" /></div><p class="event-id">事件号：<CopyId :value="id" label="更新事件号" /></p><p v-if="task" class="hint">任务号：<CopyId :value="task.task_id" label="任务号" /></p><p v-if="task?.selected_table" class="hint">本次强制更新表{{ sourceName(task.selected_table) }} · {{ task.selected_table }}</p></div>
    <dl v-if="task" class="header-times"><div><dt>受理时间</dt><dd><Timestamp :value="task.accepted_at" /></dd></div><div><dt>开始时间</dt><dd><Timestamp :value="task.started_at" empty="尚未确认" /></dd></div><div><dt>发布时间</dt><dd><Timestamp :value="task.published_at" empty="尚未确认" /></dd></div></dl>
  </header>
  <div class="detail-tools"><p class="hint" v-if="task">{{ updateModes[task.mode] || task.mode }} · <span>{{ task.operator === 'scheduler' ? '系统调度' : task.operator || '来源未提供' }}</span> · 读取于 <Timestamp :value="readAt" /></p><div class="actions"><label v-if="task" class="check-row"><input type="checkbox" v-model="auto" :disabled="terminal || busy" @change="toggleAuto">每 5 秒刷新，任务结束或查询失败后停止</label><button type="button" :disabled="busy" @click="read">刷新事件详情</button></div></div>
  <ErrorState v-if="error" :message="error" /><p v-if="error && task" class="warning">当前读取失败，保留上次已确认详情，不代表最新状态。</p>
  <template v-if="task">
    <UpdateStages :task="task" />
    <section class="detail-surface">
      <DetailTabs v-model="tab" :labels="tabs" id="update-details" />
      <section v-show="tab === 0" role="tabpanel" id="update-details-panel-0" aria-labelledby="update-details-tab-0">
        <OperationTree ref="tree" :task-id="id">
          <TaskDiagnostics :task="task" read-only mode="progress" />
        </OperationTree>
      </section>
      <section v-show="tab === 1" class="tab-content" role="tabpanel" id="update-details-panel-1" aria-labelledby="update-details-tab-1">
        <h2>转移与发布</h2><p class="hint">{{ updateMeaning(task.status, task.mode) }}</p>
        <dl class="facts"><div v-for="[key, title] in updateTimes" :key="key"><dt>{{ title }}</dt><dd><Timestamp :value="task[key]" empty="尚未确认" /></dd></div><div><dt>进度确认</dt><dd>{{ task.progress_confirmed === true ? '已确认' : task.progress_confirmed === false ? '尚未确认' : '未提供' }}</dd></div></dl>
        <p v-if="task.predecessor_id">前序任务：<RouterLink v-if="validUpdateID(task.predecessor_id)" :to="{ name: 'update-detail', params: { id: task.predecessor_id } }">{{ task.predecessor_id }}</RouterLink></p>
        <div class="actions"><button @click="showGroup('builds')">查看构建目标</button><button @click="showGroup('publication')">查看转移与发布记录</button><RouterLink to="/overview">查看当前数据组合</RouterLink></div>
      </section>
      <section v-show="tab === 2" class="tab-content" role="tabpanel" id="update-details-panel-2" aria-labelledby="update-details-tab-2">
        <h2>关联日志</h2><p class="hint">按本任务号查询本地更新过程和上游采集记录。</p><div class="actions"><button @click="openLogs('platform')">查看本地更新日志</button><button @click="openLogs('upstream')">查看上游采集日志</button></div>
      </section>
      <section v-show="tab === 3" class="tab-content" role="tabpanel" id="update-details-panel-3" aria-labelledby="update-details-tab-3">
        <h2>异常与通知</h2><p v-if="task.failure" class="warning">失败记录：{{ task.failure }}</p><p v-else class="hint">当前任务未返回失败原因。</p>
        <TaskDiagnostics :task="task" read-only mode="failures" />
        <p class="hint">通知投递状态请在通知中心查询；未返回关联记录不代表已发送。</p><RouterLink to="/notifications">查看通知记录</RouterLink>
      </section>
    </section>
    <LazyResponse v-if="response" class="panel task-response" summary="实际任务响应" :text="response.responseText" :http-status="response.status" :request-id="response.requestID" :stale="!!error" label="更新事件响应原文" />
  </template>
  <EmptyState v-else :loading="busy" title="尚未取得更新事件详情" description="可重新读取或返回更新事件列表。" />
</section>
</template>
<style scoped>
.back-link { display: inline-block; margin-bottom: 16px; font-size: 12px; }
.event-heading { display: flex; justify-content: space-between; align-items: center; gap: 24px; } .event-heading > div { min-width: 0; }
.title-line { display: flex; gap: 16px; align-items: center; } .event-id { margin-block: 8px 4px; font-size: 12px; overflow-wrap: anywhere; }
.header-times { display: flex; gap: 24px; margin: 0; font-size: 12px; } .header-times > div { border-inline-start: 1px solid var(--line); padding-inline-start: 20px; } .header-times dd { text-align: left; color: var(--ink); margin-top: 6px; }
.detail-tools { display: flex; justify-content: space-between; align-items: center; gap: 16px; margin-top: 16px; } .detail-tools .actions { flex-wrap: wrap; justify-content: flex-end; }
.detail-surface { background: var(--white); border: 1px solid var(--line); border-radius: var(--radius-panel); overflow: hidden; }
.tab-content { padding: 24px; } .tab-content > p, .tab-content > .actions { margin-top: 16px; } .tab-content .actions { flex-wrap: wrap; }
.task-response { margin-top: 20px; }
@media(max-width: 1100px) { .event-heading { flex-direction: column; align-items: stretch; } .header-times { justify-content: space-between; } .header-times > div:first-child { border: 0; padding: 0; } }
@media(max-width: 600px) { .header-times { flex-wrap: wrap; gap: 12px; } .header-times > div { border: 0; padding: 0; } .detail-tools { flex-direction: column; align-items: flex-start; } .detail-tools .actions { justify-content: flex-start; } .tab-content { padding: 16px; } }
</style>
