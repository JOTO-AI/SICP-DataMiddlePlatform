<script setup lang="ts">
import { ref, onMounted, onActivated, onDeactivated, watch } from 'vue'
import { request, describe, ApiError, type Envelope } from '../api'
import type { CatalogAPI } from '../app/catalog'
import { apiCallsPath, readCallPage, type CallPage } from '../app/apiCalls'
import { logDetailPath } from '../app/links'
import { useRequestScope } from '../composables/useRequestScope'
import Timestamp from './Timestamp.vue'
import ErrorState from './ErrorState.vue'
import EmptyState from './EmptyState.vue'
const props = defineProps<{ api: CatalogAPI }>()
const page = ref<CallPage | null>(null), busy = ref(false), error = ref(''), readAt = ref('')
const identity = ref(''), format = ref<'raw' | 'correlation'>('raw'), batch = ref(1)
const scope = useRequestScope()
const cursors = new Set<string>()
async function read(next = false) {
  if (busy.value || next && !page.value?.next_cursor) return
  if (next && batch.value >= 50) { error.value = '本次最多读取 50 批，请重新读取首批或到日志中心缩小范围。'; return }
  let path: string
  const cursor = next ? page.value!.next_cursor! : ''
  try { path = apiCallsPath(props.api, identity.value, format.value, cursor) } catch (cause) { error.value = describe(cause); return }
  const run = scope.begin(); busy.value = true; error.value = ''
  try {
    const response = await request<Envelope<unknown>>('worker', path, { signal: run.signal, domain: 'logs' })
    if (!run.current()) return
    const result = readCallPage(response.body.data, props.api)
    if (!result || next && (result.filter.from !== page.value?.filter.from || result.filter.to !== page.value?.filter.to || result.filter.test_run_id !== page.value?.filter.test_run_id) || props.api.kind === 'source' && format.value === 'correlation' && result.filter.test_run_id !== identity.value || next && result.next_cursor && (result.next_cursor === cursor || cursors.has(result.next_cursor))) throw new ApiError('日志响应范围或游标不符合当前接口。', 'invalid_response')
    if (!next) { cursors.clear(); batch.value = 1 } else { cursors.add(cursor); batch.value++ }
    page.value = result; readAt.value = new Date().toISOString()
  } catch (cause) { if (run.current()) error.value = describe(cause) }
  finally { if (run.current()) busy.value = false }
}
function reset() { scope.cancel(); page.value = null; error.value = ''; readAt.value = ''; busy.value = false; cursors.clear(); batch.value = 1 }
watch([identity, format, () => props.api.id], reset)
onMounted(() => { if (props.api.kind === 'business') void read() })
onDeactivated(() => { reset(); identity.value = '' })
onActivated(() => { if (props.api.kind === 'business' && !page.value && !busy.value) void read() })
</script>
<template>
  <section aria-label="接口调用记录">
    <h2>调用记录</h2>
    <p class="hint">历史范围默认由服务端取最近 24 小时；以下是日志事件，开始/结束分别记录，不代表请求总数或持久测试历史。</p>
    <p v-if="api.kind === 'business'">本地日志按实际 operation <code>{{ api.id }}</code> 查询。</p>
    <form v-else @submit.prevent="read()"><p>后端尚未提供按源表查询全部调用的筛选。请填写当前源接口的真实测试作业 ID 或后端给出的日志关联 ID；不以当前页筛选冒充全部历史。</p><label>测试作业 ID<input v-model="identity" autocomplete="off" maxlength="128" spellcheck="false" /></label><label>ID 格式<select v-model="format"><option value="raw">原始作业 ID</option><option value="correlation">日志关联 ID</option></select></label><button :disabled="busy || !identity" type="submit">查询关联记录</button></form>
    <button v-if="api.kind === 'business' || page" :disabled="busy" @click="read()">{{ busy ? '读取中…' : '重新读取首批' }}</button>
    <ErrorState v-if="error" :message="error" />
    <p v-if="error && page" class="warning">当前读取失败，以下保留上次成功读取的批次，不代表最新结果。</p>
    <template v-if="page"><p class="hint">范围 <Timestamp :value="page.filter.from" /> 至 <Timestamp :value="page.filter.to" /> · 第 {{ batch }} 批 · 读取于 <Timestamp :value="readAt" /></p>
      <div v-if="page.items.length" class="call-scroll" role="region" aria-label="调用日志表格" tabindex="0"><table><thead><tr><th>事件时间</th><th>类型 / 阶段</th><th>结果</th><th>HTTP</th><th>耗时（ns）</th><th>详情</th></tr></thead><tbody><tr v-for="item in page.items" :key="item.event_id"><td><Timestamp :value="item.occurred_at" /></td><td>{{ item.kind }} / {{ item.phase }}</td><td>{{ item.outcome ?? '尚未记录' }}</td><td>{{ item.http_status ?? '未记录' }}</td><td>{{ item.duration_ns ?? '未记录' }}</td><td><RouterLink v-if="logDetailPath(item.source, item.event_id)" :to="logDetailPath(item.source, item.event_id)!">查看事件</RouterLink></td></tr></tbody></table></div>
      <EmptyState v-else title="此时间范围内没有匹配的日志事件" description="空日志不证明接口从未调用，也不表示当前服务健康。" />
      <button v-if="page.next_cursor" :disabled="busy" @click="read(true)">读取下一批</button><p v-else class="hint">已到本次日志范围末批。</p>
    </template>
    <EmptyState v-else :loading="busy" title="尚未加载调用记录" description="查询后展示后端返回的真实日志事件。" />
  </section>
</template>
<style scoped>
form { display: flex; flex-wrap: wrap; gap: 12px; align-items: end; margin-bottom: 16px; }
form p { flex-basis: 100%; }
form label { flex: 1 1 240px; }
.call-scroll { overflow: auto; max-width: 100%; }
table { min-width: 720px; width: 100%; }
</style>
