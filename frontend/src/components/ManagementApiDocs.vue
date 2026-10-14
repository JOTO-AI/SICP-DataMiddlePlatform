<script setup lang="ts">
// Versioned documentation, checked against internal/admin routes and FRONTEND_ENDPOINT_BASELINE.md.
const groups = [
  { title: '更新与修复', to: '/updates', link: '前往更新事件', note: '由专用页面根据初始化、运行任务和恢复状态决定可用动作。受理不明时保留原幂等键与原载荷。', entries: [
    { method: 'POST', path: '/admin/v1/tasks/incremental', description: '按确认进度进行增量更新。' },
    { method: 'POST', path: '/admin/v1/tasks/force-full', description: '全量更新已初始化的数据。' },
    { method: 'POST', path: '/admin/v1/tables/{table}/force-full', description: '单表全量更新；是否允许由服务端重新检查。' },
    { method: 'POST', path: '/admin/v1/tasks/{task_id}/rerun', description: '重跑指定允许重跑的原任务。' },
  ], contract: '正文为空对象，幂等键放入 Idempotency-Key 请求头；HTTP 202 表示已受理，需继续读取任务状态。' },
  { title: '通知设置与测试', to: '/notifications', link: '前往通知中心', note: '测试邮件使用已保存配置中的收件人，不接受临时收件人。配置与投递结果分别确认。', entries: [
    { method: 'PUT', path: '/admin/v1/settings/notifications', description: '保存 revision、enabled、recipients、rules；revision 为字符串且须匹配当前版本。' },
    { method: 'POST', path: '/admin/v1/notifications/test', description: '提交已保存 revision 的测试作业，idempotency_key 放在 JSON 正文。' },
  ], contract: '保存冲突需重新读取并核对。作业受理返回 HTTP 202 与 Location，发送结果以作业及通知投递事实为准。' },
  { title: '日志保留与清理', to: '/settings', link: '前往系统设置', note: '清理会移除符合条件的日志，必须先在专用页面查看预览，再明确确认执行；本页不能触发预览或清理。', entries: [
    { method: 'PUT', path: '/admin/v1/settings/retention', description: '保存 revision 字符串及 platform_days/upstream_days，天数范围 1–3650。' },
    { method: 'POST', path: '/admin/v1/maintenance/cleanup/preview', description: '每次仅预览 platform 或 upstream 一个源的受保护可清理范围。' },
    { method: 'POST', path: '/admin/v1/maintenance/cleanup', description: '提交同主体有效 preview_id、revision 与 JSON idempotency_key；服务端再次核对保护条件。' },
  ], contract: '预览有效期 10 分钟；清理受理返回 HTTP 202 与 Location。无法读取结果时不得将删除数量显示为 0。' },
]
</script>
<template>
  <section aria-label="管理写接口说明">
    <h3>管理写接口说明</h3><p class="hint">静态说明基线：2026-10-14，已核对管理服务路由与端点文档；不属于后端 23 项可测试目录。</p>
    <p>操作在对应专用页面完成。接口中心只提供说明和导航，不提供管理写接口的通用执行器；页面入口不表示当前操作已获准或服务已就绪。</p>
    <p>源 token、业务 API key、数据库凭据由服务端部署管理，页面不读取或回显。管理请求使用已登录会话和 CSRF 保护；无需在接口中心填写部署凭据。</p>
    <details v-for="group in groups" :key="group.title"><summary>{{ group.title }}</summary><p>{{ group.note }}</p>
      <ul><li v-for="entry in group.entries" :key="entry.path"><code>{{ entry.method }} {{ entry.path }}</code><p>{{ entry.description }}</p></li></ul>
      <p class="hint">{{ group.contract }}</p><RouterLink :to="group.to">{{ group.link }} →</RouterLink>
    </details>
    <p class="hint">源接口测试作业仅从选定的真实源接口详情进入，固定 api_id 和目录版本；浏览器不直连上游。当前页面没有邮件发送、数据更新或日志清理按钮。</p>
  </section>
</template>
<style scoped>
details { border-top: 1px solid var(--line); margin-top: 20px; padding-top: 16px; }
summary { font-weight: 600; }
li { margin: 12px 0; }
code { overflow-wrap: anywhere; }
p { margin: 8px 0; }
</style>
