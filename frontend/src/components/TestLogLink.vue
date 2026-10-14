<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { testLogFilters, prepareTestLogs, consumeTestLogs } from '../app/testLogNavigation'
const props = defineProps<{ kind: string; requestId?: string | null; jobId?: string | null }>()
const emit = defineEmits<{ navigate: [] }>()
const navigating = ref(false)
const router = useRouter(), filters = computed(() => testLogFilters(props.kind, props.requestId, props.jobId))
async function open() {
  if (navigating.value || !filters.value || !prepareTestLogs(filters.value)) return
  navigating.value = true
  const destination = `/logs/${filters.value.source}`
  try { const failure = await router.push(destination); if (failure) consumeTestLogs(); else emit('navigate') } catch { consumeTestLogs() } finally { navigating.value = false }
}
</script>
<template><button v-if="filters" type="button" :disabled="navigating" @click="open">查看本次调用日志</button><p v-else class="hint">尚未取得可关联的{{ kind === 'business' ? '请求号' : '源测试作业号' }}，暂不能定位本次日志。</p></template>
