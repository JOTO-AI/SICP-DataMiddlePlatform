<script setup lang="ts">
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
defineProps<{ title: string }>()
</script>
<template><section><PageHeader :title="title" /><EmptyState title="暂未开放" description="该页面尚未提供操作入口。" /></section></template>
