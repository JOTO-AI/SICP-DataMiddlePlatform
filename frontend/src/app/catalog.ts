import { operationTitles } from '../queryOperations'
export interface CatalogField { type: string; required: boolean; nullable: boolean; default: unknown; limit: string; properties?: Record<string, CatalogField>; items?: CatalogField }
export interface CatalogAPI {
  id: string; version: string; purpose: string; method: string; path: string; kind: string
  implemented: boolean; readiness: string; snapshot: boolean; request: CatalogField; response: CatalogField
  request_example: unknown; success_example: unknown; failure_example: unknown; notes: string[]
}
export interface Catalog { version: string; items: CatalogAPI[] }
export const catalogGroups: Record<string, string> = { business: '业务接口', source: '上游接口' }
export function catalogName(api: CatalogAPI): string { return operationTitles[api.id] || api.id }
export function readCatalog(value: unknown): Catalog | null {
  if (!value || typeof value !== 'object' || !('version' in value) || typeof value.version !== 'string' || !value.version || !('items' in value) || !Array.isArray(value.items)) return null
  const ids = new Set<string>()
  for (const item of value.items) {
    if (!item || typeof item !== 'object' || typeof item.id !== 'string' || !item.id || ids.has(item.id) || typeof item.path !== 'string' || typeof item.method !== 'string' || typeof item.kind !== 'string' || item.version !== value.version) return null
    ids.add(item.id)
  }
  return value as Catalog
}
export function filterCatalog(items: CatalogAPI[], search: string, group: string): CatalogAPI[] {
  const term = search.trim().toLocaleLowerCase()
  return items.filter(item => (!group || item.kind === group) && [catalogName(item), item.id, item.path, item.purpose, catalogGroups[item.kind] ?? item.kind].join(' ').toLocaleLowerCase().includes(term))
}
