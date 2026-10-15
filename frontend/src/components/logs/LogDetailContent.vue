<script setup lang="ts">
import { computed, ref } from 'vue'
import { milliseconds } from '../../shared/duration'
import { logKinds, logPhases, logOutcomes, type LogDetail } from '../../app/logDetail'
import Timestamp from '../Timestamp.vue'
import CopyId from '../CopyId.vue'
import JsonText from '../JsonText.vue'
import RequestDiagnostics from '../RequestDiagnostics.vue'
import StatusBadge from '../StatusBadge.vue'
import DetailTabs from '../DetailTabs.vue'
import LogTimeline from './LogTimeline.vue'
import LogRelations from './LogRelations.vue'
import LogAttempts from './LogAttempts.vue'
const props = defineProps<{ data: LogDetail; raw: string; busy: boolean }>()
defineEmits<{ refresh: [] }>()
const tab = ref(0), tabs = ['概览', '脱敏详情', '关联记录']
const sourceName = computed(() => props.data.source === 'platform' ? '本地日志库' : '上游调用日志库')
const outcome = computed(() => props.data.outcome ? logOutcomes[props.data.outcome] : '结果未记录')
const failure = computed(() => ['failed', 'interrupted'].includes(props.data.outcome ?? ''))
const identities = [['task_id', '任务关联号'], ['step_id', '步骤关联号'], ['call_id', '逻辑调用关联号'], ['attempt_id', '尝试关联号']] as const
const businessCode = computed(() => props.data.business_code === null ? '未记录' : Number.isSafeInteger(props.data.business_code) ? props.data.business_code : '超出精确数值范围，请查看原文')
</script>
<template><div class="log-detail-content">
<header><div class="heading-row"><h1>调用日志详情</h1><StatusBadge :value="data.outcome" :label="outcome" /><p v-if="data.request_id" class="request-id">请求号：<CopyId :value="data.request_id" label="请求关联号" /></p><button class="refresh" :disabled="busy" @click="$emit('refresh')">重新读取日志详情</button></div><p class="summary-line"><span>HTTP {{ data.http_status ?? '未记录' }}</span><span>业务码 {{ businessCode }}</span><span>耗时 {{ milliseconds(data.duration_ns) }}<template v-if="data.duration_ns !== null"> ms</template></span><span>来源 {{ sourceName }}</span></p></header>
<DetailTabs v-model="tab" :labels="tabs" id="log-details" />
<section v-show="tab === 0" role="tabpanel" id="log-details-panel-0" aria-labelledby="log-details-tab-0">
<div v-if="failure" class="failure-banner"><span aria-hidden="true">!</span><div><strong>{{ logKinds[data.kind] }}{{ outcome }}<template v-if="data.http_status !== null"> · HTTP {{ data.http_status }}</template></strong><p>本事件记录了{{ outcome }}结果，请结合下方诊断和关联记录查看具体原因。</p></div></div>
<div class="overview-grid"><LogTimeline :event="data" /><section class="panel basic-info" aria-label="基本信息"><h2>基本信息</h2><dl>
<div><dt>操作</dt><dd>{{ data.operation ?? '未提供' }}</dd></div><div><dt>请求号</dt><dd><CopyId v-if="data.request_id" :value="data.request_id" label="请求关联号" /><span v-else>未提供关联</span></dd></div><div><dt>日志事件</dt><dd><CopyId :value="data.event_id" label="日志事件号" /></dd></div><div><dt>耗时（ms）</dt><dd>{{ milliseconds(data.duration_ns) }}</dd></div><div><dt>状态</dt><dd>{{ data.outcome ? logOutcomes[data.outcome] : '当前事件未记录结果' }}</dd></div><div><dt>事件类型 / 阶段</dt><dd>{{ logKinds[data.kind] }} / {{ logPhases[data.phase] }}</dd></div><div><dt>来源</dt><dd>{{ sourceName }}</dd></div><div><dt>发生时间</dt><dd><Timestamp precise :value="data.occurred_at" empty="未提供" /></dd></div><div><dt>入库时间</dt><dd><Timestamp precise :value="data.received_at" empty="未提供" /></dd></div></dl></section></div>
<p v-if="data.phase !== 'finished'" class="hint phase-note">当前事件没有结束边界，不能据此推断调用已成功或仍在运行；结束记录可能另行入库。</p>
<details v-if="data.diagnostics !== undefined || data.metadata.authentication || data.response" class="panel diagnostics"><summary>错误诊断（展开查看上游响应）</summary><RequestDiagnostics :evidence="{ diagnostics: data.diagnostics, authentication: data.metadata.authentication, response: data.response, http_status: data.http_status, business_code: data.business_code, path: data.metadata.path }" /></details>
</section>
<section v-show="tab === 0 || tab === 2" :role="tab === 2 ? 'tabpanel' : 'region'" id="log-details-panel-2" :aria-labelledby="tab === 2 ? 'log-details-tab-2' : undefined" :aria-label="tab === 0 ? '关联记录' : undefined"><LogRelations :event="data" /><div v-if="data.kind === 'call'" class="panel"><LogAttempts :call="data" /></div></section>
<section v-show="tab === 0 || tab === 1" :role="tab === 1 ? 'tabpanel' : 'region'" id="log-details-panel-1" :aria-labelledby="tab === 1 ? 'log-details-tab-1' : undefined" :aria-label="tab === 0 ? '脱敏详情' : undefined" class="panel sanitized">
<details :open="tab === 1"><summary>脱敏详情（JSON）</summary><div class="sanitized-body">
<dl class="facts"><div><dt>日志源 / 世代</dt><dd>{{ data.source }} / {{ data.epoch }}</dd></div><div><dt>实体关联号</dt><dd><CopyId :value="data.entity_id" label="日志实体关联号" /></dd></div><div v-for="[field, label] in identities" :key="field"><dt>{{ label }}</dt><dd><CopyId v-if="data[field]" :value="data[field]" :label="label" /><span v-else>未提供关联</span></dd></div><div><dt>尝试编号</dt><dd>{{ data.attempt_no ?? '未提供' }}</dd></div></dl>
<p v-if="data.test">用途：{{ data.test.purpose === 'test' ? '显式测试' : '同步' }} <CopyId v-if="data.test.test_run_id" :value="data.test.test_run_id" label="测试关联号" /></p><p v-if="data.audit">审计动作：{{ data.audit.action }} · 操作者关联号 {{ data.audit.actor_id }} · 对象关联号 {{ data.audit.object_id }}</p>
<p class="hint">正文处理：{{ data.redaction.redacted ? '已记录脱敏处理' : '无附加正文脱敏标记' }} · 原始字节数 {{ data.redaction.original_bytes }} · 字段数 {{ data.redaction.field_count }}</p>
<p v-if="data.redaction.truncated || data.response?.truncated" class="warning">服务端已截断内容，不能视为完整响应。</p><p v-if="data.response?.read_error" class="warning">响应正文未完整读取，当前仅为已收到的证据。</p>
<JsonText :text="raw" :source-truncated="data.redaction.truncated || data.response?.truncated" label="日志脱敏详情" /></div></details>
</section></div></template>
<style scoped>
.log-detail-content { min-width: 0; overflow-wrap: anywhere; } .heading-row { display: flex; align-items: center; flex-wrap: wrap; gap: 12px 20px; } .heading-row h1 { margin: 0; } .request-id { min-width: 0; font-size: 12px; flex: 1; } .request-id :deep(.copy-id) { max-width: 100%; } .refresh { margin-left: auto; font-size: 12px; }
.summary-line { display: flex; flex-wrap: wrap; color: var(--muted); gap: 8px 0; font-size: 13px; margin-block: 12px 16px; } .summary-line span + span { border-left: 1px solid var(--line); margin-left: 16px; padding-left: 16px; }
:deep(.detail-tabs) { padding-inline: 0; margin-bottom: 20px; } .failure-banner { display: flex; gap: 16px; padding: 20px 24px; border: 1px solid var(--red); border-radius: 6px; background: var(--color-danger-soft); color: var(--red); margin-bottom: 16px; } .failure-banner > span { border-radius: 50%; background: var(--red); color: var(--white); width: 24px; height: 24px; flex-shrink: 0; text-align: center; font-weight: 700; } .failure-banner p { margin-top: 8px; font-size: 13px; }
.overview-grid { display: grid; grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr); gap: 12px; } .overview-grid > .panel { margin: 0; padding: 20px; } h2 { font-size: 18px; margin-bottom: 12px; } dl { margin: 0; } .basic-info dl > div { display: grid; grid-template-columns: 100px minmax(0, 1fr); border-top: 1px solid var(--line); font-size: 13px; } .basic-info dt { border-right: 1px solid var(--line); } .basic-info dt, .basic-info dd { padding: 8px 10px; margin: 0; min-width: 0; } .basic-info dd { text-align: left; } .basic-info :deep(.copy-id) { font-size: 11px; } .phase-note { margin-block: 12px; } .diagnostics { margin-top: 16px; } .sanitized { margin-top: 16px; } summary { cursor: pointer; font-weight: 700; } .sanitized-body { margin-top: 20px; } .sanitized-body > p { margin-block: 12px; }
@media(max-width: 1000px) { .overview-grid { grid-template-columns: minmax(0, 1fr); } } @media(max-width: 600px) { .request-id { flex-basis: 100%; } .refresh { margin-left: 0; } .overview-grid > .panel { padding: 16px; } .basic-info dl > div { grid-template-columns: 85px minmax(0,1fr); } .failure-banner { padding: 16px; } }
.basic-info :deep(.copy-id) { display: flex; width: 100%; } .basic-info :deep(.copy-id code) { flex: 1; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; } .request-id { display: flex; align-items: center; gap: 8px; } .request-id :deep(.copy-id) { flex: 1; min-width: 0; max-width: 340px; } .request-id :deep(.copy-id code) { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
