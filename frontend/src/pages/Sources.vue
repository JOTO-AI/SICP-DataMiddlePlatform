<script setup lang="ts">
import { computed, ref, onActivated, onDeactivated, onUnmounted } from 'vue'
import { request, ApiError, describe, type Envelope } from '../api'
import { sourceItems, filterSources, sourceFailureLabels, type SourceState } from '../app/sources'
import { displayEnum } from '../shared/display'
import { pollingDelay } from '../app/polling'
import { useRequestScope } from '../composables/useRequestScope'
import { usePageVisibility } from '../composables/usePageVisibility'
import PageHeader from '../components/PageHeader.vue'
import FilterBar from '../components/FilterBar.vue'
import EmptyState from '../components/EmptyState.vue'
import ErrorState from '../components/ErrorState.vue'
import StatusBadge from '../components/StatusBadge.vue'
import Timestamp from '../components/Timestamp.vue'
import Drawer from '../components/Drawer.vue'
import CopyId from '../components/CopyId.vue'

const items = ref<SourceState[] | null>(null), error = ref(''), busy = ref(false), readAt = ref('')
const name = ref(''), state = ref(''), selectedId = ref(''), drawerOpen = ref(false)
const selected = computed(() => items.value?.find(item => item.id === selectedId.value))
const visible = computed(() => filterSources(items.value ?? [], name.value, state.value))
const states = computed(() => [...new Set((items.value ?? []).map(item => item.failure_state).filter((value): value is string => !!value))])
const requests = useRequestScope()
let active = false, failures = 0, timer: ReturnType<typeof setTimeout> | undefined
function cancel() { requests.cancel(); clearTimeout(timer); busy.value = false }
const pageVisible = usePageVisibility(cancel, () => { if (active) void refresh() })
async function refresh() {
  if (!active || !pageVisible.value || busy.value) return
  clearTimeout(timer); busy.value = true
  const run = requests.begin()
  let serverDelay = 0
  try {
    const result = await request<Envelope<unknown>>('worker', '/admin/v1/sources', { signal: run.signal })
    if (!active || !run.current()) return
    const next = sourceItems(result.body.data)
    if (!next) throw new ApiError('数据源响应格式异常。', 'invalid_response')
    items.value = next; error.value = ''; readAt.value = new Date().toISOString(); failures = 0
  } catch (cause) {
    if (!active || !run.current()) return
    error.value = describe(cause); failures++
    if (cause instanceof ApiError) serverDelay = pollingDelay(15000, 0, cause.headers['Retry-After'])
  } finally {
    if (active && run.current()) {
      busy.value = false
      if (pageVisible.value) timer = setTimeout(refresh, Math.max(serverDelay, pollingDelay(15000, failures)))
    }
  }
}
function openSource(id: string) { selectedId.value = id; drawerOpen.value = true }
onActivated(() => { active = true; void refresh() })
function stop() { active = false; cancel(); drawerOpen.value = false; selectedId.value = '' }
onDeactivated(stop)
onUnmounted(stop)
</script>
<template>
  <section class="sources-page">
    <PageHeader title="数据源" description="检查源状态与已确认进度；正常每 15 秒读取，隐藏页面暂停。"><button :disabled="busy" @click="refresh">{{ busy ? '读取中…' : '刷新源状态' }}</button></PageHeader>
    <p class="hint source-read">{{ items ? `已获取 ${items.length} 个源` : '尚未取得数据源' }}<template v-if="readAt"> · 本页读取：<Timestamp :value="readAt" /></template></p>
    <FilterBar label="数据源本地筛选"><label>源名称或标识<input v-model="name" type="search" placeholder="输入源标识" maxlength="128"></label><label>已获取的失败状态<select v-model="state"><option value="">全部状态</option><option v-for="value in states" :key="value" :value="value">{{ displayEnum(value, sourceFailureLabels) }}</option><option v-if="state && !states.includes(state)" :value="state">{{ displayEnum(state, sourceFailureLabels) }}（当前未返回）</option></select></label><button type="button" @click="name = ''; state = ''">清除筛选</button></FilterBar>
    <p class="hint source-read">筛选仅作用于已获取的列表，不向后端发送筛选条件。无失败记录不代表源健康；失败不推进已确认进度。</p>
    <ErrorState v-if="error" :message="error" retryable :busy="busy" @retry="refresh" />
    <p v-if="error && items" class="warning">以下为上次成功读取的快照，当前状态未确认。读取时间：<Timestamp :value="readAt" /></p>
    <section class="panel" aria-label="源列表" :aria-busy="busy">
      <EmptyState v-if="!items" :loading="busy" title="暂未读取到数据源" description="读取成功后显示后端返回的源记录。" />
      <EmptyState v-else-if="!visible.length" :title="items.length ? '没有符合本地筛选的源' : '后端返回空列表'" />
      <div v-else class="table-wrap" role="region" aria-label="数据源表格，可横向滚动" tabindex="0"><table><thead><tr><th scope="col">数据源标识</th><th scope="col">主键</th><th scope="col">成功检查</th><th scope="col">已确认进度</th><th scope="col">失败记录</th><th scope="col">操作</th></tr></thead><tbody><tr v-for="item in visible" :key="item.id"><th scope="row"><code>{{ item.id }}</code></th><td><code>{{ item.primary_key || '未提供' }}</code></td><td><Timestamp :value="item.checked_at" /></td><td><Timestamp :value="item.confirmed_until" /></td><td><StatusBadge :value="item.failure_state" :labels="sourceFailureLabels" :tone="!error && item.failure_state === 'unresolved' ? 'danger' : 'neutral'" /></td><td><button :aria-label="`查看 ${item.id} 详情`" @click="openSource(item.id)">查看详情</button></td></tr></tbody></table></div>
      <p v-if="items" class="hint source-read">显示 {{ visible.length }} / {{ items.length }} 个已获取的源</p>
    </section>
    <Drawer v-model:open="drawerOpen" :title="selectedId ? `${selectedId} · 源详情` : '源详情'">
      <p v-if="error" class="warning">旧快照，当前状态未确认。读取时间：<Timestamp :value="readAt" /></p>
      <template v-if="selected">
        <h3>基本信息</h3><dl class="facts"><div><dt>源标识</dt><dd><CopyId :value="selected.id" label="源标识" /></dd></div><div><dt>主键</dt><dd><code>{{ selected.primary_key || '未提供' }}</code></dd></div><div><dt>成功检查</dt><dd><Timestamp :value="selected.checked_at" /></dd></div><div><dt>已确认进度</dt><dd><Timestamp :value="selected.confirmed_until" /></dd></div><div><dt>失败状态</dt><dd><StatusBadge :value="selected.failure_state" :labels="sourceFailureLabels" :tone="!error && selected.failure_state === 'unresolved' ? 'danger' : 'neutral'" /></dd></div></dl>
        <h3>本次窗口</h3><dl v-if="selected.window" class="facts"><div><dt>原确认进度</dt><dd><Timestamp :value="selected.window.ConfirmedUntil" /></dd></div><div><dt>窗口开始</dt><dd><Timestamp :value="selected.window.Start" /></dd></div><div><dt>窗口结束</dt><dd><Timestamp :value="selected.window.End" /></dd></div><div><dt>窗口任务号</dt><dd><CopyId :value="selected.window_task_id" label="窗口任务号" /></dd></div></dl><p v-else class="hint">后端未提供当前窗口。</p>
        <h3 class="source-detail-heading">最近失败</h3><dl v-if="selected.last_failure" class="facts"><div><dt>失败码</dt><dd><code>{{ selected.last_failure.code }}</code></dd></div><div><dt>观测时间</dt><dd><Timestamp :value="selected.last_failure.observed_at" /></dd></div><div><dt>控制任务号</dt><dd><CopyId :value="selected.last_failure.task_id" label="失败任务号" /></dd></div><div><dt>日志关联任务号</dt><dd><CopyId :value="selected.last_failure.log_task_id" label="失败日志关联任务号" /></dd></div></dl><p v-else class="hint">后端未提供失败记录。</p>
        <h3 class="source-detail-heading">成功检查关联</h3><dl class="facts"><div><dt>控制任务号</dt><dd><CopyId :value="selected.task_id" label="控制任务号" /></dd></div><div><dt>日志关联任务号</dt><dd><CopyId :value="selected.log_task_id" label="日志关联任务号" /></dd></div></dl>
        <p class="hint">窗口、检查和确认时间分别展示；不根据失败或日志记录推算同步进度。</p>
      </template>
      <EmptyState v-else title="当前响应中没有该源" description="请关闭详情后重新读取列表。" />
    </Drawer>
  </section>
</template>
<style scoped>
.source-read { margin: 12px 0; }
.sources-page table { min-width: 900px; }
.source-detail-heading { margin-top: 24px; }
.sources-page :deep(.filter-bar fieldset) { display: grid; grid-template-columns: minmax(220px, 1fr) minmax(220px, .6fr) auto; align-items: end; gap: 16px; }
@media (max-width: 767px) { .sources-page :deep(.filter-bar fieldset) { grid-template-columns: minmax(0, 1fr); } }
</style>
