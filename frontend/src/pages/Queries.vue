<script setup lang="ts">
import { computed, reactive, ref, watch, onActivated, onDeactivated, onUnmounted } from 'vue'
import { request, describe, time, type Envelope } from '../api'
import { useRequestScope } from '../composables/useRequestScope'
import { operations, queryBody, cell } from '../queryOperations'
const index = ref(0)
const op = computed(() => operations[index.value]!)
const form = reactive({ values: Object.fromEntries(operations[0]!.fields.map(field => [field.key, ''])) as Record<string, string>, size: 20, advanced: '{}' })
const busy = ref(false), error = ref(''), snapshot = ref(''), page = ref(1), requestID = ref('')
const result = ref<Envelope<{ items: any[]; total?: number; synced_at: string }> | null>(null)
const requests = useRequestScope()
let active = false
onActivated(() => { active = true })
function deactivate() { active = false; requests.cancel(); busy.value = false }
onDeactivated(deactivate)
onUnmounted(deactivate)
let submitted: Record<string, unknown> = {}
const maxPage = computed(() => Math.max(1, Math.ceil((result.value?.data.total || 0) / Number(submitted.page_size || 20))))
const advancedKeys = computed(() => op.value.advanced)
function reset() { requests.cancel(); busy.value = false; result.value = null; snapshot.value = ''; error.value = ''; requestID.value = ''; page.value = 1 }
watch(index, () => { form.values = Object.fromEntries(op.value.fields.map(field => [field.key, ''])); form.advanced = '{}'; reset() })
watch(form, reset)
async function query(next?: number) {
  if (!active || busy.value) return
  const fresh = next === undefined
  next ??= 1
  error.value = ''
  if (fresh) {
    reset()
    try {
      submitted = queryBody(op.value, form.values, form.advanced, Number(form.size))
    } catch { error.value = '请填写必填条件；高级筛选须为 JSON 对象，且只能包含下方列出的字段。'; return }
  }
  const run = requests.begin()
  busy.value = true
  try {
    const response = await request<NonNullable<typeof result.value>>('query', op.value.path, { body: { ...submitted, ...(op.value.paginated ? { page: next } : {}) }, snapshot: fresh ? '' : snapshot.value, signal: run.signal })
    if (!active || !run.current()) return
    result.value = response.body; snapshot.value = response.snapshot; requestID.value = response.requestID || response.body.request_id || ''; page.value = next
  } catch (e) { if (!active || !run.current()) return; result.value = null; snapshot.value = ''; requestID.value = ''; error.value = describe(e) }
  finally { if (run.current()) busy.value = false }
}
</script>
<template>
  <section>
    <div class="page-heading"><div><h1>业务查询</h1><p>核验已发布的组织、人员、成绩、课表与教学班数据。</p></div><span class="tag">只读查询</span></div>
    <div class="tabs query-tabs" aria-label="查询接口"><button v-for="(item, i) in operations" :key="item.path" :class="{ active: index === i }" :aria-pressed="index === i" :disabled="busy" @click="index = i">{{ item.title }}</button></div>
    <div class="endpoint"><span class="method">POST</span><code>{{ op.path }}</code></div>
    <form class="panel query-form" @submit.prevent="query()">
      <fieldset :disabled="busy"><div class="form-grid">
        <label v-for="field in op.fields" :key="field.key">{{ field.label }}{{ field.required ? ' *' : '（可选）' }}
          <select v-if="field.semester" v-model="form.values[field.key]" :required="field.required"><option value="">{{ field.required ? '请选择学期' : '全部学期' }}</option><option value="1">第一学期</option><option value="2">第二学期</option></select>
          <input v-else v-model="form.values[field.key]" :required="field.required" :pattern="field.pattern" :placeholder="field.placeholder || `请输入${field.label}`" autocomplete="off">
        </label>
      </div>
      <details v-if="advancedKeys.length" class="advanced"><summary>高级筛选</summary><p class="hint">JSON 字段：{{ advancedKeys.join('、') }}。编码集合填写字符串数组，数值字段填写数字，is_active 填写 true 或 false。</p><label>高级筛选 JSON<textarea v-model="form.advanced" rows="4" spellcheck="false"></textarea></label></details>
      <div class="form-footer"><span class="hint">{{ op.operation === 'students.grades.batch-query' ? '不填写学号时，查询所选学年内符合筛选条件的成绩。' : ['organizations', 'students', 'staff'].includes(op.kind) ? '不填写条件时查询全部状态的数据。局部查询不能作为全量同步结果。' : '查询结果来自当前已发布的数据组合。' }}</span><div class="actions"><label v-if="op.paginated" class="inline-label">每页<select v-model="form.size"><option :value="20">20 条</option><option :value="50">50 条</option><option :value="100">100 条</option><option :value="1000">1000 条</option></select></label><button class="primary">{{ busy ? '正在查询…' : '查询数据' }}</button></div></div>
      </fieldset>
    </form>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="section-bar result-heading"><h2>查询结果</h2><span v-if="result" class="muted">{{ op.paginated ? `共 ${result.data.total} 条` : `${result.data.items.length} 条排课记录` }}</span></div>
    <div v-if="!result" class="empty" role="status"><span aria-hidden="true">⌕</span><h3>{{ busy ? '正在读取数据' : '等待查询' }}</h3><p>{{ busy ? '正在核对筛选条件并读取已发布数据。' : '填写上方条件，查询结果将在这里展示。' }}</p></div>
    <template v-else>
      <div class="result-meta"><span>同步时间 {{ time(result.data.synced_at) }}</span><span>请求号 <code>{{ requestID || '未返回' }}</code></span></div>
      <p v-if="!result.data.items.length" class="empty">没有符合条件的数据，请调整筛选条件后重新查询。</p>
      <div v-else class="table-wrap" tabindex="0" aria-label="查询结果表格，可横向滚动">
        <table v-if="op.kind === 'grades'"><thead><tr><th>学号 / 姓名</th><th>课程</th><th>学年 / 学期</th><th class="numeric">成绩</th><th class="numeric">绩点</th><th class="numeric">学分</th></tr></thead><tbody><tr v-for="row in result.data.items" :key="row.grade_id"><td>{{ row.student.student_number }}<small>{{ row.student.name ?? '姓名未提供' }}</small></td><td>{{ row.course.course_name ?? '课程名未提供' }}<small>{{ row.course.course_code }}</small></td><td>{{ row.academic_year }}<small>第 {{ row.semester }} 学期</small></td><td class="numeric">{{ row.score ?? '—' }}</td><td class="numeric">{{ row.grade_point ?? '—' }}</td><td class="numeric">{{ row.credits ?? '—' }}</td></tr></tbody></table>
        <table v-else-if="op.kind === 'timetable'"><thead><tr><th>课程 / 教学班</th><th>教师</th><th>星期 / 节次</th><th>周次</th><th>地点</th></tr></thead><tbody><tr v-for="(row, i) in result.data.items" :key="i"><td>{{ row.course_name ?? row.course_code }}<small>{{ row.teaching_class_name ?? row.teaching_class_number }}</small></td><td>{{ row.teacher_names.join('、') || '—' }}</td><td>星期{{ ['日','一','二','三','四','五','六','日'][row.weekday] ?? row.weekday }}<small>第 {{ row.sections.join('、') }} 节</small></td><td>{{ row.weeks.join('、') || '—' }}</td><td>{{ row.location ?? '—' }}</td></tr></tbody></table>
        <table v-else><thead><tr><th v-for="column in op.columns" :key="column.key">{{ column.title }}</th></tr></thead><tbody><tr v-for="(row, i) in result.data.items" :key="i"><td v-for="column in op.columns" :key="column.key">{{ cell(row, column.key) }}</td></tr></tbody></table>
      </div>
      <div v-if="op.paginated" class="pagination"><span class="hint">{{ op.kind === 'grades' ? '“—” 表示未提供数值，可能为文字成绩。' : '缺失资料显示“未提供”，不会移除对应编号。' }}翻页沿用本次快照。</span><div class="actions"><button :disabled="busy || page <= 1 || !snapshot" @click="query(page - 1)">上一页</button><span>{{ page }} / {{ maxPage }}</span><button :disabled="busy || page >= maxPage || !snapshot" @click="query(page + 1)">下一页</button></div></div>
      <details class="raw"><summary>查看完整 JSON 响应</summary><pre>{{ JSON.stringify(result, null, 2) }}</pre></details>
    </template>
  </section>
</template>
