<script setup lang="ts">
import AsyncActionDialog from '../components/AsyncActionDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import UpdateHistory from '../components/updates/UpdateHistory.vue'
import { useRouter } from 'vue-router'
import { validUpdateID } from '../app/updateEvents'
import { actionAllows, actionExplanation, readActionReasons } from '../app/updateActions'
import { computed, ref, onActivated, onDeactivated, onUnmounted } from 'vue'
import { request, describe, ApiError, type Envelope } from '../api'
import { pollingDelay } from '../app/polling'
import { usePageVisibility } from '../composables/usePageVisibility'
import { useRequestScope } from '../composables/useRequestScope'
import { newIdempotencyKey } from '../idempotency'
import { session } from '../session'
import { loadPendingRecovery, savePendingRecovery, removePendingRecovery } from '../storage/pending'
import { createTaskOperation, submitOperation, type PendingOperation } from '../api/operations'

import { sourceTables } from '../acquisition'
import { sourceName } from '../app/sourceNames'
interface Task { task_id: string; mode: string; status: string }
const router = useRouter()
const action = ref('force-full'), predecessor = ref(''), key = ref(newIdempotencyKey()), lookup = ref('')
interface Overview { items: Task[]; initialized: boolean; next_due_at?: string; cooldown_until?: string; interval_seconds: number }
const overview = ref<Overview | null>(null), historyError = ref(''), historyBusy = ref(false)
const hasActiveTask = computed(() => overview.value?.items.some(item => !['failed', 'success_published', 'success_unchanged'].includes(item.status)) ?? false)
const admissionBusy = ref(false)
const currentActiveTask = computed(() => overview.value?.items.find(item => !['failed', 'success_published', 'success_unchanged'].includes(item.status)))
const actionReasons = ref<Record<string, string> | null>(null), checking = ref(false)
const rerunAllowed = computed(() => !checking.value && !busy.value && !pending.value && !!overview.value && !historyBusy.value && !historyError.value && !hasActiveTask.value && actionAllows(actionReasons.value, 'rerun'))
const acceptedLocation = ref<{ id: string; path: string } | null>(null)
const selectedTable = ref('dwd_bzkscjzlb')
const tableUpdateAllowed = computed(() => !checking.value && !!overview.value && (overview.value.initialized || !!failedInitialization.value) && !historyBusy.value && !historyError.value && !hasActiveTask.value && !busy.value && !pending.value && actionAllows(actionReasons.value, 'table_force_full'))
const failedInitialization = computed(() => {
  const latest = overview.value?.items[0]
  return !overview.value?.initialized && latest?.mode === 'initialize' && latest.status === 'failed' ? latest : null
})
const primaryLabel = computed(() => failedInitialization.value ? '重跑失败初始化' : '强制全量更新')
const forceFullBlocked = computed(() => checking.value || !actionAllows(actionReasons.value, failedInitialization.value ? 'rerun' : 'force_full') || historyBusy.value || !overview.value || !!historyError.value || hasActiveTask.value || (!overview.value.initialized && !failedInitialization.value))
const incrementalBlocked = computed(() => checking.value || !actionAllows(actionReasons.value, 'incremental') || historyBusy.value || !overview.value || !!historyError.value || hasActiveTask.value || !overview.value.initialized)
const incrementalHelp = computed(() => {
  if (historyBusy.value || !overview.value) return '正在读取初始化和任务状态。'
  if (historyError.value) return '无法确认当前任务状态，请先刷新记录。'
  if (!overview.value.initialized) return '初始化完成后可手动增量同步；当前可先补齐失败表。'
  if (hasActiveTask.value) return '已有任务正在执行或等待恢复，结束后可启动增量同步。'
  if (!actionAllows(actionReasons.value, 'incremental')) return actionExplanation(actionReasons.value, 'incremental')
  return '从各表已确认进度立即采集增量；有变化且校验通过后自动发布，无变化则确认同步进度。'
})
const forceFullHelp = computed(() => {
  if (historyBusy.value) return '正在读取最新任务状态。'
  if (historyError.value) return '无法确认当前任务状态，请先刷新记录。'
  if (!overview.value) return '读取初始化状态后可判断是否允许强制更新。'
  if (hasActiveTask.value) return '已有更新任务正在执行或等待恢复，完成后才能再次启动。'
  if (failedInitialization.value && !actionAllows(actionReasons.value, 'rerun')) return actionExplanation(actionReasons.value, 'rerun')
  if (failedInitialization.value) return '初始化未完成，可重新采集待补齐的表，或选择单表强制更新；已完成的表会保留复用，上游冷却期间任务会等待。'
  if (!overview.value.initialized) return '等待系统创建初始化任务；任务失败后可在此重跑失败初始化。'
  if (!actionAllows(actionReasons.value, 'force_full')) return actionExplanation(actionReasons.value, 'force_full')
  return '强制全量更新会立即请求重新采集、校验并发布完整数据。'
})
const actions = computed(() => [{ id: 'incremental', title: '增量同步', text: '立即同步所有表的增量。有变化且校验通过后自动发布，无变化则确认同步进度；失败时保留当前版本和原同步进度。' }, { id: 'force-full', title: failedInitialization.value ? '重跑失败初始化' : '强制全量', text: failedInitialization.value ? '复用已经完整采集的表，重新采集待补齐的表。' : '维护操作：重新采集并构建完整数据，即使内容未变化。' }, { id: 'table-full', title: '单表强制更新', text: '只从上游完整采集所选表，其余表复用最近已完成的数据。初始化尚缺其他表时，保留本次结果，等待继续补齐。' }, { id: 'rerun', title: '失败重跑', text: '排除故障后，根据失败任务创建关联的新任务。' }])
let historyFailures = 0, historyRetryAfter: string | undefined
const pageVisible = usePageVisibility(() => { historyRequests.cancel(); historyBusy.value = false; clearTimeout(historyTimer) }, () => { if (active) void refreshHistory() })
let historyTimer: ReturnType<typeof setTimeout> | undefined
const admissionRequests = useRequestScope(), historyRequests = useRequestScope(), submitRequests = useRequestScope()
async function refreshHistory() {
  if (!active || !pageVisible.value || historyBusy.value) return
  clearTimeout(historyTimer); historyBusy.value = true; historyError.value = ''
  const run = historyRequests.begin()
  try {
    const [tasksResponse, actionsResponse] = await Promise.all([
      request<Envelope<Overview>>('worker', '/admin/v1/tasks/', { signal: run.signal }),
      request<Envelope<unknown>>('worker', '/admin/v1/overview', { signal: run.signal }),
    ])
    const result = tasksResponse.body.data, reasons = readActionReasons(actionsResponse.body.data)
    if (tasksResponse.status !== 200 || tasksResponse.body.code !== 'ok' || !result || !Array.isArray(result.items) || result.items.some(item => !item || typeof item.task_id !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(item.task_id) || typeof item.mode !== 'string' || typeof item.status !== 'string') || typeof result.initialized !== 'boolean' || actionsResponse.status !== 200 || actionsResponse.body.code !== 'ok' || !reasons) throw new ApiError('无法确认任务及操作条件，请重新读取。', 'invalid_response')
    if (!active || !run.current()) return
    actionReasons.value = reasons; overview.value = result; historyFailures = 0; historyRetryAfter = undefined
  }
  catch (e) { if (active && run.current()) { historyError.value = describe(e); historyFailures++; historyRetryAfter = e instanceof ApiError ? e.headers['Retry-After'] : undefined } }
  finally { if (run.current()) { historyBusy.value = false; if (active && pageVisible.value) historyTimer = setTimeout(refreshHistory, pollingDelay(15000, historyFailures, historyRetryAfter)) } }
}
function openTask() {
  const id = lookup.value.trim()
  if (!validUpdateID(id)) { lookupError.value = '请输入有效的完整任务号。'; return }
  lookupError.value = ''; void router.push({ name: 'update-detail', params: { id } })
}
const busy = ref(false), error = ref(''), receipt = ref(''), lookupError = ref('')
const dialogOpen = ref(false)
const selected = computed(() => actions.value.find(a => a.id === action.value)!)
// Preserve the exact operation and key until a receipt is obtained or explicitly abandoned.
const recoveryContext = { environment: window.location.origin + import.meta.env.BASE_URL, subject: session.operator }
const recovered = loadPendingRecovery(recoveryContext).find(entry => entry.operation.kind === 'task')
const recoveryAvailable = ref(true)
const pending = ref<(PendingOperation & { title: string; description: string }) | null>(recovered ? { ...recovered.operation, title: '原更新操作', description: `恢复待确认请求：${recovered.operation.path}。将使用原操作与幂等键核对受理结果。` } : null)
if (recovered) { key.value = recovered.operation.key; error.value = '已恢复待确认操作，请核对后重试原请求；尚未自动提交。' }
let active = true
onActivated(() => { active = true; void refreshHistory() })
function deactivate() { active = false; admissionRequests.cancel(); checking.value = false; if (busy.value) error.value = '已停止等待，原操作与幂等键已保留；受理结果可能未知。'; submitRequests.cancel(); busy.value = false; dialogOpen.value = false; historyRequests.cancel(); historyBusy.value = false; clearTimeout(historyTimer) }
onDeactivated(deactivate)
onUnmounted(deactivate)
async function confirm() {
  if (checking.value || busy.value) return
  error.value = ''
  if (!pending.value) {
    if (action.value === 'incremental' && incrementalBlocked.value) { error.value = incrementalHelp.value; return }
    if (action.value === 'force-full' && forceFullBlocked.value) { error.value = forceFullHelp.value; return }
    if (action.value === 'table-full' && (!tableUpdateAllowed.value || !sourceTables.includes(selectedTable.value))) { error.value = '请核对来源表与初始化状态，并等待当前任务结束。'; return }
    if (action.value === 'rerun' && !rerunAllowed.value) { error.value = '当前任务或发布状态不允许失败重跑，请刷新记录并核对操作条件。'; return }
    if (action.value === 'rerun' && !predecessor.value.trim()) { error.value = '请输入原失败任务号。'; return }
    if (!/^[\x21-\x7e]{1,128}$/.test(key.value) || key.value.includes(',')) { error.value = '幂等键须为 1–128 位可见 ASCII 字符，且不含逗号。'; return }
    const retryInitial = action.value === 'force-full' ? failedInitialization.value : null
    const rerunID = retryInitial?.task_id || (action.value === 'rerun' ? predecessor.value.trim() : '')
    if (rerunID && !/^[A-Za-z0-9_-]+$/.test(rerunID)) { error.value = '原任务号格式无效。'; return }
    if (rerunID && !overview.value?.items.some(item => item.task_id === rerunID && item.status === 'failed')) {
      const run = admissionRequests.begin(), originalAction = action.value, originalKey = key.value
      checking.value = true
      try {
        const response = await request<Envelope<Task>>('worker', `/admin/v1/tasks/${encodeURIComponent(rerunID)}`, { signal: run.signal })
        if (!active || !run.current() || action.value !== originalAction || key.value !== originalKey || predecessor.value.trim() !== rerunID) return
        if (response.status !== 200 || response.body.code !== 'ok' || response.body.data?.task_id !== rerunID || response.body.data.status !== 'failed') { error.value = '原任务尚未确认为失败，不能提交失败重跑。'; return }
      } catch (cause) { if (active && run.current()) error.value = describe(cause); return }
      finally { if (run.current()) checking.value = false }
      if (!rerunAllowed.value) { error.value = '操作条件已变化，请刷新记录后重新确认。'; return }
    }
    pending.value = {
      ...createTaskOperation(action.value === 'table-full' ? `/admin/v1/tables/${encodeURIComponent(selectedTable.value)}/force-full` : rerunID ? `/admin/v1/tasks/${encodeURIComponent(rerunID)}/rerun` : `/admin/v1/tasks/${action.value}`, key.value),
      title: selected.value.title,
      description: action.value === 'table-full' ? `将完整重采 ${selectedTable.value}，其它表复用最近完成的数据。若初始化仍缺其它表，本表成功后会保留结果，等待补齐；完整数据校验通过后统一发布。` : retryInitial ? `将复用已完成的表并重采待补齐的表，保留原失败任务 ${retryInitial.task_id} 的记录。无需重启服务。` : rerunID ? `将根据原失败任务 ${rerunID} 创建关联的新任务，原事件与失败证据保留；完整执行和发布结果须另行追踪。` : `范围：全部来源表。${selected.value.text}`,
    }
  }
  dialogOpen.value = true
}
function forceFull() { action.value = 'force-full'; confirm() }
function syncIncremental() { action.value = 'incremental'; confirm() }
function abandon() { if (pending.value) removePendingRecovery(recoveryContext, pending.value); pending.value = null; key.value = newIdempotencyKey(); error.value = ''; receipt.value = ''; admissionBusy.value = false }
async function submit() {
  if (!active || busy.value || !pending.value) return
  const run = submitRequests.begin()
  const submission = { ...pending.value }
  recoveryAvailable.value = savePendingRecovery(recoveryContext, submission)
  dialogOpen.value = false; busy.value = true; error.value = ''; receipt.value = ''; admissionBusy.value = false
  try {
    const response = await submitOperation<Task>(submission, run.signal)
    if (!run.current()) return
    lookup.value = response.body.data.task_id
    acceptedLocation.value = { id: lookup.value, path: response.location }
    receipt.value = `任务已受理：${lookup.value}。受理不代表执行完成。`
    removePendingRecovery(recoveryContext, submission)
    pending.value = null; key.value = newIdempotencyKey()
    if (run.current()) await refreshHistory()
  } catch (e) { if (!run.current()) return; admissionBusy.value = e instanceof ApiError && e.code === 'busy'; error.value = `${describe(e)} 原请求已保留；再次提交将使用相同操作与幂等键。` }
  finally { if (run.current()) busy.value = false }
}
</script>
<template>
  <section>
    <PageHeader title="更新事件" description="一次更新作为一个事件，查看执行结果、阶段与调用。"><div class="update-primary"><button class="primary" @click="forceFull" :disabled="forceFullBlocked || busy || !!pending" aria-describedby="force-full-help">{{ primaryLabel }}</button><p id="force-full-help" class="hint">{{ forceFullHelp }}</p></div></PageHeader>
    <UpdateHistory />
    <div class="section-bar next-heading"><h2>维护与详情</h2><span class="muted">日常运行无需手动操作</span></div>
    <div class="task-layout">
      <form class="panel maintenance-form" aria-label="人工维护" :aria-busy="busy" @submit.prevent="confirm">
        <div class="section-bar"><h3>人工维护</h3><button type="button" @click="refreshHistory" :disabled="historyBusy">刷新记录</button></div><p v-if="checking" role="status">正在核对原任务是否允许重跑…</p><p class="muted">仅在需要完整重建或排除故障后使用。</p>
        <div class="maintenance-sync"><button type="button" @click="syncIncremental" :disabled="incrementalBlocked || busy || !!pending" aria-describedby="incremental-help">立即增量同步</button><p id="incremental-help" class="hint">{{ incrementalHelp }}</p></div><p v-if="historyError" class="error" role="alert">{{ historyError }}</p>
        <fieldset :disabled="checking || busy || !!pending"><legend>更新方式</legend><label v-for="item in actions" :key="item.id" class="radio-row"><input v-model="action" type="radio" :value="item.id" name="action"><span><strong>{{ item.title }}</strong><small>{{ item.text }}</small></span></label>
        <label v-if="action === 'rerun'">原失败任务号 *<input v-model="predecessor" required placeholder="输入失败任务号"></label>
        <label v-if="action === 'table-full'">来源表<select v-model="selectedTable"><option v-for="name in sourceTables" :key="name" :value="name">{{ sourceName(name) }} · {{ name }}</option></select></label>
        <label class="key-label">幂等键<input v-model="key" required maxlength="128" spellcheck="false"></label></fieldset>
        <p class="hint" aria-label="后端操作条件">{{ actionExplanation(actionReasons, action === 'force-full' ? (failedInitialization ? 'rerun' : 'force_full') : action === 'table-full' ? 'table_force_full' : action === 'rerun' ? 'rerun' : 'incremental') }}</p><p v-if="action === 'table-full' && !tableUpdateAllowed" class="hint">当前不可执行单表更新：请先确认初始化已完成，或存在允许补齐的失败初始化任务，并等待当前任务和请求结束。</p><p class="hint">响应丢失时用原键重试，可避免重复创建任务。</p>
        <p v-if="pending" class="hint">已锁定请求：{{ pending.title }}。修改操作前请先结束本次请求。</p>
        <p v-if="pending && !recoveryAvailable" class="hint">当前浏览器无法保存恢复信息，刷新页面会丢失本次待确认请求。</p>
        <div class="actions"><button class="primary" :disabled="checking || busy || (!pending && ((action === 'incremental' && incrementalBlocked) || (action === 'force-full' && forceFullBlocked) || (action === 'table-full' && !tableUpdateAllowed) || (action === 'rerun' && !rerunAllowed)))">{{ busy ? '正在提交…' : pending ? '重试原请求' : '提交更新' }}</button><button v-if="pending" type="button" :disabled="busy" @click="abandon">结束本次请求</button></div>
        <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="receipt" class="success-text" role="status">{{ receipt }}</p><p v-if="receipt && acceptedLocation"><RouterLink :to="{ name: 'update-detail', params: { id: acceptedLocation.id } }">查看已受理事件详情</RouterLink></p><div v-if="admissionBusy" class="hint"><p>后端正在处理更新或发布，原请求仍保留。先读取当前任务，再决定是否重试原请求。</p><button type="button" :disabled="historyBusy" @click="refreshHistory">读取当前任务</button><RouterLink v-if="currentActiveTask && !historyError" :to="{ name: 'update-detail', params: { id: currentActiveTask.task_id } }">查看当前任务详情</RouterLink></div>
      </form>
      <section class="panel task-lookup" aria-label="查找任务详情">
        <form class="lookup" @submit.prevent="openTask"><label for="task-id">查找任务</label><div class="actions"><input id="task-id" v-model="lookup" required maxlength="128" placeholder="输入完整任务号" @input="lookupError = '';"><button>查看任务</button></div></form>
        <p class="hint">在任务详情中查看执行阶段、采集进度、失败原因与关联日志。</p><p v-if="lookupError" class="error" role="alert">{{ lookupError }}</p>
      </section>
    </div>
    <AsyncActionDialog v-model:open="dialogOpen" :title="`确认${pending?.title ?? ''}`" :description="pending?.description" confirm-label="确认提交更新" :busy="busy" @confirm="submit"><p class="hint">幂等键：{{ pending?.key }}</p></AsyncActionDialog>
  </section>
</template>

<style scoped>
.update-primary { display: flex; flex-direction: column; align-items: flex-end; gap: 8px; max-width: 380px; }
.update-primary .hint { text-align: right; }
.maintenance-sync { margin-bottom: 20px; }
.maintenance-sync .hint { margin-top: 8px; }
.maintenance-form .section-bar { margin-bottom: 0; }
.task-layout { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; }
.task-lookup .hint { margin: 12px 0 0; }
@media (max-width: 900px) { .task-layout { grid-template-columns: minmax(0, 1fr); } .update-primary { max-width: none; align-items: flex-start; } .update-primary .hint { text-align: left; } }
</style>
