<script setup lang="ts">
import { computed } from 'vue'
import { gradeQueryContext, gradeResponseFacts } from '../app/grades'
import { operationRules } from '../app/catalogSchema'
import Timestamp from './Timestamp.vue'
import JsonText from './JsonText.vue'
const props = defineProps<{ apiId: string; requestText?: string; raw: string; count: number }>()
const context = computed(() => gradeQueryContext(props.apiId, props.requestText)), facts = computed(() => gradeResponseFacts(props.raw))
</script>
<template><section aria-label="本次成绩范围" class="grade-context"><h4>{{ context.scope }}</h4><p>共 {{ facts.total }} 条成绩记录，本次响应展示 {{ count }} 条；不按学生或课程去重。</p><p class="hint">同步确认时间：<Timestamp :value="facts.syncedAt" />。这是所需源表成功检查时间的最小值，不是请求时间。</p><p class="hint">成绩、绩点和学分中的 0 按原值显示；null 不等于 0，文字成绩的 score 也可为 null，接口没有返回原文字成绩。学分取成绩记录 XF，课程与院系/行政班为当前关联信息。</p><details><summary>本次条件与成绩口径</summary><ul><li v-for="rule in operationRules[apiId]" :key="rule">{{ rule }}</li><li>source_updated_at 表示参与该条成绩的源数据最大 ETL 时间，不是当前请求时间。</li></ul><p class="hint">以下条件随本次请求保存；编辑草稿不会改写旧响应的条件。不同字段取交集，码值/学号集合内部取并集。</p><JsonText :text="requestText ?? '未保存请求条件。'" label="本次成绩请求条件" /></details></section></template>
<style scoped>.grade-context { padding: 16px; margin-block: 16px; border: 1px solid var(--line); border-radius: 8px; }.grade-context h4 { margin: 0 0 8px; }.grade-context p { overflow-wrap: anywhere; }</style>
