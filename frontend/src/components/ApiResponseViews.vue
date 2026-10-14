<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import type { ResponseHeaders } from '../api'
import { responseRecords, responseColumns, recordCell, organizationNodes, datedLessons } from '../app/responseViews'
import ResponseViewer from './ResponseViewer.vue'
import JsonText from './JsonText.vue'
const props = defineProps<{ api: CatalogAPI; raw: string; headers: ResponseHeaders; httpStatus?: number; code?: string; requestId?: string; stale?: boolean; sourceResultText?: string | null }>()
const view = ref('json'), date = ref('')
const records = computed(() => responseRecords(props.raw, props.api.kind)), columns = computed(() => responseColumns(props.api, records.value))
const tree = computed(() => organizationNodes(records.value.objects)), lessons = computed(() => datedLessons(records.value.objects))
const tabs = computed(() => [ { id: 'json', label: 'JSON' }, { id: 'table', label: '表格' }, ...(props.api.id.includes('timetable.') ? [{ id: 'schedule', label: '课表' }] : []), ...(props.api.id === 'organizations.query' ? [{ id: 'tree', label: '组织树' }] : []), { id: 'headers', label: '响应头' } ])
const dateEntries = computed(() => lessons.value.groups.find(([day]) => day === date.value)?.[1] ?? [])
watch(() => props.raw, () => { view.value = 'json'; date.value = lessons.value.groups[0]?.[0] ?? '' }, { immediate: true })
</script>
<template><div class="response-views">
  <div class="tabs" aria-label="响应视图"><button v-for="tab in tabs" :key="tab.id" type="button" :aria-pressed="view === tab.id" :class="{ active: view === tab.id }" @click="view = tab.id">{{ tab.label }}</button></div>
  <p v-if="stale && view !== 'json'" class="warning">上次读取结果，当前请求尚未取得新响应。</p>
  <template v-if="view === 'json'"><ResponseViewer :text="raw" :http-status="httpStatus" :code="code" :request-id="requestId" :stale="stale" :label="api.kind === 'source' ? '管理作业响应原文' : '响应原文'" />
    <template v-if="sourceResultText"><h4>本页源数据</h4><p class="hint">后端返回的 total/list 结果，保留原始数字；不是上游完整 HTTP 报文。</p><JsonText :text="sourceResultText" label="本页源数据 JSON" /></template>
    <details><summary>{{ api.kind === 'source' ? '管理响应头' : '实际响应头' }}</summary><JsonText :text="JSON.stringify(headers, null, 2)" :label="api.kind === 'source' ? '管理响应头 JSON' : '响应头'" /></details>
  </template>
  <div v-else-if="view === 'headers'"><p class="hint">本次响应允许展示的诊断头；未返回的字段不补造。切换视图不重新请求。</p><JsonText :text="JSON.stringify(headers, null, 2)" label="本次响应头 JSON" /></div>
  <template v-else>
    <p v-if="records.error" class="warning">{{ records.error }}</p>
    <template v-else><p class="hint">仅展示本次响应的 {{ records.rows.length }} 条记录。表格保留原始数字；最多 1000 行、24 个源字段，长单元格请查看 JSON。{{ records.truncated ? '记录超出显示上限。' : '' }}</p>
      <p v-if="!records.rows.length" class="empty">本次响应没有记录。</p>
      <div v-else-if="view === 'table'" class="table-wrap" tabindex="0" aria-label="本次响应表格"><table><thead><tr><th v-for="column in columns" :key="column.key">{{ column.title }}<small>{{ column.key }}</small></th></tr></thead><tbody><tr v-for="(row, i) in records.rows" :key="i"><td v-for="column in columns" :key="column.key">{{ recordCell(row, column.key) }}</td></tr></tbody></table></div>
      <div v-else-if="view === 'tree'" aria-label="本次组织树"><p class="warning">本次响应范围，未核验全量完整性。未返回的父节点不补造，节点未显示不等于组织不存在。</p><ol class="organization-tree"><li v-for="node in tree" :key="node.index" :style="{ marginInlineStart: `${Math.min(node.depth, 8)}rem` }"><strong>{{ recordCell(records.rows[node.index]!, 'organization_name') }}</strong> · <code>{{ recordCell(records.rows[node.index]!, 'organization_code') }}</code><p class="hint">上级：{{ recordCell(records.rows[node.index]!, 'parent_organization_code') }} · 有效：{{ recordCell(records.rows[node.index]!, 'is_active') }}{{ node.note ? ' · ' + node.note : '' }}</p></li></ol></div>
      <div v-else-if="view === 'schedule'" aria-label="本次日期课表"><p class="hint">按实际 dates 和 sections 展示本次排课；教师姓名与工号分别保留，不按下标配对。缺少日期或节次的记录仍在表格中。</p><label v-if="lessons.groups.length">上课日期<select v-model="date"><option v-for="[day] in lessons.groups" :key="day" :value="day">{{ day }}</option></select></label><div class="lesson-list"><article v-for="index in dateEntries" :key="index"><h4>{{ recordCell(records.rows[index]!, 'course_name') }} · {{ recordCell(records.rows[index]!, 'course_code') }}</h4><p>节次 {{ recordCell(records.rows[index]!, 'sections') }} · 教学周 {{ recordCell(records.rows[index]!, 'weeks') }}</p><p>地点：{{ recordCell(records.rows[index]!, 'location') }}</p><p>教师姓名：{{ recordCell(records.rows[index]!, 'teacher_names') }}</p><p>教师工号：{{ recordCell(records.rows[index]!, 'teacher_staff_numbers') }}</p></article></div><p v-if="lessons.unmapped.length" class="warning">{{ lessons.unmapped.length }} 条记录无法映射到日期课表，请在表格查看。</p><p v-if="lessons.clipped" class="warning">日期映射达到 2000 项显示上限，请查看原文。</p></div>
    </template>
  </template>
</div></template>
<style scoped>
.response-views { min-width: 0; }
.organization-tree { list-style: none; padding: 0; }
.organization-tree li { border-inline-start: 2px solid var(--line); padding: 10px; overflow-wrap: anywhere; }
.lesson-list { display: grid; gap: 12px; margin-top: 16px; }
.lesson-list article { border: 1px solid var(--line); border-radius: 8px; padding: 16px; overflow-wrap: anywhere; }
.lesson-list h4, .lesson-list p { margin: 0 0 8px; }
</style>
