<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { datedLessons, recordCell, type ResponseRecords } from '../app/responseViews'
import { calendarWeeks, timetableWeek } from '../app/timetable'
const props = defineProps<{ records: ResponseRecords }>()
const emit = defineEmits<{ 'show-table': [] }>()
const mode = ref('dates'), date = ref(''), week = ref('')
const lessons = computed(() => datedLessons(props.records.objects)), weeks = computed(() => calendarWeeks(lessons.value.groups))
const selectedWeek = computed(() => weeks.value.find(item => item.start === week.value))
const matrix = computed(() => timetableWeek(props.records.objects, lessons.value.groups, selectedWeek.value?.days ?? []))
const dateEntries = computed(() => lessons.value.groups.find(([day]) => day === date.value)?.[1] ?? [])
const cell = (index: number, key: string) => recordCell(props.records.rows[index]!, key)
watch(() => props.records, () => { mode.value = 'dates'; date.value = lessons.value.groups[0]?.[0] ?? ''; week.value = weeks.value[0]?.start ?? '' }, { immediate: true })
</script>
<template><section aria-label="本次日期课表">
  <p class="hint">两类课表均不分页、不发送快照。按实际 dates 和 sections 展示；教师姓名与工号分别保留，不按下标配对。缺日期/节次的记录仍在表格中，不因缺课程或教师而删除。</p>
  <div class="tabs" aria-label="课表排布"><button type="button" :aria-pressed="mode === 'dates'" :class="{ active: mode === 'dates' }" @click="mode = 'dates'">按日期查看</button><button type="button" :aria-pressed="mode === 'week'" :class="{ active: mode === 'week' }" @click="mode = 'week'">周课表</button></div>
  <template v-if="mode === 'dates'"><label v-if="lessons.groups.length">上课日期<select v-model="date"><option v-for="[day] in lessons.groups" :key="day" :value="day">{{ day }}</option></select></label><div class="lesson-list"><article v-for="index in dateEntries" :key="index"><h4>{{ cell(index, 'course_name') }} · {{ cell(index, 'course_code') }}</h4><p>节次 {{ cell(index, 'sections') }} · 教学周 {{ cell(index, 'weeks') }}</p><p>地点：{{ cell(index, 'location') }}</p><p>教师姓名：{{ cell(index, 'teacher_names') }}</p><p>教师工号：{{ cell(index, 'teacher_staff_numbers') }}</p></article></div></template>
  <template v-else><p class="hint">按实际日期所在的自然周排布，不推算学期开学日。教学周仍取各条记录的 weeks；同一排课可占多个节次，格子数量不是排课记录数。</p><label v-if="weeks.length">自然周<select v-model="week"><option v-for="item in weeks" :key="item.start" :value="item.start">{{ item.start }} 至 {{ item.days[6] }}</option></select></label><p v-if="matrix.weekdayMismatch" class="warning">部分记录的 weekday 与实际 dates 不一致；按日期排布，原字段仍保留于表格/JSON。</p>
    <div v-if="selectedWeek" class="table-wrap week-grid" tabindex="0" aria-label="本次周课表，可横向滚动"><table><thead><tr><th>节次</th><th v-for="(day, index) in selectedWeek.days" :key="day">星期{{ ['一','二','三','四','五','六','日'][index] }}<small>{{ day }}</small></th></tr></thead><tbody><tr v-for="section in matrix.sections" :key="section"><th scope="row">第 {{ section }} 节</th><td v-for="day in selectedWeek.days" :key="day"><template v-if="matrix.cells.get(`${day}/${section}`)?.length"><article v-for="index in matrix.cells.get(`${day}/${section}`)" :key="index"><strong>{{ cell(index, 'course_name') }}</strong><small>{{ cell(index, 'course_code') }}</small><details><summary>排课详情</summary><p>教学班：{{ cell(index, 'teaching_class_number') }}</p><p>教学周：{{ cell(index, 'weeks') }}</p><p>教师姓名：{{ cell(index, 'teacher_names') }}</p><p>教师工号：{{ cell(index, 'teacher_staff_numbers') }}</p><p>地点：{{ cell(index, 'location') }}</p><p>源星期：{{ cell(index, 'weekday') }}</p></details></article></template><span v-else class="hint">本响应无记录</span></td></tr></tbody></table></div><p v-if="matrix.clipped" class="warning">周课表达到显示上限（64 个节次、2000 项格内记录），请查看表格或 JSON。</p>
  </template>
  <p v-if="!lessons.groups.length" class="empty">本次没有可按日期映射的记录。</p><p v-if="lessons.unmapped.length" class="warning">{{ lessons.unmapped.length }} 条记录无法映射到日期课表，原记录仍在表格中。</p><button v-if="lessons.unmapped.length" type="button" @click="emit('show-table')">在表格查看所有记录</button><p v-if="lessons.clipped" class="warning">日期映射达到 2000 项显示上限，请查看原文。</p>
</section></template>
<style scoped>
.lesson-list { display: grid; gap: 12px; margin-top: 16px; }
.lesson-list article { border: 1px solid var(--line); border-radius: 8px; padding: 16px; overflow-wrap: anywhere; }
.lesson-list h4, .lesson-list p { margin: 0 0 8px; }
.week-grid { margin-top: 16px; }
.week-grid table { min-width: 1120px; table-layout: fixed; }
.week-grid th:first-child { width: 76px; }
.week-grid td { vertical-align: top; }
.week-grid article { padding: 8px; margin-bottom: 8px; background: var(--surface); border-inline-start: 2px solid var(--brand); overflow-wrap: anywhere; }
.week-grid p { white-space: normal; overflow-wrap: anywhere; }
</style>
