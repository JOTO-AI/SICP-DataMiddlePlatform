<script setup lang="ts">
import { computed } from 'vue'
import { requestDiagnostics, type EvidenceInput } from '../app/requestDiagnostics'
import Timestamp from './Timestamp.vue'
import JsonText from './JsonText.vue'
const props = defineProps<{ evidence: EvidenceInput }>()
const report = computed(() => requestDiagnostics(props.evidence))
const unavailable: Record<string, string> = {
  empty: '上游返回了 HTTP 状态，但响应正文为空。',
  invalid_json: '响应正文无法安全解析，未保存正文。',
  filtered: '响应正文经过过滤，没有可展示的错误内容。',
  success_payload_omitted: '成功响应正文未保存。',
}
</script>
<template>
  <div class="request-diagnostics">
    <p v-if="!report.items.length && !report.malformed" class="hint">本条记录未保存可展示的错误正文。</p>
    <p v-if="report.malformed" class="warning">部分请求诊断字段不完整，保留可确认的任务与日志事实。</p>
    <section v-for="(request, index) in report.items" :key="index" :aria-label="request.kind === 'authentication' ? '认证错误摘要' : '数据接口错误摘要'">
      <h4>{{ request.kind === 'authentication' ? '认证错误摘要' : '数据接口错误摘要' }}</h4>
      <p v-if="request.kind === 'authentication'">获取上游 Token 失败 · HTTP {{ request.http_status ?? '未记录' }}</p>
      <dl class="facts">
        <div><dt>接口</dt><dd>{{ request.method }} {{ request.endpoint ?? '旧记录未提供接口地址' }}</dd></div>
        <div><dt>{{ request.kind === 'authentication' ? '认证 HTTP 状态' : '数据接口 HTTP 状态' }}</dt><dd>{{ request.http_status ?? (request.sent === false ? '未发起 HTTP 请求' : request.sent === true ? '未收到 HTTP 响应' : '旧记录未提供') }}</dd></div>
        <div v-if="request.started_at"><dt>请求开始时间</dt><dd><Timestamp :value="request.started_at" /></dd></div>
        <div v-if="request.duration_ns !== undefined"><dt>请求耗时</dt><dd>{{ (request.duration_ns / 1e9).toFixed(3) }} 秒</dd></div>
        <div v-if="request.business_code !== undefined"><dt>业务码</dt><dd>{{ request.business_code }}</dd></div>
        <div v-if="request.stage"><dt>阶段</dt><dd>{{ request.stage }}</dd></div>
        <div v-if="request.failure"><dt>失败分类</dt><dd>{{ request.failure }}</dd></div>
        <div v-if="request.error_type"><dt>错误类型</dt><dd>{{ request.error_type }}</dd></div>
        <div v-if="request.response?.request_id"><dt>上游请求标识</dt><dd>{{ request.response.request_id }}</dd></div>
        <div v-if="request.response?.content_type"><dt>响应类型</dt><dd>{{ request.response.content_type }}</dd></div>
      </dl>
      <JsonText v-if="request.error" :text="request.error" :source-truncated="request.error_truncated" label="请求错误" />
      <JsonText v-if="request.response?.body" :text="request.response.body" :source-truncated="request.response.truncated" label="失败响应摘要" />
      <p v-else class="hint">{{ unavailable[request.response?.unavailable ?? ''] ?? '本条记录未保存可展示的错误正文。' }}</p>
      <p v-if="request.error_truncated" class="warning">错误说明过长，已截断。</p>
      <p v-if="request.response?.truncated" class="warning">响应摘要过长，已截断。</p>
      <p v-if="request.response?.read_error" class="warning">响应正文未完整读取，当前仅为已收到的证据。</p>
    </section>
  </div>
</template>
<style scoped>
.request-diagnostics { min-width: 0; overflow-wrap: anywhere; }
section { margin-block: 1rem; }
h4, p { margin-block: .5rem; }
</style>
