<script setup lang="ts">
import { ref, watch, onDeactivated } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import { catalogName } from '../app/catalog'
import { session } from '../session'
import Dialog from './Dialog.vue'
import RequestEditor from './RequestEditor.vue'
import EmptyState from './EmptyState.vue'
const props = defineProps<{ api: CatalogAPI; initialText?: string; needsSnapshot?: boolean }>()
const emit = defineEmits<{ close: [] }>()
// Mounted fresh per open; this context never follows a later catalog response.
const api = { ...props.api }, epoch = session.epoch
let closed = false
const open = ref(true), expanded = ref(false), text = ref(props.initialText ?? '{}')
function close() { if (closed) return; closed = true; open.value = false; text.value = ''; emit('close') }
watch([() => props.api.id, () => props.api.version, () => session.epoch], () => {
  if (props.api.id !== api.id || props.api.version !== api.version || session.epoch !== epoch) close()
})
onDeactivated(close)
</script>
<template>
  <Dialog v-model:open="open" :title="`${catalogName(api)} · 在线测试`" wide :expanded="expanded" description="打开弹窗或载入案例不会发送请求。当前可编辑参数，执行能力尚未开放。" @close="close">
    <div class="test-context"><div><code>{{ api.method }} {{ api.path }}</code><p class="hint">接口 <code>{{ api.id }}</code> · 目录版本 <code>{{ api.version }}</code></p></div><button type="button" :aria-pressed="expanded" @click="expanded = !expanded">{{ expanded ? '恢复窗口' : '展开窗口' }}</button></div>
    <section aria-label="测试请求参数"><h3>请求参数</h3><p v-if="needsSnapshot" class="warning">续页案例需要同一接口首响应的查询快照，当前草稿未携带真实快照。</p><RequestEditor v-model="text" /></section>
    <section class="test-result" aria-label="测试结果"><h3>测试结果</h3><EmptyState title="尚未测试" description="这里将展示本次实际请求的响应；文档案例不会作为测试结果。" /></section>
    <template #footer><button type="button" @click="close">关闭测试</button></template>
  </Dialog>
</template>
<style scoped>
.test-context { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; flex-wrap: wrap; margin-bottom: 24px; }
.test-context > div { min-width: 0; flex: 1 1 260px; }
.test-context code { overflow-wrap: anywhere; }
.test-result { border-top: 1px solid var(--line); margin-top: 24px; padding-top: 24px; }
@media (max-width: 767px) { .test-context > button { display: none; } }
</style>
