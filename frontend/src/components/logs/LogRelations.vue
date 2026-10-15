<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { emptyLogFilters, type LogEvent, type LogSource, type IdentityField } from '../../app/logHistory'
import { prepareTestLogs } from '../../app/testLogNavigation'
import CopyId from '../CopyId.vue'
const props = defineProps<{ event: LogEvent }>(), router = useRouter()
const relations = computed(() => [
  { field: 'request_id' as const, id: props.event.request_id, label: '同请求日志' },
  { field: 'task_id' as const, id: props.event.task_id, label: '同任务日志' },
  { field: 'test_run_id' as const, id: props.event.test?.test_run_id, label: '同测试日志' },
].filter(item => !!item.id))
async function open(source: LogSource, field: IdentityField, identity: string) {
  if (prepareTestLogs({ ...emptyLogFilters(source), identityField: field, identity, format: 'correlation' })) await router.push({ name: 'log-source', params: { source } })
}
const auditDestinations: Record<string, string> = { source_test: '/apis', notification_settings: '/notifications', notification_test: '/notifications', retention_settings: '/settings', cleanup: '/settings' }
const auditDestination = computed(() => auditDestinations[props.event.audit?.action ?? ''])
</script>
<template><section class="relations panel" aria-label="日志关联入口"><h2>关联记录</h2><div class="relation-scroll" role="region" aria-label="关联记录表格" tabindex="0"><table><thead><tr><th>数据来源</th><th>事件 / 关联标识</th><th>关联关系</th><th>说明</th><th>操作</th></tr></thead><tbody><tr><td>{{ event.source === 'platform' ? '本地日志库' : '上游调用日志库' }}</td><td><CopyId :value="event.event_id" label="当前日志事件号" /></td><td>当前事件</td><td>已读取的脱敏日志</td><td>当前详情</td></tr><template v-for="item in relations" :key="item.field"><tr v-for="source in (['platform', 'upstream'] as const)" :key="source"><td>{{ source === 'platform' ? '本地日志库' : '上游调用日志库' }}</td><td><CopyId :value="item.id!" :label="item.label + '关联号'" /></td><td>{{ item.label }}</td><td>待查询关联记录</td><td><button class="relation-action" :aria-label="item.label + ' · ' + (source === 'platform' ? '本地' : '上游')" @click="open(source, item.field, item.id!)">查看 <span aria-hidden="true">›</span></button></td></tr></template></tbody></table></div><p v-if="!relations.length" class="hint">当前事件未提供可查询的请求、任务或测试关联号。</p><RouterLink v-if="auditDestination" :to="auditDestination">打开相关管理页</RouterLink><p class="hint">关联号只用于日志筛选，不能反推原任务号或测试作业号。查询默认最近 24 小时，可在日志页调整；另一源不可用不改变本事件事实。管理页入口不代表已定位审计对象。</p></section></template>
<style scoped>.relations { margin-top: 16px; min-width: 0; } h2 { font-size: 18px; margin-bottom: 12px; } .relation-scroll { overflow: auto; } table { width: 100%; min-width: 650px; font-size: 13px; } th, td { padding: 8px 12px; border: 1px solid var(--line); } th { background: var(--color-surface-muted); } td:nth-child(2) { max-width: 250px; } .relation-action { border: 0; background: transparent; color: var(--brand); padding: 4px; font-size: 13px; white-space: nowrap; } .relation-action span { margin-left: 10px; } .hint { margin-top: 12px; }:deep(.copy-id) { display: flex; width: 100%; } :deep(.copy-id code) { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } </style>
