<script setup lang="ts">
defineProps<{ status: string; resultState: string; stale?: boolean }>()
</script>
<template><section v-if="status === 'succeeded' && resultState !== 'available'" aria-label="源结果不可获取" class="result-notice"><h4>{{ stale ? '上次作业查询未取得结果' : '作业已成功，当前结果不可获取' }}</h4><p v-if="stale" class="warning">以下说明属于上次已确认的作业读取，本次状态尚未确认。</p><p>该次查询未取得结果。结果可能尚未进入缓存，或已过期、被淘汰、服务重启后丢失、请求落在其他实例；当前回执未提供具体原因，不能断言是哪一种。</p><p>可点击“查询原作业”再次读取。不会自动提交新测试，作业成功记录也不会被改成失败；如需重新读取源数据，请先结束本次，再显式执行新测试。</p></section></template>
<style scoped>
.result-notice { border-inline-start: 3px solid var(--warning); padding: 12px 16px; margin-block: 16px; background: var(--surface); }
p + p { margin-top: 8px; }
</style>