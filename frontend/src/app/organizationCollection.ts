import type { CatalogAPI } from './catalog'
import { recordBusinessTest } from './recentTests'
import { ApiError } from '../api/http'
import { executeBusinessTest, type BusinessTestResult } from './businessTest'
import { nextPageRequest, acceptPage, pageCount, type QueryPageChain } from './queryPagination'
import { responseRecords, organizationNodes, type ResponseRecords } from './responseViews'
export const organizationLimit = { pages: 50, rows: 1000 } as const
export interface OrganizationPage { page: number; result: BusinessTestResult; records: ResponseRecords }
export function collectionBlock(chain: QueryPageChain): string {
  if (chain.blocked) return chain.blocked
  if (chain.total === null || chain.pageSize === null) return '分页信息不完整，无法加载完整范围。'
  return pageCount(chain) > organizationLimit.pages || chain.total > organizationLimit.rows ? '超过 50 页或 1000 条上限；请缩小查询范围或调整每页条数后重新查询。' : ''
}
export function collectionRecords(pages: OrganizationPage[]): ResponseRecords {
  const ordered = [...pages].sort((a, b) => a.page - b.page)
  return { rows: ordered.flatMap(page => page.records.rows), objects: ordered.flatMap(page => page.records.objects), truncated: false, error: '' }
}
export function organizationIntegrity(records: ResponseRecords): boolean {
  const codes = records.objects.map(row => row.organization_code)
  return codes.every(code => typeof code === 'string' && !!code) && new Set(codes).size === codes.length && organizationNodes(records.objects).every(node => !node.note || node.note === '上级为 null')
}
export function filteredOrganizations(requestText: string): boolean {
  return Object.entries(JSON.parse(requestText)).some(([key, value]) => !['page', 'page_size'].includes(key) && value !== null)
}
// Each page remains an actual HTTP result; combined rows are never presented as one response.
export async function loadOrganizations(api: CatalogAPI, chain: QueryPageChain, seed: BusinessTestResult, signal: AbortSignal, progress: (pages: OrganizationPage[]) => void): Promise<OrganizationPage[]> {
  if (api.id !== 'organizations.query') throw new Error('当前接口不是组织查询。')
  const blocked = collectionBlock(chain)
  if (blocked) throw new Error(blocked)
  const pages: OrganizationPage[] = []
  function add(result: BusinessTestResult, page: number) {
    signal.throwIfAborted()
    if (!result.response) throw result.error ?? new Error('未取得组织分页响应。')
    acceptPage(chain, result, page)
    const records = responseRecords(result.response.responseText ?? '', 'business')
    if (records.error || records.truncated) throw new Error(records.error || '组织记录超过显示上限。')
    const codes = [...collectionRecords(pages).objects, ...records.objects].map(row => row.organization_code)
    if (codes.some(code => typeof code !== 'string' || !code) || new Set(codes).size !== codes.length) throw new Error('组织编号缺失或重复，本页未接纳，无法确认完整性。')
    pages.push({ page, result, records }); progress([...pages])
  }
  // Validate even a single-page seed before explicitly marking its scope complete.
  nextPageRequest(api, chain, chain.page)
  add(seed, chain.page)
  for (let page = 1; page <= pageCount(chain); page++) {
    if (page === chain.page) continue
    signal.throwIfAborted()
    const result = await executeBusinessTest(api, nextPageRequest(api, chain, page), signal, chain.snapshot)
    signal.throwIfAborted()
    try { add(result, page); recordBusinessTest(api, result) }
    catch (cause) {
      const response = result.response
      recordBusinessTest(api, response ? { ...result, response: undefined, error: cause instanceof ApiError ? cause : new ApiError('组织分页未接纳。', 'invalid_response', response.requestID, response.status) } : result)
      throw cause
    }
  }
  if (collectionRecords(pages).rows.length !== chain.total) throw new Error('已接纳记录数与响应总数不一致，无法确认完整性。')
  return pages
}