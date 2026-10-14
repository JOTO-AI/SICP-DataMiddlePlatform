<script setup lang="ts">
import { computed, ref, watch, onDeactivated } from 'vue'
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
const props = defineProps<{ api: CatalogAPI; initialText?: string; needsSnapshot?: boolean }>()
const emit = defineEmits<{ close: [] }>()
// Mounted fresh per open; this context never follows a later catalog response.
const api = { ...props.api }, epoch = session.epoch
let closed = false
const open = ref(true), expanded = ref(false), text = ref(editableExample(api, props.initialText ?? '{}'))
const editor = ref<InstanceType<typeof RequestEditor> | null>(null), validation = ref(''), selectedExample = ref('minimal')
const parameters = testParameters(api), examples = requestExamples(api), needsSnapshot = ref(!!props.needsSnapshot)
const validate = (value: string) => validateTestRequest(api, value)
function check() { validation.value = typeof editor.value?.getRequestText() === 'string' ? '参数校验通过；尚未发送请求。' : '' }
function reset() { validation.value = ''; needsSnapshot.value = false; editor.value?.loadText('{}') }
function load() { const example = examples.find(item => item.id === selectedExample.value); if (!example) return; validation.value = ''; needsSnapshot.value = !!example.snapshot; editor.value?.loadText(editableExample(api, JSON.stringify(example.body, null, 2))) }
function updateText(value: string) { validation.value = ''; executionError.value = ''; text.value = value; markStale() }
const requests = useRequestScope(), busy = ref(false), result = ref<BusinessTestResult | null>(null), executionError = ref(''), resultStale = ref(false)
const response = computed(() => result.value?.response ?? result.value?.error)
const responseCode = computed(() => { try { const body = JSON.parse(response.value?.responseText ?? ''); return typeof body.code === 'string' ? body.code : undefined } catch { return undefined } })
function markStale() { if (result.value) resultStale.value = true }
async function execute() {
  if (busy.value || closed || api.kind !== 'business') return
  const draft = editor.value?.getRequestText()
  if (draft == null) return
  if (needsSnapshot.value) { executionError.value = '续页案例尚未绑定首响应快照，请先从第一页查询。'; return }
  const run = requests.begin(); busy.value = true; result.value = null; resultStale.value = false; executionError.value = ''; validation.value = ''
  try { const value = await executeBusinessTest(api, draft, run.signal); if (run.current() && !closed) result.value = value }
  catch (cause) { if (run.current() && !closed) executionError.value = cause instanceof ApiError ? describe(cause) : '参数不符合当前接口约定。' }
  finally { if (run.current()) busy.value = false }
}
function close() { if (closed) return; closed = true; requests.cancel(); busy.value = false; result.value = null; open.value = false; text.value = ''; emit('close') }
watch([() => props.api.id, () => props.api.version, () => session.epoch], () => {
  if (props.api.id !== api.id || props.api.version !== api.version || session.epoch !== epoch) close()
})
onDeactivated(close)
</script>
<template>
  <Dialog v-model:open="open" :title="`${catalogName(api)} · 在线测试`" wide :expanded="expanded" :description="api.kind === 'business' ? '打开或填入案例不会执行。点击执行测试后读取本地业务服务；关闭会停止等待响应。' : '打开或填入案例不会执行。源测试作业执行尚未开放。'" @close="close">
    <div class="test-context"><div><code>{{ api.method }} {{ api.path }}</code><p class="hint">接口 <code>{{ api.id }}</code> · 目录版本 <code>{{ api.version }}</code></p></div><button type="button" :aria-pressed="expanded" @click="expanded = !expanded">{{ expanded ? '恢复窗口' : '展开窗口' }}</button></div>
    <section aria-label="测试请求参数"><h3>请求参数</h3><p v-if="needsSnapshot" class="warning">续页案例需要同一接口首响应的查询快照，当前草稿未携带真实快照。</p><div class="test-examples"><label>填入案例<select v-model="selectedExample" :disabled="busy"><option v-for="example in examples" :key="example.id" :value="example.id">{{ example.title }}</option></select></label><button type="button" :disabled="busy" @click="load">填入所选案例</button><button type="button" :disabled="busy" @click="reset">重置参数</button></div><p class="hint">填入案例或重置会替换当前草稿，不发送请求。标识占位符请替换为实际值；实体是否存在、数据组合及权限仍由服务端确认。</p><p v-if="api.kind === 'source'" class="hint">仅编辑 page/page_size；api_id 和 version 固定为上方接口。省略、null 或 0 使用服务端默认分页；幂等键由执行链管理。</p><RequestEditor ref="editor" :model-value="text" @update:model-value="updateText" @edited="markStale" :parameters="parameters" :validate-text="validate" :disabled="busy" /><button type="button" :disabled="busy" @click="check">校验参数</button><p v-if="validation" role="status">{{ validation }}</p></section>
    <section class="test-result" aria-label="测试结果"><h3>测试结果</h3><ErrorState v-if="executionError" :message="executionError" />
      <EmptyState v-if="!result" :loading="busy" title="尚未测试" :description="busy ? '等待本次实际查询响应，不会自动重试。' : '这里将展示本次实际请求的响应；文档案例不会作为测试结果。'" />
      <template v-else><p class="hint">本次开始于 <Timestamp :value="result.startedAt" /> · 浏览器等待 {{ result.durationMs }} ms</p><ErrorState v-if="result.error" :message="describe(result.error)" />
        <p v-if="!response?.status" class="hint">未取得 HTTP 响应。</p>
        <ResponseViewer v-if="response" :text="response.responseText ?? '未取得响应正文。'" :http-status="response.status || undefined" :code="responseCode" :request-id="response.requestID" label="响应原文" :stale="resultStale" />
        <details><summary>实际响应头</summary><p class="hint">仅展示允许的诊断响应头；未返回的字段不补造。</p><JsonText :text="JSON.stringify(response?.headers ?? {}, null, 2)" label="响应头" /></details>
      </template></section>
    <template #footer><button v-if="api.kind === 'business'" type="button" class="primary" :disabled="busy" @click="execute">{{ busy ? '等待响应…' : '执行测试' }}</button><button type="button" @click="close">{{ busy ? '停止等待并关闭' : '关闭测试' }}</button></template>
  </Dialog>
</template>
<style scoped>
.test-context { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; flex-wrap: wrap; margin-bottom: 24px; }
.test-context > div { min-width: 0; flex: 1 1 260px; }
.test-context code { overflow-wrap: anywhere; }
.test-examples { display: flex; flex-wrap: wrap; gap: 12px; align-items: end; margin: 16px 0; }
.test-result { border-top: 1px solid var(--line); margin-top: 24px; padding-top: 24px; }
@media (max-width: 767px) { .test-context > button { display: none; } }
</style>
