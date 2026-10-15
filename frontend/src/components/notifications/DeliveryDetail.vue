<script setup lang="ts">
import { ref, onMounted, onDeactivated } from 'vue'
import { request, ApiError, describe, type Envelope } from '../../api'
import { notificationID, readDeliveryDetail, deliveryStatuses, type DeliveryDetail } from '../../app/notifications'
import { useRequestScope } from '../../composables/useRequestScope'
import Timestamp from '../Timestamp.vue'
import CopyId from '../CopyId.vue'
import ErrorState from '../ErrorState.vue'
import EmptyState from '../EmptyState.vue'
const props = defineProps<{ id: string }>(), scope = useRequestScope(), value = ref<DeliveryDetail | null>(null), busy = ref(false), error = ref('')
async function read() {
  if (busy.value || !notificationID(props.id)) return
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', '/admin/v1/notifications/' + encodeURIComponent(props.id), { signal: run.signal })
    if (!run.current()) return
    const data = readDeliveryDetail(response.body.data, props.id)
    if (response.status !== 200 || response.body.code !== 'ok' || !data) throw new ApiError('投递详情标识或尝试记录不符合当前请求。', 'invalid_response')
    value.value = data
  } catch (cause) { if (run.current()) error.value = cause instanceof ApiError && cause.status === 404 ? '该投递暂不可查询，不能据此认定从未发送。' : describe(cause) }
  finally { if (run.current()) busy.value = false }
}
onMounted(() => void read()); onDeactivated(() => { scope.cancel(); value.value = null; busy.value = false })
</script>
<template><div><button :disabled="busy" @click="read">刷新投递详情</button><ErrorState v-if="error" :message="error" /><p v-if="error && value" class="warning">保留上次已确认的投递事实。</p><template v-if="value"><p>投递号：<CopyId :value="value.delivery.id" label="投递号" /></p><dl class="facts"><div><dt>收件人</dt><dd>{{ value.delivery.recipient }}</dd></div><div><dt>状态</dt><dd>{{ deliveryStatuses[value.delivery.status] || value.delivery.status }}</dd></div><div><dt>事实号</dt><dd>{{ value.delivery.fact_id }}</dd></div><div><dt>配置版本</dt><dd>{{ value.delivery.revision }}</dd></div><div><dt>截止时间</dt><dd><Timestamp :value="value.delivery.deadline" /></dd></div><div><dt>下次尝试</dt><dd><Timestamp :value="value.delivery.next_attempt_at" empty="未安排" /></dd></div><div><dt>通道接受时间</dt><dd><Timestamp :value="value.delivery.accepted_at" empty="未确认" /></dd></div></dl><p v-if="value.delivery.status === 'unknown'" class="warning">发送结果未知，可能已被通道处理；此页不提供自动重发。</p><h3>逐次尝试</h3><ol><li v-for="item in value.attempts" :key="item.number">第 {{ item.number }} 次 · {{ deliveryStatuses[item.outcome] || item.outcome }} · <Timestamp :value="item.started_at" /> → <Timestamp :value="item.finished_at" empty="结束未确认" /></li></ol><p v-if="value.attempts.length < value.delivery.attempts" class="hint">当前详情仅返回部分尝试记录。</p></template><EmptyState v-else :loading="busy" title="尚未取得投递详情" /></div></template>
<style scoped>p { margin-block: 12px; } dd, li { overflow-wrap: anywhere; } li { padding-block: 8px; }</style>