<script setup lang="ts">
import { computed } from 'vue'
import { displayEnum } from '../shared/display'
type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'progress'
const props = defineProps<{ value?: string | null; labels?: Record<string, string>; label?: string; tone?: Tone; icon?: 'check' }>()
const defaults: Record<string, string> = { queued: '排队中', accepted: '已受理', running: '执行中', published_pending_progress: '已发布，正在确认进度', success_published: '发布成功', success_unchanged: '检查完成，无变化', succeeded: '已完成', failed: '执行失败', unknown: '未确认', ready: '已就绪', not_ready: '尚未就绪' }
const tones: Record<string, Tone> = { ready: 'success', success_published: 'success', success_unchanged: 'success', succeeded: 'success', failed: 'danger', not_ready: 'warning', queued: 'progress', accepted: 'progress', running: 'progress', published_pending_progress: 'progress' }
const text = computed(() => props.label ?? displayEnum(props.value, { ...defaults, ...props.labels }))
const tone = computed(() => props.tone ?? (props.value && Object.hasOwn(tones, props.value) ? tones[props.value] : 'neutral'))
</script>
<template><span class="tag" :class="tone" :title="value ?? undefined"><svg v-if="icon === 'check'" class="status-check" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><circle cx="10" cy="10" r="9" fill="currentColor" /><path d="m6 10 2.5 2.5L14 7" fill="none" stroke="white" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" /></svg>{{ text }}</span></template>

<style scoped>
.status-check { width: 16px; height: 16px; flex-shrink: 0; margin-right: 6px; color: var(--color-success-text); }
</style>
