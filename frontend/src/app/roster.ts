import type { QueryPageChain } from './queryPagination'
import { acceptPage, pageCount } from './queryPagination'
import type { BusinessTestResult } from './businessTest'
import { responseRecords } from './responseViews'
export interface RosterContext {
  request: { academic_year: string; semester: string; teaching_class_number: string; planned_course_code: string }
  expectedCount: number | null
  combination: string
  version: string
}
const keys = ['academic_year', 'semester', 'teaching_class_number', 'planned_course_code'] as const
export function rosterContext(row: Map<string, string>, requestText: string, combination: string, version: string): RosterContext | null {
  try {
    const request = JSON.parse(requestText), teaching_class_number = JSON.parse(row.get('teaching_class_number') ?? 'null'), planned_course_code = JSON.parse(row.get('planned_course_code') ?? 'null')
    if (typeof request.academic_year !== 'string' || !/^\d{4}-\d{4}$/.test(request.academic_year) || !['1', '2'].includes(request.semester) ||
      [teaching_class_number, planned_course_code].some(value => typeof value !== 'string' || !value.trim() || value.length > 512)) return null
    const count = JSON.parse(row.get('student_count') ?? 'null')
    return { request: { academic_year: request.academic_year, semester: request.semester, teaching_class_number, planned_course_code },
      expectedCount: Number.isSafeInteger(count) && count >= 0 ? count : null,
      combination: /^[A-Za-z0-9_.:-]{1,128}$/.test(combination) ? combination : '', version }
  } catch { return null }
}
export interface RosterProgress { pages: Map<number, string[]>; totalPages: number | null; total: number | null; uniqueCount: number; rowCount: number; complete: boolean; issue: string; matched: boolean }
export function recordRosterPage(context: RosterContext, chain: QueryPageChain, result: BusinessTestResult, previous?: RosterProgress | null): RosterProgress {
  const state: RosterProgress = { pages: new Map(previous?.pages), totalPages: chain.total === null || chain.pageSize === null ? null : pageCount(chain), total: chain.total, uniqueCount: previous?.uniqueCount ?? 0, rowCount: previous?.rowCount ?? 0, complete: false, issue: '', matched: false }
  try {
    if (previous?.issue) throw new Error(previous.issue)
    if (chain.blocked || chain.total === null || chain.pageSize === null) throw new Error(chain.blocked || '名单分页信息不完整。')
    if (chain.version !== context.version) throw new Error('目录版本已变化，请重新查询教学班后再核对。')
    const request = JSON.parse(result.requestText ?? '{}')
    if (keys.some(key => request[key] !== context.request[key])) throw new Error('查询条件已偏离所选教学班，不能与原 student_count 核对。')
    if (!result.response) throw new Error('本页没有成功响应；请重新执行第一页后核对。')
    if (!context.combination || chain.combination !== context.combination || result.response?.headers['X-SICP-Combination'] !== context.combination) throw new Error('教学班与名单的数据组合不同或未确认，不能核对人数；请重新查询教学班。')
    if (pageCount(chain) > 50 || chain.total > 1000) throw new Error('人数核对最多保存 50 页 / 1000 条；请调整每页条数或缩小范围，当前未完整核验。')

    acceptPage(chain, result, chain.page)
    const data = result.response.body.data as Record<string, unknown>
    if (keys.some(key => data[key] !== context.request[key])) throw new Error('名单响应中的教学班或学年学期不一致，本页未计入。')
    const records = responseRecords(result.response.responseText ?? '', 'business')
    if (records.error || records.truncated) throw new Error(records.error || '记录显示超限。')
    const ids = records.objects.map(row => row.student_number)
    if (ids.some(id => typeof id !== 'string' || !id.trim())) throw new Error('名单中存在缺失或非字符串学号，不能核对人数。')
    const old = state.pages.get(chain.page)
    if (old && JSON.stringify(old) !== JSON.stringify(ids)) throw new Error('同一快照的已读取页内容发生变化，不能核对人数。')
    state.pages.set(chain.page, ids as string[])
    const all = [...state.pages.values()].flat()
    state.rowCount = all.length; state.uniqueCount = new Set(all).size
    state.complete = state.pages.size === state.totalPages && state.rowCount === state.total
    state.matched = state.complete && state.uniqueCount === context.expectedCount && state.uniqueCount === state.rowCount
    if (state.complete && state.uniqueCount !== state.rowCount) state.issue = '完整分页中存在重复学号；保留原记录并按学号去重计数，不能判为核对通过。'
  } catch (cause) { state.issue = cause instanceof Error ? cause.message : '名单核对未完成。' }
  return state
}