<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { emptyLogFilters, type LogEvent, type LogSource, type IdentityField } from '../../app/logHistory'
import { prepareTestLogs } from '../../app/testLogNavigation'
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
<template><nav aria-label="日志关联入口" class="relations"><template v-for="item in relations" :key="item.field"><button v-for="source in (['platform', 'upstream'] as const)" :key="source" @click="open(source, item.field, item.id!)">{{ item.label }} · {{ source === 'platform' ? '本地' : '上游' }}</button></template><RouterLink v-if="auditDestination" :to="auditDestination">打开相关管理页</RouterLink><p class="hint">关联号只用于日志筛选，不能反推原任务号或测试作业号。查询默认最近 24 小时，可在日志页调整；另一源不可用不改变本事件事实。管理页入口不代表已定位审计对象。</p></nav></template>
<style scoped>.relations { display: flex; flex-wrap: wrap; gap: 8px; margin-block: 20px; } p { flex-basis: 100%; }</style>