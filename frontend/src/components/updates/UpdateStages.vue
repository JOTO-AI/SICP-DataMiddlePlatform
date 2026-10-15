<script setup lang="ts">
import { computed } from 'vue'
import Timestamp from '../Timestamp.vue'
import type { UpdateDetail } from '../../app/updateEvents'
const props = defineProps<{ task: UpdateDetail }>()
const stages = ['获取封存', '校验比较', '第一份构建', '查询转移', '排空', '第二份构建', '发布']
const current = computed(() => {
  const progress = props.task.work_progress
  const phase = progress && typeof progress === 'object' && 'phase' in progress ? progress.phase : ''
  return ['acquiring', 'sealing'].includes(String(phase)) ? 0 : ['validating_input', 'checking_unchanged', 'content_check'].includes(String(phase)) ? 1 : phase === 'draining' ? 4 : phase === 'publishing' ? 6 : -1
})
const active = computed(() => ['running', 'published_pending_progress'].includes(props.task.status))
</script>
<template><section class="stage-strip panel" aria-label="更新阶段进度"><ol><li v-for="(stage, index) in stages" :key="stage" :class="{ confirmed: index === 6 && task.published_at, current: active && current === index }"><span class="stage-marker" aria-hidden="true">{{ index === 6 && task.published_at ? '✓' : index + 1 }}</span><strong>{{ stage }}</strong><Timestamp v-if="index === 6 && task.published_at" :value="task.published_at" /><span v-else>{{ active && current === index ? '执行中' : '尚未确认' }}</span></li></ol><p class="hint">各阶段以实际记录为准；两份构建的先后顺序未提供时，在操作树中按物理副本查看。</p></section></template>
<style scoped>
.stage-strip { margin-block: 20px; padding: 20px 16px 12px; }
ol { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); list-style: none; padding: 0; margin: 0 0 12px; }
li { display: flex; flex-direction: column; align-items: center; gap: 6px; text-align: center; position: relative; font-size: 12px; min-width: 0; color: var(--muted); }
li::before { content: ''; position: absolute; height: 1px; background: var(--line); top: 13px; width: 100%; left: -50%; }
li:first-child::before { display: none; }
.stage-marker { border-radius: 50%; width: 26px; height: 26px; background: var(--surface); border: 1px solid var(--control); z-index: 1; display: grid; place-items: center; }
strong { color: var(--ink); font-size: 14px; } .confirmed .stage-marker { color: var(--white); background: var(--green); border-color: var(--green); } .current .stage-marker { color: var(--white); background: var(--brand); border-color: var(--brand); } .current strong, .current > span:last-child { color: var(--brand); }
@media(max-width: 800px) { ol { grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 18px 8px; } li::before { display: none; } }
@media(max-width: 450px) { ol { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
