<script setup lang="ts">
import { computed, ref, onDeactivated, onUnmounted } from 'vue'
import { request, describe, time, type Envelope } from './api'
interface Task { task_id: string; mode: string; status: string; operator?: string; predecessor_id?: string; accepted_at: string; started_at?: string; ready_at?: string; published_at?: string; checked_at?: string; finished_at?: string; progress_confirmed?: boolean; confirmed_until?: Record<string, string>; counts?: Record<string, number>; failure?: string }
const actions = [{ id: 'sync', title: '增量同步', text: '采集源数据变化，完成校验后发布。' }, { id: 'force-full', title: '强制全量', text: '重新采集并构建完整数据，即使内容未变化。' }, { id: 'initialize', title: '首次初始化', text: '仅用于尚未建立完整数据的环境。' }, { id: 'rerun', title: '失败重跑', text: '根据失败任务创建关联的新任务。' }]
const action = ref('sync'), predecessor = ref(''), key = ref(crypto.randomUUID()), lookup = ref('')
const task = ref<Task | null>(null), busy = ref(false), loading = ref(false), error = ref(''), taskError = ref(''), receipt = ref(''), auto = ref(false)
const dialog = ref<HTMLDialogElement | null>(null)
const selected = computed(() => actions.find(a => a.id === action.value)!)
// Preserve the exact operation and key until a receipt is obtained or explicitly abandoned.
const pending = ref<{ path: string; key: string; title: string } | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined
let active = true
const terminal = computed(() => !!task.value?.finished_at || ['success_published', 'success_unchanged', 'failed'].includes(task.value?.status || ''))
const statusLabels: Record<string, string> = { accepted: '已受理', running: '执行中', success_published: '发布成功', success_unchanged: '检查完成，无变化', failed: '执行失败' }
const stages = computed(() => task.value ? [{ name: '已受理', at: task.value.accepted_at }, { name: '开始执行', at: task.value.started_at }, { name: '构建就绪', at: task.value.ready_at }, { name: '发布完成', at: task.value.published_at }, { name: '检查完成', at: task.value.checked_at }, { name: '任务结束', at: task.value.finished_at }] : [])
function stop() { auto.value = false; clearTimeout(timer) }
onDeactivated(() => { active = false; stop() })
onUnmounted(() => { active = false; stop() })
function confirm() {
  stop()
  error.value = ''
  if (!pending.value) {
    if (!/^[\x21-\x7e]{1,128}$/.test(key.value) || key.value.includes(',')) { error.value = '幂等键须为 1–128 位可见 ASCII 字符，且不含逗号。'; return }
    pending.value = { path: action.value === 'rerun' ? `/admin/v1/tasks/${encodeURIComponent(predecessor.value.trim())}/rerun` : `/admin/v1/tasks/${action.value}`, key: key.value, title: selected.value.title }
  }
  dialog.value?.showModal()
}
function abandon() { pending.value = null; key.value = crypto.randomUUID(); error.value = ''; receipt.value = '' }
async function submit() {
  if (!pending.value) return
  dialog.value?.close(); busy.value = true; error.value = ''; receipt.value = ''
  try {
    const response = await request<Envelope<Task>>('worker', pending.value.path, { body: {}, key: pending.value.key })
    lookup.value = response.body.data.task_id
    receipt.value = `任务已受理：${lookup.value}。受理不代表执行完成。`
    pending.value = null; key.value = crypto.randomUUID()
    await fetchTask()
  } catch (e) { error.value = `${describe(e)} 原请求已保留；再次提交将使用相同操作与幂等键。` }
  finally { busy.value = false }
}
async function fetchTask() {
  if (loading.value || !lookup.value.trim()) return
  clearTimeout(timer); active = true; loading.value = true; taskError.value = ''; task.value = null
  try {
    const response = await request<Envelope<Task>>('worker', `/admin/v1/tasks/${encodeURIComponent(lookup.value.trim())}`)
    task.value = response.body.data
    if (terminal.value) stop()
  } catch (e) { taskError.value = describe(e); stop() }
  finally { loading.value = false; if (active && auto.value) timer = setTimeout(fetchTask, 5000) }
}
function toggleAuto() { if (auto.value) { active = true; void fetchTask() } else stop() }
</script>
<template>
  <section>
    <div class="page-heading"><div><h1>更新任务</h1><p>提交数据更新，并按任务号追踪后端执行结果。</p></div><span class="tag">管理权限</span></div>
    <div class="task-layout">
      <form class="panel" @submit.prevent="confirm">
        <h2>发起更新</h2><p class="muted">每次更新由后台异步执行。</p>
        <fieldset :disabled="busy || !!pending"><legend>更新方式</legend><label v-for="item in actions" :key="item.id" class="radio-row"><input v-model="action" type="radio" :value="item.id" name="action"><span><strong>{{ item.title }}</strong><small>{{ item.text }}</small></span></label>
        <label v-if="action === 'rerun'">原失败任务号 *<input v-model="predecessor" required placeholder="输入失败任务号"></label>
        <label class="key-label">幂等键<input v-model="key" required maxlength="128" spellcheck="false"></label></fieldset>
        <p class="hint">响应丢失时用原键重试，可避免重复创建任务。</p>
        <p v-if="pending" class="hint">已锁定请求：{{ pending.title }}。修改操作前请先结束本次请求。</p>
        <div class="actions"><button class="primary" :disabled="busy || loading">{{ busy ? '正在提交…' : pending ? '重试原请求' : '提交更新' }}</button><button v-if="pending" type="button" :disabled="busy" @click="abandon">结束本次请求</button></div>
        <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="receipt" class="success-text" role="status">{{ receipt }}</p>
      </form>
      <div class="task-detail">
        <form class="lookup" @submit.prevent="stop(); fetchTask()"><label for="task-id">查找任务</label><div class="actions"><input id="task-id" v-model="lookup" required :disabled="loading || busy" placeholder="输入完整任务号" @input="stop(); task = null; taskError = ''"><button :disabled="loading || busy">{{ loading ? '查询中…' : '查看任务' }}</button></div></form>
        <p class="hint">当前接口按任务号查询。本页面不提供服务器历史任务列表。</p>
        <p v-if="taskError" class="error" role="alert">{{ taskError }}</p>
        <template v-if="task">
          <div class="section-bar"><h2>执行详情</h2><span class="tag" :class="{ warning: task.status === 'failed', success: task.status.startsWith('success_') }">{{ statusLabels[task.status] || task.status }}</span></div>
          <p class="task-id">{{ task.task_id }}</p>
          <label class="check-row"><input type="checkbox" v-model="auto" :disabled="terminal || busy || loading" @change="toggleAuto">每 5 秒刷新，任务结束或查询失败后停止</label>
          <dl class="facts"><div><dt>更新模式</dt><dd>{{ task.mode }}</dd></div><div><dt>操作人</dt><dd>{{ task.operator || '未提供' }}</dd></div><div><dt>进度确认</dt><dd>{{ task.progress_confirmed ? '已确认' : '未确认' }}</dd></div><div v-if="task.predecessor_id"><dt>前序任务</dt><dd>{{ task.predecessor_id }}</dd></div></dl>
          <p v-if="task.failure" class="error">失败原因：{{ task.failure }}</p>
          <ol class="timeline"><li v-for="stage in stages" :key="stage.name" :class="{ done: stage.at }"><span class="timeline-dot" aria-hidden="true"></span><span>{{ stage.name }}</span><time>{{ time(stage.at) }}</time></li></ol>
          <p class="hint">未发生的阶段不代表失败；无变化检查可能无需重新发布。</p>
          <details class="raw"><summary>查看完整任务结果与源进度</summary><pre>{{ JSON.stringify(task, null, 2) }}</pre></details>
        </template>
        <div v-else class="empty" role="status"><span aria-hidden="true">↻</span><h3>{{ loading ? '正在读取任务' : '等待查看任务' }}</h3><p>提交更新后自动查看，也可输入已有任务号。</p></div>
      </div>
    </div>
    <dialog ref="dialog" aria-labelledby="confirm-title"><h2 id="confirm-title">确认{{ pending?.title }}</h2><p>此操作将请求后台执行数据更新。{{ action === 'force-full' ? '强制全量会重新采集和构建完整数据。' : '' }}</p><p class="hint">幂等键：{{ pending?.key }}</p><div class="actions"><button autofocus @click="dialog?.close()">暂不提交</button><button class="primary" @click="submit">确认提交更新</button></div></dialog>
  </section>
</template>
