<script setup lang="ts">
withDefaults(defineProps<{ busy?: boolean; label?: string }>(), { label: '筛选条件' })
const emit = defineEmits<{ submit: [] }>()
</script>
<template><form class="panel filter-bar" :aria-label="label" :aria-busy="busy" @submit.prevent="!busy && emit('submit')"><fieldset :disabled="busy"><slot /></fieldset></form></template>
