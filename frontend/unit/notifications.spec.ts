import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { readNotificationPage, readDeliveryDetail, notificationPath } from '../src/app/notifications'
test('notification decisions and per-recipient attempts retain existing backend facts and string revisions', () => {
  const decisions = managementCapture('decisions').response.data as any, deliveries = managementCapture('deliveries').response.data as any, detail = managementCapture('delivery_detail').response.data as any
  expect(readNotificationPage(decisions, 'decisions')).toEqual(decisions)
  expect(decisions.items[0].decision).toBe('disabled'); expect(decisions.items[0].deliveries).toBe('0')
  expect(readNotificationPage(deliveries, 'deliveries')).toEqual(deliveries)
  expect(readDeliveryDetail(detail, deliveries.items[0].id)).toEqual(detail)
  expect(detail.delivery.status).toBe('unknown'); expect(detail.attempts[0].finished_at).toBeNull()
  expect(notificationPath('deliveries', 'opaque-next')).toBe('/admin/v1/notifications?cursor=opaque-next')
})
test('notification readers reject identity, pagination and count contradictions without inventing delivery', () => {
  const page = managementCapture('decisions').response.data as any, detail = managementCapture('delivery_detail').response.data as any
  expect(readNotificationPage({ ...page, items: [...page.items, ...page.items] }, 'decisions')).toBeNull()
  expect(readNotificationPage({ ...page, items: [{ ...page.items[0], accepted: '1' }] }, 'decisions')).toBeNull()
  expect(readNotificationPage({ ...page, next_cursor: '' }, 'decisions')).toBeNull()
  expect(readDeliveryDetail(detail, 'wrong-id')).toBeNull()
  expect(readDeliveryDetail({ ...detail, attempts: [...detail.attempts, ...detail.attempts] }, detail.delivery.id)).toBeNull()
})
import { readManagementJob, terminalManagementJob, notificationTestFact } from '../src/app/managementJobs'
import { createJobOperation } from '../src/api/operations'
test('mail test uses captured revision-only input and validates actual job identity separately from delivery', () => {
  const capture = managementCapture('test_notifications'), job = capture.response.data as any
  expect(readManagementJob(job, 'notification_test', job.id)).toEqual(job)
  expect(readManagementJob(job, 'cleanup')).toBeNull()
  expect(readManagementJob(job, 'notification_test', 'different')).toBeNull()
  expect(terminalManagementJob(job)).toBe(false)
  expect(notificationTestFact(job)).toBe('explicit:' + job.id)
  const input = capture.request as { revision: string; idempotency_key: string }
  expect(JSON.parse(createJobOperation('/admin/v1/notifications/test', { revision: input.revision }, input.idempotency_key).payload)).toEqual(input)
  expect(notificationTestFact(job)).not.toBe((managementCapture('deliveries').response.data as any).items[0].fact_id)
})