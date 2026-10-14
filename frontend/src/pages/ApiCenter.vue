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
const Queries = defineAsyncComponent(() => import('./Queries.vue'))
const queryView = ref(false), catalog = ref<Catalog | null>(null), busy = ref(false), error = ref(''), readAt = ref('')
const search = ref(''), group = ref('business')
const groups = computed(() => [...new Set(catalog.value?.items.map(item => item.kind) ?? [])])
const visible = computed(() => filterCatalog(catalog.value?.items ?? [], search.value, group.value))
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
      <PageHeader title="接口中心" description="浏览接口目录、Endpoint 与实现状态；查看目录不会执行接口。"><button :disabled="busy" @click="refresh">{{ busy ? '读取中…' : '刷新目录' }}</button><button @click="queryView = true">业务数据核验</button></PageHeader>
      <p class="hint catalog-meta">{{ catalog ? `后端目录 ${catalog.items.length} 项 · 版本 ${catalog.version}` : '尚未取得后端目录' }}<template v-if="readAt"> · 本页读取 <Timestamp :value="readAt" /></template></p>
      <FilterBar class="catalog-filter" label="接口目录筛选"><label>名称、路径或分组<input v-model="search" type="search" maxlength="256" placeholder="例如：学生成绩、/students/、上游接口"></label><label>接口分组<select v-model="group"><option value="">全部分组</option><option v-for="kind in groups" :key="kind" :value="kind">{{ catalogGroups[kind] || kind }}</option></select></label><button type="button" @click="search = ''; group = ''">清除筛选</button></FilterBar>
      <ErrorState v-if="error" :message="error" retryable :busy="busy" @retry="refresh" />
      <p v-if="error && catalog" class="warning">以下为上次成功读取的目录，当前实现与就绪状态未确认。</p>
      <section class="panel catalog-list" aria-label="后端接口目录" :aria-busy="busy">
        <EmptyState v-if="!catalog" :loading="busy" title="暂未读取到目录" />
        <EmptyState v-else-if="!visible.length" title="没有符合条件的接口" description="搜索只作用于本次已获取目录。" />
        <div v-else class="table-wrap" tabindex="0" role="region" aria-label="接口目录表格"><table><thead><tr><th scope="col">名称</th><th scope="col">分组 / 方法</th><th scope="col">Endpoint</th><th scope="col">实现 / 就绪</th><th scope="col">操作</th></tr></thead><tbody><tr v-for="api in visible" :key="api.id"><th scope="row">{{ catalogName(api) }}<small><code>{{ api.id }}</code></small></th><td>{{ catalogGroups[api.kind] || api.kind }}<small><code>{{ api.method }}</code></small></td><td><code>{{ api.path }}</code></td><td><StatusBadge :label="api.implemented === true ? '已实现' : api.implemented === false ? '未实现' : '实现状态未知'" tone="neutral" /><small>{{ api.readiness === 'unknown' ? '就绪状态未观测' : `目录就绪：${api.readiness ?? '未提供'}` }}</small></td><td><RouterLink :to="{ name: 'api-detail', params: { apiId: api.id } }" :aria-label="`查看 ${catalogName(api)} 文档`">查看文档</RouterLink></td></tr></tbody></table></div>
        <p v-if="catalog" class="hint catalog-meta">显示 {{ visible.length }} / {{ catalog.items.length }} 项。已实现不代表当前可查询或源测试成功。</p>
      </section>
      <section class="panel catalog-notes" aria-label="补充说明"><h2>补充说明</h2><p class="hint">以下来源于控制台设计与现有管理接口说明，不计入后端可测试目录。</p><h3>管理操作</h3><p>更新维护、日志读取、通知和系统设置在对应页面执行；接口中心不提供通用写接口执行器。</p><div class="actions"><RouterLink to="/updates">更新事件</RouterLink><RouterLink to="/logs">日志中心</RouterLink><RouterLink to="/notifications">通知中心</RouterLink><RouterLink to="/settings">系统设置</RouterLink></div><h3>行政班查询 · 契约待定</h3><p>本地独立查询的方法与路径尚未确定。上游行政班源与教学班查询均不替代此接口。</p></section>
    </template>
    <button v-else class="catalog-back" @click="queryView = false">返回接口目录</button>
    <KeepAlive><Queries v-if="queryView" /></KeepAlive>
  </section>
</template>
<style scoped>
.catalog-meta { margin: 12px 0; }
.catalog-filter :deep(fieldset) { display: grid; grid-template-columns: minmax(200px, 1fr) minmax(160px, .4fr) auto; align-items: end; gap: 16px; }
.catalog-list, .catalog-notes { margin-top: 20px; }
.catalog-list table { min-width: 850px; }
.catalog-list th:first-child { min-width: 180px; }
.catalog-notes h3 { margin-top: 20px; }
.catalog-notes p { margin-top: 8px; }
.catalog-notes .actions { margin-top: 12px; gap: 20px; }
.catalog-back { margin-bottom: 20px; }
@media (max-width: 767px) { .catalog-filter :deep(fieldset) { grid-template-columns: minmax(0, 1fr); } }
</style>
