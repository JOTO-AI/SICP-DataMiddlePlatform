<script setup lang="ts">
import type { ParameterDefinition } from '../shared/json'
defineProps<{ parameters: ParameterDefinition[] }>()
</script>
<template><div class="table-wrap parameter-table" tabindex="0" aria-label="接口参数表，可横向滚动"><table><thead><tr><th>参数</th><th>类型</th><th>必填 / 可空</th><th>默认值</th><th>说明</th></tr></thead><tbody><tr v-for="parameter in parameters" :key="parameter.name"><td><code>{{ parameter.name }}</code></td><td>{{ parameter.type }}</td><td>{{ parameter.required ? '必填' : '可选' }} / {{ parameter.nullable ? '允许 null' : '不允许 null' }}</td><td><code>{{ parameter.defaultText ?? '未声明' }}</code></td><td>{{ parameter.description || '未提供' }}</td></tr></tbody></table><p v-if="!parameters.length" class="hint">未声明参数。</p></div></template>
