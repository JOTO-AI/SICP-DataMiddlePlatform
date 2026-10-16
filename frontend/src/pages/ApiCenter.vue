<script setup lang="ts">
import { computed, ref, onActivated, onDeactivated, onUnmounted, defineAsyncComponent } from 'vue'
import { request, describe, ApiError, type Envelope } from '../api'
import { readCatalog, filterCatalog, catalogName, catalogGroups, type Catalog } from '../app/catalog'
import { useRequestScope } from '../composables/useRequestScope'
import PageHeader from '../components/PageHeader.vue'
import FilterBar from '../components/FilterBar.vue'
import EmptyState from '../components/EmptyState.vue'
import ErrorState from '../components/ErrorState.vue'
import StatusBadge from '../components/StatusBadge.vue'
import Timestamp from '../components/Timestamp.vue'
import ManagementApiDocs from '../components/ManagementApiDocs.vue'
import RecentTests from '../components/RecentTests.vue'
const BusinessVerification = defineAsyncComponent(() => import('../components/BusinessVerification.vue'))
const queryView = ref(false), catalog = ref<Catalog | null>(null), busy = ref(false), error = ref(''), readAt = ref('')
const search = ref(''), group = ref('business'), implementation = ref('')
const groups = computed(() => [...new Set(catalog.value?.items.map(item => item.kind) ?? [])].sort((a, b) => a === 'business' ? -1 : b === 'business' ? 1 : a.localeCompare(b)))
const visible = computed(() => filterCatalog(catalog.value?.items ?? [], search.value, group.value).filter(api => !implementation.value || (implementation.value === 'implemented' ? api.implemented === true : implementation.value === 'unimplemented' ? api.implemented === false : api.implemented !== true && api.implemented !== false)))
const requests = useRequestScope()
let active = false
async function refresh() {
  if (!active || busy.value) return
  busy.value = true
  const run = requests.begin()
  try {
    const result = await request<Envelope<unknown>>('worker', '/admin/v1/apis', { signal: run.signal })
    if (!active || !run.current()) return
    const data = readCatalog(result.body.data)
    if (!data) throw new ApiError('接口目录响应格式或版本不一致。', 'invalid_response')
    catalog.value = data; readAt.value = new Date().toISOString(); error.value = ''
  } catch (cause) { if (active && run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
onActivated(() => { active = true; if (!catalog.value) void refresh() })
function stop() { active = false; requests.cancel(); busy.value = false }
onDeactivated(stop)
onUnmounted(stop)
</script>

<template>
  <section>
    <template v-if="!queryView">
      <PageHeader title="接口中心" description="查看接口说明、参数与响应，在接口详情中在线测试。"><button :disabled="busy" @click="refresh">{{ busy ? '读取中…' : '刷新目录' }}</button><button @click="queryView = true">业务数据核验</button></PageHeader>
      <FilterBar class="catalog-filter" label="接口目录筛选"><label class="catalog-search"><span class="sr-only">名称、路径或分组</span><input v-model="search" type="search" maxlength="256" placeholder="搜索接口名称或路径" :disabled="group === 'management'"></label><label><span class="sr-only">实现状态</span><select v-model="implementation" :disabled="group === 'management'"><option value="">全部实现状态</option><option value="implemented">已实现</option><option value="unimplemented">未实现</option><option value="unknown">实现状态未知</option></select></label><button type="button" @click="search = ''; group = ''; implementation = ''">清除筛选</button></FilterBar>
      <ErrorState v-if="error" :message="error" retryable :busy="busy" @retry="refresh" />
      <p v-if="error && catalog" class="warning">以下为上次成功读取的目录，当前实现与就绪状态未确认。</p>
      <section class="panel catalog-list" aria-label="后端接口目录" :aria-busy="busy">
        <nav class="catalog-categories" aria-label="接口分类"><button v-for="kind in groups" :key="kind" :aria-pressed="group === kind" :class="{ active: group === kind }" @click="group = kind">{{ catalogGroups[kind] || kind }} <span>{{ catalog?.items.filter(api => api.kind === kind).length }}</span></button><button :aria-pressed="group === ''" :class="{ active: group === '' }" @click="group = ''">全部接口</button><button :aria-pressed="group === 'management'" :class="{ active: group === 'management' }" @click="group = 'management'">管理接口文档</button></nav>
        <div class="catalog-content">
          <template v-if="group === 'management'"><h2>管理接口文档</h2><p class="hint">管理操作请前往对应页面，以下文档不计入可测试接口目录。</p><ManagementApiDocs /></template>
          <template v-else>
            <EmptyState v-if="!catalog" :loading="busy" title="暂未读取到目录" />
            <EmptyState v-else-if="!visible.length" title="没有符合条件的接口" description="搜索只作用于本次已获取目录。" />
            <div v-else class="table-wrap" tabindex="0" role="region" aria-label="接口目录表格"><table><thead><tr><th scope="col">名称</th><th scope="col">方法</th><th scope="col">Endpoint</th><th scope="col">实现状态</th><th scope="col">操作</th></tr></thead><tbody><tr v-for="api in visible" :key="api.id"><th scope="row">{{ catalogName(api) }}</th><td><span class="method" :data-method="api.method">{{ api.method }}</span></td><td><code>{{ api.path }}</code></td><td><StatusBadge :icon="api.implemented === true ? 'check' : undefined" :label="api.implemented === true ? '已实现' : api.implemented === false ? '未实现' : '实现状态未知'" tone="neutral" /></td><td><RouterLink :to="{ name: 'api-detail', params: { apiId: api.id } }" :aria-label="`查看 ${catalogName(api)} 文档`">查看文档</RouterLink></td></tr></tbody></table></div>
            <p v-if="catalog" class="hint catalog-meta">显示 {{ visible.length }} / {{ catalog.items.length }} 项。已实现不代表当前可用，请在详情中查看服务状态。</p>
            <section class="catalog-notes" aria-label="补充说明"><h3>其他接口</h3><div class="catalog-pending"><strong>行政班查询 · 契约待定</strong><p class="hint">本地查询方法与路径尚未确定，上游行政班源及教学班查询不替代此接口。</p></div></section>
          </template>
        </div>
      </section>
      <p class="hint catalog-meta">{{ catalog ? `后端目录 ${catalog.items.length} 项 · 版本 ${catalog.version}` : '尚未取得后端目录' }}<template v-if="readAt"> · 本页读取 <Timestamp :value="readAt" /></template></p>
      <RecentTests />
    </template>
    <button v-else class="catalog-back" @click="queryView = false">返回接口目录</button>
    <KeepAlive><BusinessVerification v-if="queryView" /></KeepAlive>
  </section>
</template>
<style scoped>
.catalog-filter { border: 0; background: transparent; padding: 0; }
.catalog-filter :deep(fieldset) { display: grid; grid-template-columns: minmax(200px, 400px) minmax(150px, 210px) auto; align-items: center; justify-content: start; gap: 16px; }
.catalog-list { margin-top: 24px; padding: 0; overflow: hidden; }
.catalog-categories { flex-direction: row; gap: 24px; padding: 0 24px; overflow-x: auto; border-bottom: 1px solid var(--line); }
.catalog-categories button { flex-shrink: 0; border: 0; border-bottom: 3px solid transparent; border-radius: 0; padding: 14px 2px; color: var(--muted); background: transparent; }
.catalog-categories button.active { color: var(--color-primary-text); border-bottom-color: var(--brand); }
.catalog-categories button span { margin-left: 4px; font-variant-numeric: tabular-nums; }
.catalog-content { padding: 20px; }
.catalog-list table { min-width: 780px; }
.catalog-list th:first-child { min-width: 140px; }
.catalog-list td, .catalog-list th { vertical-align: middle; height: 44px; padding: 8px 14px; }
.catalog-list tbody th { font-weight: 550; white-space: nowrap; }
.catalog-list td:nth-child(3) { width: 45%; }
.catalog-list td code { color: var(--muted); }
.catalog-meta { margin: 12px 0 0; }
.catalog-notes { margin-top: 24px; padding-top: 20px; border-top: 1px solid var(--line); }
.catalog-pending { display: flex; gap: 24px; align-items: baseline; margin-top: 12px; padding: 12px 14px; border: 1px solid var(--line); border-radius: var(--radius-control); }
.catalog-pending strong { white-space: nowrap; font-size: 13px; font-weight: 550; }
.catalog-back { margin-bottom: 20px; }
@media (max-width: 767px) {
  .catalog-filter :deep(fieldset) { grid-template-columns: minmax(0, 1fr) auto; }
  .catalog-search { grid-column: 1 / -1; }
  .catalog-content { padding: 12px; }
  .catalog-categories { padding: 0 12px; gap: 20px; }
  .catalog-pending { flex-direction: column; gap: 6px; }
}
</style>
