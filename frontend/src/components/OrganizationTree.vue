<script setup lang="ts">
import { computed } from 'vue'
import { organizationNodes, recordCell, type ResponseRecords } from '../app/responseViews'
const props = defineProps<{ records: ResponseRecords }>()
const nodes = computed(() => organizationNodes(props.records.objects))
</script>
<template><ol class="organization-tree"><li v-for="node in nodes" :key="node.index" :style="{ marginInlineStart: `${Math.min(node.depth, 8)}rem` }"><strong>{{ recordCell(records.rows[node.index]!, 'organization_name') }}</strong> · <code>{{ recordCell(records.rows[node.index]!, 'organization_code') }}</code><p class="hint">上级：{{ recordCell(records.rows[node.index]!, 'parent_organization_code') }} · 类别代码：{{ recordCell(records.rows[node.index]!, 'category_code') }} · 类别：{{ recordCell(records.rows[node.index]!, 'category_name') }} · 有效：{{ recordCell(records.rows[node.index]!, 'is_active') }}{{ node.note ? ' · ' + node.note : '' }}</p></li></ol></template>
<style scoped>
.organization-tree { list-style: none; padding: 0; }
.organization-tree li { border-inline-start: 2px solid var(--line); padding: 10px; overflow-wrap: anywhere; }
@media (max-width: 767px) { .organization-tree li { margin-inline-start: 0 !important; } }
</style>