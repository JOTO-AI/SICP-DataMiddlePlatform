<script setup lang="ts">
import { ref, onActivated, onDeactivated, onUnmounted } from 'vue'
import { consumeSourceLogs } from '../app/logNavigation'
import type { LogTaskContext } from '../app/links'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import CopyId from '../components/CopyId.vue'
const context = ref<LogTaskContext | null>(null)
onActivated(() => { context.value = consumeSourceLogs() })
onDeactivated(() => { context.value = null })
onUnmounted(() => { context.value = null })
</script>
<template><section><PageHeader title="日志中心" /><section v-if="context" class="panel" aria-label="待查询的源日志"><h2>上游日志关联</h2><p>已接收源记录中的日志关联任务号。实际查询将在日志中心功能完成后开放。</p><CopyId :value="context.task_id" label="日志关联任务号" /><p class="hint">使用后端关联号，不再次哈希；尚未查询日志。</p></section><EmptyState title="暂未开放" description="该页面尚未提供日志查询。" /></section></template>
