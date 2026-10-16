<script setup lang="ts">
import { ref, computed, onActivated, onDeactivated, onUnmounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { prepareRoster, consumeRoster } from '../app/rosterNavigation'
import type { RosterContext } from '../app/roster'
import { request, describe, ApiError, type Envelope } from '../api'
import ApiOverview from '../components/ApiOverview.vue'
import { catalogName, readCatalogAPI, type CatalogAPI } from '../app/catalog'
import { useRequestScope } from '../composables/useRequestScope'
import ApiDirectory from '../components/ApiDirectory.vue'
import CopyId from '../components/CopyId.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import ErrorState from '../components/ErrorState.vue'
import EmptyState from '../components/EmptyState.vue'
import Timestamp from '../components/Timestamp.vue'
import CatalogSchema from '../components/CatalogSchema.vue'
import SourceRequestDocs from '../components/SourceRequestDocs.vue'
import ApiRequestExamples from '../components/ApiRequestExamples.vue'
import ApiCallRecords from '../components/ApiCallRecords.vue'
import ResponseContract from '../components/ResponseContract.vue'
import ApiTestDialog from '../components/ApiTestDialog.vue'
import type { RequestExample } from '../app/requestExamples'
import { operationRules, commonBusinessRules } from '../app/catalogSchema'
import ExampleViewer from '../components/ExampleViewer.vue'
const router = useRouter(), draftRoster = ref<RosterContext | null>(null)
async function openRoster(context: RosterContext) {
  if (!prepareRoster(context)) return
  closeTest()
  try { if (await router.push('/apis/teaching-classes.students.query')) { consumeRoster(); error.value = '名单接口跳转未完成，请重新选择教学班。' } } catch { consumeRoster(); error.value = '名单接口跳转失败，请重新选择教学班。' }
}
const draftOpen = ref(false), draftText = ref(''), draftSnapshot = ref(false), draftKey = ref(0)
function openTest() { draftRoster.value = null; draftText.value = '{}'; draftSnapshot.value = false; draftKey.value++; draftOpen.value = true }
function closeTest() { draftRoster.value = null; draftOpen.value = false; draftText.value = ''; draftSnapshot.value = false }
function loadExample(example: RequestExample) { draftRoster.value = null; draftText.value = JSON.stringify(example.body, null, 2); draftSnapshot.value = !!example.snapshot; draftKey.value++; draftOpen.value = true }
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
    if (apiId === 'teaching-classes.students.query' && !draftOpen.value) {
      const context = consumeRoster()
      if (context) { draftRoster.value = context; draftText.value = JSON.stringify(context.request, null, 2); draftSnapshot.value = false; draftKey.value++; draftOpen.value = true }
    }
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
function stop() { if (apiId === 'teaching-classes.students.query') consumeRoster(); draftRoster.value = null; draftOpen.value = false; draftText.value = ''; draftSnapshot.value = false; active = false; requests.cancel(); busy.value = false }
onDeactivated(stop)
onUnmounted(stop)
</script>
<template>
  <section class="api-detail">
    <ApiDirectory :selected="apiId" />
    <div class="api-document">
    <RouterLink class="api-back" to="/apis">← 接口目录</RouterLink>
    <PageHeader :title="api ? catalogName(api) : '接口详情'"><button :disabled="busy" @click="refresh">{{ busy ? '读取中…' : '刷新详情' }}</button><button v-if="api" class="primary" :disabled="busy || !!error" @click="openTest">在线测试</button></PageHeader>
    <ErrorState v-if="error" :message="error" retryable :busy="busy" @retry="refresh" />
    <p v-if="error && api" class="warning">当前详情未确认，以下为上次成功读取的目录快照。</p>
    <template v-if="api">
      <section class="panel api-identity" aria-label="接口信息">
        <div class="endpoint"><span class="method" :data-method="api.method">{{ api.method }}</span><CopyId :value="api.path" label="接口地址" /></div>
        <div class="api-states"><StatusBadge :icon="api.implemented === true ? 'check' : undefined" :label="api.implemented === true ? '已实现' : api.implemented === false ? '未实现' : '实现状态未知'" tone="neutral" /><span class="hint">目录就绪：{{ api.readiness === 'unknown' ? '未观测' : api.readiness || '未提供' }}</span><StatusBadge v-if="api.kind === 'business'" :label="error ? '当前就绪状态未确认' : healthLabel" :tone="!error && !healthError && ready === true ? 'success' : 'neutral'" /><StatusBadge v-else label="上游就绪未探测" tone="neutral" /></div>
        <p v-if="healthError" class="warning">查询健康读取失败：{{ healthError }}。上次读取 <Timestamp :value="healthReadAt" />，当前状态未确认。</p>
        <p class="hint">目录版本 <code>{{ api.version }}</code> · 读取时间 <Timestamp :value="readAt" /></p>
        <p v-if="api.kind === 'source'" class="hint">实现状态不代表上游可用；单次源测试只能证明该次调用结果。</p><p v-else class="hint">健康检查表示查询服务状态，不代表当前参数一定能查询成功。</p>
      </section>
      <div class="tabs api-tabs" role="tablist" aria-label="接口文档"><button v-for="(title, index) in tabs" :id="`api-tab-${index}`" :key="title" ref="tabButtons" role="tab" :aria-selected="tab === index" aria-controls="api-doc-panel" :tabindex="tab === index ? 0 : -1" :class="{ active: tab === index }" @click="tab = index" @keydown="keyTab">{{ title }}</button></div>
      <section id="api-doc-panel" class="panel api-tab-panel" role="tabpanel" :aria-labelledby="`api-tab-${tab}`" tabindex="0">
        <ApiOverview v-if="tab === 0" :api="api" />
        <template v-else-if="tab === 1"><h2>参数</h2><SourceRequestDocs v-if="api.kind === 'source'" :api="api" @response="tab = 3" /><template v-else><h3>本地 POST · JSON Body</h3><p class="hint">字段通过 application/json 请求正文提交，不放入 Query。浏览器使用同源会话网关；业务服务密钥由服务端持有。</p><CatalogSchema :api="api" :schema="api.request" label="参数字段表" parameters /><div class="parameter-examples"><ExampleViewer :text="json(api.request_example)" title="请求示例" /><ExampleViewer :text="json(api.success_example)" title="响应示例" /></div><h3>组合规则</h3><ul><li v-for="rule in operationRules[api.id] ?? []" :key="rule">{{ rule }}</li></ul><details><summary>通用解析与分页规则</summary><ul><li v-for="rule in commonBusinessRules" :key="rule">{{ rule }}</li></ul></details></template></template>
        <ApiRequestExamples v-else-if="tab === 2" :api="api" @load="loadExample" />
        <template v-else-if="tab === 3"><ResponseContract :api="api" /><h2>响应字段</h2><p class="hint">{{ api.kind === 'source' ? '此 schema 描述 data.list 中的一条源记录。' : '此 schema 描述成功响应的 data 业务字段。' }}完整 HTTP 外壳见下方案例。</p><CatalogSchema :api="api" :schema="api.response" label="响应字段表" /><ExampleViewer :text="json(api.success_example)" title="成功响应案例" /><ExampleViewer :text="json(api.failure_example)" title="失败响应案例" /></template>
        <ApiCallRecords v-else :api="api" />
      </section>
    </template>
    <EmptyState v-else :loading="busy" title="暂未取得接口详情" description="请重新读取或返回接口目录选择接口。" />
    </div>
    <ApiTestDialog v-if="draftOpen && api" :key="draftKey" :api="api" :initial-text="draftText" :needs-snapshot="draftSnapshot" :roster-context="draftRoster" @close="closeTest" @roster="openRoster" />
  </section>
</template>
<style scoped>

.api-detail { display: grid; grid-template-columns: 224px minmax(0, 1fr); gap: 20px; align-items: start; }
.api-document { min-width: 0; border: 1px solid var(--line); border-radius: var(--radius-panel); background: var(--white); padding: 24px; }
.api-back { display: inline-block; margin-bottom: 16px; font-size: 13px; }
.api-document > :deep(.page-heading) { margin-bottom: 16px; align-items: start; gap: 16px; }
.api-document > :deep(.page-heading p) { max-width: 65ch; font-size: 13px; }
.api-document > :deep(.page-heading .actions) { flex-shrink: 0; }
.api-identity { padding: 0; border: 0; background: transparent; }
.api-identity .endpoint { padding: 12px 16px; margin: 0 0 12px; background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius-control); align-items: center; }
.endpoint :deep(.copy-id) { flex: 1; min-width: 0; width: 100%; }
.endpoint :deep(.copy-id code) { max-width: none; color: var(--color-primary-text); font-size: 13px; }
.endpoint :deep(.copy-id button) { margin-left: auto; }
.api-states { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; margin-bottom: 12px; }
.api-identity > p { margin-top: 8px; }
.api-tabs { margin-top: 20px; overflow-x: auto; }
.api-tabs button { flex-shrink: 0; }
.api-tab-panel { margin-top: 20px; padding: 0; border: 0; background: transparent; }
.api-tab-panel h2 { margin-bottom: 16px; }
.api-tab-panel h3 { margin: 16px 0 8px; }
.api-tab-panel li { margin-bottom: 8px; overflow-wrap: anywhere; }
.parameter-examples { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-top: 24px; }
@media (max-width: 1100px) { .api-detail { grid-template-columns: minmax(0, 1fr); gap: 16px; } }
@media (max-width: 767px) {
 .api-document { padding: 16px; }
 .api-document > :deep(.page-heading) { flex-direction: column; }
 .api-document > :deep(.page-heading .actions) { justify-content: start; width: 100%; }
 .parameter-examples { grid-template-columns: minmax(0, 1fr); }
 .api-identity .endpoint { padding: 10px; }
}
</style>
