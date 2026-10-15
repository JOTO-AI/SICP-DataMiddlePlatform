<script setup lang="ts">
import { ref, computed, onMounted, onActivated, onDeactivated, onUnmounted } from 'vue'
import { request, ApiError, describe, type Envelope } from '../../api'
import { createJobOperation, submitOperation, type PendingOperation } from '../../api/operations'
import { readManagementJob, terminalManagementJob, notificationTestFact, type ManagementJob } from '../../app/managementJobs'
import { decimalString } from '../../app/notifications'
import type { NotificationSettings } from '../../app/notificationSettings'
import { loadPendingRecovery, savePendingRecovery, removePendingRecovery } from '../../storage/pending'
import { session } from '../../session'
import { useRequestScope } from '../../composables/useRequestScope'
import { usePageVisibility } from '../../composables/usePageVisibility'
import type { CloseReason } from '../../shared/dialog'
import AsyncActionDialog from '../AsyncActionDialog.vue'
import ErrorState from '../ErrorState.vue'
import StatusBadge from '../StatusBadge.vue'
import Timestamp from '../Timestamp.vue'
import CopyId from '../CopyId.vue'
import NotificationHistory from './NotificationHistory.vue'
import DeliveryDetail from './DeliveryDetail.vue'
const props = defineProps<{ settings: NotificationSettings | null; configured: boolean; blocked: boolean }>()
const context = { environment: window.location.origin + import.meta.env.BASE_URL, subject: session.operator }
const pending = ref<PendingOperation | null>(null), location = ref(''), job = ref<ManagementJob | null>(null), open = ref(false), sending = ref(false), loading = ref(false), error = ref(''), notice = ref(''), submitted = ref(false), refused = ref(false), snapshotRecipients = ref<string[]>([]), selected = ref('')
const submitScope = useRequestScope(), readScope = useRequestScope()
let active = true, timer: ReturnType<typeof setTimeout> | undefined, reads = 0
const visible = usePageVisibility(() => { readScope.cancel(); loading.value = false; clearTimeout(timer) }, () => { if (active && location.value && !terminalManagementJob(job.value)) void track() })
const unavailable = computed(() => props.blocked ? '请先保存或比较本地修改，再使用已保存配置测试。' : !props.settings ? '尚未取得已保存配置。' : !props.configured ? '邮件通道尚未配置。' : !props.settings.enabled || !props.settings.recipients.length ? '已保存配置未启用或没有收件人。' : '')
const revision = computed(() => pending.value ? String(JSON.parse(pending.value.payload).revision) : '')
function prepare() {
  if (pending.value || unavailable.value || !props.settings) return
  pending.value = createJobOperation('/admin/v1/notifications/test', { revision: props.settings.revision })
  snapshotRecipients.value = [...props.settings.recipients]; submitted.value = false; refused.value = false; error.value = ''; notice.value = ''; open.value = true
}
function close(reason: CloseReason) {
  if (reason === 'programmatic') return
  if (sending.value) { submitScope.cancel(); sending.value = false; error.value = '已停止等待，受理结果仍需确认；原版本和幂等键保留。' }
  if (!submitted.value) { pending.value = null; snapshotRecipients.value = [] }
}
async function submit() {
  if (!active || sending.value || !pending.value) return
  const operation = pending.value, run = submitScope.begin(); sending.value = true; submitted.value = true; refused.value = false; error.value = ''
  if (!savePendingRecovery(context, operation, location.value || undefined)) notice.value = '浏览器无法保存恢复凭据，请保留此页和幂等键。'
  try {
    const result = await submitOperation<ManagementJob>(operation, run.signal)
    if (!run.current()) return
    const value = readManagementJob(result.body.data, 'notification_test')
    if (!value) throw new ApiError('测试作业回执不符合契约，请保留原请求重试。', 'invalid_response')
    location.value = result.location; job.value = value
    savePendingRecovery(context, operation, location.value)
    notice.value = '测试作业已受理，不代表邮件已送达。'; open.value = false; reads = 0; void track()
  } catch (cause) {
    if (run.current()) {
      refused.value = cause instanceof ApiError && cause.status >= 400 && cause.status < 500 && !['invalid_response', 'cancelled', 'timeout'].includes(cause.code)
      error.value = describe(cause) + (refused.value ? ' 服务端拒绝本次受理，可结束此请求后重新核对配置。' : ' 受理结果未确认，请复用原请求与幂等键。')
    }
  } finally { if (run.current()) sending.value = false }
}
async function track(manual = false) {
  if (!active || !visible.value || loading.value || !location.value || !pending.value) return
  if (manual) reads = 0
  clearTimeout(timer); const run = readScope.begin(); loading.value = true; error.value = ''; reads++
  try {
    const response = await request<Envelope<unknown>>('worker', location.value, { signal: run.signal })
    if (!run.current()) return
    const value = readManagementJob(response.body.data, 'notification_test', location.value.split('/').at(-1))
    if (response.status !== 200 || response.body.code !== 'ok' || !value) throw new ApiError('测试作业状态与当前编号不一致。', 'invalid_response')
    job.value = value
    if (terminalManagementJob(value)) { removePendingRecovery(context, pending.value!); notice.value = '作业处理已结束；邮件是否被接受仍须查看逐人投递。' }
    else if (reads < 15) timer = setTimeout(() => void track(), 3000)
    else notice.value = '本轮已读取 15 次，停止自动等待；可手动刷新原作业，不会再次发起测试。'
  } catch (cause) { if (run.current()) error.value = describe(cause) + ' 保留原作业和最后确认状态，自动读取已停止。' }
  finally { if (run.current()) loading.value = false }
}
function finish() {
  if (!pending.value || sending.value || !refused.value && !terminalManagementJob(job.value)) return
  removePendingRecovery(context, pending.value); clearTimeout(timer); readScope.cancel(); pending.value = null; job.value = null; location.value = ''; selected.value = ''; error.value = ''; notice.value = ''; open.value = false; snapshotRecipients.value = []
}
function activate() {
  active = true
  if (pending.value) return
  const restored = loadPendingRecovery(context).find(r => r.operation.path === '/admin/v1/notifications/test')
  if (!restored) return
  const payload = JSON.parse(restored.operation.payload)
  if (!decimalString(payload.revision) || payload.revision === '0') return
  pending.value = restored.operation; location.value = restored.location ?? ''; submitted.value = true; notice.value = '已恢复原测试请求，仅恢复版本和幂等键，不会自动提交。'
  if (location.value) { reads = 0; void track() }
}
function leave() { active = false; clearTimeout(timer); readScope.cancel(); submitScope.cancel(); loading.value = false; sending.value = false; pending.value = null; job.value = null; location.value = ''; snapshotRecipients.value = []; selected.value = ''; open.value = false }
onMounted(activate); onActivated(activate); onDeactivated(leave); onUnmounted(leave)
</script>
<template><section aria-label="测试邮件" class="mail-test"><h3>测试邮件</h3><p class="hint">只使用已保存且允许的配置版本和地址；不会提交临时收件人。更改配置可能使尚未处理的测试失效。</p><button v-if="!pending" type="button" :disabled="!!unavailable" @click="prepare">发送测试邮件</button><p v-if="!pending && unavailable" class="hint">{{ unavailable }}</p><template v-if="pending"><p>锁定配置版本 {{ revision }} · 幂等键 <CopyId :value="pending.key" label="测试邮件幂等键" /></p><button v-if="!location" :disabled="sending" @click="open = true">原键重试测试请求</button><button v-if="location" :disabled="loading || sending" @click="track(true)">刷新测试作业</button><button v-if="refused || terminalManagementJob(job)" :disabled="sending" @click="finish">结束此测试请求</button></template><ErrorState v-if="error" :message="error" /><p v-if="notice" role="status">{{ notice }}</p><template v-if="job"><p>测试作业：<CopyId :value="job.id" label="测试邮件作业号" /> <StatusBadge :value="job.status" /></p><p>受理：<Timestamp :value="job.accepted_at" /> · 结束：<Timestamp :value="job.finished_at" empty="尚未确认" /></p><p v-if="job.summary.decision" class="hint">作业决策：{{ job.summary.decision === 'queued' ? '已创建待发送投递' : job.summary.decision === 'settings_changed' ? '配置已变化，未创建本次投递' : '请查看服务端作业记录' }}</p><NotificationHistory v-if="notificationTestFact(job)" kind="deliveries" :fact-id="notificationTestFact(job)!" @select="selected = $event" /><DeliveryDetail v-if="selected" :key="selected" :id="selected" /></template><AsyncActionDialog v-model:open="open" title="确认发送测试邮件" description="这是独立发送操作，服务端会重新核对已保存版本。" :busy="sending" confirm-label="确认发送测试邮件" :error="error" @confirm="submit" @close="close"><p>配置版本 {{ revision }}</p><p v-if="snapshotRecipients.length">已保存收件人：{{ snapshotRecipients.join('，') }}</p><p v-else>恢复请求未缓存邮箱地址，实际收件人以该版本后台记录为准。</p><p>重复等待或原键重试只确认同一作业，不新建测试请求。</p></AsyncActionDialog></section></template>
<style scoped>.mail-test { margin-top: 24px; padding-top: 20px; border-top: 1px solid var(--line); min-width: 0; } p { margin-block: 12px; overflow-wrap: anywhere; } button { margin-right: 8px; }</style>