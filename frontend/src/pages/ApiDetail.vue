<script setup lang="ts">
import { ref, computed, onActivated, onDeactivated, onUnmounted, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { request, describe, ApiError, type Envelope } from '../api'
import { catalogName, readCatalogAPI, type CatalogAPI } from '../app/catalog'
import { useRequestScope } from '../composables/useRequestScope'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import ErrorState from '../components/ErrorState.vue'
import EmptyState from '../components/EmptyState.vue'
import Timestamp from '../components/Timestamp.vue'
import JsonText from '../components/JsonText.vue'
import ExampleViewer from '../components/ExampleViewer.vue'
const apiId = String(useRoute().params.apiId)
const api = ref<CatalogAPI | null>(null), busy = ref(false), error = ref(''), readAt = ref('')
const ready = ref<boolean | null>(null), healthError = ref(''), healthReadAt = ref('')
const tabs = ['概览', '参数', '请求案例', '响应案例', '调用记录']
const tab = ref(0), tabButtons = ref<HTMLButtonElement[]>([])
const requests = useRequestScope()
let active = false
const healthLabel = computed(() => healthError.value ? '查询服务状态未确认' : ready.value === true ? '查询服务已就绪' : ready.value === false ? '查询服务尚未就绪' : '查询服务状态未观测')
const json = (value: unknown) => value === undefined ? '后端未提供。' : JSON.stringify(value, null, 2)
async function refresh() {
  if (!active || busy.value) return
  busy.value = true
  const run = requests.begin()
  try {
    const response = await request<Envelope<unknown>>('worker', `/admin/v1/apis/${encodeURIComponent(apiId)}`, { signal: run.signal })
    if (!active || !run.current()) return
    const item = readCatalogAPI(response.body.data, apiId)
    if (!item) throw new ApiError('接口详情标识或格式与当前页面不一致。', 'invalid_response')
    api.value = item; error.value = ''; readAt.value = new Date().toISOString()
    if (item.kind === 'business') {
      try {
        const result = await request<{ ready?: boolean }>('query', '/readyz', { health: true, signal: run.signal })
        if (!active || !run.current()) return
        ready.value = typeof result.body.ready === 'boolean' ? result.status === 200 && result.body.ready : null
        healthReadAt.value = new Date().toISOString(); healthError.value = ''
      } catch (cause) { if (active && run.current()) healthError.value = describe(cause) }
    } else { ready.value = null; healthError.value = ''; healthReadAt.value = '' }
  } catch (cause) { if (active && run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
async function keyTab(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  tab.value = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (tab.value + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
  await nextTick(); tabButtons.value[tab.value]?.focus()
}
onActivated(() => { active = true; void refresh() })
function stop() { active = false; requests.cancel(); busy.value = false }
onDeactivated(stop)
onUnmounted(stop)
</script>
<template>
  <section class="api-detail">
    <RouterLink to="/apis">← 接口目录</RouterLink>
    <PageHeader :title="api ? catalogName(api) : '接口详情'" :description="api?.purpose"><button :disabled="busy" @click="refresh">{{ busy ? '读取中…' : '刷新详情' }}</button></PageHeader>
    <ErrorState v-if="error" :message="error" retryable :busy="busy" @retry="refresh" />
    <p v-if="error && api" class="warning">当前详情未确认，以下为上次成功读取的目录快照。</p>
    <template v-if="api">
      <section class="panel api-identity" aria-label="接口信息">
        <div class="endpoint"><span class="method">{{ api.method }}</span><code>{{ api.path }}</code></div>
        <div class="api-states"><StatusBadge :label="api.implemented === true ? '已实现' : api.implemented === false ? '未实现' : '实现状态未知'" tone="neutral" /><span class="hint">目录就绪：{{ api.readiness === 'unknown' ? '未观测' : api.readiness || '未提供' }}</span><StatusBadge v-if="api.kind === 'business'" :label="error ? '当前就绪状态未确认' : healthLabel" :tone="!error && !healthError && ready === true ? 'success' : 'neutral'" /><StatusBadge v-else label="上游就绪未探测" tone="neutral" /></div>
        <p v-if="healthError" class="warning">查询健康读取失败：{{ healthError }}。上次读取 <Timestamp :value="healthReadAt" />，当前状态未确认。</p>
        <p class="hint">目录版本 <code>{{ api.version }}</code> · 读取时间 <Timestamp :value="readAt" /></p>
        <p v-if="api.kind === 'source'" class="hint">实现状态不代表上游可用；单次源测试只能证明该次调用结果。</p><p v-else class="hint">健康检查表示查询服务状态，不代表当前参数一定能查询成功。</p>
      </section>
      <div class="tabs api-tabs" role="tablist" aria-label="接口文档"><button v-for="(title, index) in tabs" :id="`api-tab-${index}`" :key="title" ref="tabButtons" role="tab" :aria-selected="tab === index" aria-controls="api-doc-panel" :tabindex="tab === index ? 0 : -1" :class="{ active: tab === index }" @click="tab = index" @keydown="keyTab">{{ title }}</button></div>
      <section id="api-doc-panel" class="panel api-tab-panel" role="tabpanel" :aria-labelledby="`api-tab-${tab}`" tabindex="0">
        <template v-if="tab === 0"><h2>概览</h2><dl class="facts"><div><dt>接口标识</dt><dd><code>{{ api.id }}</code></dd></div><div><dt>接口分组</dt><dd>{{ api.kind === 'business' ? '本地业务接口' : api.kind === 'source' ? '上游接口' : api.kind }}</dd></div><div><dt>查询快照</dt><dd>{{ api.snapshot === true ? '使用查询快照' : api.snapshot === false ? '不使用查询快照' : '后端未提供' }}</dd></div></dl><ul v-if="api.notes?.length"><li v-for="(note, index) in api.notes" :key="index">{{ note }}</li></ul><p v-else class="hint">后端目录未提供附加说明。</p></template>
        <template v-else-if="tab === 1"><h2>参数</h2><p class="hint">以下为后端目录的参数定义。</p><JsonText :text="json(api.request)" label="参数定义" /></template>
        <ExampleViewer v-else-if="tab === 2" :text="json(api.request_example)" title="请求案例" />
        <template v-else-if="tab === 3"><ExampleViewer :text="json(api.success_example)" title="成功响应案例" /><ExampleViewer :text="json(api.failure_example)" title="失败响应案例" /></template>
        <template v-else><h2>调用记录</h2><EmptyState title="尚未加载调用记录" description="日志关联读取将在日志功能接入后开放。此处不将文档案例或本地空记录当作历史调用。" /></template>
      </section>
    </template>
    <EmptyState v-else :loading="busy" title="暂未取得接口详情" description="请重新读取或返回接口目录选择接口。" />
  </section>
</template>
<style scoped>
.api-detail > .page-heading { margin-top: 16px; }
.api-identity .endpoint { margin: 0 0 12px; }
.api-states { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 12px; }
.api-identity > p { margin-top: 8px; }
.api-tabs { margin-top: 24px; overflow-x: auto; }
.api-tabs button { flex-shrink: 0; }
.api-tab-panel { margin-top: 16px; }
.api-tab-panel li { margin-bottom: 8px; overflow-wrap: anywhere; }
</style>
