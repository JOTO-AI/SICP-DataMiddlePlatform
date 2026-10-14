import { managementCapture } from './backend'
// Project the existing captured task through operations.Repository.Events' exact DTO mapping.
// Do not invent another task or use normal business/person records as update events.
export function recordedUpdateEvent() {
  const task = managementCapture('update_detail').response.data as Record<string, unknown>
  return { id: task.task_id, task_id: task.task_id, mode: task.mode, status: task.status, predecessor_id: task.predecessor_id,
    accepted_at: task.accepted_at, started_at: task.started_at, ready_at: task.ready_at, published_at: task.published_at, checked_at: task.checked_at, finished_at: task.finished_at }
}