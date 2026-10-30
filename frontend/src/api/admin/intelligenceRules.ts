import { apiClient } from '../client'

export interface IntelligenceRule {
  name: string
  type: 'contains' | 'regex'
  pattern: string
  enabled: boolean
}
export interface IntelligenceRulesConfig { rules: IntelligenceRule[] }

export async function getRules(): Promise<IntelligenceRulesConfig> {
  return (await apiClient.get<IntelligenceRulesConfig>('/admin/settings/intelligence-rules')).data
}
export async function saveRules(value: IntelligenceRulesConfig): Promise<IntelligenceRulesConfig> {
  return (await apiClient.put<IntelligenceRulesConfig>('/admin/settings/intelligence-rules', value)).data
}
