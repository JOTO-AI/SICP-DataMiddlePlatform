<script setup lang="ts">
import { ref, reactive, watch, computed, onMounted, onActivated, onDeactivated } from 'vue'
import { request, describe, ApiError, type Envelope } from '../../api'
import { updateListPath, readUpdatePage, updateModes, updateStatuses, emptyUpdateFilters, type UpdatePage } from '../../app/updateEvents'
import { useRequestScope } from '../../composables/useRequestScope'
import ErrorState from '../ErrorState.vue'
import EmptyState from '../EmptyState.vue'
import StatusBadge from '../StatusBadge.vue'
import Timestamp from '../Timestamp.vue'
import CopyId from '../CopyId.vue'
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
      if ((f.mode ?? '') !== filters.mode || (f.status ?? '') !== filters.status || (f.task_id ?? '') !== filters.task_id || filters.from && (Date.parse(f.from) !== Date.parse(filters.from) || Date.parse(f.to) !== Date.parse(filters.to)) || next && page.value?.filter && JSON.stringify(f) !== JSON.stringify(page.value.filter)) throw new ApiError('更新事件筛选范围与当前查询不一致。', 'invalid_response')
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
<template><section class="panel update-history" aria-label="更新事件历史"><div class="section-bar"><h2>更新事件历史</h2><button type="button" :disabled="busy" @click="read()">重新读取事件首批</button></div><p class="hint">按服务端受理时间顺序读取，每批 50 条。阅读期间不自动刷新或重排；下方调度状态刷新不会改变此列表。筛选由后端执行，不提供总数；任务状态仍会随执行推进。</p><form class="event-filters" aria-label="更新事件筛选" @submit.prevent="read()"><label>受理开始<input v-model="filters.from" type="datetime-local" /></label><label>受理结束<input v-model="filters.to" type="datetime-local" /></label><label>更新模式<select v-model="filters.mode"><option value="">全部模式</option><option v-for="(title, mode) in updateModes" :key="mode" :value="mode">{{ title }}</option></select></label><label>更新结果<select v-model="filters.status"><option value="">全部状态</option><option v-for="(title, status) in updateStatuses" :key="status" :value="status">{{ title }}</option></select></label><label>定位任务号<input v-model="filters.task_id" maxlength="128" autocomplete="off" spellcheck="false" /></label><div class="actions"><button type="button" @click="recentWeek">填入最近 7 天</button><button type="button" @click="Object.assign(filters, emptyUpdateFilters())">重置筛选</button><button class="primary" type="submit" :disabled="busy">查询更新事件</button></div><p class="hint">时间留空查询服务端最近 24 小时，自定义最多 7 天，按受理时间筛选；输入时间使用浏览器时区。修改条件后重新查询。</p></form><ErrorState v-if="error" :message="error" /><p v-if="error && page" class="warning">保留上次成功读取的历史批次；当前读取未确认，可重新读取首批。</p><template v-if="page"><p class="hint"><template v-if="page.filter">受理范围 <Timestamp :value="page.filter.from" /> 至 <Timestamp :value="page.filter.to" /> · </template>第 {{ index + 1 }} 批 · 本批 {{ page.items.length }} 条 · 最近读取 <Timestamp :value="readAt" /></p><div v-if="page.items.length" class="table-wrap" tabindex="0" aria-label="更新事件表格"><table><thead><tr><th>事件 / 任务编号</th><th>模式</th><th>状态</th><th>实际开始</th><th>结束时间</th><th>详情</th></tr></thead><tbody><tr v-for="item in page.items" :key="item.id"><td><CopyId :value="item.id" label="更新事件号" /><small>关联任务：{{ item.task_id }}</small></td><td>{{ updateModes[item.mode] || item.mode }}</td><td><StatusBadge :value="item.status" /></td><td><Timestamp :value="item.started_at" empty="尚未确认" /></td><td><Timestamp :value="item.finished_at" empty="尚未确认" /></td><td><RouterLink :to="{ name: 'update-detail', params: { id: item.id } }">查看事件详情</RouterLink></td></tr></tbody></table></div><EmptyState v-else title="本批没有更新事件" description="仅表示此次查询没有返回事件，不推断系统从未运行。" /><div class="actions"><button type="button" :disabled="busy || index === 0" @click="index--">上一批事件</button><button type="button" :disabled="busy || expired || !page.next_cursor && index + 1 >= pages.length" @click="read(true)">下一批事件</button></div><p v-if="!page.next_cursor && index + 1 >= pages.length" class="hint">已到本次查询末批。</p></template><EmptyState v-else :loading="busy" title="尚未读取更新事件" /></section></template>
<style scoped>
.update-history { margin-block: 24px; } table { min-width: 850px; } .actions { margin-top: 16px; }
.event-filters { display: flex; flex-wrap: wrap; gap: 12px; margin-block: 16px; align-items: end; } .event-filters label { flex: 1 1 180px; min-width: 0; } .event-filters p { flex-basis: 100%; }
</style>