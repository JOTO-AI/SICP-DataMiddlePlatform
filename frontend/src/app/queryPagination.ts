import type { CatalogAPI } from './catalog'
import { operations } from '../queryOperations'
import type { BusinessTestResult } from './businessTest'
import { ApiError } from '../api/http'
export interface QueryPageChain {
  readonly apiId: string; readonly version: string; readonly requestText: string
  readonly snapshot: string; readonly combination: string; readonly page: number; readonly pageSize: number | null; readonly total: number | null; readonly blocked: string
}
export const paginatedAPI = (api: CatalogAPI) => api.kind === 'business' && !!operations.find(operation => operation.operation === api.id)?.paginated
export const validSnapshot = (value: string) => /^[0-9a-f]{64}$/.test(value)
export const pageCount = (chain: QueryPageChain) => chain.total === null || chain.pageSize === null ? 1 : Math.min(1000000, Math.max(1, Math.ceil(chain.total / chain.pageSize)))
export function firstPageChain(api: CatalogAPI, result: BusinessTestResult): QueryPageChain | null {
  if (!paginatedAPI(api) || !result.response || !result.requestText) return null
  const body = JSON.parse(result.requestText), response = result.response
  const size = body.page_size ?? api.request.properties?.page_size?.default
  const data = response.body.data as { total?: unknown; items?: unknown } | null
  const snapshot = response.snapshot, combination = response.headers['X-SICP-Combination'] ?? ''
  const validTotal = typeof data?.total === 'number' && Number.isSafeInteger(data.total) && data.total >= 0
  const validSize = typeof size === 'number' && Number.isSafeInteger(size) && size > 0 && size <= 1000
  const blocked = !validSnapshot(snapshot) ? '首响应未提供有效快照，不能续页；请显式重新查询第一页。'
    : !/^[A-Za-z0-9_.:-]{1,128}$/.test(combination) ? '首响应未提供有效数据组合，不能确认后续页面来自同一组合。'
    : !validTotal || !validSize ? '响应总数或每页条数不完整，不能推断下一页。'
    : !Array.isArray(data?.items) || data.items.length > Number(size) || data.items.length > Number(data.total) ? '首响应记录结构或条数不符合分页约定，不能续页。' : ''
  return Object.freeze({ apiId: api.id, version: api.version, requestText: result.requestText, snapshot, combination, page: 1, pageSize: validSize ? size : null, total: validTotal ? data.total as number : null, blocked })
}
export function nextPageRequest(api: CatalogAPI, chain: QueryPageChain, page: number): string {
  if (!paginatedAPI(api) || chain.apiId !== api.id || chain.version !== api.version || chain.blocked || !validSnapshot(chain.snapshot) || !chain.combination) throw new ApiError('当前接口没有可继续使用的查询快照，请从第一页重新查询。', 'snapshot_required')
  if (!Number.isSafeInteger(page) || page < 1 || page > pageCount(chain)) throw new ApiError('页码超出本次查询范围。', 'invalid_argument')
  return JSON.stringify({ ...JSON.parse(chain.requestText), page })
}
export function acceptPage(chain: QueryPageChain, result: BusinessTestResult, page: number): QueryPageChain {
  if (!result.response) {
    return Object.freeze({ ...chain, blocked: result.error?.code === 'conflict' ? '查询冲突：快照可能失效或条件不匹配；教学班关联也可能存在跨学期歧义。请核对后从第一页重新查询，不会自动重试。' : chain.blocked })
  }
  const response = result.response, data = response.body.data as { total?: unknown; items?: unknown } | null
  if (response.snapshot !== chain.snapshot || response.headers['X-SICP-Combination'] !== chain.combination || data?.total !== chain.total || !Array.isArray(data?.items) || data.items.length !== Math.min(chain.pageSize ?? 0, (chain.total ?? 0) - (page - 1) * (chain.pageSize ?? 0))) {
    throw new ApiError('续页快照、数据组合、总数或记录结构发生变化；本页未接纳，请从第一页重新查询。', 'snapshot_mismatch', response.requestID, response.status, response.headers, response.responseText)
  }
  return Object.freeze({ ...chain, page })
}
