<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { time } from './api'
import type { WorkProgress } from './workProgress'
const props = defineProps<{ progress?: WorkProgress; status: string }>()
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { timer = setInterval(() => { now.value = Date.now() }, 1000) })
onUnmounted(() => clearInterval(timer))
const ended = computed(() => props.status === 'failed' || props.status.startsWith('success_'))
const stale = computed(() => !!props.progress && (!Number.isFinite(Date.parse(props.progress.heartbeat_at)) || now.value - Date.parse(props.progress.heartbeat_at) > 45000))
const labels: Record<string, string> = {
  preparing: '准备执行', recovering: '恢复中断的任务', acquiring: '采集与复用数据', sealing: '校验全部采集数据',
  validating_input: '校验构建输入', building_table: '构建表数据', validating_target: '校验构建结果',
  writing_table: '写入数据库', validating_copy: '校验数据库与业务查询', checking_unchanged: '核验数据无变化',
  publishing: '发布数据版本', confirming_progress: '确认同步进度',
}
const phase = computed(() => labels[props.progress?.phase || ''] || '处理数据')
const title = computed(() => ended.value ? `结束前阶段：${phase.value}` : stale.value ? `最近阶段：${phase.value}` : `正在${phase.value}`)
const elapsed = computed(() => {
  const seconds = Math.max(0, Math.floor(((ended.value ? Date.parse(props.progress?.heartbeat_at || '') : now.value) - Date.parse(props.progress?.started_at || '')) / 1000))
  if (!Number.isFinite(seconds)) return '未知'
  return seconds < 60 ? `${seconds} 秒` : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
})
</script>
<template>
  <section v-if="progress" class="work-progress" aria-label="当前执行阶段">
    <div class="section-bar"><h3>{{ title }}</h3><span class="tag" :class="{ progress: !ended && !stale, warning: !ended && stale }">{{ ended ? '已结束' : stale ? '状态待确认' : '执行器在线' }}</span></div>
    <p v-if="progress.table || progress.instance"><span v-if="progress.table">当前表：<code>{{ progress.table }}</code></span><span v-if="progress.instance"> · 数据库副本：{{ progress.instance }}</span></p>
    <p class="hint">本阶段耗时：{{ elapsed }} · 执行器最近心跳：{{ time(progress.heartbeat_at) }}</p>
    <p v-if="!ended && stale" class="error" role="status">超过 45 秒未收到执行器心跳，暂时无法确认是否仍在执行，请检查服务状态。</p>
    <p v-else-if="!ended" class="hint">每 10 秒上报一次心跳。心跳表示执行器在线，不代表数据处理进度已推进。</p>
  </section>
</template>
