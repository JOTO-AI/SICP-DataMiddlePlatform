import { ApiError } from './api/http'
export { request, ApiError } from './api/http'
export type { Envelope, Result, RequestOptions, ErrorDomain } from './api/http'

export function describe(error: unknown) {
  return error instanceof ApiError ? [error.message, error.code, error.requestID && `请求号 ${error.requestID}`].filter(Boolean).join(' · ') : '操作失败，请重试。'
}
export function time(value?: string | null) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '尚未发生' }

export type { ResponseHeaders } from './api/metadata'
