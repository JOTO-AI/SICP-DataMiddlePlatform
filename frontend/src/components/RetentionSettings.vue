<script setup lang="ts">
import { ref, computed, onMounted, onActivated, onDeactivated } from 'vue'
import { request, ApiError, describe, type Envelope } from '../api'
import { readRetention, retentionPayload, type Retention } from '../app/retention'
import { useRequestScope } from '../composables/useRequestScope'
import ErrorState from './ErrorState.vue'
const emit = defineEmits<{ changed: [revision: string] }>()
const saved = ref<Retention | null>(null), comparison = ref<Retention | null>(null), platform = ref(''), upstream = ref(''), busy = ref(false), saving = ref(false), conflict = ref(false), error = ref(''), notice = ref('')
const scope = useRequestScope(), dirty = computed(() => !!saved.value && (platform.value !== String(saved.value.platform_days) || upstream.value !== String(saved.value.upstream_days)))
let active = true
function adopt(value: Retention) { saved.value = value; platform.value = String(value.platform_days); upstream.value = String(value.upstream_days); comparison.value = null; conflict.value = false; error.value = ''; emit('changed', value.revision) }
async function read() {
  if (!active || busy.value || saving.value) return
  const run = scope.begin(); busy.value = true; error.value = ''; notice.value = ''
  try {
    const result = await request<Envelope<unknown>>('worker', '/admin/v1/settings/retention', { signal: run.signal })
    if (!run.current()) return
    const value = readRetention(result.body.data)
    if (result.status !== 200 || result.body.code !== 'ok' || !value) throw new ApiError('保留期响应不符合契约。', 'invalid_response')
    if (dirty.value || conflict.value) { comparison.value = value; emit('changed', value.revision) }
    else adopt(value)
  } catch (cause) { if (run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
async function save() {
  if (!active || busy.value || saving.value || conflict.value || !saved.value) return
  let payload: Retention
  try { payload = retentionPayload(saved.value.revision, platform.value, upstream.value) }
  catch (cause) { error.value = cause instanceof Error ? cause.message : '保留期输入无效。'; return }
  const run = scope.begin(); saving.value = true; error.value = ''; notice.value = ''
  try {
    const result = await request<Envelope<unknown>>('worker', '/admin/v1/settings/retention', { method: 'PUT', body: payload, signal: run.signal })
    if (!run.current()) return
    const value = readRetention(result.body.data)
    if (result.status !== 200 || result.body.code !== 'ok' || !value || BigInt(value.revision) <= BigInt(payload.revision) || value.platform_days !== payload.platform_days || value.upstream_days !== payload.upstream_days) throw new ApiError('保存结果尚未确认，请读取并比较。', 'invalid_response')
    adopt(value); notice.value = '保留期已保存；本次没有执行日志清理。'
  } catch (cause) {
    if (run.current()) { error.value = describe(cause); conflict.value = true }
  } finally { if (run.current()) saving.value = false }
}
function rebase() { if (!comparison.value || busy.value || saving.value) return; saved.value = comparison.value; comparison.value = null; conflict.value = false; error.value = ''; notice.value = '已使用最新版本，保留本地编辑；请核对后再次保存。'; emit('changed', saved.value.revision) }
function activate() { active = true; if (!saved.value) void read() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; scope.cancel(); saved.value = null; comparison.value = null; platform.value = ''; upstream.value = ''; busy.value = false; saving.value = false; conflict.value = false; error.value = ''; notice.value = '' })
</script>
<template><section id="retention" class="panel" aria-label="日志保留期"><div class="section-bar"><h2>日志保留期</h2><button :disabled="busy || saving" @click="read">读取保留期</button></div><ErrorState v-if="error" :message="error" /><p v-if="notice" role="status">{{ notice }}</p><template v-if="saved"><p>上次确认配置：版本 {{ saved.revision }} · 平台 {{ saved.platform_days }} 天 · 上游 {{ saved.upstream_days }} 天</p><p v-if="conflict" class="warning">版本冲突或保存未确认，本地编辑仍保留。上方是上次确认值，请重新读取比较。</p><form @submit.prevent="save" novalidate><fieldset :disabled="busy || saving"><div class="form-grid"><label>平台日志保留天数<input v-model="platform" inputmode="numeric" type="text" maxlength="4" /></label><label>上游日志保留天数<input v-model="upstream" inputmode="numeric" type="text" maxlength="4" /></label></div><p class="hint">每源 1 至 3650 天；保存策略不立即删除记录。未保存编辑离页后清除。</p><button type="submit" class="primary" :disabled="conflict">保存保留期</button><span v-if="dirty" class="hint"> 有未保存修改</span></fieldset></form></template><p v-else role="status">{{ busy ? '正在读取保留期…' : '尚未取得保留期，不提供默认保存值。' }}</p><section v-if="comparison"><h3>最新服务端保留期</h3><p>版本 {{ comparison.revision }} · 平台 {{ comparison.platform_days }} 天 · 上游 {{ comparison.upstream_days }} 天</p><button :disabled="busy || saving" @click="adopt(comparison!)">采用服务端保留期</button> <button :disabled="busy || saving" @click="rebase">保留编辑，使用最新版本</button></section></section></template>
<style scoped>.panel { margin-block: 20px; } p { margin-block: 12px; } fieldset { border: 0; padding: 0; min-width: 0; } .form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; } input { width: 100%; } @media(max-width: 600px) { .form-grid { grid-template-columns: minmax(0, 1fr); } }</style>