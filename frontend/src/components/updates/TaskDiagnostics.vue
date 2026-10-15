<script setup lang="ts">
import { computed } from 'vue'
import type { WorkProgress as Progress } from '../../workProgress'
import type { Acquisition } from '../../acquisition'
import WorkProgress from './WorkProgress.vue'
import AcquisitionProgress from './AcquisitionProgress.vue'
import FailedAttempts, { type FailedAttempt } from './FailedAttempts.vue'
import { record } from '../../app/notifications'
const props = defineProps<{ task: { status: string; started_at?: string | null; finished_at?: string | null; failure?: string | null; diagnostics_unavailable?: boolean; work_progress?: unknown; acquisition?: unknown; failed_attempts?: unknown }; allowUpdate?: boolean; readOnly?: boolean; mode?: 'progress' | 'failures' }>()
defineEmits<{ updateTable: [table: string] }>()
const progress = computed(() => record(props.task.work_progress) && typeof props.task.work_progress.phase === 'string' && (!props.task.work_progress.tables || Array.isArray(props.task.work_progress.tables) && props.task.work_progress.tables.every(t => typeof t === 'string')) ? props.task.work_progress as unknown as Progress : undefined)
const acquisition = computed(() => record(props.task.acquisition) && Array.isArray(props.task.acquisition.tables) && props.task.acquisition.tables.every(t => record(t) && typeof t.table === 'string' && typeof t.status === 'string') ? props.task.acquisition as unknown as Acquisition : undefined)
function validAttempt(item: unknown): boolean {
  if (!record(item) || typeof item.source !== 'string' || typeof item.at !== 'string' || typeof item.failure !== 'string') return false
  return true
}
const attempts = computed(() => Array.isArray(props.task.failed_attempts) ? props.task.failed_attempts.filter(validAttempt) as unknown as FailedAttempt[] : undefined)
const malformed = computed(() => props.task.work_progress != null && !progress.value || props.task.acquisition != null && !acquisition.value || props.task.failed_attempts != null && (!Array.isArray(props.task.failed_attempts) || attempts.value?.length !== props.task.failed_attempts.length))
</script>
<template><section aria-label="任务执行与诊断"><p v-if="malformed" class="warning">部分诊断字段不完整，保留可确认的任务事实。</p><WorkProgress v-if="mode !== 'failures'" :progress="progress" :status="task.status" :started-at="task.started_at ?? undefined" :finished-at="task.finished_at ?? undefined" /><AcquisitionProgress v-if="mode !== 'failures'" :progress="acquisition" :status="task.status" :allow-update="allowUpdate" :read-only="readOnly" @update-table="$emit('updateTable', $event)" /><FailedAttempts v-if="mode !== 'progress'" :attempts="attempts" :unavailable="task.diagnostics_unavailable" :failed="task.failure === 'acquisition_failed'" /><p v-if="readOnly"><RouterLink to="/updates">前往更新操作核对重跑与单表更新条件</RouterLink></p></section></template>