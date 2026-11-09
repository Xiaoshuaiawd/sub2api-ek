import { apiClient } from '../client'

export interface AccountInventoryStatus {
  target_count: number
  enabled: boolean
  available: number
  pending: number
  activating: number
  active: number
  failed: number
  retired: number
  duplicate: number
}

export interface AccountInventoryRow {
  id: number
  email: string
  state: 'pending' | 'activating' | 'active' | 'failed' | 'retired' | 'duplicate'
  account_id?: number
  attempts: number
  error_code?: string
  created_at: string
}

export interface AccountInventoryPage {
  items: AccountInventoryRow[]
  total: number
  page: number
  page_size: number
}

export interface AccountInventoryImportResult {
  total: number
  imported: number
  skipped: number
}

const base = '/admin/accounts/inventory'

export async function getStatus(): Promise<AccountInventoryStatus> {
  return (await apiClient.get(`${base}/status`)).data
}

export async function saveConfig(targetCount: number, enabled: boolean): Promise<AccountInventoryStatus> {
  return (await apiClient.put(`${base}/config`, { target_count: targetCount, enabled })).data
}

export async function importLines(content: string): Promise<AccountInventoryImportResult> {
  return (await apiClient.post(`${base}/import`, { content }, { timeout: 120000 })).data
}

export async function listRows(page = 1, pageSize = 50): Promise<AccountInventoryPage> {
  return (await apiClient.get(base, { params: { page, page_size: pageSize } })).data
}
