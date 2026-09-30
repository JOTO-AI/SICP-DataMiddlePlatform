<script setup lang="ts">
import { computed } from 'vue'
import { time } from './api'
import type { Acquisition, TableAcquisition } from './acquisition'
const props = defineProps<{ progress?: Acquisition; status: string; compact?: boolean; allowUpdate?: boolean }>()
defineEmits<{ updateTable: [table: string] }>()
const tables = computed(() => props.progress?.tables || [])
const completed = computed(() => tables.value.filter(table => table.status === 'collected').length)
const visible = computed(() => props.compact ? tables.value.filter(table => table.status === 'acquiring') : tables.value)
const ended = computed(() => props.status === 'failed' || props.status.startsWith('success_'))
function label(table: TableAcquisition) {
  if (table.status === 'collected') return table.reused ? '复用已完成数据' : '采集完成'
  if (table.status === 'failed') return '采集失败／待补齐'
  if (table.status === 'waiting') return ended.value ? '未开始' : '等待采集'
  return ended.value ? '采集已停止' : '正在采集'
}
function percent(table: TableAcquisition) {
  return table.total_pages ? `${Math.floor(table.completed_pages / table.total_pages * 1000) / 10}%` : ''
}
</script>
<template>
  <section class="acquisition-progress" aria-label="逐表采集进度">
    <div class="section-bar"><h3>上游采集进度</h3><span v-if="tables.length" class="muted">已完成 {{ completed }} / {{ tables.length }} 张表</span></div>
    <template v-if="progress">
      <ul class="acquisition-tables">
        <li v-for="table in visible" :key="table.table">
          <div class="acquisition-heading"><code>{{ table.table }}</code><span>{{ label(table) }}</span></div>
          <button v-if="!compact" type="button" :disabled="!allowUpdate" :aria-label="`强制更新 ${table.table}`" @click="$emit('updateTable', table.table)">强制更新此表</button>
          <template v-if="table.total_pages !== null">
            <div class="acquisition-count">{{ table.completed_pages }} / {{ table.total_pages }} 页 · {{ percent(table) }}</div>
            <progress :value="table.completed_pages" :max="table.total_pages" :aria-label="`${table.table} 采集进度`">{{ percent(table) }}</progress>
          </template>
          <p v-else class="hint">{{ table.status === 'acquiring' ? '等待首个分页结果，暂未取得总页数。' : '总页数待开始采集后获取。' }}</p>
        </li>
      </ul>
      <p v-if="tables.length && completed === tables.length">所有表采集完成，构建与发布结果请查看任务状态。</p>
      <p v-if="tables.some(table => table.status === 'failed')" class="hint">{{ ended ? '已完成的表可以保留复用。补齐缺失表后，系统会校验并统一发布完整数据。' : '已有表采集失败，其余表会继续；当前任务结束后可单独更新失败表。' }}</p>
      <p class="hint">页数按校验并保存成功的页面统计，100% 表示该表采集完成。最近进度：{{ time(progress.updated_at) }}</p>
    </template>
    <p v-else class="hint">{{ ended ? '该任务没有逐页采集记录。' : status === 'accepted' ? '任务等待执行，开始采集后显示页数。' : '暂未收到逐页采集进度。' }}</p>
  </section>
</template>
