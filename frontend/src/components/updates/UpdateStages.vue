<script setup lang="ts">
import { computed } from 'vue'
import Timestamp from '../Timestamp.vue'
import { updateStageStates } from '../../app/updateStageStates'
import type { UpdateDetail } from '../../app/updateEvents'
const props = defineProps<{ task: UpdateDetail }>()
const stages = computed(() => updateStageStates(props.task))
</script>
<template><section class="stage-strip panel" aria-label="更新阶段进度"><ol><li v-for="(stage, index) in stages" :key="stage.title" :class="{ confirmed: stage.state === 'done', current: stage.state === 'active' }"><span class="stage-marker" aria-hidden="true">{{ stage.state === 'done' ? '✓' : stage.state === 'skipped' ? '—' : index + 1 }}</span><strong>{{ stage.title }}</strong><span>{{ stage.label }}</span><Timestamp v-if="stage.at" :value="stage.at" /><small v-else-if="stage.note">{{ stage.note }}</small></li></ol><p class="hint">阶段状态综合任务结果和采集进度展示；已完成但未记录独立时间的阶段不补写时间。各次执行记录见操作树。</p></section></template>
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
