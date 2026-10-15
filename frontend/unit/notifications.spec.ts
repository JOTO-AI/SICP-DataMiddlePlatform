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