<script setup lang="ts">
import { ref, reactive, computed, onMounted, onActivated, onDeactivated } from 'vue'
import { request, describe, ApiError, type Envelope } from '../../api'
import { operationPath, readOperationPage, operationTitles, expandableOperation, type OperationNode } from '../../app/updateOperations'
import { useRequestScope } from '../../composables/useRequestScope'
import { operationKinds, operationStates, operationMeaning } from '../../app/updateMeaning'
import ErrorState from '../ErrorState.vue'
import Timestamp from '../Timestamp.vue'
import StatusBadge from '../StatusBadge.vue'
import JsonText from '../JsonText.vue'
const props = defineProps<{ taskId: string }>()
interface Branch { items: OperationNode[]; cursor: string | null; cursors: string[]; loaded: boolean; error: string; expired: boolean }
const branches = reactive(new Map<string, Branch>()), expanded = reactive(new Set<string>()), selected = ref<OperationNode | null>(null), busy = ref<string | null>(null)
const scope = useRequestScope(); let active = true
const branch = (parent: string): Branch => { if (!branches.has(parent)) branches.set(parent, { items: [], cursor: null, cursors: [], loaded: false, error: '', expired: false }); return branches.get(parent)! }
const rows = computed(() => {
  const out: { node?: OperationNode; parent: string; depth: number; key: string }[] = []
  function visit(parent: string, depth: number) {
    for (const node of branches.get(parent)?.items ?? []) {
      out.push({ node, parent, depth, key: parent + '/' + node.id })
      if (expanded.has(node.id) && expandableOperation(node)) visit(node.id, depth + 1)
    }
    out.push({ parent, depth, key: 'footer/' + parent })
  }
  visit('', 0); return out
})
async function load(parent = '', next = false) {
  if (!active || busy.value !== null) return
  const b = branch(parent), cursor = next ? b.cursor : ''
  if (next && (!cursor || b.expired)) return
  if (next && b.cursors.length >= 19) { b.error = '此分支最多读取 20 批，请重新加载操作树。'; return }
  const count = [...branches.values()].reduce((total, v) => total + v.items.length, 0)
  if (count - (next ? 0 : b.items.length) + 50 > 1000) { b.error = '操作树最多保留 1000 个节点，请重新加载操作树。'; return }
  const run = scope.begin(); busy.value = parent; b.error = ''
  try {
    const response = await request<Envelope<unknown>>('worker', operationPath(props.taskId, parent, cursor ?? ''), { signal: run.signal })
    if (!active || !run.current()) return
    const value = readOperationPage(response.body.data, parent)
    if (response.status !== 200 || response.body.code !== 'ok' || !value || next && (value.items.some(n => b.items.some(old => old.id === n.id)) || value.next_cursor && (value.next_cursor === cursor || b.cursors.includes(value.next_cursor)))) throw new ApiError('操作节点或游标不属于当前父节点，未拼接此批。', 'invalid_response')
    b.items = next ? [...b.items, ...value.items] : value.items; b.cursor = value.next_cursor; b.loaded = true; b.expired = false
    b.cursors = next ? [...b.cursors, cursor!] : []
  } catch (cause) { if (active && run.current()) { b.expired = cause instanceof ApiError && cause.code === 'revision_conflict'; b.error = b.expired ? '此分支游标已失效，请重新加载操作树。' : describe(cause) } }
  finally { if (run.current()) busy.value = null }
}
function toggle(node: OperationNode) {
  if (expanded.has(node.id)) { expanded.delete(node.id); if (busy.value === node.id || node.id === 'trace' && busy.value?.startsWith('trace:')) { scope.cancel(); busy.value = null } }
  else { expanded.add(node.id); if (!branch(node.id).loaded) void load(node.id) }
}
function reset() { scope.cancel(); busy.value = null; branches.clear(); expanded.clear(); selected.value = null; void load() }
function activate() { active = true; if (!branch('').loaded) void load() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; scope.cancel(); busy.value = null })
</script>
<template><section class="panel operation-tree" aria-label="更新操作树"><div class="section-bar"><h2>更新操作树</h2><button type="button" @click="reset">重新加载操作树</button></div><p class="hint">展开时读取对应阶段，每批 50 个节点。各恢复会话、物理副本和尝试独立保留；不把重复尝试累计为任务净进度。诊断读取失败不改变任务结果。</p><div class="operation-layout"><div class="operation-list" aria-label="操作节点"><template v-for="row in rows" :key="row.key"><div v-if="row.node" class="operation-row" :style="{ paddingInlineStart: row.depth * 12 + 'px' }"><button v-if="expandableOperation(row.node)" type="button" :aria-expanded="expanded.has(row.node.id)" :disabled="busy !== null && !expanded.has(row.node.id)" @click="toggle(row.node)">{{ expanded.has(row.node.id) ? '收起' : '展开' }} {{ operationKinds[row.node.id] || operationKinds[row.node.kind] || row.node.kind }}</button><button class="node-select" type="button" :aria-pressed="selected === row.node" @click="selected = row.node">查看节点 {{ row.node.id }}</button><StatusBadge :value="row.node.state" :labels="operationStates" /></div><div v-else class="branch-footer" :style="{ paddingInlineStart: row.depth * 12 + 'px' }"><ErrorState v-if="branches.get(row.parent)?.error" :message="branches.get(row.parent)!.error" /><p v-if="busy === row.parent" role="status">正在读取此分支…</p><p v-else-if="branches.get(row.parent)?.loaded && !branches.get(row.parent)?.items.length" class="hint">此分支未返回节点；不推断阶段已执行或成功。</p><button v-if="branches.get(row.parent)?.cursor" type="button" :disabled="busy !== null || branches.get(row.parent)?.expired" @click="load(row.parent, true)">加载更多 {{ operationTitles[row.parent] || row.parent || '根节点' }}</button><button v-if="branches.get(row.parent)?.error && !branches.get(row.parent)?.loaded" type="button" :disabled="busy !== null" @click="load(row.parent)">重试此分支</button></div></template></div><section class="node-detail" aria-label="操作节点详情"><template v-if="selected"><h3>{{ operationKinds[selected.kind] || selected.kind }}</h3><p class="hint">{{ operationMeaning(selected) }}</p><p>节点：{{ selected.id }}</p><p>事实来源：{{ selected.evidence }}</p><p>物理副本：{{ selected.instance ?? '未提供' }} · 槽位：{{ selected.slot ?? '未提供' }} · 代次：{{ selected.generation ?? '未提供' }}</p><p>开始：<Timestamp :value="selected.started_at" empty="未观测" /> · 结束：<Timestamp :value="selected.finished_at" empty="未观测" /></p><p>已记录数量：{{ selected.count ?? '未知' }}</p><template v-if="selected.build"><p>恢复会话：{{ selected.build.session_id }}</p><p>执行实例：{{ selected.build.instance }} · 所属代次：{{ selected.build.owner_generation }} · 第 {{ selected.build.attempt }} 次尝试</p><p>本次耗时（纳秒）：{{ selected.build.duration_ns ?? '未观测' }}</p><p>累计耗时（纳秒，含重复执行）：{{ selected.build.cumulative_ns ?? '未观测' }}</p></template><JsonText :text="JSON.stringify(selected, null, 2)" label="操作节点实际字段" /></template><p v-else class="hint">选择节点查看实际字段、时间、恢复会话及尝试信息。</p></section></div></section></template>
<style scoped>
.operation-tree { margin-block: 20px; } .operation-layout { display: grid; grid-template-columns: minmax(240px, 1fr) minmax(0, 1.4fr); gap: 20px; margin-top: 18px; } .operation-list, .node-detail { min-width: 0; overflow-wrap: anywhere; } .operation-row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; padding-block: 10px; border-bottom: 1px solid var(--line); } .node-select { text-align: start; overflow-wrap: anywhere; max-width: 100%; } .branch-footer { margin-block: 8px; } .node-detail > p { margin-block: 12px; } @media(max-width: 768px) { .operation-layout { grid-template-columns: minmax(0, 1fr); } }
</style>