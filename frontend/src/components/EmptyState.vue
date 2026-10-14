<script setup lang="ts">
withDefaults(defineProps<{ title?: string; description?: string; loading?: boolean }>(), { title: '没有符合条件的记录' })
</script>
<template><div class="empty" role="status" :aria-busy="loading"><div v-if="loading" class="loading-lines" aria-hidden="true"><i></i><i></i><i></i></div><h3>{{ loading ? '正在读取数据' : title }}</h3><p v-if="description">{{ description }}</p><slot /></div></template>
