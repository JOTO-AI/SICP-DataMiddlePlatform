<script setup lang="ts">
import { milliseconds } from "../../shared/duration"
import { ref, reactive, computed, onMounted, onActivated, onDeactivated } from 'vue'
import { request, describe, ApiError, type Envelope } from '../../api'
import { operationPath, readOperationPage, operationTitles, expandableOperation, type OperationNode } from '../../app/updateOperations'
import { useRequestScope } from '../../composables/useRequestScope'
import { operationKinds, operationStates, operationMeaning } from '../../app/updateMeaning'
import { sourceName } from '../../app/sourceNames'
import ErrorState from '../ErrorState.vue'
import Timestamp from '../Timestamp.vue'
import StatusBadge from '../StatusBadge.vue'
import JsonText from '../JsonText.vue'
const states = { ...operationStates, collected: '采集完成', ready: '构建就绪', building: '构建中', succeeded: '已完成', failed: '失败', running: '执行中', acquiring: '采集中', checking: '校验中', reusing: '复用中' }
const stateLabel = (state: string) => states[state as keyof typeof states] || state
const nodeTitle = (node: OperationNode) => node.kind === 'acquisition' && node.parent_operation_id ? sourceName(node.id) : (operationKinds[node.kind] || node.kind) + (node.instance ? ' · ' + node.instance : '')
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
async function showGroup(group: string) { if (!Object.hasOwn(operationTitles, group) || busy.value !== null) return; expanded.add(group); if (!branch(group).loaded) await load(group); const root = branches.get('')?.items.find(n => n.id === group); if (root) selected.value = root }
defineExpose({ showGroup })
function reset() { scope.cancel(); busy.value = null; branches.clear(); expanded.clear(); selected.value = null; void load() }
function activate() { active = true; if (!branch('').loaded) void load() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; scope.cancel(); busy.value = null })
</script>
<template>
<section class="operation-tree" aria-label="更新操作树">
  <div class="operation-layout">
    <aside class="operation-list" aria-label="操作节点">
      <div class="section-bar"><h2>事件操作树</h2><button class="reload-tree" type="button" @click="reset" aria-label="重新加载操作树" title="重新加载操作树">↻</button></div>
      <template v-for="row in rows" :key="row.key">
        <div v-if="row.node" class="operation-row" :class="{ selected: selected === row.node }" :style="{ paddingInlineStart: row.depth * 16 + 'px' }">
          <button v-if="expandableOperation(row.node)" class="branch-toggle" type="button" :aria-label="(expanded.has(row.node.id) ? '收起 ' : '展开 ') + (operationKinds[row.node.id] || operationKinds[row.node.kind] || row.node.kind)" :aria-expanded="expanded.has(row.node.id)" :disabled="busy !== null && !expanded.has(row.node.id)" @click="toggle(row.node)">{{ expanded.has(row.node.id) ? '⌄' : '›' }}</button><span v-else class="leaf-mark" aria-hidden="true"></span>
          <button class="node-select" type="button" :aria-label="'查看节点 ' + row.node.id" :aria-pressed="selected === row.node" @click="selected = row.node"><span class="node-title">{{ nodeTitle(row.node) }}</span><small>{{ stateLabel(row.node.state) }}</small></button>
        </div>
        <div v-else class="branch-footer" :style="{ paddingInlineStart: row.depth * 16 + 'px' }"><ErrorState v-if="branches.get(row.parent)?.error" :message="branches.get(row.parent)!.error" /><p v-if="busy === row.parent" role="status">正在读取此分支…</p><p v-else-if="branches.get(row.parent)?.loaded && !branches.get(row.parent)?.items.length" class="hint">此分支未返回节点；不推断阶段已执行或成功。</p><button v-if="branches.get(row.parent)?.cursor" type="button" :disabled="busy !== null || branches.get(row.parent)?.expired" @click="load(row.parent, true)">加载更多 {{ operationTitles[row.parent] || row.parent || '根节点' }}</button><button v-if="branches.get(row.parent)?.error && !branches.get(row.parent)?.loaded" type="button" :disabled="busy !== null" @click="load(row.parent)">重试此分支</button></div>
      </template>
      <p class="hint tree-note">展开阶段查看各次执行记录。</p>
    </aside>
    <section class="node-detail" aria-label="操作节点详情">
      <template v-if="selected">
        <div class="section-bar"><h2>{{ nodeTitle(selected) }}</h2><StatusBadge :value="selected.state" :labels="states" /></div>
        <dl class="node-facts"><div><dt>实例 / 槽位</dt><dd>{{ selected.instance ?? '未提供' }} / {{ selected.slot ?? '未提供' }}</dd></div><div><dt>目标代次</dt><dd>{{ selected.generation ?? '未提供' }}</dd></div><div><dt>开始时间</dt><dd><Timestamp :value="selected.started_at" empty="未观测" /></dd></div><div><dt>结束时间</dt><dd><Timestamp :value="selected.finished_at" empty="未观测" /></dd></div><div><dt>已记录数量</dt><dd>{{ selected.count ?? '未知' }}</dd></div><div><dt>本次耗时</dt><dd>{{ selected.build ? milliseconds(selected.build.duration_ns, '未观测') : '未观测' }}<template v-if="selected.build?.duration_ns != null"> ms</template></dd></div></dl>
        <p class="node-meaning hint">{{ operationMeaning(selected) }}</p>
        <details class="node-raw"><summary>节点标识与实际字段</summary><p>节点：{{ selected.id }}</p><p>事实来源：{{ selected.evidence }}</p><template v-if="selected.build"><p>恢复会话：{{ selected.build.session_id }}</p><p>执行实例：{{ selected.build.instance }} · 所属代次：{{ selected.build.owner_generation }} · 第 {{ selected.build.attempt }} 次尝试</p><p>累计耗时（ms，含重复执行）：{{ milliseconds(selected.build.cumulative_ns, '未观测') }}</p></template><JsonText :text="JSON.stringify(selected, null, 2)" label="操作节点实际字段" /></details>
      </template>
      <h2 v-else>任务执行</h2>
      <slot />
      <section class="build-targets" aria-label="本轮构建目标"><div class="section-bar"><h3>本轮构建目标</h3><button type="button" :disabled="busy !== null" @click="showGroup('builds')">读取构建目标</button></div><div v-if="branches.get('builds')?.items.length" class="table-wrap"><table><thead><tr><th>实例 / 槽位</th><th>目标代次</th><th>状态</th></tr></thead><tbody><tr v-for="node in branches.get('builds')!.items" :key="node.id"><td>{{ node.instance ?? '未提供' }} / {{ node.slot ?? '未提供' }}</td><td>{{ node.generation ?? '未提供' }}</td><td><StatusBadge :value="node.state" :labels="states" /></td></tr></tbody></table></div><p v-else class="hint">{{ branches.get('builds')?.loaded ? '本批未返回构建目标。' : '尚未读取构建目标。' }}</p><p class="hint">旧版本查询位置请查看“转移与发布”。构建就绪不等于数据已发布。</p></section>
    </section>
  </div>
</section>
</template>
<style scoped>
.operation-layout { display: grid; grid-template-columns: minmax(220px, .8fr) minmax(0, 1.7fr); }
.operation-list { padding: 20px 16px; border-inline-end: 1px solid var(--line); } .node-detail { padding: 20px 24px; } .operation-list, .node-detail { min-width: 0; overflow-wrap: anywhere; }
.operation-list h2 { font-size: 16px; } .reload-tree, .branch-toggle { border: 0; background: transparent; padding: 0; min-width: 26px; }
.operation-row { display: flex; align-items: center; gap: 6px; border-radius: var(--radius-control); margin-block: 3px; } .operation-row.selected { background: var(--selected); color: var(--brand); } .node-select { border: 0; background: transparent; text-align: left; white-space: normal; padding: 8px 6px; flex: 1; min-width: 0; } .node-select small { margin-top: 2px; } .node-title { color: inherit; } .leaf-mark { width: 8px; height: 8px; border: 1px solid var(--control); border-radius: 50%; margin-inline: 9px; flex-shrink: 0; }
.branch-footer { margin-block: 8px; } .tree-note { margin-top: 20px; }
.node-facts { display: grid; grid-template-columns: 1fr 1fr; border: 1px solid var(--line); margin: 0; } .node-facts > div { display: grid; grid-template-columns: minmax(80px, .8fr) minmax(0, 1.2fr); border-bottom: 1px solid var(--line); font-size: 12px; } .node-facts dt { background: var(--surface); padding: 8px; } .node-facts dd { padding: 8px; text-align: left; }
.node-meaning { margin-top: 12px; } .node-raw { margin-block: 12px 20px; } .build-targets { margin-top: 24px; } .build-targets > p { margin-top: 10px; }
@media(max-width: 800px) { .operation-layout { grid-template-columns: minmax(0, 1fr); } .operation-list { border-inline-end: 0; border-bottom: 1px solid var(--line); } .node-detail { padding: 20px 16px; } }
@media(max-width: 500px) { .node-facts { grid-template-columns: minmax(0, 1fr); } }
</style>
