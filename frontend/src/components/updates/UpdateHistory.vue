<script setup lang="ts">
import { ref, computed, onMounted, onActivated, onDeactivated } from 'vue'
import { request, describe, ApiError, type Envelope } from '../../api'
import { updateListPath, readUpdatePage, updateModes, type UpdatePage } from '../../app/updateEvents'
import { useRequestScope } from '../../composables/useRequestScope'
import ErrorState from '../ErrorState.vue'
import EmptyState from '../EmptyState.vue'
import StatusBadge from '../StatusBadge.vue'
import Timestamp from '../Timestamp.vue'
import CopyId from '../CopyId.vue'
const pages = ref<UpdatePage[]>([]), index = ref(0), busy = ref(false), error = ref(''), readAt = ref('')
const page = computed(() => pages.value[index.value]), scope = useRequestScope(), cursors = new Set<string>()
let active = true
async function read(next = false) {
  if (!active || busy.value) return
  if (next && index.value + 1 < pages.value.length) { index.value++; return }
  const cursor = next ? page.value?.next_cursor : ''
  if (next && !cursor) return
  if (next && pages.value.length >= 20) { error.value = '本次最多保留 20 批 / 1000 条，请重新读取首批。'; return }
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', updateListPath(cursor ?? ''), { signal: run.signal })
    if (!active || !run.current()) return
    const value = readUpdatePage(response.body.data)
    const seen = new Set(pages.value.flatMap(page => page.items.map(item => item.id)))
    if (response.status !== 200 || response.body.code !== 'ok' || !value || next && (value.items.some(item => seen.has(item.id)) || value.next_cursor && (value.next_cursor === cursor || cursors.has(value.next_cursor)))) throw new ApiError('更新事件响应标识、记录或游标不符合当前分页链。', 'invalid_response')
    if (next) { cursors.add(cursor!); pages.value.push(value); index.value++ }
    else { pages.value = [value]; index.value = 0; cursors.clear() }
    readAt.value = new Date().toISOString()
  } catch (cause) { if (active && run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
function activate() { active = true; if (!pages.value.length) void read() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; scope.cancel(); busy.value = false })
</script>
<template><section class="panel update-history" aria-label="更新事件历史"><div class="section-bar"><h2>更新事件历史</h2><button type="button" :disabled="busy" @click="read()">重新读取事件首批</button></div><p class="hint">按服务端受理时间顺序读取，每批 50 条。阅读期间不自动刷新或重排；下方调度状态刷新不会改变此列表。当前接口不提供总数及全历史筛选。</p><ErrorState v-if="error" :message="error" /><p v-if="error && page" class="warning">保留上次成功读取的历史批次；当前读取未确认，可重新读取首批。</p><template v-if="page"><p class="hint">第 {{ index + 1 }} 批 · 本批 {{ page.items.length }} 条 · 最近读取 <Timestamp :value="readAt" /></p><div v-if="page.items.length" class="table-wrap" tabindex="0" aria-label="更新事件表格"><table><thead><tr><th>事件 / 任务编号</th><th>模式</th><th>状态</th><th>实际开始</th><th>结束时间</th><th>详情</th></tr></thead><tbody><tr v-for="item in page.items" :key="item.id"><td><CopyId :value="item.id" label="更新事件号" /><small>关联任务：{{ item.task_id }}</small></td><td>{{ updateModes[item.mode] || item.mode }}</td><td><StatusBadge :value="item.status" /></td><td><Timestamp :value="item.started_at" empty="尚未确认" /></td><td><Timestamp :value="item.finished_at" empty="尚未确认" /></td><td><RouterLink :to="{ name: 'update-detail', params: { id: item.id } }">查看事件详情</RouterLink></td></tr></tbody></table></div><EmptyState v-else title="本批没有更新事件" description="仅表示此次查询没有返回事件，不推断系统从未运行。" /><div class="actions"><button type="button" :disabled="busy || index === 0" @click="index--">上一批事件</button><button type="button" :disabled="busy || !page.next_cursor && index + 1 >= pages.length" @click="read(true)">下一批事件</button></div><p v-if="!page.next_cursor && index + 1 >= pages.length" class="hint">已到本次查询末批。</p></template><EmptyState v-else :loading="busy" title="尚未读取更新事件" /></section></template>
<style scoped>
.update-history { margin-block: 24px; } table { min-width: 850px; } .actions { margin-top: 16px; }
</style>