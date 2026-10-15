import { test, expect } from '@playwright/test'
import { operationMeaning, updateMeaning } from '../src/app/updateMeaning'
import { managementCapture } from '../fixtures/backend'
import { readOperationPage } from '../src/app/updateOperations'
test('recorded unknown control node does not acquire invented completion', () => {
  const node = readOperationPage(managementCapture('operation_nodes').response.data, '')!.items[0]!
  expect(operationMeaning(node)).toContain('保持未知')
})
test('backend enum semantics distinguish route shift, publication, staged progress and diagnostic attempts', () => {
  // Contract code branches, not synthetic normal task/operation records.
  const meaning = (kind: string, state: string, evidence = 'control') => operationMeaning({ kind, state, evidence })
  expect(meaning('route_shift', 'confirmed')).toContain('不等于发布新数据组合')
  expect(meaning('bundle_publish', 'confirmed')).toContain('同步进度是否提交仍以任务')
  expect(meaning('bundle_publish', 'pending')).toContain('发布尚未确认')
  expect(meaning('progress', 'staged')).toContain('尚未确认提交')
  expect(meaning('progress', 'confirmed')).toContain('成功进度计划已确认')
  expect(meaning('cleanup', 'unknown')).toContain('没有记录不代表已清理')
  expect(meaning('write_copy', 'succeeded', 'platform_log')).toContain('不代表整个更新完成')
  expect(updateMeaning('success_unchanged', 'incremental')).toContain('沿用已发布的数据组合')
  expect(updateMeaning('success_published', 'force_full')).toContain('强制全量重建并发布完成')
  expect(updateMeaning('published_pending_progress', 'incremental')).toContain('尚待确认')
  expect(updateMeaning('failed', 'incremental')).toContain('实际状态为准')
})