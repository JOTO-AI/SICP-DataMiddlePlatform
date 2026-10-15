<script setup lang="ts">
import { computed } from 'vue'
import { schemaRows } from '../app/catalogSchema'
import type { CatalogAPI } from '../app/catalog'
import { sourceQueryFields } from '../app/sourceQueryFields'
import CatalogSchema from './CatalogSchema.vue'
const props = defineProps<{ api: CatalogAPI }>()
const emit = defineEmits<{ response: [] }>()
const fields = computed(() => schemaRows(props.api, props.api.response).rows)
</script>
<template>
  <div class="source-doc-columns">
  <section aria-label="上游 GET Query">
    <h3>上游 GET · Query</h3>
    <p class="hint">下表来自提供方文档。原文 page=1、pageSize=15 是示例，提供方默认值、分页上限及可空规则未说明。源筛选、排序和时间范围的规则以提供方契约为准。</p>
    <div v-if="sourceQueryFields[api.id]" class="query-scroll" role="region" aria-label="上游 Query 字段表" tabindex="0"><table>
      <thead><tr><th scope="col">参数</th><th scope="col">提供方类型</th><th scope="col">必填</th><th scope="col">提供方默认值</th><th scope="col">说明</th></tr></thead>
      <tbody><tr v-for="field in sourceQueryFields[api.id]" :key="field.name"><th scope="row"><code>{{ field.name }}</code></th><td>{{ field.type }}</td><td>{{ field.required }}</td><td>{{ field.defaultText }}</td><td>{{ field.description }}</td></tr></tbody>
    </table></div>
    <p v-else class="warning">此源暂无已核对的提供方 Query 文档。</p>
  </section>
  <aside class="source-fields" aria-label="源记录字段摘要"><h3>源记录字段</h3><p class="hint">展示后端定义的前 {{ Math.min(fields.length, 8) }} / {{ fields.length }} 个字段。</p><div class="query-scroll" tabindex="0" role="region" aria-label="源字段摘要表"><table><thead><tr><th>字段</th><th>说明</th></tr></thead><tbody><tr v-for="field in fields.slice(0, 8)" :key="field.path"><th scope="row"><code>{{ field.path }}</code></th><td>{{ field.description }}</td></tr></tbody></table></div><button class="source-response-link" type="button" @click="emit('response')">查看完整响应字段</button><h3>测试边界</h3><p>每次只读取指定的一页，不执行全量采集，也不推进同步进度。</p><p class="hint">上游凭据由服务端配置，浏览器不读取或编辑。</p></aside>
  </div>
  <section class="controlled-test-docs" aria-label="浏览器受控测试 JSON">
    <h3>浏览器受控测试 · JSON Body</h3>
    <p><code>POST /admin/v1/api-tests</code></p>
    <p>浏览器提交测试作业，由服务端使用部署配置访问上游；每次至多一页，不推进同步进度。不接受自定义地址、令牌、排序、时间范围或源业务筛选。</p>
    <dl class="facts">
      <div><dt><code>api_id</code></dt><dd>必填字符串，固定为当前源 <code>{{ api.id }}</code>。</dd></div>
      <div><dt><code>version</code></dt><dd>必填字符串，须匹配目录版本 <code>{{ api.version }}</code>。</dd></div>
      <div><dt><code>idempotency_key</code></dt><dd>必填字符串，1–128 位字母、数字、下划线、冒号或连字符；同一请求重试保留原键与原载荷。</dd></div>
    </dl>
    <p class="hint">以下分页默认值与限制由本地受控测试定义，不是提供方默认值：<code>page → page</code>，<code>page_size → pageSize</code>。作业最长 30 秒（含排队），结果最多 2 MiB，临时结果十分钟内过期。</p>
    <CatalogSchema :api="api" :schema="api.request" label="参数字段表" parameters />
  </section>
</template>
<style scoped>
.source-doc-columns { display: grid; grid-template-columns: minmax(0, 1.2fr) minmax(0, .8fr); gap: 24px; }
.source-doc-columns > * { min-width: 0; }
.source-fields { border-inline-start: 1px solid var(--line); padding-inline-start: 24px; }
.source-fields table { min-width: 280px; }
.source-fields h3 { margin-bottom: 12px; }
.source-fields p { margin-block: 8px; font-size: 13px; }
.source-response-link { margin: 16px 0 24px; }
.controlled-test-docs { margin-top: 32px; padding-top: 24px; border-top: 1px solid var(--line); }
.query-scroll { overflow: auto; max-width: 100%; margin-top: 16px; }
table { min-width: 520px; width: 100%; }
th, td { vertical-align: top; overflow-wrap: anywhere; }
tbody th { font-weight: 400; }
@media (max-width: 1300px) { .source-doc-columns { grid-template-columns: minmax(0, 1fr); } .source-fields { border-inline-start: 0; border-top: 1px solid var(--line); padding-inline-start: 0; padding-top: 20px; } }
</style>
