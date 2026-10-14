const dayMS = 86400000
export function calendarWeeks(groups: [string, number[]][]) {
  const starts = new Set<string>()
  for (const [date] of groups) {
    const day = new Date(date), offset = (day.getUTCDay() + 6) % 7
    if (!Number.isFinite(day.getTime())) continue
    starts.add(new Date(day.getTime() - offset * dayMS).toISOString().split('T')[0]!)
  }
  return [...starts].sort().map(start => ({ start, days: Array.from({ length: 7 }, (_, index) => new Date(Date.parse(start) + index * dayMS).toISOString().split('T')[0]!) }))
}
export function timetableWeek(rows: Record<string, unknown>[], groups: [string, number[]][], days: string[]) {
  const entries = new Map(groups), sections = new Set<number>(), cells = new Map<string, number[]>()
  let clipped = false, count = 0, weekdayMismatch = false
  for (const day of days) for (const index of entries.get(day) ?? []) {
    const row = rows[index]!, values = Array.isArray(row.sections) ? row.sections : []
    const weekday = new Date(day).getUTCDay() || 7
    if (typeof row.weekday === 'number' && row.weekday !== weekday) weekdayMismatch = true
    if (values.length > 64) clipped = true
    for (const section of new Set(values.slice(0, 64))) {
      if (typeof section !== 'number' || !Number.isSafeInteger(section) || section <= 0) continue
      if (!sections.has(section) && sections.size >= 64 || count >= 2000) { clipped = true; continue }
      sections.add(section)
      const key = `${day}/${section}`
      cells.set(key, [...(cells.get(key) ?? []), index]); count++
    }
  }
  return { sections: [...sections].sort((a, b) => a - b), cells, clipped, weekdayMismatch }
}
