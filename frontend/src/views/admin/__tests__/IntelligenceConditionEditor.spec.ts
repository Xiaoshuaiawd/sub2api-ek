import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import IntelligenceConditionEditor from '../settings/IntelligenceConditionEditor.vue'
import type { IntelligenceCondition } from '@/api/admin/intelligenceRules'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('IntelligenceConditionEditor', () => {
  it('shows logic and original conditions and updates a keyword without changing regex or structure', async () => {
    const node: IntelligenceCondition = { op: 'all', children: [
      { op: 'contains_any', text: 'plain', values: ['鹈鹕', 'pelican'] },
      { op: 'not', children: [{ op: 'regex', text: 'visual', pattern: 'spinner' }] },
    ] }
    const wrapper = mount(IntelligenceConditionEditor, { props: { modelValue: node } })
    expect(wrapper.text()).toContain('admin.settings.intelligenceRules.conditionOp.all')
    expect(wrapper.text()).toContain('admin.settings.intelligenceRules.conditionOp.not')
    expect((wrapper.get('[data-testid="condition-values"]').element as HTMLTextAreaElement).value).toBe('鹈鹕\npelican')
    expect((wrapper.get('[data-testid="condition-pattern"]').element as HTMLTextAreaElement).value).toBe('spinner')
    expect(wrapper.text()).toContain('admin.settings.intelligenceRules.conditionRegexHint')
    await wrapper.get('[data-testid="condition-values"]').setValue('企鹅\npelican')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual({
      ...node, children: [{ ...node.children![0], values: ['企鹅', 'pelican'] }, node.children![1]],
    })
    expect(node.children![0].values).toEqual(['鹈鹕', 'pelican'])
  })

  it('can change group logic and add or delete conditions without mutating the source', async () => {
    const node: IntelligenceCondition = { op: 'all', children: [{ op: 'contains_any', text: 'plain', values: ['foo'] }] }
    const wrapper = mount(IntelligenceConditionEditor, { props: { modelValue: node } })
    await wrapper.get('[data-testid="condition-operator"]').setValue('any')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual({ ...node, op: 'any' })
    await wrapper.get('[data-testid="condition-add-regex"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[1]?.[0]).toEqual({ ...node, children: [...node.children!, { op: 'regex', text: 'plain', pattern: '' }] })
    await wrapper.get('[data-testid="condition-remove"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[2]?.[0]).toEqual({ ...node, children: [] })
    expect(node.children).toHaveLength(1)
  })

  it('splits pasted CRLF lines without removing meaningful spaces', async () => {
    const node: IntelligenceCondition = { op: 'contains_any', text: 'plain', values: [' to '] }
    const wrapper = mount(IntelligenceConditionEditor, { props: { modelValue: node } })
    await wrapper.get('[data-testid="condition-values"]').setValue(' to \r\nrange')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual({ ...node, values: [' to ', 'range'] })
  })
})
