<script setup lang="ts">
import { ref, computed, onActivated, onDeactivated, onUnmounted } from 'vue'
import { request, describe, ApiError, type Envelope } from '../api'
import { readCatalog, filterCatalog, catalogName, catalogGroups, type Catalog } from '../app/catalog'
import { useRequestScope } from '../composables/useRequestScope'
const props = defineProps<{ selected: string }>()
const catalog = ref<Catalog | null>(null), search = ref(''), error = ref(''), busy = ref(false), expanded = ref(false)
const scope = useRequestScope()
let active = false
const matches = computed(() => filterCatalog(catalog.value?.items ?? [], search.value, ''))
const groups = computed(() => [...new Set(matches.value.map(api => api.kind))].sort((a, b) => a === 'business' ? -1 : b === 'business' ? 1 : a.localeCompare(b)))
async function read() {
  if (!active || busy.value) return
  busy.value = true
  const run = scope.begin()
  try {
    const response = await request<Envelope<unknown>>('worker', '/admin/v1/apis', { signal: run.signal })
    if (!active || !run.current()) return
    const data = readCatalog(response.body.data)
    if (!data) throw new ApiError('接口目录格式不完整。', 'invalid_response')
    catalog.value = data; error.value = ''
  } catch (cause) { if (active && run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
onActivated(() => { active = true; if (!catalog.value) void read() })
function stop() { active = false; scope.cancel(); busy.value = false }
onDeactivated(stop); onUnmounted(stop)
</script>
<template>
  <aside class="api-directory" aria-label="接口列表">
    <div class="directory-heading"><h2>接口列表</h2><button class="directory-toggle" type="button" :aria-expanded="expanded" aria-controls="api-directory-links" @click="expanded = !expanded">{{ expanded ? '收起目录' : '展开目录' }}</button></div>
    <div id="api-directory-links" class="directory-body" :class="{ expanded }">
      <label class="directory-search"><span class="sr-only">搜索接口列表</span><input v-model="search" type="search" placeholder="搜索接口名称或路径" maxlength="256"></label>
      <p v-if="busy" class="hint" role="status">正在读取接口列表…</p>
      <p v-if="error" class="error" role="alert">{{ error }} <button type="button" :disabled="busy" @click="read">重试目录</button></p>
      <p v-if="error && catalog" class="hint">以下目录来自上次成功读取。</p>
      <nav aria-label="选择接口">
        <section v-for="kind in groups" :key="kind"><h3>{{ catalogGroups[kind] || kind }} <span>{{ matches.filter(api => api.kind === kind).length }}</span></h3><RouterLink v-for="api in matches.filter(api => api.kind === kind)" :key="api.id" :to="{ name: 'api-detail', params: { apiId: api.id } }" :aria-current="props.selected === api.id ? 'page' : undefined" :class="{ selected: props.selected === api.id }" @click="expanded = false">{{ catalogName(api) }}</RouterLink></section>
      </nav>
      <p v-if="catalog && !matches.length" class="hint">没有符合条件的接口。</p>
      <RouterLink class="directory-return" to="/apis">返回接口中心</RouterLink>
    </div>
  </aside>
</template>
<style scoped>
.api-directory { padding: 20px 12px; border: 1px solid var(--line); border-radius: var(--radius-panel); background: var(--white); align-self: start; position: sticky; top: 16px; max-height: calc(100dvh - 32px); overflow: auto; min-width: 0; }
.directory-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 0 8px; }
h2 { font-size: 16px; }
.directory-search { margin: 16px 0 20px; }
.directory-search input { font-size: 13px; }
nav h3 { display: flex; justify-content: space-between; padding: 8px; margin-top: 8px; font-size: 13px; }
nav h3 span { color: var(--muted); font-weight: 400; }
nav a { padding: 8px 12px; font-size: 13px; }
.directory-return { display: block; margin: 20px 8px 0; font-size: 13px; }
.directory-toggle { display: none; }
@media (max-width: 1100px) {
  .api-directory { position: static; max-height: none; padding: 12px; }
  .directory-toggle { display: inline-flex; }
  .directory-body { display: none; }
  .directory-body.expanded { display: block; }
  nav { max-height: 45dvh; overflow: auto; }
}
</style>
