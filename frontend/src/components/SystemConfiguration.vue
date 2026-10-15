<script setup lang="ts">
import { computed } from 'vue'
import { statusObject, statusConfigured, statusState, type SystemStatus } from '../app/systemStatus'
const props = defineProps<{ value: SystemStatus }>()
const runtimeLabels: Record<string, string> = { retention: '日志保留期', notification_settings: '邮件通知配置' }
const deploymentLabels: Record<string, string> = { credentials: '凭据配置', log_capacity: '日志容量上限', connection_limits: '连接限制', snapshot_key: '快照签名密钥' }
const fields = (value: unknown, labels: Record<string, string>) => Array.isArray(value) ? value.filter((key): key is string => typeof key === 'string' && Object.hasOwn(labels, key)) : []
const runtime = computed(() => fields(props.value.runtime_fields, runtimeLabels)), deployment = computed(() => fields(props.value.deployment_fields, deploymentLabels))
</script>
<template><section aria-label="配置管理范围"><h3>运行时配置</h3><ul v-if="runtime.length"><li v-for="key in runtime" :key="key"><RouterLink :to="key === 'retention' ? '/settings/retention' : '/notifications'">{{ runtimeLabels[key] }}</RouterLink></li></ul><p v-else>尚未取得可修改配置范围。</p><h3>部署配置</h3><p class="hint">以下项目由部署环境管理。控制台没有保存入口，只展示服务端提供的配置摘要。</p><ul v-if="deployment.length"><li v-for="key in deployment" :key="key">{{ deploymentLabels[key] }}（部署时设置）</li></ul><p v-else>尚未取得部署配置范围。</p><dl class="facts"><div v-for="(label, key) in { source_configured: '上游凭据', smtp_configured: 'SMTP 配置', log_cursor_configured: '日志游标签名配置' }" :key="key"><dt>{{ label }}</dt><dd>{{ statusConfigured(statusObject(value.credentials)[key]) }}</dd></div><div><dt>SMTP 可达性</dt><dd>{{ statusState(value.smtp_reachability) }}</dd></div></dl><p class="hint">配置已提供不代表服务可达；密码、令牌、连接串和文件路径不在此显示。</p></section></template>
<style scoped>h3 { margin-block: 20px 12px; } p, li { margin-block: 10px; } dd { overflow-wrap: anywhere; }</style>