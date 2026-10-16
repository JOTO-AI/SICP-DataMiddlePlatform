<script setup lang="ts">
import { computed, ref, watch, nextTick, onActivated, onDeactivated, onUnmounted } from 'vue'
import { formatResponseJSON } from '../shared/formatResponseJSON'
import { boundedText } from '../shared/json'
const props = withDefaults(defineProps<{ text: string; label?: string; sourceTruncated?: boolean; pretty?: boolean }>(), { label: 'JSON 内容' })
const expanded = ref(false), message = ref(''), copying = ref(false), manual = ref(false), selection = ref<HTMLTextAreaElement | null>(null)
const raw = ref(false)
const formatted = computed(() => props.pretty ? formatResponseJSON(props.text) : null)
const displayText = computed(() => props.pretty && !raw.value && formatted.value !== null ? formatted.value : props.text)
const displayLabel = computed(() => props.pretty && !raw.value && formatted.value !== null ? props.label.replace(/原文$/, 'JSON') : props.label)
const visible = computed(() => boundedText(displayText.value, expanded.value))
let active = true, generation = 0
function resetCopy() { generation++; copying.value = false; manual.value = false; message.value = '' }
watch(() => props.text, () => { expanded.value = false; raw.value = false; resetCopy() })
watch(raw, () => { expanded.value = false; resetCopy() })
watch(expanded, resetCopy)
onActivated(() => { active = true })
onDeactivated(() => { active = false; resetCopy() })
onUnmounted(() => { active = false; resetCopy() })
async function copy() {
  if (copying.value) return
  const run = ++generation, text = visible.value.text
  const current = () => active && generation === run
  copying.value = true; message.value = ''; manual.value = false
  try {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable')
    await navigator.clipboard.writeText(text)
    if (current()) message.value = '已复制当前显示文本'
  } catch {
    if (!current()) return
    manual.value = true; message.value = '请选中下方文本后手动复制'
    await nextTick(); if (current()) { selection.value?.focus(); selection.value?.select() }
  } finally { if (current()) copying.value = false }
}
</script>
<template><div class="json-viewer">
  <div v-if="pretty" class="tabs" aria-label="JSON 显示方式"><button type="button" :disabled="formatted === null" :aria-pressed="!raw && formatted !== null" :class="{ active: !raw && formatted !== null }" @click="raw = false">格式化 JSON</button><button type="button" :aria-pressed="raw || formatted === null" :class="{ active: raw || formatted === null }" @click="raw = true">查看原文</button></div><p v-if="pretty && formatted === null" class="hint">当前内容无法格式化，按原文显示。</p>
  <div class="json-toolbar"><span class="hint">{{ visible.text.length }} / {{ displayText.length }} 字符</span><div class="actions"><button v-if="displayText.length > 4096" type="button" :aria-expanded="expanded" @click="expanded = !expanded">{{ expanded ? '收起内容' : '展开内容（最多 65536 字符）' }}</button><button type="button" :disabled="copying || !text" @click="copy">{{ copying ? '复制中…' : '复制当前显示文本' }}</button></div></div>
  <p v-if="sourceTruncated" class="warning">服务端已截断内容，当前文本不是完整响应。</p>
  <p v-if="visible.truncated" class="hint">显示已截断，仅展示前 {{ visible.text.length }} 字符；复制仅包含当前显示文本。</p>
  <pre :aria-label="displayLabel" tabindex="0">{{ visible.text }}</pre>
  <p v-if="message" class="hint" role="status">{{ message }}</p><textarea v-if="manual" ref="selection" :aria-label="`手动复制${displayLabel}`" :value="visible.text" readonly rows="6" spellcheck="false" />
</div></template>
