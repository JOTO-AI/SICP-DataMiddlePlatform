<script setup lang="ts">
import { computed } from 'vue'
import { displayTimestamp, validTimestamp } from '../shared/display'
const props = defineProps<{ value?: string | null; empty?: string }>()
const valid = computed(() => validTimestamp(props.value) ? props.value : undefined)
</script>
<template><time :datetime="valid" :title="value ?? undefined">{{ displayTimestamp(value, empty) }}</time></template>
