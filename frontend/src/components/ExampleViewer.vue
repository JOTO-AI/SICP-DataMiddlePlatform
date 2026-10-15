<script setup lang="ts">
import JsonText from './JsonText.vue'
withDefaults(defineProps<{ text: string; title?: string; loadable?: boolean }>(), { title: '请求示例' })
const emit = defineEmits<{ load: [text: string] }>()
</script>
<template><section class="example-viewer"><div class="section-bar"><h3>{{ title }}</h3><button v-if="loadable" type="button" @click="emit('load', text)">载入参数</button></div><p class="hint">文档示例，仅供参考；载入参数不会发送请求。</p><JsonText :text="text" label="示例 JSON" /></section></template>

<style scoped>
.example-viewer { min-width: 0; border: 1px solid var(--line); border-radius: var(--radius-control); overflow: hidden; }
.section-bar { padding: 10px 14px; margin: 0; background: var(--surface); }
h3 { font-size: 14px; }
.example-viewer > p { margin: 10px 14px; }
.example-viewer :deep(.json-viewer) { padding: 0 14px 14px; }
.example-viewer :deep(pre) { border: 0; padding: 12px 0; margin: 0; min-height: 120px; }
</style>
