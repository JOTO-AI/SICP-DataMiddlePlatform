<script setup lang="ts">
import Dialog from './Dialog.vue'
import type { CloseReason } from '../shared/dialog'
defineProps<{ open: boolean; title: string; description?: string }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; close: [reason: CloseReason] }>()
</script>
<template><Dialog :open="open" :title="title" :description="description" variant="drawer" close-label="关闭详情" @update:open="emit('update:open', $event)" @close="emit('close', $event)"><slot /><template v-if="$slots.footer" #footer><slot name="footer" /></template></Dialog></template>
