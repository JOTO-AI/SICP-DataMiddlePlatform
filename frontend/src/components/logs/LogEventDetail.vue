<script setup lang="ts">
import { ref, onMounted, onActivated, onDeactivated } from 'vue'
import { request, describe, ApiError, type Envelope } from '../../api'
import { readLogDetail, type LogDetail } from '../../app/logDetail'
import { logDetailPath } from '../../app/links'
import type { LogSource } from '../../app/logHistory'
import { objectTokens } from '../../shared/jsonObject'
import { useRequestScope } from '../../composables/useRequestScope'
import ErrorState from '../ErrorState.vue'
import EmptyState from '../EmptyState.vue'
import LogDetailContent from './LogDetailContent.vue'
import LogAttempts from './LogAttempts.vue'
import LogRelations from './LogRelations.vue'
const props = defineProps<{ source: LogSource; eventId: string; epoch?: string; sequence?: string }>()
const scope = useRequestScope(), data = ref<LogDetail | null>(null), raw = ref(''), busy = ref(false), error = ref('')
let active = true
async function read() {
  if (!active || busy.value) return
  const path = logDetailPath(props.source, props.eventId)
  if (!path) { error.value = '日志来源或事件号无效。'; return }
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', '/admin/v1' + path, { signal: run.signal, captureResponse: true, domain: 'logs' })
    if (!active || !run.current()) return
    const value = readLogDetail(response.body.data, props.source, props.eventId, props.epoch ?? data.value?.epoch, props.sequence ?? data.value?.sequence)
    if (response.status !== 200 || response.body.code !== 'ok' || !value || !response.responseText || response.responseText.length > 2 * 1024 * 1024) throw new ApiError('日志详情与所选事件、世代或字段契约不一致，请返回历史重新查询。', 'invalid_response')
    const text = objectTokens(response.responseText).get('data')
    if (!text) throw new ApiError('未取得日志详情原文。', 'invalid_response')
    data.value = value; raw.value = text
  } catch (cause) { if (active && run.current()) error.value = cause instanceof ApiError && cause.status === 404 ? '该事件暂不可查询，可能已过保留期或来源已变化；不能据此判断从未执行。' : describe(cause) }
  finally { if (run.current()) busy.value = false }
}
function activate() { active = true; if (!data.value) void read() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; scope.cancel(); data.value = null; raw.value = ''; busy.value = false; error.value = '' })
</script>
<template><div><div class="section-bar"><h2>事件内容</h2><button type="button" :disabled="busy" @click="read">重新读取日志详情</button></div><ErrorState v-if="error" :message="error" /><p v-if="data && error" class="warning">保留上次确认的事件内容，本次读取未成功。</p><LogDetailContent v-if="data" :data="data" :raw="raw" /><EmptyState v-else :loading="busy" title="尚未取得日志详情" description="这里只读取所选事件，不重新调用业务或上游接口。" /><LogRelations v-if="data" :event="data" /><LogAttempts v-if="data?.source === 'upstream' && data.kind === 'call'" :call="data" /></div></template>