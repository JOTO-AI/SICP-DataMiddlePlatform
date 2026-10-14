<script setup lang="ts">
import { ref, watch, useId } from 'vue'
import { parseRequestObject, parseEditableJSON, parameterError, jsonError, type ParameterDefinition } from '../shared/json'
const props = withDefaults(defineProps<{ modelValue: string; parameters?: ParameterDefinition[]; disabled?: boolean }>(), { parameters: () => [] })
const emit = defineEmits<{ 'update:modelValue': [text: string] }>()
const mode = ref<'json' | 'parameters'>('json'), error = ref(''), rows = ref<{ name: string; text: string; error: string }[]>([])
const id = useId(), dirty = ref(false)
watch(() => props.modelValue, () => { if (mode.value === 'parameters') { mode.value = 'json'; rows.value = []; dirty.value = false } error.value = '' })
function toParameters() {
  if (mode.value === 'parameters' || props.disabled) return
  try {
    const body = parseRequestObject(props.modelValue)
    const names = [...new Set([...props.parameters.map(item => item.name), ...Object.keys(body)])]
    rows.value = names.map(name => ({ name, text: Object.hasOwn(body, name) ? JSON.stringify(body[name], null, 2) : '', error: '' }))
    dirty.value = false; error.value = ''; mode.value = 'parameters'
  } catch (cause) { error.value = jsonError(cause) }
}
function toJSON() {
  if (mode.value === 'json' || props.disabled) return
  if (!dirty.value) { mode.value = 'json'; error.value = ''; return props.modelValue }
  const body: Record<string, unknown> = Object.create(null)
  for (const row of rows.value) {
    row.error = ''
    const definition = props.parameters.find(item => item.name === row.name)
    if (!row.text.trim()) { if (definition?.required) row.error = '此参数必填'; continue }
    try { body[row.name] = parseEditableJSON(row.text); if (definition) row.error = parameterError(body[row.name], definition) }
    catch (cause) { row.error = jsonError(cause) }
  }
  if (rows.value.some(row => row.error)) { error.value = '请修正标出的参数；编辑文本已保留。'; return }
  const text = JSON.stringify(body, null, 2)
  try { parseRequestObject(text) } catch (cause) { error.value = jsonError(cause); return }
  mode.value = 'json'; error.value = ''; emit('update:modelValue', text); return text
}
// The caller must read through this method before sending; null means the draft is invalid.
function getRequestText(): string | null {
  const text = mode.value === 'json' ? props.modelValue : toJSON()
  if (text === undefined) return null
  try {
    const body = parseRequestObject(text)
    for (const definition of props.parameters) {
      if (!Object.hasOwn(body, definition.name)) { if (definition.required) throw new Error(`${definition.name}：此参数必填`); continue }
      const issue = parameterError(body[definition.name], definition)
      if (issue) throw new Error(`${definition.name}：${issue}`)
    }
    error.value = ''; return text
  } catch (cause) { error.value = jsonError(cause); return null }
}
function loadText(text: string) { mode.value = 'json'; rows.value = []; dirty.value = false; error.value = ''; emit('update:modelValue', text) }
defineExpose({ getRequestText, loadText })
</script>
<template><div class="request-editor" :aria-busy="disabled || undefined">
  <div class="tabs" aria-label="请求编辑方式"><button type="button" :disabled="disabled" :aria-pressed="mode === 'parameters'" :class="{ active: mode === 'parameters' }" @click="toParameters">参数编辑</button><button type="button" :disabled="disabled" :aria-pressed="mode === 'json'" :class="{ active: mode === 'json' }" @click="toJSON">JSON 编辑</button></div>
  <p v-if="error" :id="`${id}-error`" class="error" role="alert">{{ error }}</p>
  <label v-if="mode === 'json'">请求 JSON<textarea :value="modelValue" :disabled="disabled" :aria-invalid="!!error" :aria-describedby="error ? `${id}-error` : undefined" rows="8" spellcheck="false" @input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)" /></label>
  <template v-else><p class="hint">每项填写 JSON 值：字符串使用双引号，数组使用方括号；留空表示省略。切回 JSON 应用修改；未修改参数时切回 JSON 保留原文。</p><div class="parameter-editor-rows"><label v-for="(row, index) in rows" :key="row.name"><code>{{ row.name }}</code><textarea v-model="row.text" :aria-label="row.name" :disabled="disabled" :aria-invalid="!!row.error" :aria-describedby="row.error ? `${id}-${index}` : undefined" rows="2" spellcheck="false" @input="dirty = true" /><span v-if="row.error" :id="`${id}-${index}`" class="field-error">{{ row.error }}</span></label></div><p v-if="!rows.length" class="hint">没有已声明或已填写的参数，可切回 JSON 添加。</p></template>
</div></template>
