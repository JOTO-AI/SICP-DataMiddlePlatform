<script setup lang="ts">
import { computed } from 'vue'
import { recentTests, clearRecentTests } from '../app/recentTests'
import { operationTitles } from '../queryOperations'
import Timestamp from './Timestamp.vue'
import CopyId from './CopyId.vue'
import StatusBadge from './StatusBadge.vue'
const props = defineProps<{ apiId?: string }>()
const visible = computed(() => recentTests.value.filter(entry => !props.apiId || entry.apiId === props.apiId))
const states: Record<string, string> = { queued: '排队中', running: '执行中', succeeded: '成功', failed: '失败', interrupted: '中断', partial_failed: '部分失败' }
</script>
<template><section aria-label="近期测试摘要" class="recent-tests"><h3>近期测试摘要</h3><p class="hint">仅本标签页内存中的最近 50 条观测，刷新、退出登录或清除后消失。不是服务端持久历史：业务每次查询单独记录，源作业保留最近观测。仅保留接口、状态、观测时间、请求号和作业号等摘要，不保留参数、名单、成绩或响应正文。</p><button type="button" :disabled="!recentTests.length" @click="clearRecentTests">清除全部近期摘要</button><p v-if="!visible.length" class="hint">暂无本会话的测试摘要；打开文档或填入案例不会生成记录。</p>
<ol v-else><li v-for="entry in visible" :key="entry.localID"><div class="recent-heading"><RouterLink :to="{ name: 'api-detail', params: { apiId: entry.apiId } }">{{ operationTitles[entry.apiId] || entry.apiId }}</RouterLink><StatusBadge :label="entry.title" :tone="entry.tone" /></div><p class="hint">浏览器观测于 <Timestamp :value="entry.observedAt" /> · 目录版本 {{ entry.version }} · {{ entry.kind === 'source' ? '最近管理读取' : '本地业务请求' }} HTTP {{ entry.httpStatus ?? '未取得' }}<template v-if="entry.durationMs !== null"> · 等待 {{ entry.durationMs }} ms</template></p><p v-if="entry.requestID">请求号：<CopyId :value="entry.requestID" label="请求号" /></p><p v-if="entry.jobID">源作业号：<CopyId :value="entry.jobID" label="源作业号" /></p><p v-if="entry.kind === 'business'" class="hint">本地查询未提供统一测试号；请求号不替代 test_run_id。</p><template v-else><p class="hint">最近作业状态：{{ states[entry.jobStatus ?? ''] || '未确认' }}。{{ entry.resultState === 'available' ? '当时读取到结果，不保证现在仍可获取；摘要不保存结果。' : entry.resultState === 'result_unavailable' ? '当时未取得缓存结果；不能据此推断作业失败。' : '当前结果可获取性未确认。' }}</p><p class="hint">此处不自动查询或重测；持久历史与统一关联仍待后端契约补齐。</p></template></li></ol>
</section></template>
<style scoped>
.recent-tests { margin-top: 24px; border-top: 1px solid var(--line); padding-top: 20px; min-width: 0; overflow-wrap: anywhere; }
ol { list-style: none; padding: 0; } li { padding-block: 16px; border-bottom: 1px solid var(--line); } .recent-heading { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; } li p { margin-block: 8px; }
</style>