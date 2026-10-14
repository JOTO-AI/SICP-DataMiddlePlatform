<script setup lang="ts">
import type { CatalogAPI } from '../app/catalog'
defineProps<{ api: CatalogAPI }>()
</script>
<template>
  <section aria-label="完整 HTTP 响应契约">
    <h3>完整 HTTP 响应外壳</h3>
    <template v-if="api.kind === 'business'">
      <p>业务字段 schema 只描述 <code>data</code>。完整 JSON 为 <code>{ code, message, request_id, data }</code>；code 是字符串，成功为 <code>ok</code>，失败时 data 为 null。HTTP 状态码与业务 code 分开判断，不能把 code 当作数值 200。</p>
      <p><code>X-Request-ID</code> 是本次请求标识；成功查询的 <code>X-SICP-Combination</code> 表示数据组合。{{ api.snapshot ? '分页成功响应提供 X-Query-Snapshot，续页使用当前接口原快照。' : '此接口不分页，不使用 X-Query-Snapshot。' }}</p>
    </template>
    <template v-else>
      <p>提供方完整响应包含数值 <code>code</code>、<code>message</code> 和 <code>data</code>；data 分页容器含 <code>total</code> 与 <code>list</code>。下方源 schema 只描述 list 中一条记录；字段保持上游原名，json 类型保留原始数字语义。目录案例可能省略提供方未明确保证的字段。</p>
      <p>浏览器受控测试由管理服务返回另一层字符串 code 外壳：受理为 HTTP 202 / <code>accepted</code>；随后读取 <code>{ job, result_state, result }</code>。result 是受控采集结果的 total/list，不是提供方完整 HTTP 响应；作业状态、提供方 HTTP 状态与业务码分别解释。</p>
    </template>
    <p class="hint">以下成功/失败案例是后端目录文档示例，没有在当前页面执行查询，不代表当前数据或可用性。</p>
  </section>
</template>
