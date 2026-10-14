<script setup lang="ts">
import { ref, watch, onDeactivated } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import { catalogName } from '../app/catalog'
import { session } from '../session'
import Dialog from './Dialog.vue'
import RequestEditor from './RequestEditor.vue'
import EmptyState from './EmptyState.vue'
import { testParameters, validateTestRequest, editableExample } from '../app/testRequest'
import { requestExamples } from '../app/requestExamples'
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
function updateText(value: string) { validation.value = ''; text.value = value }
function close() { if (closed) return; closed = true; open.value = false; text.value = ''; emit('close') }
watch([() => props.api.id, () => props.api.version, () => session.epoch], () => {
  if (props.api.id !== api.id || props.api.version !== api.version || session.epoch !== epoch) close()
})
onDeactivated(close)
</script>
<template>
  <Dialog v-model:open="open" :title="`${catalogName(api)} · 在线测试`" wide :expanded="expanded" description="打开弹窗或载入案例不会发送请求。当前可编辑参数，执行能力尚未开放。" @close="close">
    <div class="test-context"><div><code>{{ api.method }} {{ api.path }}</code><p class="hint">接口 <code>{{ api.id }}</code> · 目录版本 <code>{{ api.version }}</code></p></div><button type="button" :aria-pressed="expanded" @click="expanded = !expanded">{{ expanded ? '恢复窗口' : '展开窗口' }}</button></div>
    <section aria-label="测试请求参数"><h3>请求参数</h3><p v-if="needsSnapshot" class="warning">续页案例需要同一接口首响应的查询快照，当前草稿未携带真实快照。</p><div class="test-examples"><label>填入案例<select v-model="selectedExample"><option v-for="example in examples" :key="example.id" :value="example.id">{{ example.title }}</option></select></label><button type="button" @click="load">填入所选案例</button><button type="button" @click="reset">重置参数</button></div><p class="hint">填入案例或重置会替换当前草稿，不发送请求。标识占位符请替换为实际值；实体是否存在、数据组合及权限仍由服务端确认。</p><p v-if="api.kind === 'source'" class="hint">仅编辑 page/page_size；api_id 和 version 固定为上方接口。省略、null 或 0 使用服务端默认分页；幂等键由执行链管理。</p><RequestEditor ref="editor" :model-value="text" @update:model-value="updateText" :parameters="parameters" :validate-text="validate" /><button type="button" @click="check">校验参数</button><p v-if="validation" role="status">{{ validation }}</p></section>
    <section class="test-result" aria-label="测试结果"><h3>测试结果</h3><EmptyState title="尚未测试" description="这里将展示本次实际请求的响应；文档案例不会作为测试结果。" /></section>
    <template #footer><button type="button" @click="close">关闭测试</button></template>
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
