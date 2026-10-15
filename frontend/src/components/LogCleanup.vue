<script setup lang="ts">
import { ref, computed, watch, onMounted, onActivated, onDeactivated, onUnmounted } from 'vue'
import { request, ApiError, describe, type Envelope } from '../api'
import { createJobOperation, submitOperation, type PendingOperation } from '../api/operations'
import { readCleanupPreview, previewReason, cleanupResult, type CleanupPreview } from '../app/cleanup'
import { readManagementJob, terminalManagementJob, type ManagementJob } from '../app/managementJobs'
import { loadPendingRecovery, savePendingRecovery, removePendingRecovery } from '../storage/pending'
import { session } from '../session'
import { useRequestScope } from '../composables/useRequestScope'
import { usePageVisibility } from '../composables/usePageVisibility'
import type { CloseReason } from '../shared/dialog'
import AsyncActionDialog from './AsyncActionDialog.vue'
import ErrorState from './ErrorState.vue'
import Timestamp from './Timestamp.vue'
import StatusBadge from './StatusBadge.vue'
import CopyId from './CopyId.vue'
const props = defineProps<{ revision: string }>()
const source = ref<'platform' | 'upstream'>('platform'), preview = ref<CleanupPreview | null>(null), pending = ref<PendingOperation | null>(null), location = ref(''), job = ref<ManagementJob | null>(null), busy = ref(false), sending = ref(false), loading = ref(false), open = ref(false), submitted = ref(false), rejected = ref(false), error = ref(''), notice = ref(''), now = ref(Date.now())
const scope = useRequestScope(), submitScope = useRequestScope(), readScope = useRequestScope(), context = { environment: window.location.origin + import.meta.env.BASE_URL, subject: session.operator }
const rawJob = ref('')
let active = true, clock: ReturnType<typeof setInterval> | undefined, timer: ReturnType<typeof setTimeout> | undefined, reads = 0
const visible = usePageVisibility(() => { readScope.cancel(); loading.value = false; clearTimeout(timer); clearInterval(clock) }, () => { if (!active) return; startClock(); if (location.value && !terminalManagementJob(job.value)) void read() })
const result = computed(() => job.value ? cleanupResult(job.value, rawJob.value || undefined, preview.value?.source) : null)
function startClock() { clearInterval(clock); now.value = Date.now(); if (visible.value) clock = setInterval(() => { now.value = Date.now() }, 1000) }
const invalid = computed(() => previewReason(preview.value, props.revision, now.value)), sourceLabel = computed(() => preview.value ? preview.value.source === 'platform' ? '平台日志' : '上游日志' : '来源待作业回执确认')
watch(source, () => { scope.cancel(); busy.value = false; preview.value = null; error.value = ''; notice.value = '' })
async function inspect() {
  if (!active || busy.value || pending.value) return
  const run = scope.begin(), selected = source.value; busy.value = true; preview.value = null; error.value = ''; notice.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', '/admin/v1/maintenance/cleanup/preview', { method: 'POST', body: { source: selected }, signal: run.signal })
    if (!run.current()) return
    const value = readCleanupPreview(response.body.data, selected)
    if (response.status !== 200 || response.body.code !== 'ok' || !value) throw new ApiError('清理预览与所选来源不一致或字段无效。', 'invalid_response')
    preview.value = value; now.value = Date.now()
  } catch (cause) { if (run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
function prepare() {
  now.value = Date.now()
  if (pending.value || invalid.value || !preview.value || preview.value.count === '0') return
  pending.value = createJobOperation('/admin/v1/maintenance/cleanup', { preview_id: preview.value.id, revision: preview.value.revision }); submitted.value = false; rejected.value = false; open.value = true; error.value = ''
}
function close(reason: CloseReason) {
  if (reason === 'programmatic') return
  if (sending.value) { submitScope.cancel(); sending.value = false; error.value = '已停止等待，不能认定清理未执行；请保留原键核对。' }
  if (!submitted.value) pending.value = null
}
async function submit() {
  if (!active || sending.value || !pending.value) return
  now.value = Date.now()
  if (!submitted.value && invalid.value) { error.value = invalid.value; return }
  const operation = pending.value, run = submitScope.begin(); sending.value = true; submitted.value = true; error.value = ''; rejected.value = false
  if (!savePendingRecovery(context, operation)) notice.value = '浏览器无法保存恢复凭据，请保留此页和幂等键。'
  try {
    const response = await submitOperation<ManagementJob>(operation, run.signal)
    if (!run.current()) return
    const value = readManagementJob(response.body.data, 'cleanup')
    if (!value) throw new ApiError('清理回执不符合契约，请保留原请求核对。', 'invalid_response')
    job.value = value; location.value = response.location; savePendingRecovery(context, operation, location.value); open.value = false; notice.value = '清理作业已受理，实际删除结果尚需确认。'; reads = 0; void read()
  } catch (cause) {
    if (run.current()) { rejected.value = cause instanceof ApiError && cause.status >= 400 && cause.status < 500 && cause.code !== 'invalid_response'; error.value = describe(cause) + (rejected.value ? ' 本次受理被拒绝；结束此请求后重新预览，不能沿用过期或旧策略预览。' : ' 受理未确认，请使用原请求和原键核对。') }
  } finally { if (run.current()) sending.value = false }
}
async function read(manual = false) {
  if (!active || !visible.value || loading.value || !pending.value || !location.value) return
  if (manual) reads = 0
  clearTimeout(timer); const run = readScope.begin(); loading.value = true; error.value = ''; reads++
  try {
    const response = await request<Envelope<unknown>>('worker', location.value, { signal: run.signal, captureResponse: true })
    if (!run.current()) return
    const value = readManagementJob(response.body.data, 'cleanup', location.value.split('/').at(-1))
    if (response.status !== 200 || response.body.code !== 'ok' || !value) throw new ApiError('清理作业响应与当前编号不一致。', 'invalid_response')
    job.value = value; rawJob.value = response.responseText ?? ''
    if (terminalManagementJob(value)) removePendingRecovery(context, pending.value!)
    else if (reads < 15) timer = setTimeout(() => void read(), 3000)
    else notice.value = '本轮已读取 15 次，请手动刷新原作业；不会再次提交清理。'
  } catch (cause) { if (run.current()) error.value = describe(cause) + ' 保留原作业和最后确认状态。' }
  finally { if (run.current()) loading.value = false }
}
function finish() { if (!pending.value || sending.value || !rejected.value && !terminalManagementJob(job.value)) return; removePendingRecovery(context, pending.value); readScope.cancel(); clearTimeout(timer); rawJob.value = ''; loading.value = false; pending.value = null; preview.value = null; location.value = ''; job.value = null; open.value = false; error.value = ''; notice.value = '' }
function activate() {
  active = true; startClock()
  if (pending.value) return
  const recovery = loadPendingRecovery(context).find(item => item.operation.path === '/admin/v1/maintenance/cleanup')
  if (recovery) { pending.value = recovery.operation; location.value = recovery.location ?? ''; submitted.value = true; notice.value = '已恢复原清理请求，不会自动提交；原来源与数量以后台作业结果为准。'; reads = 0; if (location.value) void read() }
}
function leave() { active = false; clearInterval(clock); clearTimeout(timer); rawJob.value = ''; scope.cancel(); submitScope.cancel(); readScope.cancel(); preview.value = null; pending.value = null; job.value = null; location.value = ''; busy.value = false; sending.value = false; loading.value = false; open.value = false; error.value = ''; notice.value = '' }
onMounted(activate); onActivated(activate); onDeactivated(leave); onUnmounted(leave)
</script>
<template><section class="panel" aria-label="日志清理"><div class="section-bar"><h2>日志清理</h2></div><p class="hint">每次只处理一个日志源，两个源分别执行，不承诺同时成功。仅清理过保留期且满足保护条件的日志，不处理业务数据。</p><label>清理日志源<select v-model="source" :disabled="busy || !!pending"><option value="platform">平台日志</option><option value="upstream">上游日志</option></select></label><button :disabled="busy || !!pending" @click="inspect">预览所选源</button><ErrorState v-if="error" :message="error" /><p v-if="notice" role="status">{{ notice }}</p><section v-if="preview" aria-label="清理预览"><h3>{{ sourceLabel }}预览</h3><dl class="facts"><div><dt>可清理数量</dt><dd>{{ preview.count }}</dd></div><div><dt>序号范围（下界不含，上界包含）</dt><dd>{{ preview.floor }} → {{ preview.through }}</dd></div><div><dt>截止时间</dt><dd><Timestamp :value="preview.cutoff" /></dd></div><div><dt>有效期至</dt><dd><Timestamp :value="preview.expires_at" /></dd></div><div><dt>策略版本</dt><dd>{{ preview.revision }}</dd></div><div><dt>存储世代</dt><dd>{{ preview.epoch }}</dd></div></dl><p class="hint">保护策略：保留仍被持久对象引用的日志，只有引用满足退役条件才可清理；预览数量不是已删除数量。</p><p v-if="invalid" class="warning">{{ invalid }}</p><p v-if="preview.count === '0'">本次没有可清理记录，无需提交。</p><button v-if="!pending" :disabled="!!invalid || preview.count === '0'" @click="prepare">确认清理此源</button></section><template v-if="pending"><p>原请求幂等键：<CopyId :value="pending.key" label="清理幂等键" /></p><button v-if="!location" :disabled="sending" @click="open = true">原键核对清理请求</button><button v-if="location" :disabled="loading" @click="read(true)">刷新清理作业</button><button v-if="rejected || terminalManagementJob(job)" :disabled="sending" @click="finish">结束此请求并重新预览</button></template><section v-if="job" aria-label="清理作业"><h3>清理作业</h3><p><CopyId :value="job.id" label="清理作业号" /> <StatusBadge :value="job.status" /></p><p>受理时间：<Timestamp :value="job.accepted_at" /> · 结束时间：<Timestamp :value="job.finished_at" empty="尚未确认" /></p><template v-if="result"><p>结果来源：{{ result.source === 'platform' ? '平台日志' : '上游日志' }}</p><p>实际确认删除：{{ result.deleted }} 条</p><p>确认回执：<CopyId :value="result.receipt" label="清理确认回执" /></p></template><p v-else class="warning">实际删除数量：未确认。排队、运行中或结果待核对时，不以零条代替；请继续查询原作业。</p><p class="hint">此结果仅属于该日志源，另一日志源需要独立预览和作业。服务端终态与删除确认分别核对。</p></section><AsyncActionDialog v-model:open="open" title="确认日志清理" description="清理不可撤回。服务端会重新核对预览期限和保留策略。" :busy="sending" :can-submit="submitted || !invalid" :error="error" confirm-label="提交此源清理" @confirm="submit" @close="close"><p>来源：{{ sourceLabel }}</p><p v-if="preview">预览 {{ preview.id }} · 策略版本 {{ preview.revision }} · 预计 {{ preview.count }} 条</p><p v-else>恢复请求不缓存预览内容；本次只核对原 preview_id 和原幂等键，不新建清理范围。</p></AsyncActionDialog></section></template>
<style scoped>.panel { margin-block: 20px; } p { margin-block: 12px; overflow-wrap: anywhere; } select { margin: 8px 12px 12px 0; } button { margin-right: 8px; } dd { overflow-wrap: anywhere; } h3 { margin-block: 20px 12px; }</style>