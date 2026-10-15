<script setup lang="ts">
import { ref, reactive, watch, computed, onMounted, onActivated, onDeactivated } from 'vue'
import { request, describe, ApiError, type Envelope } from '../../api'
import { updateListPath, readUpdatePage, updateModes, updateStatuses, emptyUpdateFilters, type UpdatePage } from '../../app/updateEvents'
import { useRequestScope } from '../../composables/useRequestScope'
import { timestampMicros } from '../../shared/timestamp'
import ErrorState from '../ErrorState.vue'
import EmptyState from '../EmptyState.vue'
import StatusBadge from '../StatusBadge.vue'
import Timestamp from '../Timestamp.vue'
import { useRouter } from 'vue-router'
import { emptyLogFilters } from '../../app/logHistory'
import { prepareTestLogs } from '../../app/testLogNavigation'
import { updateDuration } from '../../app/updateEvents'
const router = useRouter(), timeRange = ref('day')
async function openLogs(id: string) {
  if (prepareTestLogs({ ...emptyLogFilters('platform'), identityField: 'task_id', identity: id, format: 'raw' })) await router.push({ name: 'log-source', params: { source: 'platform' } })
}
function changeRange() {
  filters.from = ''; filters.to = ''
  if (timeRange.value === 'week') recentWeek()
}
function resetFilters() { timeRange.value = 'day'; Object.assign(filters, emptyUpdateFilters()) }
const pages = ref<UpdatePage[]>([]), index = ref(0), busy = ref(false), error = ref(''), readAt = ref('')
const page = computed(() => pages.value[index.value]), scope = useRequestScope(), cursors = new Set<string>()
const filters = reactive(emptyUpdateFilters()), expired = ref(false)
let active = true
watch(filters, () => { scope.cancel(); pages.value = []; index.value = 0; cursors.clear(); busy.value = false; error.value = ''; expired.value = false }, { flush: 'sync' })
function recentWeek() {
  const end = new Date(), start = new Date(end.getTime() - 7 * 86400000)
  const local = (value: Date) => new Date(value.getTime() - value.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
  filters.from = local(start); filters.to = local(end)
}
async function read(next = false) {
  if (!active || busy.value || next && expired.value) return
  if (next && index.value + 1 < pages.value.length) { index.value++; return }
  const cursor = next ? page.value?.next_cursor : ''
  if (next && !cursor) return
  if (next && pages.value.length >= 20) { error.value = '本次最多保留 20 批 / 1000 条，请重新读取首批。'; return }
  let path: string
  try { path = updateListPath(cursor ?? '', filters) } catch (cause) { error.value = cause instanceof Error ? cause.message : '更新筛选无效。'; return }
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', path, { signal: run.signal })
    if (!active || !run.current()) return
    const value = readUpdatePage(response.body.data)
    if (!value?.filter && (Object.values(filters).some(Boolean) || next && page.value?.filter)) throw new ApiError('更新事件未返回已提交的筛选范围。', 'invalid_response')
    if (value?.filter) {
      const f = value.filter
      if ((f.mode ?? '') !== filters.mode || (f.status ?? '') !== filters.status || (f.task_id ?? '') !== filters.task_id || filters.from && (timestampMicros(f.from) !== timestampMicros(new Date(filters.from).toISOString()) || timestampMicros(f.to) !== timestampMicros(new Date(filters.to).toISOString())) || next && page.value?.filter && JSON.stringify(f) !== JSON.stringify(page.value.filter)) throw new ApiError('更新事件筛选范围与当前查询不一致。', 'invalid_response')
    }
    const seen = new Set(pages.value.flatMap(page => page.items.map(item => item.id)))
    if (response.status !== 200 || response.body.code !== 'ok' || !value || next && (value.items.some(item => seen.has(item.id)) || value.next_cursor && (value.next_cursor === cursor || cursors.has(value.next_cursor)))) throw new ApiError('更新事件响应标识、记录或游标不符合当前分页链。', 'invalid_response')
    if (next) { cursors.add(cursor!); pages.value.push(value); index.value++ }
    else { pages.value = [value]; index.value = 0; cursors.clear() }
    expired.value = false; readAt.value = new Date().toISOString()
  } catch (cause) { if (active && run.current()) { expired.value = cause instanceof ApiError && cause.code === 'revision_conflict'; error.value = expired.value ? '更新事件游标已失效，请重新读取首批；不会自动拼接新旧范围。' : describe(cause) } }
  finally { if (run.current()) busy.value = false }
}
function activate() { active = true; if (!pages.value.length) void read() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; scope.cancel(); busy.value = false })
</script>
<template>
  <section class="update-history" aria-label="更新事件历史">
    <form class="panel event-filters" aria-label="更新事件筛选" @submit.prevent="read()">
      <div class="filter-row">
        <label>时间<select v-model="timeRange" @change="changeRange"><option value="day">最近 24 小时</option><option value="week">最近 7 天</option><option value="custom">自定义时间</option></select></label>
        <label>更新模式<select v-model="filters.mode"><option value="">全部模式</option><option v-for="(title, mode) in updateModes" :key="mode" :value="mode">{{ title }}</option></select></label>
        <label>更新结果<select v-model="filters.status"><option value="">全部状态</option><option v-for="(title, status) in updateStatuses" :key="status" :value="status">{{ title }}</option></select></label>
        <label class="identity-filter">定位任务号<input v-model="filters.task_id" maxlength="128" placeholder="请输入事件号 / 任务号" autocomplete="off" spellcheck="false" /></label>
        <button class="primary" type="submit" :disabled="busy">查询更新事件</button>
      </div>
      <div v-if="timeRange === 'custom'" class="custom-range">
        <label>受理开始<input v-model="filters.from" type="datetime-local" /></label><label>受理结束<input v-model="filters.to" type="datetime-local" /></label>
        <p class="hint">按受理时间筛选，最多 7 天；输入时间使用浏览器时区。</p>
      </div>
      <div v-if="timeRange !== 'day' || filters.mode || filters.status || filters.task_id" class="filter-reset"><button type="button" @click="resetFilters">重置筛选</button><span class="hint">修改条件后重新查询。</span></div>
    </form>
    <ErrorState v-if="error" :message="error" />
    <p v-if="error && page" class="warning">保留上次成功读取的历史批次；当前读取未确认，可重新读取首批。</p>
    <section class="panel event-results" aria-label="事件查询结果" :aria-busy="busy">
      <div class="result-toolbar"><p class="hint">列表不自动刷新，查看任务详情可跟踪执行进度。</p><button type="button" :disabled="busy" @click="read()">重新读取事件首批</button></div>
      <template v-if="page">
        <div v-if="page.items.length" class="table-wrap" tabindex="0" aria-label="更新事件表格">
          <table><thead><tr><th>事件号 / 任务号</th><th>模式</th><th>结果</th><th>开始时间</th><th>耗时</th><th>操作</th></tr></thead>
            <tbody><tr v-for="item in page.items" :key="item.id">
              <td class="event-identity"><RouterLink :to="{ name: 'update-detail', params: { id: item.id } }"><code>{{ item.id }}</code></RouterLink><small>事件号与任务号相同</small></td>
              <td>{{ updateModes[item.mode] || item.mode }}</td><td><StatusBadge :value="item.status" /></td>
              <td><Timestamp :value="item.started_at" empty="尚未确认" /></td><td class="event-duration">{{ updateDuration(item) }}</td>
              <td><div class="row-actions"><RouterLink :to="{ name: 'update-detail', params: { id: item.id } }">查看事件详情</RouterLink><button type="button" @click="openLogs(item.task_id)">查看日志</button></div></td>
            </tr></tbody>
          </table>
        </div>
        <EmptyState v-else title="本批没有更新事件" description="可调整筛选条件或重新读取。" />
        <div class="pagination"><p class="hint">第 {{ index + 1 }} 批 · 本批 {{ page.items.length }} 条</p><div class="actions"><button type="button" :disabled="busy || index === 0" @click="index--">上一批事件</button><span class="batch-index" aria-label="当前批次">{{ index + 1 }}</span><button type="button" :disabled="busy || expired || !page.next_cursor && index + 1 >= pages.length" @click="read(true)">下一批事件</button><span class="hint">每批 50 条</span></div></div>
        <div class="read-meta hint"><span v-if="page.filter">受理范围 <Timestamp :value="page.filter.from" /> 至 <Timestamp :value="page.filter.to" /></span><span>最近读取 <Timestamp :value="readAt" /></span><span v-if="!page.next_cursor && index + 1 >= pages.length">已到本次查询末批。</span></div>
      </template>
      <EmptyState v-else :loading="busy" title="尚未读取更新事件" description="设置筛选条件后点击查询。" />
    </section>
  </section>
</template>
<style scoped>
.update-history { display: flex; flex-direction: column; gap: 20px; }
.filter-row { display: grid; grid-template-columns: 1fr 1fr 1fr minmax(180px, 1.4fr) auto; align-items: end; gap: 16px; }
.filter-row label, .custom-range label { font-size: 12px; color: var(--muted); min-width: 0; }
.filter-row input, .filter-row select, .filter-row button { min-height: 42px; }
.custom-range { display: flex; flex-wrap: wrap; align-items: end; gap: 16px; margin-top: 16px; }
.filter-reset { display: flex; align-items: center; gap: 12px; margin-top: 12px; }
.filter-reset button, .row-actions button { padding: 0; border: 0; background: transparent; color: var(--brand); min-height: 32px; font-weight: 550; }
.result-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 16px; margin-bottom: 16px; }
table { min-width: 820px; } td { vertical-align: middle; height: 68px; }
.event-identity { width: 26%; } .event-identity a { color: var(--ink); text-decoration: none; } .event-identity a:hover { color: var(--brand); text-decoration: underline; }
.event-duration { white-space: nowrap; font-variant-numeric: tabular-nums; }
.row-actions { display: flex; gap: 16px; align-items: center; white-space: nowrap; } .row-actions a { text-decoration: none; }
.row-actions button { font-size: inherit; } .row-actions button:hover, .row-actions a:hover { text-decoration: underline; }
.batch-index { display: grid; place-items: center; width: 36px; min-height: 38px; border-radius: var(--radius-control); color: var(--white); background: var(--brand); font-variant-numeric: tabular-nums; }
.read-meta { display: flex; flex-wrap: wrap; gap: 8px 20px; margin-top: 12px; }
@media (max-width: 1150px) { .filter-row { grid-template-columns: repeat(3, minmax(0, 1fr)); } .identity-filter { grid-column: span 2; } }
@media (max-width: 600px) { .filter-row { grid-template-columns: minmax(0, 1fr); } .identity-filter { grid-column: auto; } .result-toolbar, .pagination { align-items: flex-start; flex-direction: column; } .pagination .actions { flex-wrap: wrap; } .event-filters, .event-results { padding: 16px; } .custom-range label { width: 100%; } }
</style>
