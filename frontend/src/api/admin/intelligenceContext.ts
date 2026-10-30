import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface IntelligenceContextRule {
  id: string
  name: string
  type: string
  pattern?: string
}

export interface IntelligenceContext {
  id?: string
  request_id: string | null
  content_type: string
  content_encoding?: string
  request_body: string | null
  request_body_base64?: string
  matched_rules: IntelligenceContextRule[]
}

export interface IntelligenceContextSummary {
  id: string
  request_id: string | null
  created_at: string
  matched_rules: IntelligenceContextRule[]
  usage_log_id?: number | null
}

export async function getIntelligenceContext(id: number, signal?: AbortSignal): Promise<IntelligenceContext> {
  const { data } = await apiClient.get<IntelligenceContext>(`/admin/usage/${id}/intelligence-context`, { signal })
  return data
}

export async function getIntelligenceContextById(id: string, signal?: AbortSignal): Promise<IntelligenceContext> {
  const { data } = await apiClient.get<IntelligenceContext>(`/admin/usage/intelligence-contexts/${encodeURIComponent(id)}`, { signal })
  return data
}

export async function listIntelligenceContexts(page: number, pageSize: number, signal?: AbortSignal): Promise<PaginatedResponse<IntelligenceContextSummary>> {
  const { data } = await apiClient.get<PaginatedResponse<IntelligenceContextSummary>>('/admin/usage/intelligence-contexts', { params: { page, page_size: pageSize }, signal })
  return data
}
