<script setup lang="ts">
import { computed, reactive, ref, onMounted, onActivated, onDeactivated } from 'vue'
import { request, ApiError, describe, type Envelope } from '../../api'
import { configurableRules, channelResults, readNotificationSettings, readNotificationSettingsView, notificationPayload, type NotificationDraft, type NotificationSettingsView, type NotificationSettings } from '../../app/notificationSettings'
import { notificationRules } from '../../app/notifications'
import { useRequestScope } from '../../composables/useRequestScope'
import ErrorState from '../ErrorState.vue'
import Timestamp from '../Timestamp.vue'
import MailTest from './MailTest.vue'
const reads = useRequestScope(), writes = useRequestScope()
const view = ref<NotificationSettingsView | null>(null), saved = ref<NotificationSettings | null>(null), comparison = ref<NotificationSettings | null>(null), busy = ref(false), saving = ref(false), error = ref(''), notice = ref(''), conflict = ref(false)
const draft = reactive<NotificationDraft>({ enabled: false, recipients: [], rules: {} })
const dirty = computed(() => !!saved.value && JSON.stringify({ enabled: draft.enabled, recipients: draft.recipients, rules: draft.rules }) !== JSON.stringify({ enabled: saved.value.enabled, recipients: saved.value.recipients, rules: Object.fromEntries(configurableRules.map(k => [k, saved.value!.rules[k] === true])) }))
let active = true
function adopt(value: NotificationSettings) { saved.value = { ...value, recipients: [...value.recipients], rules: { ...value.rules } }; draft.enabled = value.enabled; draft.recipients = [...value.recipients]; draft.rules = Object.fromEntries(configurableRules.map(k => [k, value.rules[k] === true])); comparison.value = null; conflict.value = false; error.value = ''; notice.value = '' }
async function read() {
  if (!active || busy.value || saving.value) return
  const run = reads.begin(); busy.value = true; error.value = ''; notice.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', '/admin/v1/settings/notifications', { signal: run.signal })
    if (!run.current()) return
    const data = readNotificationSettingsView(response.body.data)
    if (response.status !== 200 || response.body.code !== 'ok' || !data) throw new ApiError('邮件配置响应不符合当前契约。', 'invalid_response')
    view.value = data
    if (dirty.value || conflict.value) { comparison.value = data.settings; notice.value = '已读取服务端配置，本地编辑仍保留。请比较后决定是否采用。' }
    else adopt(data.settings)
  } catch (cause) { if (run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
async function save() {
  if (!active || saving.value || busy.value || conflict.value || !saved.value || !view.value) return
  let payload: ReturnType<typeof notificationPayload>
  try { payload = notificationPayload(saved.value.revision, draft, view.value.channel_diagnostics.configured) }
  catch (cause) { error.value = cause instanceof Error ? cause.message : '邮件配置无效。'; return }
  const run = writes.begin(); saving.value = true; error.value = ''; notice.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', '/admin/v1/settings/notifications', { method: 'PUT', body: payload, signal: run.signal })
    if (!run.current()) return
    const data = readNotificationSettings(response.body.data)
    if (response.status !== 200 || response.body.code !== 'ok' || !data || BigInt(data.revision) <= BigInt(payload.revision) || data.enabled !== payload.enabled || JSON.stringify(data.recipients) !== JSON.stringify(payload.recipients) || configurableRules.some(k => (data.rules[k] === true) !== payload.rules[k])) throw new ApiError('保存结果尚未确认，请重新读取并比较；本地编辑仍保留。', 'invalid_response')
    adopt(data); view.value = { ...view.value!, settings: data }; notice.value = '配置已保存；本次没有发送邮件。'
  } catch (cause) {
    if (run.current()) {
      error.value = describe(cause)
      if (cause instanceof ApiError && (cause.code === 'revision_conflict' || cause.code === 'invalid_response' || !cause.status || cause.status >= 500)) conflict.value = true
    }
  }
  finally { if (run.current()) saving.value = false }
}
function keepEdits() {
  if (!comparison.value || busy.value || saving.value) return
  const value = comparison.value
  saved.value = { ...value, recipients: [...value.recipients], rules: { ...value.rules } }
  comparison.value = null; conflict.value = false; error.value = ''; notice.value = '已使用最新版本作为编辑依据，本地内容保留；请核对后再次点击保存。'
}
function activate() { active = true; if (!view.value) void read() }
onMounted(activate); onActivated(activate)
onDeactivated(() => { active = false; reads.cancel(); writes.cancel(); busy.value = false; saving.value = false; view.value = null; saved.value = null; comparison.value = null; draft.recipients = []; draft.rules = {}; draft.enabled = false; error.value = ''; notice.value = ''; conflict.value = false })
</script>
<template><section class="panel" aria-label="邮件通知设置"><div class="section-bar"><h2>邮件通知设置</h2><button :disabled="busy || saving" @click="read">读取邮件配置</button></div><ErrorState v-if="error" :message="error" /><p v-if="notice" role="status">{{ notice }}</p><template v-if="view && saved"><p class="hint">通道：{{ view.channel_state === 'configured' ? '配置已提供' : '尚未配置' }} · 配置版本 {{ saved.revision }} · 保存时间 <Timestamp :value="saved.updated_at" /></p><p v-if="view.channel_state === 'unconfigured'" class="hint">邮件通道尚未配置，仍可保存关闭状态；查询、更新和日志不受影响。</p><p v-if="conflict" class="warning">版本冲突或保存结果未确认，本地编辑仍保留。请先读取邮件配置并比较，不自动覆盖。</p><section aria-label="邮件通道观察"><h3>最近实际发送观察</h3><p>{{ Object.hasOwn(channelResults, view.channel_diagnostics.last_result) ? channelResults[view.channel_diagnostics.last_result] : '未识别的观察状态' }}</p><p>观察时间：<Timestamp :value="view.channel_diagnostics.observed_at" empty="未提供" /></p><p class="hint">配置已提供只表示部署配置存在；尚未观测不代表通道健康，单次接受也不代表收件箱送达。</p></section><form @submit.prevent="save"><fieldset :disabled="busy || saving"><label class="check-row"><input v-model="draft.enabled" type="checkbox" />启用邮件通知</label><p class="hint">最多 20 个地址。清空收件人会保存为关闭；保存配置不会发送测试邮件。未保存编辑仅留在当前页面，离开后清除。关闭或删地址只影响尚未发送的投递，不会撤回已被通道接受的邮件。</p><p v-if="draft.enabled && !draft.recipients.length" class="warning">没有收件人，本次将保存为关闭通知。</p><div v-for="(_, index) in draft.recipients" :key="index" class="recipient"><label :for="`recipient-${index}`">收件人 {{ index + 1 }}</label><input :id="`recipient-${index}`" v-model="draft.recipients[index]" type="text" inputmode="email" autocomplete="off" spellcheck="false" maxlength="254" /><button type="button" :aria-label="`删除收件人 ${index + 1}`" @click="draft.recipients.splice(index, 1)">删除</button></div><button type="button" :disabled="draft.recipients.length >= 20" @click="draft.recipients.push('')">添加收件人</button><fieldset class="rules"><legend>当前支持的通知规则</legend><p class="hint">当前仅支持以下三类规则；更多规则和阈值待服务端提供保存契约后接入。</p><label v-for="rule in configurableRules" :key="rule" class="check-row"><input v-model="draft.rules[rule]" type="checkbox" />{{ notificationRules[rule] }}</label></fieldset><button type="submit" class="primary" :disabled="conflict">{{ saving ? '保存中…' : '保存邮件配置' }}</button><span v-if="dirty" class="hint"> 有未保存修改</span></fieldset></form><section v-if="comparison" class="comparison"><h3>最新服务端配置</h3><p>版本 {{ comparison.revision }} · {{ comparison.enabled ? '已启用' : '已关闭' }} · {{ comparison.recipients.join('，') || '无收件人' }}</p><ul><li v-for="rule in configurableRules" :key="rule">{{ notificationRules[rule] }}：{{ comparison.rules[rule] ? '开启' : '关闭' }}</li></ul><p>服务端保存时间：<Timestamp :value="comparison.updated_at" /></p><button :disabled="saving || busy" @click="keepEdits">使用此版本，保留本地编辑</button> <button :disabled="saving || busy" @click="adopt(comparison!)">采用服务端配置，放弃本地编辑</button></section></template><p v-else role="status">{{ busy ? '正在读取邮件配置…' : '尚未取得配置，不提供默认保存值。' }}</p><MailTest :settings="saved" :configured="view?.channel_diagnostics.configured === true" :blocked="dirty || conflict || busy || saving" /></section></template>
<style scoped>.panel { margin-block: 20px; } p { margin-block: 12px; } fieldset { border: 0; min-width: 0; padding: 0; } .rules { margin-block: 20px; } .rules label { margin-block: 8px; } .recipient { display: grid; grid-template-columns: 90px minmax(0, 1fr) auto; gap: 10px; align-items: center; margin-block: 12px; } .comparison { margin-top: 20px; overflow-wrap: anywhere; } @media(max-width: 600px) { .recipient { grid-template-columns: minmax(0, 1fr) auto; } .recipient label { grid-column: 1 / -1; } }</style>