<script setup lang="ts">
import JsonText from './JsonText.vue'
import CopyId from './CopyId.vue'
defineProps<{ text: string; httpStatus?: number; code?: string; requestId?: string; sourceTruncated?: boolean; stale?: boolean }>()
</script>
<template><section class="response-viewer" aria-label="响应内容"><p v-if="stale" class="warning">上次读取结果，当前请求尚未取得新响应。</p><div v-if="httpStatus !== undefined || code !== undefined || requestId" class="result-meta"><span v-if="httpStatus !== undefined">HTTP {{ httpStatus }}</span><span v-if="code !== undefined">业务 code：<code>{{ code }}</code></span><CopyId v-if="requestId" :value="requestId" label="请求号" /></div><JsonText :text="text" label="响应 JSON" :source-truncated="sourceTruncated" /></section></template>
