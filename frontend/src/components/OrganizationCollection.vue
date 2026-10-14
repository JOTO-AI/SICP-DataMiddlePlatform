<script setup lang="ts">
import { computed, ref, onUnmounted } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import type { BusinessTestResult } from '../app/businessTest'
import { pageCount, type QueryPageChain } from '../app/queryPagination'
import { collectionBlock, collectionRecords, filteredOrganizations, organizationIntegrity, loadOrganizations, type OrganizationPage } from '../app/organizationCollection'
import { recordCell, responseColumns } from '../app/responseViews'
import { useRequestScope } from '../composables/useRequestScope'
import { describe } from '../api'
import OrganizationTree from './OrganizationTree.vue'
import ResponseViewer from './ResponseViewer.vue'
import JsonText from './JsonText.vue'
const props = defineProps<{ api: CatalogAPI; chain: QueryPageChain; seed: BusinessTestResult }>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const requests = useRequestScope(), phase = ref('idle'), pages = ref<OrganizationPage[]>([]), error = ref(''), view = ref('tree'), selectedPage = ref(1)
const records = computed(() => collectionRecords(pages.value)), columns = computed(() => responseColumns(props.api, records.value))
const block = computed(() => collectionBlock(props.chain)), filtered = computed(() => filteredOrganizations(props.chain.requestText))
const integrity = computed(() => organizationIntegrity(records.value))
const actual = computed(() => pages.value.find(page => page.page === selectedPage.value)?.result.response)
async function load() {
  if (phase.value === 'loading' || block.value) return
  const run = requests.begin(); phase.value = 'loading'; error.value = ''; pages.value = []; selectedPage.value = props.chain.page; emit('busy', true)
  try {
    await loadOrganizations(props.api, props.chain, props.seed, run.signal, value => { if (run.current()) pages.value = value })
    if (run.current()) phase.value = 'complete'
  } catch (cause) { if (run.current()) { phase.value = 'failed'; error.value = cause instanceof Error ? cause.message : describe(cause) } }
  finally { if (run.current()) emit('busy', false) }
}
function stop() { requests.cancel(); phase.value = 'stopped'; emit('busy', false) }
onUnmounted(() => emit('busy', false))
</script>
<template><section aria-label="组织范围完整性" class="organization-collection"><h4>组织范围完整性</h4><p class="hint">点击后沿用本次条件、快照和数据组合逐页读取，最多 50 页 / 1000 条。不会自动重试或更换快照；关闭或修改条件将清除已加载集合。</p><p v-if="block" class="warning">{{ block }}</p><p role="status" :data-state="phase">{{ phase === 'complete' ? '当前筛选范围已完整加载' : phase === 'loading' ? '正在加载，尚未完整' : phase === 'failed' ? '加载失败，尚未完整' : phase === 'stopped' ? '已停止，尚未完整' : '尚未核验完整性' }} · 已接纳 {{ pages.length }} / {{ pageCount(chain) }} 页、{{ records.rows.length }} / {{ chain.total ?? '未知' }} 条</p><p v-if="error" class="warning">{{ error }}</p><button v-if="phase !== 'loading'" type="button" :disabled="!!block || phase === 'complete'" @click="load">加载当前条件的完整组织</button><button v-else type="button" @click="stop">停止加载组织</button>
<template v-if="pages.length"><p v-if="phase === 'complete' && integrity" class="hint">{{ filtered ? '当前条件包含筛选，不代表全部组织。' : '本次无筛选查询的分页已全部接纳。' }} 集合内父子关系完整；不代表外部源数据完整。</p><p v-else class="warning">组织关系未确认完整：可能仍有未加载页、缺失父节点、关系歧义或闭环。保留已接纳的原记录，不补造父节点。</p><div class="actions"><button type="button" :aria-pressed="view === 'tree'" @click="view = 'tree'">已加载组织树</button><button type="button" :aria-pressed="view === 'table'" @click="view = 'table'">已加载组织表格</button><button type="button" :aria-pressed="view === 'pages'" @click="view = 'pages'">逐页响应</button></div>
<OrganizationTree v-if="view === 'tree'" :records="records" />
<div v-else-if="view === 'table'" class="table-wrap" tabindex="0" aria-label="已加载组织表格"><table><thead><tr><th v-for="column in columns" :key="column.key">{{ column.title }}<small>{{ column.key }}</small></th></tr></thead><tbody><tr v-for="(row, i) in records.rows" :key="i"><td v-for="column in columns" :key="column.key">{{ recordCell(row, column.key) }}</td></tr></tbody></table></div>
<template v-else><p class="hint">每页均为实际请求响应；集合表格不是单次 HTTP 响应。</p><label>已接纳页<select v-model="selectedPage"><option v-for="page in [...pages].sort((a,b) => a.page - b.page)" :key="page.page" :value="page.page">第 {{ page.page }} 页</option></select></label><template v-if="actual"><ResponseViewer :text="actual.responseText ?? ''" :http-status="actual.status" :request-id="actual.requestID" label="组织分页响应原文" /><details><summary>本页响应头</summary><JsonText :text="JSON.stringify(actual.headers, null, 2)" label="组织分页响应头" /></details></template></template>
</template></section></template>
<style scoped>
.organization-collection { margin-top: 24px; padding: 16px; border: 1px solid var(--line); overflow-wrap: anywhere; min-width: 0; }
.actions { margin-block: 16px; }
</style>