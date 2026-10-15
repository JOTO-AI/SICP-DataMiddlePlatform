<script setup lang="ts">
import { computed, ref, onDeactivated } from 'vue'
import ResponseViewer from './ResponseViewer.vue'
const props = defineProps<{ summary: string; text?: string; value?: unknown; httpStatus?: number; requestId?: string; stale?: boolean; label?: string }>()
const expanded = ref(false)
const raw = computed(() => props.text ?? JSON.stringify(props.value, null, 2) ?? '')
function toggle(event: Event) { expanded.value = (event.target as HTMLDetailsElement).open }
onDeactivated(() => { expanded.value = false })
</script>
<template><details :open="expanded" @toggle="toggle"><summary>{{ summary }}</summary><ResponseViewer v-if="expanded" :text="raw" :http-status="httpStatus" :request-id="requestId" :stale="stale" :label="label" /></details></template>