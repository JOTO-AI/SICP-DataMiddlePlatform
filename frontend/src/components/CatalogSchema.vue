<script setup lang="ts">
import { computed } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import { schemaRows } from '../app/catalogSchema'
const props = defineProps<{ api: CatalogAPI; schema: unknown; label: string }>()
const result = computed(() => schemaRows(props.api, props.schema))
</script>
<template>
  <div>
    <p class="hint">必填表示字段必须存在，可空表示允许 null，两者独立。默认值为“未声明”时不能据此推断默认值。中文说明依据已核对的接口文档；类型与限制以本次后端目录为准。</p>
    <p v-if="result.incomplete" class="warning" role="status">定义存在无效结构或超出展示上限（12 层 / 1000 行），以下不是完整字段列表。</p>
    <div class="schema-scroll" role="region" :aria-label="label" tabindex="0"><table>
      <thead><tr><th scope="col">字段路径 / 中文语义</th><th scope="col">类型</th><th scope="col">必填</th><th scope="col">可空</th><th scope="col">默认值</th><th scope="col">限制</th></tr></thead>
      <tbody><tr v-for="row in result.rows" :key="row.path"><th scope="row"><code>{{ row.path }}</code><span>{{ row.description }}</span></th><td><code>{{ row.type }}</code></td><td>{{ row.required }}</td><td>{{ row.nullable }}</td><td><code>{{ row.defaultText }}</code></td><td>{{ row.limit }}</td></tr></tbody>
    </table></div>
  </div>
</template>
<style scoped>
.schema-scroll { overflow: auto; max-width: 100%; margin-top: 16px; }
table { min-width: 760px; width: 100%; }
th, td { vertical-align: top; overflow-wrap: anywhere; }
tbody th { width: 32%; font-weight: 400; }
tbody th span { display: block; margin-top: 6px; }
td:last-child { min-width: 220px; }
</style>
