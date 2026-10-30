import { apiClient } from '../client'

export interface IntelligenceRule {
  name: string
  type: 'contains' | 'regex'
  pattern: string
  enabled: boolean
}
export interface IntelligenceBuiltinRule {
  id: string
  name: string
  description: string
  enabled: boolean
}
export interface IntelligenceBuiltinExtension {
  type: 'contains' | 'regex'
  pattern: string
}
export interface IntelligenceRulesConfig {
  rules: IntelligenceRule[]
  builtin_disabled_ids?: string[]
  builtins?: IntelligenceBuiltinRule[]
  builtin_extensions?: Record<string, IntelligenceBuiltinExtension[]>
}

export async function getRules(): Promise<IntelligenceRulesConfig> {
  return (await apiClient.get<IntelligenceRulesConfig>('/admin/settings/intelligence-rules')).data
}
export async function saveRules(value: IntelligenceRulesConfig): Promise<IntelligenceRulesConfig> {
  return (await apiClient.put<IntelligenceRulesConfig>('/admin/settings/intelligence-rules', value)).data
}
