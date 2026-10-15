<script setup lang="ts">
import Timestamp from '../Timestamp.vue'
import { logDetailPath } from '../../app/links'
import RequestDiagnostics from '../RequestDiagnostics.vue'
import type { ErrorResponse } from '../../app/requestDiagnostics'

export interface FailedAttempt {
  event_id?: string; log_source?: string; authentication?: { endpoint: string; reason: string; http_status?: number; response?: ErrorResponse }
  diagnostics?: unknown; task_id?: string; call_id?: string; attempt_id?: string
  path?: string; mode?: string; page_size?: number; start_time?: string; end_time?: string
  at: string; source: string; page: number; attempt: number; failure: string
  http_status?: number; business_code?: number; duration_ns?: number
  response?: ErrorResponse
}
defineProps<{ attempts?: FailedAttempt[]; unavailable?: boolean; failed?: boolean }>()
const reasons: Record<string, string> = { authentication: '上游认证失败', http: '上游返回 HTTP 错误', business: '上游返回业务错误', request_timeout: '请求超时', transport: '网络请求失败', cancelled: '请求已取消', budget: '请求预算耗尽或被取消', structure: '响应结构异常', response_limit: '响应超过大小限制' }
</script>

<template>
  <section v-if="unavailable || attempts?.length || failed" class="failure-evidence" aria-label="采集失败记录">
    <h3>采集失败记录</h3>
    <p v-if="unavailable" class="error" role="status">暂时无法读取调用日志，任务状态不受影响。请稍后刷新详情。</p>
    <template v-if="attempts?.length">
      <p class="hint">最近 20 次失败尝试，按时间倒序排列，包含重试和并行请求取消。错误摘要已脱敏。</p>
      <details v-for="(item, index) in attempts" :key="`${item.source}-${item.at}-${item.page}-${item.attempt}-${index}`" :open="index === 0">
        <summary>第 {{ item.page }} 页 · 第 {{ item.attempt }} 次尝试 · {{ item.http_status ? `HTTP ${item.http_status}` : reasons[item.failure] || item.failure }}</summary>
        <dl class="facts">
          <div><dt>来源表</dt><dd>{{ item.source }}</dd></div>
          <div><dt>发生时间</dt><dd><Timestamp :value="item.at" /></dd></div>
          <div><dt>失败类型</dt><dd>{{ reasons[item.failure] || item.failure }}</dd></div>
          <div v-if="item.duration_ns != null"><dt>耗时</dt><dd>{{ (item.duration_ns / 1e9).toFixed(2) }} 秒</dd></div>
          <div v-if="item.business_code != null"><dt>业务码</dt><dd>{{ Number.isSafeInteger(item.business_code) ? item.business_code : '超出精确数值范围，请查看日志原文' }}</dd></div>
          <div v-if="item.response?.request_id"><dt>上游请求标识</dt><dd>{{ item.response.request_id }}</dd></div>
        </dl>
        <p v-if="item.event_id && item.log_source && logDetailPath(item.log_source, item.event_id)"><RouterLink :to="logDetailPath(item.log_source!, item.event_id!)!">查看此失败尝试日志</RouterLink></p>
        <p v-if="item.failure === 'request_timeout'" class="hint">超时表示本端未及时收到结果，不代表原平台未处理请求。</p>
        <RequestDiagnostics :evidence="item" />
        <details v-if="item.path || item.task_id || item.call_id || item.attempt_id">
          <summary>请求关联与采集参数</summary>
          <dl class="facts">
            <div v-if="item.path"><dt>目标数据接口</dt><dd>{{ item.path }}</dd></div>
            <div v-if="item.mode"><dt>采集模式</dt><dd>{{ item.mode }}</dd></div>
            <div v-if="item.page_size"><dt>每页条数</dt><dd>{{ item.page_size }}</dd></div>
            <div v-if="item.start_time || item.end_time"><dt>增量时间范围</dt><dd>{{ item.start_time ?? '未提供' }} ～ {{ item.end_time ?? '未提供' }}</dd></div>
            <div v-if="item.task_id"><dt>任务日志关联号</dt><dd>{{ item.task_id }}</dd></div>
            <div v-if="item.call_id"><dt>调用日志关联号</dt><dd>{{ item.call_id }}</dd></div>
            <div v-if="item.attempt_id"><dt>尝试日志关联号</dt><dd>{{ item.attempt_id }}</dd></div>
          </dl>
        </details>
      </details>
    </template>
    <p v-else-if="!unavailable" class="hint">暂未查到失败请求记录。日志可能尚在写入，也可能失败发生在分页校验或保存阶段。可稍后刷新详情。</p>
  </section>
</template>

<style scoped>
.failure-evidence { margin-block: 1.25rem; }
details { border-bottom: 1px solid var(--line); padding-block: .75rem; }
summary { cursor: pointer; font-weight: 600; overflow-wrap: anywhere; }
dd { overflow-wrap: anywhere; }
</style>
