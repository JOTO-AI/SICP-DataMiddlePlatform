<script setup lang="ts">
import { useRoute } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import LogEventDetail from '../components/logs/LogEventDetail.vue'
import type { LogSource } from '../app/logHistory'
const route = useRoute(), source = String(route.params.source) as LogSource, eventId = String(route.params.eventId)
</script>
<template><section><RouterLink :to="{ name: 'log-source', params: { source } }">← 返回此源日志</RouterLink><PageHeader title="日志详情" description="按日志源和事件号读取后端脱敏证据，发生时间与入库时间分别展示。" /><section class="panel"><LogEventDetail :source="source" :event-id="eventId" /></section></section></template>
<style scoped>.page-heading { margin-top: 16px; } .panel { overflow-wrap: anywhere; }</style>