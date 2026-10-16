<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import type { ResponseHeaders } from '../api'
import { responseRecords, responseColumns, recordCell } from '../app/responseViews'
import ResponseViewer from './ResponseViewer.vue'
import JsonText from './JsonText.vue'
import GradeResultContext from './GradeResultContext.vue'
import TimetableViews from './TimetableViews.vue'
import OrganizationTree from './OrganizationTree.vue'
import PersonDetails from './PersonDetails.vue'
import { rosterContext, type RosterContext } from '../app/roster'
const props = withDefaults(defineProps<{ api: CatalogAPI; allowRecords?: boolean; requestText?: string; raw: string; headers: ResponseHeaders; httpStatus?: number; code?: string; requestId?: string; stale?: boolean; sourceResultText?: string | null }>(), { allowRecords: true })
const emit = defineEmits<{ roster: [context: RosterContext] }>()
const view = ref('json'), selectedPerson = ref<number | null>(null)
const personnel = computed(() => ['students.query', 'staff.query'].includes(props.api.id))
const records = computed(() => responseRecords(props.raw, props.api.kind)), columns = computed(() => responseColumns(props.api, records.value))
const classes = computed(() => props.api.id === 'staff.teaching-classes.query')
const rosterLinks = computed(() => classes.value ? records.value.rows.map(row => rosterContext(row, props.requestText ?? '{}', props.headers['X-SICP-Combination'] ?? '', props.api.version)) : [])

const tabs = computed(() => [ { id: 'json', label: 'JSON' }, ...(props.allowRecords === false ? [] : [{ id: 'table', label: '表格' }]), ...(props.allowRecords !== false && props.api.id.includes('timetable.') ? [{ id: 'schedule', label: '课表' }] : []), ...(props.allowRecords !== false && props.api.id === 'organizations.query' ? [{ id: 'tree', label: '组织树' }] : []), { id: 'headers', label: '响应头' } ])
watch(() => props.raw, () => { view.value = 'json'; selectedPerson.value = null }, { immediate: true })
</script>
<template><div class="response-views"><GradeResultContext v-if="allowRecords !== false && api.id.startsWith('students.grades.') && !records.error" :api-id="api.id" :request-text="requestText" :raw="raw" :count="records.rows.length" />
  <div class="tabs" aria-label="响应视图"><button v-for="tab in tabs" :key="tab.id" type="button" :aria-pressed="view === tab.id" :class="{ active: view === tab.id }" @click="view = tab.id">{{ tab.label }}</button></div>
  <p v-if="stale && view !== 'json'" class="warning">上次读取结果，当前请求尚未取得新响应。</p>
  <template v-if="view === 'json'"><ResponseViewer pretty :text="raw" :http-status="httpStatus" :code="code" :request-id="requestId" :stale="stale" :label="api.kind === 'source' ? '管理作业响应原文' : '响应原文'" />
    <template v-if="sourceResultText"><h4>本页源数据</h4><p class="hint">后端返回的 total/list 结果，保留原始数字；不是上游完整 HTTP 报文。</p><JsonText pretty :text="sourceResultText" label="本页源数据 JSON" /></template>
    <details><summary>{{ api.kind === 'source' ? '管理响应头' : '实际响应头' }}</summary><JsonText :text="JSON.stringify(headers, null, 2)" :label="api.kind === 'source' ? '管理响应头 JSON' : '响应头'" /></details>
  </template>
  <div v-else-if="view === 'headers'"><p class="hint">本次响应允许展示的诊断头；未返回的字段不补造。切换视图不重新请求。</p><JsonText :text="JSON.stringify(headers, null, 2)" label="本次响应头 JSON" /></div>
  <template v-else-if="allowRecords !== false">
    <p v-if="records.error" class="warning">{{ records.error }}</p>
    <template v-else><p class="hint">仅展示本次响应的 {{ records.rows.length }} 条记录。表格保留原始数字；最多 1000 行、24 个源字段，长单元格请查看 JSON。{{ records.truncated ? '记录超出显示上限。' : '' }}</p>
      <p v-if="!records.rows.length" class="empty">本次响应没有记录。</p>
      <div v-else-if="view === 'table'" class="table-wrap" tabindex="0" aria-label="本次响应表格"><table><thead><tr><th v-for="column in columns" :key="column.key">{{ column.title }}<small>{{ column.key }}</small></th><th v-if="personnel">详情</th><th v-if="classes">学生名单</th></tr></thead><tbody><tr v-for="(row, i) in records.rows" :key="i"><td v-for="column in columns" :key="column.key">{{ recordCell(row, column.key) }}</td><td v-if="personnel"><button type="button" :aria-expanded="selectedPerson === i" @click="selectedPerson = selectedPerson === i ? null : i">{{ selectedPerson === i ? '收起' : '查看' }}第 {{ i + 1 }} 条字段详情</button></td><td v-if="classes"><button type="button" :disabled="stale || !rosterLinks[i]" @click="emit('roster', rosterLinks[i]!)">查看第 {{ i + 1 }} 条教学班名单</button><small v-if="!rosterLinks[i]">学年学期或教学班/计划课程标识不完整</small></td></tr></tbody></table></div>
      <div v-else-if="view === 'tree'" aria-label="本次组织树"><p class="warning">本次响应范围，未核验全量完整性。未返回的父节点不补造，节点未显示不等于组织不存在。</p><OrganizationTree :records="records" /></div>
      <TimetableViews v-else-if="view === 'schedule'" :records="records" @show-table="view = 'table'" /><PersonDetails v-if="view === 'table' && personnel && selectedPerson !== null && records.rows[selectedPerson]" :api="api" :row="records.rows[selectedPerson]!" :index="selectedPerson" @close="selectedPerson = null" />
    </template>
  </template>
</div></template>
<style scoped>
.response-views { min-width: 0; }


</style>
