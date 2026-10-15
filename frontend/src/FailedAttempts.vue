<script setup lang="ts">
import { time } from './api'

interface ErrorResponse { body?: string; request_id?: string; content_type?: string; truncated?: boolean; read_error?: boolean; unavailable?: string }
interface RequestEvidence {
  kind: string; method?: string; endpoint?: string; started_at?: string; duration_ns?: number
  sent?: boolean; http_status?: number; business_code?: string; stage?: string; failure?: string
  error?: string; error_type?: string; error_truncated?: boolean; response?: ErrorResponse
}
export interface FailedAttempt {
  at: string; source: string; page: number; attempt: number; failure: string
  event_id?: string; task_id?: string; call_id?: string; attempt_id?: string; path?: string
  mode?: string; page_size?: number; start_time?: string; end_time?: string
  http_status?: number; business_code?: number; duration_ns?: number
  response?: ErrorResponse; diagnostics?: RequestEvidence[]
  authentication?: { endpoint: string; reason: string; http_status?: number; response?: ErrorResponse }
}
defineProps<{ attempts?: FailedAttempt[]; unavailable?: boolean; failed?: boolean }>()
const reasons: Record<string, string> = {'authentication': '获取上游 Token 失败', 'http': '上游返回 HTTP 错误', 'business': '上游返回业务错误', 'request_timeout': '请求超时', 'timeout': '请求超时', 'transport': '网络请求失败', 'cancelled': '请求已取消', 'budget': '请求预算耗尽或被取消', 'structure': '响应结构异常', 'response_limit': '响应超过大小限制', 'invalid_token': '认证响应缺少有效 Token', 'expired_token': '收到的 Token 已过期', 'read': '响应读取失败', 'configuration': '请求配置异常', 'unknown': '未分类错误'}
const stages: Record<string, string> = {'prepare': '准备请求', 'schedule': '等待或调度请求', 'transport': '连接或发送请求', 'http': '检查 HTTP 状态', 'read': '读取响应', 'decode': '解析响应', 'validate': '校验响应', 'business': '检查业务结果'}
function requests(item: FailedAttempt): RequestEvidence[] {
  if (item.diagnostics?.length) return item.diagnostics
  const result: RequestEvidence[] = []
  if (item.http_status != null || item.response || !item.authentication) result.push({ kind: 'data', endpoint: item.path, http_status: item.http_status, business_code: item.business_code?.toString(), response: item.response })
  if (item.authentication) result.push({ kind: 'authentication', ...item.authentication, failure: item.authentication.reason })
  return result
}
function summary(item: FailedAttempt): string {
  const last = requests(item).at(-1)
  if (last?.kind === 'authentication') return '获取上游 Token 失败' + (last.http_status ? ' · HTTP ' + last.http_status : '')
  return last?.http_status ? `HTTP ${last.http_status}` : reasons[item.failure] || item.failure
}
function status(request: RequestEvidence): string {
  if (request.http_status) return String(request.http_status)
  return request.sent === true ? '未收到 HTTP 响应' : request.sent === false ? '未发起 HTTP 请求' : '旧版记录未提供'
}
</script>

<template>
  <section v-if="unavailable || attempts?.length || failed" class="failure-evidence" aria-label="采集失败记录">
    <h3>采集失败记录</h3>
    <p v-if="unavailable" class="error" role="status">暂时无法读取调用日志，任务状态不受影响。请稍后刷新详情。</p>
    <template v-else-if="attempts?.length">
      <p class="hint">最近 20 次失败尝试，按时间倒序排列，包含重试和并行请求取消。错误摘要已脱敏。</p>
      <details v-for="(item, index) in attempts" :key="item.event_id || `${item.source}-${item.at}-${item.page}-${item.attempt}-${index}`" :open="index === 0">
        <summary>第 {{ item.page }} 页 · 第 {{ item.attempt }} 次尝试 · {{ summary(item) }}</summary>
        <dl class="facts">
          <div><dt>受影响的表</dt><dd>{{ item.source }}</dd></div>
          <div><dt>发生时间</dt><dd>{{ time(item.at) }}</dd></div>
          <div><dt>失败类型</dt><dd>{{ reasons[item.failure] || item.failure }}</dd></div>
          <div v-if="item.duration_ns != null"><dt>本次尝试耗时</dt><dd>{{ (item.duration_ns / 1e9).toFixed(2) }} 秒</dd></div>
        </dl>
        <section v-for="(request, position) in requests(item)" :key="position" :aria-label="request.kind === 'authentication' ? '认证错误摘要' : '数据接口错误摘要'">
          <h4>{{ request.kind === 'authentication' ? '认证接口错误摘要' : '数据接口错误摘要' }}</h4>
          <dl class="facts">
            <div><dt>调用接口</dt><dd>{{ request.method }} {{ request.endpoint || '旧版记录未提供' }}</dd></div>
            <div><dt>{{ request.kind === 'authentication' ? '认证 HTTP 状态' : '数据接口 HTTP 状态' }}</dt><dd>{{ status(request) }}</dd></div>
            <div v-if="request.business_code != null"><dt>业务码</dt><dd>{{ request.business_code }}</dd></div>
            <div v-if="request.stage"><dt>失败阶段</dt><dd>{{ stages[request.stage] || request.stage }}</dd></div>
            <div v-if="request.failure"><dt>请求失败原因</dt><dd>{{ reasons[request.failure] || request.failure }}</dd></div>
            <div v-if="request.started_at"><dt>请求开始时间</dt><dd>{{ time(request.started_at) }}</dd></div>
            <div v-if="request.duration_ns != null"><dt>请求耗时</dt><dd>{{ (request.duration_ns / 1e9).toFixed(2) }} 秒</dd></div>
            <div v-if="request.response?.request_id"><dt>上游请求标识</dt><dd>{{ request.response.request_id }}</dd></div>
            <div v-if="request.error_type"><dt>异常类型</dt><dd>{{ request.error_type }}</dd></div>
          </dl>
          <p v-if="request.error" class="error-detail">{{ request.error }}</p>
          <pre v-if="request.response?.body">{{ request.response.body }}</pre>
          <p v-else-if="request.sent === false" class="hint">失败发生在请求发出之前，没有上游响应正文。</p>
          <p v-else-if="request.sent && !request.http_status" class="hint">未收到 HTTP 响应，请查看以上网络或超时错误。</p>
          <p v-else-if="request.response?.unavailable === 'empty'" class="hint">上游返回了 HTTP 状态，但响应正文为空。</p>
          <p v-else-if="request.response?.unavailable === 'success_payload_omitted'" class="hint">响应包含成功数据或认证信息，未保存正文；请查看以上校验或调度错误。</p>
          <p v-else-if="request.response?.unavailable === 'filtered'" class="hint">响应内容经过脱敏过滤，没有可展示的错误正文。</p>
          <p v-else-if="request.response?.unavailable" class="hint">响应正文未能提取出可安全展示的内容：{{ request.response.unavailable }}。</p>
          <p v-else class="hint">本条记录未保存可展示的错误正文，可能是旧版记录或响应没有错误说明。新版本不会补写历史正文。</p>
          <p v-if="request.response?.truncated || request.error_truncated" class="hint">错误摘要过长，已截断。</p>
          <p v-if="request.response?.read_error" class="hint">响应正文未完整读取，以上仅为已收到的错误信息。</p>
        </section>
        <details>
          <summary>请求关联与采集参数</summary>
          <dl class="facts">
            <div v-if="item.task_id"><dt>任务日志关联 ID</dt><dd>{{ item.task_id }}</dd></div>
            <div v-if="item.call_id"><dt>调用 ID</dt><dd>{{ item.call_id }}</dd></div>
            <div v-if="item.attempt_id"><dt>尝试 ID</dt><dd>{{ item.attempt_id }}</dd></div>
            <div v-if="item.event_id"><dt>事件 ID</dt><dd>{{ item.event_id }}</dd></div>
            <div v-if="item.path"><dt>目标数据接口</dt><dd>{{ item.path }}</dd></div>
            <div v-if="item.mode"><dt>采集模式</dt><dd>{{ item.mode }}</dd></div>
            <div><dt>页码 / 尝试次数</dt><dd>{{ item.page }} / {{ item.attempt }}</dd></div>
            <div v-if="item.page_size"><dt>每页条数</dt><dd>{{ item.page_size }}</dd></div>
            <div v-if="item.start_time || item.end_time"><dt>采集窗口</dt><dd>{{ item.start_time }} ～ {{ item.end_time }}</dd></div>
          </dl>
        </details>
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
dd, .error-detail { overflow-wrap: anywhere; }
</style>
