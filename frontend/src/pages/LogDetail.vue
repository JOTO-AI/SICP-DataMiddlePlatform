<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, onBeforeRouteUpdate, type LocationQuery } from 'vue-router'
import LogEventDetail from '../components/logs/LogEventDetail.vue'
import type { LogSource } from '../app/logHistory'
const route = useRoute(), detailPath = route.path, source = String(route.params.source) as LogSource, eventId = String(route.params.eventId)
// Snapshot the detail query: cached pages must not react to another route and start hidden reads.
const query = ref<LocationQuery>({ ...route.query })
onBeforeRouteUpdate(to => { if (to.path === detailPath) query.value = { ...to.query } })
const epoch = computed(() => typeof query.value.epoch === 'string' ? query.value.epoch : undefined)
const sequence = computed(() => typeof query.value.sequence === 'string' ? query.value.sequence : undefined)
const back = computed(() => typeof query.value.from === 'string' && ['/logs', '/logs/platform', '/logs/upstream'].includes(query.value.from) ? query.value.from : '/logs/' + (source === 'upstream' ? 'upstream' : 'platform'))
</script>
<template><section class="log-detail-page"><RouterLink class="back-link" :to="back">← 返回此源日志</RouterLink><LogEventDetail :key="epoch + '/' + sequence" :source="source" :event-id="eventId" :epoch="epoch" :sequence="sequence" /></section></template>
<style scoped>.log-detail-page { min-width: 0; overflow-wrap: anywhere; } .back-link { display: inline-block; font-size: 12px; margin-bottom: 16px; }</style>
