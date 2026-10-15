<script setup lang="ts">
import { computed } from 'vue'
import { displayTimestamp, validTimestamp } from '../shared/display'
const props = defineProps<{ value?: string | null; empty?: string; precise?: boolean }>()
const valid = computed(() => validTimestamp(props.value) ? props.value : undefined)
const text = computed(() => displayTimestamp(props.value, props.empty) + (props.precise && valid.value ? (props.value?.match(/\.\d+/)?.[0] ?? '') : ''))
</script>
<template><time :datetime="valid" :title="value ?? undefined">{{ text }}</time></template>
