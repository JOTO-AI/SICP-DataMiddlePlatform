<script setup lang="ts">
import { nextTick, ref } from 'vue'
const props = defineProps<{ modelValue: number; labels: string[]; id: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: number] }>()
const buttons = ref<HTMLButtonElement[]>([])
function key(event: KeyboardEvent, index: number) {
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? props.labels.length - 1 : event.key === 'ArrowRight' ? (index + 1) % props.labels.length : event.key === 'ArrowLeft' ? (index + props.labels.length - 1) % props.labels.length : null
  if (next === null) return
  event.preventDefault(); emit('update:modelValue', next); void nextTick(() => buttons.value[next]?.focus())
}
</script>
<template><div class="tabs detail-tabs" role="tablist" :aria-label="id"><button v-for="(label, index) in labels" :key="label" :ref="el => { if (el) buttons[index] = el as HTMLButtonElement }" role="tab" :id="id + '-tab-' + index" :aria-controls="id + '-panel-' + index" :aria-selected="modelValue === index" :tabindex="modelValue === index ? 0 : -1" :class="{ active: modelValue === index }" @click="emit('update:modelValue', index)" @keydown="key($event, index)">{{ label }}</button></div></template>
<style scoped>.detail-tabs { padding-inline: 20px; gap: 32px; } .detail-tabs button { font-size: 14px; padding: 14px 0; } @media(max-width: 600px) { .detail-tabs { padding-inline: 16px; gap: 24px; } }</style>
