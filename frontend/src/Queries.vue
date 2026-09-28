<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { request, describe, time, type Envelope } from './api'
const operations = [
  { title: '学生成绩', path: '/api/v1/sicp/students/grades/query', identity: 'student_number', label: '学号', grade: true },
  { title: '批量成绩', path: '/api/v1/sicp/students/grades/batch-query', identity: 'student_numbers', label: '学号列表', grade: true },
  { title: '学生课表', path: '/api/v1/sicp/students/timetable/query', identity: 'student_number', label: '学号', grade: false },
  { title: '教师课表', path: '/api/v1/sicp/staff/timetable/query', identity: 'staff_number', label: '工号', grade: false },
]
const index = ref(0)
const op = computed(() => operations[index.value]!)
const form = reactive({ identity: '', year: '', semester: '', course: '', size: 20, advanced: '{}' })
const busy = ref(false), error = ref(''), snapshot = ref(''), page = ref(1), requestID = ref('')
const result = ref<Envelope<{ items: any[]; total?: number; synced_at: string }> | null>(null)
let submitted: Record<string, unknown> = {}
const maxPage = computed(() => Math.max(1, Math.ceil((result.value?.data.total || 0) / Number(submitted.page_size || 20))))
const advancedKeys = computed(() => op.value.grade ? ['course_code', 'course_attribute_codes', 'course_nature_codes', 'course_type_codes', 'course_department_code', 'course_department_name', 'score_min', 'score_max', ...(index.value === 1 ? ['student_department_code', 'student_department_name', 'class_code', 'class_name'] : [])] : index.value === 3 ? ['teaching_class_number'] : [])
function reset() { result.value = null; snapshot.value = ''; error.value = ''; requestID.value = ''; page.value = 1 }
watch(index, () => { form.advanced = '{}'; reset() })
watch(form, reset)
async function query(next?: number) {
  const fresh = next === undefined
  next ??= 1
  error.value = ''
  if (fresh) {
    reset()
    try {
      const extra = JSON.parse(form.advanced)
      if (!extra || Array.isArray(extra) || typeof extra !== 'object' || Object.keys(extra).some(key => !advancedKeys.value.includes(key))) throw new Error()
      submitted = { ...extra }
      if (form.identity.trim()) submitted[op.value.identity] = index.value === 1 ? form.identity.split(/[\s,，]+/).filter(Boolean) : form.identity.trim()
      if (form.year) submitted.academic_year = form.year
      if (form.semester) submitted.semester = form.semester
      if (form.course) submitted[op.value.grade ? 'course_name' : 'course_query'] = form.course
      if (op.value.grade) { submitted.page_size = Number(form.size); submitted.page = 1 }
    } catch { error.value = '高级筛选须为 JSON 对象，且只能包含下方列出的字段。'; return }
  }
  busy.value = true
  try {
    const response = await request<NonNullable<typeof result.value>>('query', op.value.path, { body: { ...submitted, ...(op.value.grade ? { page: next } : {}) }, snapshot: fresh ? '' : snapshot.value })
    result.value = response.body; snapshot.value = response.snapshot; requestID.value = response.requestID || response.body.request_id || ''; page.value = next
  } catch (e) { error.value = describe(e) }
  finally { busy.value = false }
}
</script>
<template>
  <section>
    <div class="page-heading"><div><h1>业务查询</h1><p>核验已发布的成绩与课表数据。</p></div><span class="tag">只读查询</span></div>
    <div class="tabs" aria-label="查询接口"><button v-for="(item, i) in operations" :key="item.path" :class="{ active: index === i }" :aria-pressed="index === i" :disabled="busy" @click="index = i">{{ item.title }}</button></div>
    <div class="endpoint"><span class="method">POST</span><code>{{ op.path }}</code></div>
    <form class="panel query-form" @submit.prevent="query()">
      <fieldset :disabled="busy"><div class="form-grid">
        <label>{{ op.label }}{{ index === 1 ? '（可选）' : ' *' }}<input v-model="form.identity" :required="index !== 1" :placeholder="index === 1 ? '多个学号以逗号或空格分隔' : `请输入${op.label}`" autocomplete="off"></label>
        <label>学年{{ index === 0 ? '（可选）' : ' *' }}<input v-model="form.year" :required="index !== 0" :pattern="op.grade ? '(all|[0-9]{4}-[0-9]{4})' : '[0-9]{4}-[0-9]{4}'" placeholder="如 2025-2026" title="相邻两年，如 2025-2026；成绩支持 all"></label>
        <label>学期{{ op.grade ? '（可选）' : ' *' }}<select v-model="form.semester" :required="!op.grade"><option value="">{{ op.grade ? '全部学期' : '请选择学期' }}</option><option value="1">第一学期</option><option value="2">第二学期</option></select></label>
        <label>{{ op.grade ? '课程名称' : '课程检索' }}<input v-model="form.course" placeholder="可选，输入课程关键词"></label>
      </div>
      <details v-if="advancedKeys.length" class="advanced"><summary>高级筛选</summary><p class="hint">JSON 字段：{{ advancedKeys.join('、') }}。编码集合填写字符串数组，成绩范围填写数值。</p><label>高级筛选 JSON<textarea v-model="form.advanced" rows="4" spellcheck="false"></textarea></label></details>
      <div class="form-footer"><span class="hint">{{ index === 1 ? '不填写学号时，查询所选学年内符合筛选条件的成绩。' : '查询结果来自当前已发布的数据组合。' }}</span><div class="actions"><label v-if="op.grade" class="inline-label">每页<select v-model="form.size"><option :value="20">20 条</option><option :value="50">50 条</option><option :value="100">100 条</option></select></label><button class="primary">{{ busy ? '正在查询…' : '查询数据' }}</button></div></div>
      </fieldset>
    </form>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="section-bar result-heading"><h2>查询结果</h2><span v-if="result" class="muted">{{ op.grade ? `共 ${result.data.total} 条` : `${result.data.items.length} 条排课记录` }}</span></div>
    <div v-if="!result" class="empty" role="status"><span aria-hidden="true">⌕</span><h3>{{ busy ? '正在读取数据' : '等待查询' }}</h3><p>{{ busy ? '正在核对筛选条件并读取已发布数据。' : '填写上方条件，查询结果将在这里展示。' }}</p></div>
    <template v-else>
      <div class="result-meta"><span>同步时间 {{ time(result.data.synced_at) }}</span><span>请求号 <code>{{ requestID || '未返回' }}</code></span></div>
      <p v-if="!result.data.items.length" class="empty">没有符合条件的数据，请调整筛选条件后重新查询。</p>
      <div v-else class="table-wrap" tabindex="0" aria-label="查询结果表格，可横向滚动">
        <table v-if="op.grade"><thead><tr><th>学号 / 姓名</th><th>课程</th><th>学年 / 学期</th><th class="numeric">成绩</th><th class="numeric">绩点</th><th class="numeric">学分</th></tr></thead><tbody><tr v-for="row in result.data.items" :key="row.grade_id"><td>{{ row.student.student_number }}<small>{{ row.student.name ?? '姓名未提供' }}</small></td><td>{{ row.course.course_name ?? '课程名未提供' }}<small>{{ row.course.course_code }}</small></td><td>{{ row.academic_year }}<small>第 {{ row.semester }} 学期</small></td><td class="numeric">{{ row.score ?? '—' }}</td><td class="numeric">{{ row.grade_point ?? '—' }}</td><td class="numeric">{{ row.credits ?? '—' }}</td></tr></tbody></table>
        <table v-else><thead><tr><th>课程 / 教学班</th><th>教师</th><th>星期 / 节次</th><th>周次</th><th>地点</th></tr></thead><tbody><tr v-for="(row, i) in result.data.items" :key="i"><td>{{ row.course_name ?? row.course_code }}<small>{{ row.teaching_class_name ?? row.teaching_class_number }}</small></td><td>{{ row.teacher_names.join('、') || '—' }}</td><td>星期{{ ['日','一','二','三','四','五','六','日'][row.weekday] ?? row.weekday }}<small>第 {{ row.sections.join('、') }} 节</small></td><td>{{ row.weeks.join('、') || '—' }}</td><td>{{ row.location ?? '—' }}</td></tr></tbody></table>
      </div>
      <div v-if="op.grade" class="pagination"><span class="hint">“—” 表示未提供数值，可能为文字成绩。翻页沿用本次快照。</span><div class="actions"><button :disabled="busy || page <= 1 || !snapshot" @click="query(page - 1)">上一页</button><span>{{ page }} / {{ maxPage }}</span><button :disabled="busy || page >= maxPage || !snapshot" @click="query(page + 1)">下一页</button></div></div>
      <details class="raw"><summary>查看完整 JSON 响应</summary><pre>{{ JSON.stringify(result, null, 2) }}</pre></details>
    </template>
  </section>
</template>
