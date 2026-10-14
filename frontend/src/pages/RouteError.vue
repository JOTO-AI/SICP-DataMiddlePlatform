<script setup lang="ts">
import PageHeader from '../components/PageHeader.vue'
defineProps<{ status: 403 | 404 }>()
</script>
<template><section><PageHeader :title="status === 403 ? '无权访问' : '页面不存在'" :description="status === 403 ? '当前账号没有访问此页面的权限。' : '请检查页面地址，或返回运行总览。'" /><p class="hint">{{ status }}</p><RouterLink to="/overview">返回运行总览</RouterLink></section></template>
