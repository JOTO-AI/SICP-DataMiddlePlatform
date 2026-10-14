import { test, expect } from '@playwright/test'
import { backendCapture } from '../fixtures/backend'
import { responseRecords, datedLessons } from '../src/app/responseViews'
import { calendarWeeks, timetableWeek } from '../src/app/timetable'
test('both timetable week views place only recorded date and section pairs without merging course records', () => {
  for (const id of ['students.timetable.query', 'staff.timetable.query']) {
    const records = responseRecords(JSON.stringify(backendCapture(id).response), 'business')
    const lessons = datedLessons(records.objects), weeks = calendarWeeks(lessons.groups)
    expect(weeks).toHaveLength(1)
    expect(weeks[0]!.days).toEqual(['2026-09-07', '2026-09-08', '2026-09-09', '2026-09-10', '2026-09-11', '2026-09-12', '2026-09-13'])
    const matrix = timetableWeek(records.objects, lessons.groups, weeks[0]!.days)
    expect(matrix.sections).toEqual([1, 2])
    expect(matrix.cells.get('2026-09-07/1')).toEqual([0, 2])
    expect(matrix.cells.get('2026-09-07/2')).toEqual([1, 3])
    expect([...matrix.cells.values()].flat().length).toBe(records.rows.length)
    expect(matrix.clipped).toBe(false); expect(matrix.weekdayMismatch).toBe(false)
  }
})
test('missing associations remain records; missing dates stay in the table and weekday contradictions are visible', () => {
  const records = responseRecords(JSON.stringify(backendCapture('students.timetable.query').response), 'business')
  const rows = structuredClone(records.objects)
  // Missing-field protocol boundaries on existing rows; no new courses, people or lessons.
  rows[0]!.course_name = null; rows[0]!.course_code = ''; rows[0]!.teacher_names = []
  rows[1]!.dates = null; rows[2]!.weekday = 2
  const lessons = datedLessons(rows), weeks = calendarWeeks(lessons.groups), matrix = timetableWeek(rows, lessons.groups, weeks[0]!.days)
  expect(rows).toHaveLength(records.rows.length)
  expect(matrix.cells.get('2026-09-07/1')).toContain(0)
  expect(lessons.unmapped).toEqual([1]); expect(matrix.weekdayMismatch).toBe(true)
  expect(rows[0]!.teacher_staff_numbers).toEqual(records.objects[0]!.teacher_staff_numbers)
  expect(calendarWeeks([['2026-01-01', []]])[0]!.start).toBe('2025-12-29')
})
