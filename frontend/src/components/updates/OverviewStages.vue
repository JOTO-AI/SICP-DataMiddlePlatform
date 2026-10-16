<script setup lang="ts">
import { computed } from 'vue'
import { updateStageStates, acquisitionSummary, operationGroupSummary, type StageState } from '../../app/updateStageStates'
import type { UpdateDetail } from '../../app/updateEvents'
const props = defineProps<{ task: UpdateDetail }>()
const stages = computed(() => {
  const all = updateStageStates(props.task)
  const builds = [all[2]!, all[5]!]
  const state = builds.every(s => s.state === 'done') ? 'done' : builds.every(s => s.state === 'skipped') ? 'skipped' : builds.some(s => s.state === 'failed') ? 'failed' : (operationGroupSummary('builds', props.task) === '副本构建中' || builds.some(s => s.state === 'active')) ? 'active' : builds.every(s => s.state === 'pending') ? 'pending' : 'unknown'
  const build: StageState = { title: '构建数据', state, label: ({ done: '已完成', skipped: '无需构建', failed: '失败', active: '构建中', pending: '未开始', unknown: '未记录完整结果' })[state] }
  return [{ ...all[0]!, title: '获取数据', state: acquisitionSummary(props.task)?.complete ? 'done' : all[0]!.state }, { ...all[1]!, title: '校验比较' }, build, all[6]!]
})
</script>
<template><ol class="overview-stages" aria-label="最近更新阶段"><li v-for="(stage, index) in stages" :key="stage.title" :class="stage.state"><span class="stage-dot" aria-hidden="true">{{ stage.state === 'done' ? '✓' : stage.state === 'failed' ? '!' : stage.state === 'skipped' ? '—' : index + 1 }}</span><strong>{{ stage.title }}</strong><small>{{ stage.label }}</small></li></ol></template>
<style scoped>
.overview-stages { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); padding: 0; margin: 24px 0 20px; list-style: none; }
li { display: flex; flex-direction: column; align-items: center; gap: 5px; position: relative; text-align: center; min-width: 0; font-size: 13px; }
li::before { content: ''; position: absolute; height: 1px; background: var(--line); top: 12px; width: 100%; left: -50%; }
li:first-child::before { display: none; }
.stage-dot { display: grid; place-items: center; width: 24px; height: 24px; border-radius: 50%; background: var(--muted); color: white; z-index: 1; font-weight: 700; }
.done .stage-dot { background: var(--green); }.failed .stage-dot { background: var(--red); }.active .stage-dot { background: var(--brand); }
small { color: var(--muted); overflow-wrap: anywhere; }.failed small { color: var(--red); }
@media(max-width: 450px) { .overview-stages { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px 8px; } li::before { display: none; } }
</style>
