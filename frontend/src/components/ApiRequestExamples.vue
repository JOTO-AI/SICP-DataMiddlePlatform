<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import { requestExamples, requestCommand, type RequestExample } from '../app/requestExamples'
import JsonText from './JsonText.vue'
const props = defineProps<{ api: CatalogAPI }>()
const emit = defineEmits<{ load: [example: RequestExample] }>()
const examples = computed(() => requestExamples(props.api)), selected = ref('minimal')
const current = computed(() => examples.value.find(item => item.id === selected.value) ?? examples.value[0])
watch(() => props.api.id, () => { selected.value = 'minimal' })
</script>
<template>
  <section v-if="current" aria-label="请求案例选择">
    <label>案例类型<select v-model="selected"><option v-for="example in examples" :key="example.id" :value="example.id">{{ example.title }}</option></select></label>
    <h3>{{ current.title }}</h3><p>{{ current.explanation }}</p>
    <JsonText :text="JSON.stringify(current.body, null, 2)" label="请求案例 JSON" />
    <button type="button" @click="emit('load', current)">使用此案例测试</button><p class="hint">只载入当前接口的参数草稿，不发送请求。载入新案例会替换原草稿。</p>
    <details v-if="api.kind !== 'source' || current.id !== 'invalid'"><summary>复制 curl 命令（POSIX shell）</summary>
      <p class="hint">基址与凭据均为占位符，需在可信终端自行替换。不要将真实凭据填入页面。{{ api.kind === 'source' ? '这是直接访问提供方的 GET 命令，不是浏览器提交测试作业的命令。' : '这是直接访问业务服务的 POST 命令；浏览器使用同源会话代理。' }}</p>
      <JsonText :text="requestCommand(api, current)" label="curl 命令" />
    </details>
  </section>
  <p v-else class="warning">后端未提供可用的请求案例。</p>
</template>
<style scoped>
h3 { margin-top: 20px; }
button { margin-top: 12px; }
details { margin-top: 24px; }
</style>
