<script setup lang="ts">
import { ref, watch, nextTick, useId, onActivated, onDeactivated, onUnmounted } from 'vue'
import { registerDialog, trapDialogTab, type CloseReason } from '../shared/dialog'
const props = withDefaults(defineProps<{ open: boolean; title: string; description?: string; variant?: 'dialog' | 'drawer'; wide?: boolean; expanded?: boolean; closeLabel?: string }>(), { variant: 'dialog', closeLabel: '关闭弹窗' })
const emit = defineEmits<{ 'update:open': [value: boolean]; close: [reason: CloseReason] }>()
const dialog = ref<HTMLDialogElement | null>(null), heading = ref<HTMLElement | null>(null)
const titleId = useId(), descriptionId = useId()
let active = true, generation = 0, release: (() => void) | undefined
function closeNative() {
  const wasOpen = !!dialog.value?.open
  dialog.value?.close()
  release?.(); release = undefined
  return wasOpen
}
function dismiss(reason: CloseReason) {
  if (!props.open && !dialog.value?.open) return
  generation++; closeNative(); emit('update:open', false); emit('close', reason)
}
async function synchronize() {
  const run = ++generation
  if (!props.open) { if (closeNative()) emit('close', 'programmatic'); return }
  await nextTick()
  if (!active || run !== generation || !props.open || !dialog.value || dialog.value.open) return
  const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
  release = registerDialog(dialog.value, opener)
  try { dialog.value.showModal() } catch (error) { release(); release = undefined; throw error }
  const initial = dialog.value.querySelector<HTMLElement>('[autofocus]') ?? heading.value
  initial?.focus({ preventScroll: true })
}
watch(() => props.open, synchronize, { immediate: true })
onActivated(() => { active = true; void synchronize() })
onDeactivated(() => { active = false; dismiss('deactivated') })
onUnmounted(() => { active = false; generation++; closeNative() })
function nativeClosed() { if (dialog.value?.open) return; if (release) { release(); release = undefined; emit('update:open', false); emit('close', 'programmatic') } }
let backdropPressed = false
function pressBackdrop(event: PointerEvent) { backdropPressed = event.target === dialog.value }
function backdrop(event: MouseEvent) {
  const element = dialog.value
  if (!backdropPressed || !element || event.target !== element) return
  backdropPressed = false
  const box = element.getBoundingClientRect()
  if (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom) dismiss('backdrop')
}
defineExpose({ dismiss })
</script>
<template>
  <Teleport to="body"><dialog ref="dialog" class="ui-dialog" :class="{ 'ui-drawer': variant === 'drawer', 'ui-dialog-wide': wide, 'ui-dialog-expanded': wide && expanded }" :aria-labelledby="titleId" :aria-describedby="description ? descriptionId : undefined" aria-modal="true" tabindex="-1" @cancel.prevent="dismiss('escape')" @close="nativeClosed" @pointerdown="pressBackdrop" @click="backdrop" @keydown="dialog && trapDialogTab($event, dialog)">
    <header class="dialog-header"><h2 :id="titleId" ref="heading" tabindex="-1">{{ title }}</h2><button class="dialog-close" type="button" :aria-label="closeLabel" @click="dismiss('dismiss')"><span aria-hidden="true">×</span></button></header>
    <div class="dialog-body"><p v-if="description" :id="descriptionId" class="dialog-description">{{ description }}</p><slot /></div>
    <footer v-if="$slots.footer" class="dialog-footer"><slot name="footer" /></footer>
  </dialog></Teleport>
</template>
