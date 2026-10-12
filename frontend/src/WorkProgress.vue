<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { time } from './api'
import { operationTitles as operations } from './queryOperations'
import type { WorkProgress } from './workProgress'
const props = defineProps<{ progress?: WorkProgress; status: string; startedAt?: string; finishedAt?: string }>()
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { timer = setInterval(() => { now.value = Date.now() }, 1000) })
onUnmounted(() => clearInterval(timer))
const ended = computed(() => props.status === 'failed' || props.status.startsWith('success_'))
const stale = computed(() => !!props.progress && (!Number.isFinite(Date.parse(props.progress.heartbeat_at)) || now.value - Date.parse(props.progress.heartbeat_at) > 45000))
const stalled = computed(() => !!props.progress?.unit && now.value - Date.parse(props.progress.last_progress_at || props.progress.started_at) > 45000)
const labels: Record<string, string> = {
  preparing: '准备执行', recovering: '恢复中断的任务', acquiring: '采集与复用数据', sealing: '校验全部采集数据',
  validating_input: '校验构建输入', building_table: '构建表数据', validating_target: '校验构建结果',
  writing_table: '写入数据库', validating_copy: '校验数据库与业务查询', checking_unchanged: '核验数据无变化',
  publishing: '发布数据版本', confirming_progress: '确认同步进度',
  content_check: '核对数据库内容', business_check: '核对业务案例', index_check: '核对索引', draining: '等待旧读者退出',
}
const phase = computed(() => labels[props.progress?.phase || ''] || '处理数据')
const title = computed(() => ended.value ? `${props.status === 'failed' ? '失败' : '结束'}前阶段：${phase.value}` : stale.value ? `最近阶段：${phase.value}` : `当前阶段：${phase.value}`)
const endTime = computed(() => ended.value ? Date.parse(props.finishedAt || props.progress?.heartbeat_at || '') : now.value)
function duration(ms: number) {
  const seconds = Math.max(0, Math.floor(ms / 1000))
  if (!Number.isFinite(seconds)) return '未知'
  return seconds < 60 ? `${seconds} 秒` : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
}
const elapsed = computed(() => duration(endTime.value - Date.parse(props.progress?.started_at || '')))
const totalElapsed = computed(() => duration(endTime.value - Date.parse(props.startedAt || '')))
const unit = computed(() => props.progress?.unit === 'cases' ? '案例' : '行')
const completed = computed(() => props.progress?.completed ?? 0)
const percent = computed(() => props.progress?.total_known && (props.progress.total ?? 0) > 0 ? Math.min(100, Math.floor(completed.value / props.progress.total! * 100)) : undefined)
</script>
<template>
  <section v-if="progress" class="work-progress" aria-label="当前执行阶段">
    <div class="section-bar"><h3>{{ title }}</h3><span class="tag" :class="{ progress: !ended && !stale, warning: !ended && stale }">{{ ended ? '已结束' : stale ? '状态待确认' : '执行器在线' }}</span></div>
    <p v-if="progress.table || progress.instance"><span v-if="progress.table">当前表：<code>{{ progress.table }}</code></span><span v-if="progress.instance"> · 数据库副本：{{ progress.instance }}</span></p>
    <p v-if="progress.tables?.length">当前构建的表：<span v-for="(table,index) in progress.tables" :key="table">{{ index ? '、' : '' }}<code>{{ table }}</code></span></p>
    <p v-if="progress.detail">检查项目：{{ operations[progress.detail] || '业务查询' }}</p>
    <template v-if="progress.unit">
      <p>已处理 {{ completed.toLocaleString() }}<template v-if="progress.total_known"> / {{ (progress.total ?? 0).toLocaleString() }}</template> {{ unit }}<template v-if="percent !== undefined"> · {{ percent }}%</template><template v-if="progress.batches"> · 已提交 {{ progress.batches }} 批</template></p>
      <progress v-if="percent !== undefined" :value="completed" :max="progress.total" aria-label="当前阶段工作量" />
      <p class="hint">最后实际进展：{{ progress.last_progress_at ? time(progress.last_progress_at) : '本阶段尚无已确认进展' }}。行数完成后仍需等待完整校验与发布。</p>
    </template>
    <p v-if="(progress.attempt ?? 0) > 1" class="hint">本阶段第 {{ progress.attempt }} 次执行 · 历次累计 {{ progress.cumulative_completed ?? 0 }} {{ unit }} · 历次累计耗时 {{ duration(progress.cumulative_elapsed_ms ?? 0) }}</p>
    <p v-if="startedAt" class="hint">任务总耗时：{{ totalElapsed }}</p>
    <p class="hint">本阶段耗时：{{ elapsed }} · 执行器最近心跳：{{ time(progress.heartbeat_at) }}</p>
    <p v-if="!ended && stale" class="error" role="status">超过 45 秒未收到执行器心跳，暂时无法确认是否仍在执行，请检查服务状态。</p>
    <p v-else-if="!ended && stalled" class="error" role="status">执行器在线，但超过 45 秒没有已确认的工作量进展；请检查当前阶段。</p>
    <p v-else-if="!ended" class="hint">每 10 秒上报一次心跳。心跳表示执行器在线，不代表数据处理进度已推进。</p>
  </section>
</template>
