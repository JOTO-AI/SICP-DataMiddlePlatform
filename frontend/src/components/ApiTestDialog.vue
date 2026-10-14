<script setup lang="ts">
import { computed, ref, reactive, watch, onDeactivated } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import { catalogName } from '../app/catalog'
import { session } from '../session'
import Dialog from './Dialog.vue'
import RequestEditor from './RequestEditor.vue'
import EmptyState from './EmptyState.vue'
import { testParameters, validateTestRequest, editableExample } from '../app/testRequest'
import { requestExamples } from '../app/requestExamples'
import { executeBusinessTest, type BusinessTestResult } from '../app/businessTest'
import { useRequestScope } from '../composables/useRequestScope'
import { ApiError, describe } from '../api'
import ResponseViewer from './ResponseViewer.vue'
import JsonText from './JsonText.vue'
import ErrorState from './ErrorState.vue'
import Timestamp from './Timestamp.vue'
import { useSourceTest } from '../composables/useSourceTest'
import CopyId from './CopyId.vue'
import StatusBadge from './StatusBadge.vue'
import { businessOutcome, sourceOutcome, errorOutcome } from '../app/testOutcome'
const props = defineProps<{ api: CatalogAPI; initialText?: string; needsSnapshot?: boolean }>()
const emit = defineEmits<{ close: [] }>()
// Mounted fresh per open; this context never follows a later catalog response.
const api = { ...props.api }, epoch = session.epoch
let closed = false
const open = ref(true), expanded = ref(false), text = ref(editableExample(api, props.initialText ?? '{}'))
const editor = ref<InstanceType<typeof RequestEditor> | null>(null), validation = ref(''), validationError = ref(''), selectedExample = ref('minimal')
const parameters = testParameters(api), examples = requestExamples(api), needsSnapshot = ref(!!props.needsSnapshot)
const source = reactive(useSourceTest(api))
if (source.operation) { const saved = JSON.parse(source.operation.payload); text.value = JSON.stringify({ page: saved.page, page_size: saved.page_size }, null, 2) }
const sourceContext = computed(() => source.operation ? JSON.parse(source.operation.payload) as { api_id: string; version: string; page: number; page_size: number } : null)
const locked = computed(() => busy.value || source.locked)
const sourceStatus: Record<string, string> = { queued: '已受理，排队中', running: '执行中', succeeded: '作业执行成功', failed: '作业执行失败', interrupted: '执行中断', partial_failed: '部分失败' }
async function executeSource() {
  if (closed || source.busy) return
  const draft = source.operation ? undefined : editor.value?.getRequestText()
  if (!source.operation && draft == null) return
  validation.value = ''; await source.execute(draft ?? undefined)
}
const validate = (value: string) => validateTestRequest(api, value)
function check() { validation.value = typeof editor.value?.getRequestText() === 'string' ? '参数校验通过；尚未发送请求。' : '' }
function reset() { validation.value = ''; needsSnapshot.value = false; editor.value?.loadText('{}') }
function load() { const example = examples.find(item => item.id === selectedExample.value); if (!example) return; validation.value = ''; needsSnapshot.value = !!example.snapshot; editor.value?.loadText(editableExample(api, JSON.stringify(example.body, null, 2))) }
function updateText(value: string) { validation.value = ''; executionError.value = ''; text.value = value; markStale() }
const requests = useRequestScope(), busy = ref(false), result = ref<BusinessTestResult | null>(null), executionError = ref(''), preflightError = ref<ApiError | null>(null), resultStale = ref(false)
const outcome = computed(() => api.kind === 'business'
  ? preflightError.value ? errorOutcome(preflightError.value) : businessOutcome({ busy: busy.value, validationError: validationError.value || executionError.value, result: result.value })
  : sourceOutcome({ busy: source.busy, validationError: validationError.value, pending: !!source.operation, job: source.job, response: source.response, error: source.error, waitingStopped: source.waitingStopped }))
const response = computed(() => result.value?.response ?? result.value?.error)
const responseCode = computed(() => { try { const body = JSON.parse(response.value?.responseText ?? ''); return typeof body.code === 'string' ? body.code : undefined } catch { return undefined } })
function markStale() { validationError.value = ''; preflightError.value = null; executionError.value = ''; if (result.value) resultStale.value = true }
async function execute() {
  if (busy.value || closed || api.kind !== 'business') return
  const draft = editor.value?.getRequestText()
  if (draft == null) return
  if (needsSnapshot.value) { executionError.value = '续页案例尚未绑定首响应快照，请先从第一页查询。'; return }
  const run = requests.begin(); busy.value = true; result.value = null; resultStale.value = false; preflightError.value = null; executionError.value = ''; validation.value = ''
  try { const value = await executeBusinessTest(api, draft, run.signal); if (run.current() && !closed) result.value = value }
  catch (cause) { if (run.current() && !closed) { preflightError.value = cause instanceof ApiError ? cause : null; executionError.value = cause instanceof ApiError ? describe(cause) : '参数不符合当前接口约定。' } }
  finally { if (run.current()) busy.value = false }
}
function close() { if (closed) return; closed = true; requests.cancel(); source.stop(); busy.value = false; result.value = null; open.value = false; text.value = ''; emit('close') }
watch([() => props.api.id, () => props.api.version, () => session.epoch], () => {
  if (props.api.id !== api.id || props.api.version !== api.version || session.epoch !== epoch) close()
})
onDeactivated(close)
</script>
<template>
  <Dialog v-model:open="open" :title="`${catalogName(api)} · 在线测试`" wide :expanded="expanded" :description="api.kind === 'business' ? '打开或填入案例不会执行。点击执行测试后读取本地业务服务；关闭会停止等待响应。' : '源测试只读取指定的一页，不推进同步进度。关闭只停止等待，不取消后台作业；重新打开不会自动提交。'" @close="close">
    <div class="test-context"><div><code>{{ api.method }} {{ api.path }}</code><p class="hint">接口 <code>{{ api.id }}</code> · 目录版本 <code>{{ api.version }}</code></p></div><button type="button" :aria-pressed="expanded" @click="expanded = !expanded">{{ expanded ? '恢复窗口' : '展开窗口' }}</button></div>
    <section aria-label="测试请求参数"><h3>请求参数</h3><p v-if="needsSnapshot" class="warning">续页案例需要同一接口首响应的查询快照，当前草稿未携带真实快照。</p><div class="test-examples"><label>填入案例<select v-model="selectedExample" :disabled="locked"><option v-for="example in examples" :key="example.id" :value="example.id">{{ example.title }}</option></select></label><button type="button" :disabled="locked" @click="load">填入所选案例</button><button type="button" :disabled="locked" @click="reset">重置参数</button></div><p class="hint">填入案例或重置会替换当前草稿，不发送请求。标识占位符请替换为实际值；实体是否存在、数据组合及权限仍由服务端确认。</p><p v-if="api.kind === 'source'" class="hint">仅编辑 page/page_size；新测试使用上方接口与版本，恢复操作使用下方保存的固定参数。省略、null 或 0 使用服务端默认分页；幂等键由执行链管理。</p><RequestEditor ref="editor" :model-value="text" @update:model-value="updateText" @edited="markStale" @validation-error="validationError = $event" :parameters="parameters" :validate-text="validate" :disabled="locked" /><button type="button" :disabled="locked" @click="check">校验参数</button><p v-if="validation" role="status">{{ validation }}</p></section>
    <section class="test-result" aria-label="测试结果"><h3>测试结果</h3><div class="test-outcome" role="status" aria-label="测试状态" :data-state="outcome.id"><StatusBadge :label="outcome.title" :tone="outcome.tone" /><p>{{ outcome.description }}</p></div><ErrorState v-if="executionError" :message="executionError" />
      <EmptyState v-if="api.kind === 'business' && !result" :loading="busy" title="尚未测试" :description="busy ? '等待本次实际查询响应，不会自动重试。' : '这里将展示本次实际请求的响应；文档案例不会作为测试结果。'" />
      <template v-else-if="api.kind === 'business' && result"><p class="hint">本次开始于 <Timestamp :value="result.startedAt" /> · 浏览器等待 {{ result.durationMs }} ms</p><ErrorState v-if="result.error" :message="describe(result.error)" />
        <p v-if="!response?.status" class="hint">未取得 HTTP 响应。</p>
        <ResponseViewer v-if="response" :text="response.responseText ?? '未取得响应正文。'" :http-status="response.status || undefined" :code="responseCode" :request-id="response.requestID" label="响应原文" :stale="resultStale" />
        <details><summary>实际响应头</summary><p class="hint">仅展示允许的诊断响应头；未返回的字段不补造。</p><JsonText :text="JSON.stringify(response?.headers ?? {}, null, 2)" label="响应头" /></details>
      </template>
      <template v-if="api.kind === 'source'"><ErrorState v-if="source.error" :message="describe(source.error)" />
        <EmptyState v-if="!source.operation" title="尚未测试" description="点击执行源测试后提交一页读取作业；案例不是实际结果。" />
        <template v-else>
          <p class="hint">本次固定参数：{{ sourceContext?.api_id }} · {{ sourceContext?.version }} · 第 {{ sourceContext?.page }} 页 · 每页 {{ sourceContext?.page_size }} 条。确认本次结束前不修改参数。</p>
          <CopyId :value="source.operation.key" label="幂等键" />
          <CopyId v-if="source.job" :value="source.job.id" label="作业编号" />
          <p v-if="source.location" class="hint">作业位置：<code>{{ source.location }}</code></p>
          <p role="status">{{ source.job ? sourceStatus[source.job.status] : source.busy ? '正在提交，等待受理回执…' : source.rejected ? '服务端拒绝受理' : '受理结果待确认' }}</p>
          <p v-if="!source.location && !source.rejected" class="hint">尚无已确认作业位置。重试仅使用此处保存的原参数与原幂等键，不自动再次提交。</p>
          <p v-if="source.job && !source.terminal" class="hint">运行中尚无结果不代表失败；当前仅查询作业，不自动补页。</p>
          <p v-if="source.waitingStopped" class="warning">本轮等待已达上限，作业状态待确认；可继续查询原作业，不会提交新测试。</p>
          <p v-if="!source.recoveryAvailable" class="warning">浏览器无法保存恢复信息，请保留幂等键与作业位置，避免重复提交。</p>

          <template v-if="source.response">
            <p class="hint">以下 HTTP 状态与响应头属于管理作业查询，不是上游响应。</p>
            <ResponseViewer :text="source.response.responseText ?? ''" :http-status="source.response.status" :code="source.response.body.code" :request-id="source.response.requestID" label="管理作业响应原文" :stale="!!source.error || source.busy" />
            <template v-if="source.job?.status === 'succeeded' && source.response.body.data.result_state === 'available'"><h4>本页源数据</h4><p class="hint">后端返回的 total/list 结果，保留原始数字；不是上游完整 HTTP 报文。</p><JsonText :text="source.response.resultText ?? ''" label="本页源数据 JSON" /></template>
            <p v-else-if="source.terminal && source.job?.status === 'succeeded'" class="warning">作业已成功，但本次查询未取得结果。结果可能尚未进入缓存或已过期、淘汰、服务重启、落在其他实例；可查询原作业，不会自动重测。</p>
            <p v-else-if="source.terminal" class="warning">作业已结束，未取得可展示的源数据，请查看作业摘要。</p>
            <details><summary>管理响应头</summary><JsonText :text="JSON.stringify(source.response.headers, null, 2)" label="管理响应头 JSON" /></details>
          </template>
          <details v-if="source.error?.responseText"><summary>本次管理错误响应</summary><ResponseViewer :text="source.error.responseText" :http-status="source.error.status || undefined" :request-id="source.error.requestID" label="管理错误响应原文" /></details>
          <button v-if="source.terminal || source.rejected" type="button" :disabled="source.busy" @click="source.reset">结束本次，准备新测试</button>
        </template>
      </template></section>
    <template #footer><button v-if="api.kind === 'business'" type="button" class="primary" :disabled="locked" @click="execute">{{ busy ? '等待响应…' : '执行测试' }}</button><button v-if="api.kind === 'source'" type="button" class="primary" :disabled="source.busy" @click="executeSource">{{ source.busy ? '等待作业…' : source.location ? '查询原作业' : source.operation ? '原键重试受理' : '执行源测试' }}</button><button type="button" @click="close">{{ busy || source.busy ? '停止等待并关闭' : '关闭测试' }}</button></template>
  </Dialog>
</template>
<style scoped>
.test-context { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; flex-wrap: wrap; margin-bottom: 24px; }
.test-context > div { min-width: 0; flex: 1 1 260px; }
.test-context code { overflow-wrap: anywhere; }
.test-examples { display: flex; flex-wrap: wrap; gap: 12px; align-items: end; margin: 16px 0; }
.test-outcome { margin-block: 16px; }
.test-result { border-top: 1px solid var(--line); margin-top: 24px; padding-top: 24px; }
@media (max-width: 767px) { .test-context > button { display: none; } }
</style>
