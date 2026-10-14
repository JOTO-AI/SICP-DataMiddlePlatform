<script setup lang="ts">
import CopyId from './CopyId.vue'
defineProps<{ message: string; requestId?: string; retryable?: boolean; busy?: boolean }>()
const emit = defineEmits<{ retry: [] }>()
</script>
<template><div class="error" role="alert"><p>{{ message }}</p><CopyId v-if="requestId" :value="requestId" label="请求号" /><button v-if="retryable" type="button" :disabled="busy" @click="emit('retry')">重新读取</button></div></template>
