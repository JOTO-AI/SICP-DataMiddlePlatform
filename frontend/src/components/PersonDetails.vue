<script setup lang="ts">
import { computed } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import { fieldDescription } from '../app/catalogSchema'
import { personFields } from '../app/personFields'
const props = defineProps<{ api: CatalogAPI; row: Map<string, string>; index: number }>()
defineEmits<{ close: [] }>()
const details = computed(() => personFields(props.row))
</script>
<template><section aria-label="人员字段详情" class="person-details"><div class="actions"><h4>第 {{ index + 1 }} 条人员记录 · 字段详情</h4><button type="button" @click="$emit('close')">收起字段详情</button></div><p class="hint">只读取本次响应，未返回字段不补造。代码保留原值，不推断未知代码的名称；null 不转为空字符串。关联单位是当前数据，不能作为历史归属。最多 200 个字段、6 层展开，长值请查看响应 JSON。</p><p v-if="details.truncated" class="warning">字段超过展示上限，请查看响应 JSON。</p><dl><div v-for="field in details.fields" :key="field.path"><dt>{{ fieldDescription(api, field.key) }}<code>{{ field.path }}</code></dt><dd>{{ field.value }}</dd></div></dl></section></template>
<style scoped>
.person-details { border: 1px solid var(--line); padding: 16px; margin-top: 16px; }
dl { margin: 0; } dl > div { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 2fr); gap: 16px; padding-block: 10px; border-top: 1px solid var(--line); overflow-wrap: anywhere; } dt code { display: block; } dd { margin: 0; white-space: pre-wrap; }
@media (max-width: 767px) { dl > div { grid-template-columns: 1fr; gap: 8px; } }
</style>