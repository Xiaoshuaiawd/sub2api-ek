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
  condition?: IntelligenceCondition
  default_condition?: IntelligenceCondition
}
export interface IntelligenceCondition {
  op: 'all' | 'any' | 'not' | 'contains_any' | 'regex'
  children?: IntelligenceCondition[]
  values?: string[]
  pattern?: string
  text?: 'plain' | 'qin' | 'visual'
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
  builtin_conditions?: Record<string, IntelligenceCondition>
}

export async function getRules(): Promise<IntelligenceRulesConfig> {
  return (await apiClient.get<IntelligenceRulesConfig>('/admin/settings/intelligence-rules')).data
}
export async function saveRules(value: IntelligenceRulesConfig): Promise<IntelligenceRulesConfig> {
  return (await apiClient.put<IntelligenceRulesConfig>('/admin/settings/intelligence-rules', value)).data
}
