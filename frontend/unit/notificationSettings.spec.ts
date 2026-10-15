import { test, expect } from '@playwright/test'
import { managementCapture } from '../fixtures/backend'
import { readNotificationSettingsView, readNotificationSettings, notificationPayload, normalizeRecipient } from '../src/app/notificationSettings'
test('mail settings preserve existing string revision, rules and exact save contract', () => {
  const view = managementCapture('notification_settings').response.data as any, saved = managementCapture('save_notifications')
  expect(readNotificationSettingsView(view)).toEqual(view)
  expect(notificationPayload(view.settings.revision, view.settings, view.channel_diagnostics.configured)).toEqual(saved.request)
  expect(readNotificationSettings(saved.response.data)).toEqual(saved.response.data)
  expect(readNotificationSettings({ ...view.settings, revision: 1 })).toBeNull()
  expect(readNotificationSettingsView({ ...view, channel_diagnostics: { ...view.channel_diagnostics, configured: true } })).toBeNull()
})
test('recipient validation uses the recorded address and rejects duplicates, bad format and exceeded count', () => {
  const view = managementCapture('notification_settings').response.data as any, address = (managementCapture('deliveries').response.data as any).items[0].recipient
  expect(normalizeRecipient(' ' + address + ' ')).toBe(address)
  expect(() => notificationPayload('1', { ...view.settings, recipients: [address, address] }, true)).toThrow(/重复/)
  expect(() => notificationPayload('1', { ...view.settings, recipients: Array(21).fill(address) }, true)).toThrow(/20/)
  expect(() => normalizeRecipient(address.replace('@', ''))).toThrow()
  expect(() => normalizeRecipient(address + '\n')).toThrow()
  expect(notificationPayload('1', { ...view.settings, enabled: true, recipients: [] }, false).enabled).toBe(false)
})