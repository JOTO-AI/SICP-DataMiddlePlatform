<script setup lang="ts">
import { time } from './api'

interface ErrorResponse { body?: string; request_id?: string; content_type?: string; truncated?: boolean; read_error?: boolean }
export interface FailedAttempt {
  at: string; source: string; page: number; attempt: number; failure: string
  http_status?: number; business_code?: number; duration_ns?: number
  response?: ErrorResponse
  authentication?: { endpoint: string; reason: string; http_status?: number; response?: ErrorResponse }
}
defineProps<{ attempts?: FailedAttempt[]; unavailable?: boolean; failed?: boolean }>()
const reasons: Record<string, string> = { authentication: '获取上游 Token 失败', http: '上游返回 HTTP 错误', business: '上游返回业务错误', request_timeout: '请求超时', timeout: '认证请求超时', transport: '网络请求失败', cancelled: '请求已取消', budget: '请求预算耗尽或被取消', structure: '响应结构异常', response_limit: '响应超过大小限制', invalid_token: '认证响应缺少有效 Token', expired_token: '收到的 Token 已过期', read: '认证响应读取失败', configuration: '认证请求配置异常' }
</script>

<template>
  <section v-if="unavailable || attempts?.length || failed" class="failure-evidence" aria-label="采集失败记录">
    <h3>采集失败记录</h3>
    <p v-if="unavailable" class="error" role="status">暂时无法读取调用日志，任务状态不受影响。请稍后刷新详情。</p>
    <template v-else-if="attempts?.length">
      <p class="hint">最近 20 次失败尝试，按时间倒序排列，包含重试和并行请求取消。错误摘要已脱敏。</p>
      <details v-for="(item, index) in attempts" :key="`${item.at}-${item.page}-${item.attempt}`" :open="index === 0">
        <summary>第 {{ item.page }} 页 · 第 {{ item.attempt }} 次尝试 · {{ item.authentication ? `获取上游 Token 失败${item.authentication.http_status ? ` · HTTP ${item.authentication.http_status}` : ''}` : item.http_status ? `HTTP ${item.http_status}` : reasons[item.failure] || item.failure }}</summary>
        <dl class="facts">
          <div><dt>来源表</dt><dd>{{ item.source }}</dd></div>
          <div><dt>发生时间</dt><dd>{{ time(item.at) }}</dd></div>
          <div><dt>失败类型</dt><dd>{{ reasons[item.failure] || item.failure }}</dd></div>
          <template v-if="item.authentication">
            <div><dt>认证接口</dt><dd>{{ item.authentication.endpoint }}</dd></div>
            <div><dt>认证失败原因</dt><dd>{{ reasons[item.authentication.reason] || item.authentication.reason }}</dd></div>
            <div v-if="item.authentication.http_status != null"><dt>认证 HTTP 状态</dt><dd>{{ item.authentication.http_status }}</dd></div>
            <div v-if="item.authentication.response?.request_id"><dt>认证请求标识</dt><dd>{{ item.authentication.response.request_id }}</dd></div>
            <div v-if="item.http_status != null"><dt>数据接口 HTTP 状态</dt><dd>{{ item.http_status }}</dd></div>
          </template>
          <div v-if="item.duration_ns != null"><dt>耗时</dt><dd>{{ (item.duration_ns / 1e9).toFixed(2) }} 秒</dd></div>
          <div v-if="item.business_code != null"><dt>业务码</dt><dd>{{ item.business_code }}</dd></div>
          <div v-if="item.response?.request_id"><dt>上游请求标识</dt><dd>{{ item.response.request_id }}</dd></div>
        </dl>
        <section v-if="item.authentication" aria-label="认证错误摘要">
          <h4>认证接口错误摘要</h4>
          <pre v-if="item.authentication.response?.body">{{ item.authentication.response.body }}</pre>
          <p v-else class="hint">本条记录未保存认证错误正文，可能是旧版记录或未收到可展示的错误说明。新版本不会补写历史正文。</p>
          <p v-if="item.authentication.response?.truncated" class="hint">认证错误摘要过长，已截断。</p>
          <p v-if="item.authentication.response?.read_error" class="hint">认证响应正文未完整读取，以上仅为已收到的错误信息。</p>
        </section>
        <h4 v-if="item.authentication && item.response?.body">数据接口错误摘要</h4>
        <pre v-if="item.response?.body">{{ item.response.body }}</pre>
        <p v-else-if="!item.authentication" class="hint">未记录可展示的响应摘要，可能是旧版记录、未收到响应或响应没有错误说明。</p>
        <p v-if="item.response?.truncated" class="hint">响应摘要过长，已截断。</p>
        <p v-if="item.response?.read_error" class="hint">响应正文未完整读取，以上仅为已收到的错误信息。</p>
      </details>
    </template>
    <p v-else class="hint">暂未查到失败请求记录。日志可能尚在写入，也可能失败发生在分页校验或保存阶段。可稍后刷新详情。</p>
  </section>
</template>

<style scoped>
.failure-evidence { margin-block: 1.25rem; }
details { border-bottom: 1px solid var(--line); padding-block: .75rem; }
summary { cursor: pointer; font-weight: 600; overflow-wrap: anywhere; }
pre { white-space: pre-wrap; overflow-wrap: anywhere; font: inherit; background: var(--surface); padding: .75rem; border: 1px solid var(--line); border-radius: .4rem; }
dd { overflow-wrap: anywhere; }
</style>
