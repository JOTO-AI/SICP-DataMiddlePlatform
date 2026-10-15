<script setup lang="ts">
import { ref, nextTick, onUnmounted, onActivated, onDeactivated, watch } from 'vue'
const props = withDefaults(defineProps<{ value?: string | null; label?: string }>(), { label: '编号' })
const message = ref(''), copying = ref(false), manual = ref(false), input = ref<HTMLInputElement | null>(null)
let alive = true, generation = 0
function reset() { generation++; copying.value = false; manual.value = false; message.value = '' }
watch(() => props.value, reset)
onActivated(() => { alive = true })
onDeactivated(() => { alive = false; reset() })
onUnmounted(() => { alive = false; reset() })
async function copy() {
  if (!props.value || copying.value) return
  const run = ++generation
  const current = () => alive && run === generation
  copying.value = true; manual.value = false; message.value = ''
  try {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable')
    await navigator.clipboard.writeText(props.value)
    if (current()) message.value = '已复制'
  } catch {
    if (!current()) return
    manual.value = true; message.value = '请选中编号后复制'
    await nextTick(); if (current()) { input.value?.focus(); input.value?.select() }
  } finally { if (current()) copying.value = false }
}
</script>
<template><span class="copy-id"><code :title="value ?? undefined">{{ value || '未提供' }}</code><button type="button" :disabled="!value || copying" :aria-label="`复制${label}`" @click="copy">{{ copying ? '复制中…' : '复制' }}</button><span class="hint" role="status">{{ message }}</span><input v-if="manual" ref="input" :aria-label="`手动复制${label}`" :value="value" readonly @focus="($event.target as HTMLInputElement).select()"></span></template>
