import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import IntelligenceRulesSection from '../settings/IntelligenceRulesSection.vue'

const { getRules, saveRules } = vi.hoisted(() => ({ getRules: vi.fn(), saveRules: vi.fn() }))
vi.mock('@/api/admin/intelligenceRules', () => ({ getRules, saveRules }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('IntelligenceRulesSection', () => {
  beforeEach(() => { getRules.mockReset(); saveRules.mockReset() })

  it('loads rules, adds and saves a new regex rule', async () => {
    getRules.mockResolvedValue({ rules: [{ name: '现有', type: 'contains', pattern: '测试题', enabled: true }] })
    saveRules.mockImplementation(async value => value)
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    expect(wrapper.findAll('[data-testid="intelligence-rule"]')).toHaveLength(1)
    await wrapper.get('[data-testid="intelligence-add"]').trigger('click')
    const rows = wrapper.findAll('[data-testid="intelligence-rule"]')
    await rows[1].get('[data-testid="rule-name"]').setValue('新题')
    await rows[1].get('[data-testid="rule-type"]').setValue('regex')
    await rows[1].get('[data-testid="rule-pattern"]').setValue('糖果.*抽取')
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    await flushPromises()
    expect(saveRules).toHaveBeenCalledWith({ rules: [
      { name: '现有', type: 'contains', pattern: '测试题', enabled: true },
      { name: '新题', type: 'regex', pattern: '糖果.*抽取', enabled: true },
    ] })
  })

  it('preserves edited rules and displays server validation errors', async () => {
    getRules.mockResolvedValue({ rules: [{ name: '题目', type: 'regex', pattern: '(', enabled: true }] })
    saveRules.mockRejectedValue({ message: 'invalid regular expression' })
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('invalid regular expression')
    expect((wrapper.get('[data-testid="rule-pattern"]').element as HTMLTextAreaElement).value).toBe('(')
  })

  it('does not save an empty replacement after loading fails', async () => {
    getRules.mockRejectedValue({ message: 'cannot load' })
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    expect(wrapper.text()).toContain('cannot load')
    expect(wrapper.find('[data-testid="intelligence-save"]').exists()).toBe(false)
  })

  it('rejects an oversized UTF-8 pattern before saving', async () => {
    getRules.mockResolvedValue({ rules: [{ name: '题目', type: 'contains', pattern: '糖'.repeat(1366), enabled: true }] })
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    expect(wrapper.text()).toContain('admin.settings.intelligenceRules.tooLong')
    expect(saveRules).not.toHaveBeenCalled()
  })

  it('saves disabled rules and deletes removed rules', async () => {
    getRules.mockResolvedValue({ rules: [
      { name: '保留', type: 'contains', pattern: '测试', enabled: true },
      { name: '删除', type: 'regex', pattern: 'candy', enabled: true },
    ] })
    saveRules.mockImplementation(async value => value)
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    const rows = wrapper.findAll('[data-testid="intelligence-rule"]')
    await rows[0].get('[data-testid="rule-enabled"]').setValue(false)
    await rows[1].get('[data-testid="rule-remove"]').trigger('click')
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    await flushPromises()
    expect(saveRules).toHaveBeenCalledWith({ rules: [{ name: '保留', type: 'contains', pattern: '测试', enabled: false }] })
  })
})
