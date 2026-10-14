<script setup lang="ts">
import { ref } from 'vue'
import Dialog from './Dialog.vue'
import ErrorState from './ErrorState.vue'
import type { CloseReason } from '../shared/dialog'
withDefaults(defineProps<{ open: boolean; title: string; description?: string; busy?: boolean; confirmLabel?: string; canSubmit?: boolean; error?: string }>(), { confirmLabel: '确认提交', canSubmit: true })
const emit = defineEmits<{ 'update:open': [value: boolean]; close: [reason: CloseReason]; confirm: [] }>()
const frame = ref<InstanceType<typeof Dialog> | null>(null)
function close() { frame.value?.dismiss('dismiss') }
</script>
<template><Dialog ref="frame" :open="open" :title="title" :description="description" @update:open="emit('update:open', $event)" @close="emit('close', $event)">
  <slot /><p v-if="busy" class="hint" role="status">正在等待响应。停止等待不会取消已提交的后台操作，受理结果可能仍需确认。</p><ErrorState v-if="error" :message="error" />
  <template #footer><button type="button" @click="close">{{ busy ? '停止等待' : '暂不提交' }}</button><button class="primary" type="button" :disabled="busy || !canSubmit" @click="!busy && canSubmit && emit('confirm')">{{ busy ? '正在等待…' : confirmLabel }}</button></template>
</Dialog></template>
