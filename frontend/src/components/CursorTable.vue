<script setup lang="ts">
import { displayValue, fieldValue } from '../shared/display'
import EmptyState from './EmptyState.vue'
import ErrorState from './ErrorState.vue'
interface Column { key: string; title: string; numeric?: boolean }
withDefaults(defineProps<{ rows: Record<string, unknown>[]; columns: Column[]; rowKey?: string; nextCursor?: string | null; previousAvailable?: boolean; loading?: boolean; error?: string; label?: string }>(), { label: '查询结果' })
const emit = defineEmits<{ next: [cursor: string]; previous: []; retry: [] }>()
</script>
<template>
  <section class="cursor-table" :aria-label="label" :aria-busy="loading">
    <ErrorState v-if="error" :message="error" :busy="loading" retryable @retry="emit('retry')" />
    <EmptyState v-if="!rows.length" :loading="loading" :title="error ? '暂时无法读取记录' : '没有符合条件的记录'" :description="loading ? '正在读取当前筛选条件的结果。' : error ? '修复连接或调整条件后重新读取。' : '可调整筛选条件后重新查询。'" />
    <p v-else-if="error" class="hint">以下为上次成功读取的结果。</p>
    <div v-if="rows.length" class="table-wrap" tabindex="0" :aria-label="`${label}表格，可横向滚动`"><table><thead><tr><th v-for="column in columns" :key="column.key" scope="col" :class="{ numeric: column.numeric }">{{ column.title }}</th></tr></thead><tbody><tr v-for="(row, index) in rows" :key="String(rowKey ? fieldValue(row, rowKey) ?? index : index)"><td v-for="column in columns" :key="column.key" :class="{ numeric: column.numeric }"><slot :name="column.key" :row="row" :value="fieldValue(row, column.key)">{{ displayValue(fieldValue(row, column.key)) }}</slot></td></tr></tbody></table></div>
    <div class="pagination"><span class="hint">{{ !rows.length && (loading || error) ? '尚未取得当前页记录' : `${rows.length} 条当前页记录` }}</span><div class="actions"><button type="button" :disabled="loading || !!error || !previousAvailable" @click="emit('previous')">上一页</button><button type="button" :disabled="loading || !!error || !nextCursor" @click="nextCursor && emit('next', nextCursor)">下一页</button></div></div>
  </section>
</template>
