<script setup lang="ts">
import StatusBadge from './StatusBadge.vue'
defineProps<{ state: 'connected' | 'connecting' | 'disconnected' | 'paused' | 'unknown'; detail?: string }>()
const labels = { connected: '已连接', connecting: '正在连接', disconnected: '连接中断', paused: '已暂停', unknown: '连接未知' }
</script>
<template><span class="connection-status" role="status"><StatusBadge :value="state" :labels="labels" :tone="state === 'connected' ? 'success' : state === 'disconnected' ? 'danger' : state === 'connecting' ? 'progress' : 'neutral'" /><span v-if="detail" class="hint">{{ detail }}</span></span></template>
