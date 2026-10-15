<script setup lang="ts">
import { computed, ref, onMounted, onActivated, onDeactivated } from 'vue'
import { request, ApiError, describe, type Envelope } from '../../api'
import { notificationPath, readNotificationPage, notificationRules, decisionStatuses, deliveryStatuses, type NotificationKind, type NotificationPage, type NotificationDecision, type NotificationDelivery } from '../../app/notifications'
import { useRequestScope } from '../../composables/useRequestScope'
import Timestamp from '../Timestamp.vue'
import ErrorState from '../ErrorState.vue'
import EmptyState from '../EmptyState.vue'
const props = defineProps<{ kind: NotificationKind; factId?: string }>(), emit = defineEmits<{ select: [id: string] }>()
const scope = useRequestScope(), pages = ref<NotificationPage[]>([]), index = ref(0), busy = ref(false), error = ref('')
const title = computed(() => props.kind === 'decisions' ? '通知决策' : '逐人投递'), current = computed(() => pages.value[index.value])
const decisions = computed(() => current.value?.items as NotificationDecision[] ?? []), deliveries = computed(() => ((current.value?.items ?? []) as NotificationDelivery[]).filter(item => !props.factId || item.fact_id === props.factId))
let active = true
async function read(next = false) {
  if (!active || busy.value || next && (!current.value?.next_cursor || pages.value.length >= 20 && index.value + 1 >= pages.value.length)) return
  if (next && index.value + 1 < pages.value.length) { index.value++; return }
  const cursor = next ? current.value!.next_cursor! : '', run = scope.begin(); busy.value = true; error.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', notificationPath(props.kind, cursor), { signal: run.signal })
    if (!run.current()) return
    const value = readNotificationPage(response.body.data, props.kind)
    if (response.status !== 200 || response.body.code !== 'ok' || !value || next && (value.next_cursor === cursor || value.next_cursor && pages.value.some(p => p.next_cursor === value.next_cursor) || value.items.some(item => pages.value.some(p => p.items.some(old => old.id === item.id))))) throw new ApiError('通知记录或游标与本次读取不一致。', 'invalid_response')
    if (next) { pages.value.push(value); index.value++ } else { pages.value = [value]; index.value = 0 }
  } catch (cause) { if (run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
function activate() { active = true; if (!pages.value.length) void read() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; scope.cancel(); pages.value = []; index.value = 0; busy.value = false; error.value = '' })
</script>
<template><section class="panel" :aria-label="title"><div class="section-bar"><h2>{{ title }}</h2><button :disabled="busy" @click="read()">重新读取{{ title }}</button></div><p v-if="kind === 'decisions'" class="hint">通知关闭或规则关闭时，错误决策仍可查询；零投递不代表没有异常。</p><p v-else class="hint">按每位收件人追踪；通道接受不代表收件箱已送达，未知结果不会自动重发。</p><p v-if="factId" class="hint">仅展示本批中与此测试事实号完全一致的投递；后端尚无该字段的全历史筛选，可逐批继续读取。没有匹配记录不代表尚未创建或从未发送。</p><ErrorState v-if="error" :message="error" /><p v-if="error && current" class="warning">本次读取失败，保留已确认批次。</p><template v-if="current && (kind === 'decisions' ? decisions.length : deliveries.length)"><div class="notification-table" role="region" :aria-label="title + '表格'" tabindex="0"><table v-if="kind === 'decisions'"><thead><tr><th>发生时间 / 规则</th><th>决策</th><th>投递 / 接受 / 待处理 / 未知</th><th>配置版本</th><th>事实编号</th></tr></thead><tbody><tr v-for="item in decisions" :key="item.id"><td><Timestamp :value="item.occurred_at" /><br />{{ notificationRules[item.code] || item.code }}</td><td>{{ decisionStatuses[item.decision] || item.decision }}</td><td>{{ item.deliveries }} / {{ item.accepted }} / {{ item.pending }} / {{ item.unknown }}</td><td>{{ item.revision }}</td><td>{{ item.id }}</td></tr></tbody></table><table v-else><thead><tr><th>收件人</th><th>规则 / 状态</th><th>创建时间</th><th>尝试次数</th><th>详情</th></tr></thead><tbody><tr v-for="item in deliveries" :key="item.id"><td>{{ item.recipient }}</td><td>{{ notificationRules[item.rule_id] || item.rule_id }} / {{ deliveryStatuses[item.status] || item.status }}</td><td><Timestamp :value="item.created_at" /></td><td>{{ item.attempts }}</td><td><button @click="emit('select', item.id)">查看投递详情</button></td></tr></tbody></table></div></template><EmptyState v-else :loading="busy" :title="current ? '本批没有通知记录' : '尚未取得通知记录'" description="空结果只描述本次范围，不代表从未发生错误。" /><div v-if="current" class="paging"><button :disabled="busy || index === 0" @click="index--">上一批{{ title }}</button><span>第 {{ index + 1 }} 批</span><button :disabled="busy || !current.next_cursor || pages.length >= 20 && index + 1 >= pages.length" @click="read(true)">下一批{{ title }}</button><span v-if="current.next_cursor && pages.length >= 20" class="hint">已达 20 批上限，请重新查询。</span></div></section></template>
<style scoped>.panel { margin-block: 20px; } p { margin-block: 12px; } .notification-table { max-width: 100%; overflow: auto; } table { min-width: 720px; width: 100%; } td { overflow-wrap: anywhere; } .paging { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; margin-top: 16px; }</style>