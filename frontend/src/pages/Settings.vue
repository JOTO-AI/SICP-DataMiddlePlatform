<script setup lang="ts">
import { ref } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import SystemStatus from '../components/SystemStatus.vue'
import RetentionSettings from '../components/RetentionSettings.vue'
import LogCleanup from '../components/LogCleanup.vue'
const revision = ref('')
</script>
<template><section><PageHeader title="系统设置" description="查看实际运行状态，管理日志保留策略与维护操作。" /><SystemStatus /><RetentionSettings @changed="revision = $event" /><LogCleanup :revision="revision" /></section></template>