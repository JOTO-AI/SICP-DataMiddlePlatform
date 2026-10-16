<script setup lang="ts">
import { computed } from 'vue'
import type { CatalogAPI } from '../app/catalog'
import { catalogPurpose, catalogOverview, catalogNote } from '../app/catalogCopy'
const props = defineProps<{ api: CatalogAPI }>()
const overview = computed(() => catalogOverview(props.api))
</script>
<template>
  <div class="api-overview">
    <h2>接口说明</h2>
    <p class="purpose">{{ catalogPurpose(api) }}</p>
    <template v-if="overview">
      <h3>适用场景</h3><p>{{ overview.usage }}</p>
      <h3>返回内容</h3><p>{{ overview.result }}</p>
    </template>
    <details class="technical-notes">
      <summary>接口标识与调用约束</summary>
      <dl class="facts"><div><dt>接口标识</dt><dd><code>{{ api.id }}</code></dd></div><div><dt>接口分组</dt><dd>{{ api.kind === 'business' ? '本地业务接口' : api.kind === 'source' ? '上游接口' : api.kind }}</dd></div><div><dt>查询快照</dt><dd>{{ api.snapshot === true ? '使用查询快照' : api.snapshot === false ? '不使用查询快照' : '后端未提供' }}</dd></div></dl>
      <ul v-if="api.notes?.length"><li v-for="(note, index) in api.notes" :key="index">{{ catalogNote(note) }}</li></ul>
    </details>
  </div>
</template>
<style scoped>
.api-overview > p { max-width: 70ch; line-height: 1.8; overflow-wrap: anywhere; }
.purpose { font-size: 16px; }
.api-overview h3 { margin: 24px 0 8px; }
.technical-notes { margin-top: 28px; padding-top: 16px; border-top: 1px solid var(--line); }
.technical-notes summary { cursor: pointer; font-weight: 600; }
.technical-notes .facts { margin-top: 16px; }
.technical-notes li { margin-bottom: 8px; overflow-wrap: anywhere; }
</style>
