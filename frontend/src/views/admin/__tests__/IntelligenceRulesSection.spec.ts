import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import IntelligenceRulesSection from '../settings/IntelligenceRulesSection.vue'

const { getRules, saveRules } = vi.hoisted(() => ({ getRules: vi.fn(), saveRules: vi.fn() }))
vi.mock('@/api/admin/intelligenceRules', () => ({ getRules, saveRules }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('IntelligenceRulesSection', () => {
  beforeEach(() => { getRules.mockReset(); saveRules.mockReset() })

  it('lets an administrator explicitly edit an existing custom rule and save it', async () => {
    getRules.mockResolvedValue({ rules: [{ name: '旧规则', type: 'contains', pattern: '旧问题', enabled: true }] })
    saveRules.mockImplementation(async value => value)
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    const row = wrapper.get('[data-testid="intelligence-rule"]')
    expect(row.text()).toContain('旧问题')
    await row.get('[data-testid="rule-edit"]').trigger('click')
    await row.get('[data-testid="rule-name"]').setValue('修改后的规则')
    await row.get('[data-testid="rule-type"]').setValue('regex')
    await row.get('[data-testid="rule-pattern"]').setValue('新.*问题')
    await row.get('[data-testid="rule-edit"]').trigger('click')
    expect(wrapper.text()).toContain('admin.settings.intelligenceRules.unsaved')
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    await flushPromises()
    expect(saveRules).toHaveBeenCalledWith({ builtin_disabled_ids: [], builtin_extensions: {}, rules: [{ name: '修改后的规则', type: 'regex', pattern: '新.*问题', enabled: true }] })
  })

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
    expect(saveRules).toHaveBeenCalledWith({ builtin_disabled_ids: [], builtin_extensions: {}, rules: [
      { name: '现有', type: 'contains', pattern: '测试题', enabled: true },
      { name: '新题', type: 'regex', pattern: '糖果.*抽取', enabled: true },
    ] })
  })

  it('preserves edited rules and displays server validation errors', async () => {
    getRules.mockResolvedValue({ rules: [{ name: '题目', type: 'regex', pattern: '(', enabled: true }] })
    saveRules.mockRejectedValue({ message: 'invalid regular expression' })
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    await wrapper.get('[data-testid="rule-edit"]').trigger('click')
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
    expect(saveRules).toHaveBeenCalledWith({ builtin_disabled_ids: [], builtin_extensions: {}, rules: [{ name: '保留', type: 'contains', pattern: '测试', enabled: false }] })
  })

  it('shows built-in rules and persists their enabled state alongside custom rules', async () => {
    getRules.mockResolvedValue({ rules: [{ name: '自定义', type: 'contains', pattern: 'abc', enabled: true }], builtin_disabled_ids: [], builtins: [{ id: 'builtin.pelican_bicycle', name: '鹈鹕骑自行车', description: '鹈鹕和自行车', enabled: true }] })
    saveRules.mockImplementation(async value => ({ ...value, builtins: [{ id: 'builtin.pelican_bicycle', name: '鹈鹕骑自行车', description: '鹈鹕和自行车', enabled: false }] }))
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    expect(wrapper.text()).toContain('鹈鹕骑自行车')
    await wrapper.get('[data-testid="builtin-rule-enabled"]').setValue(false)
    expect(wrapper.text()).toContain('admin.settings.intelligenceRules.unsaved')
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    await flushPromises()
    expect(saveRules).toHaveBeenCalledWith({ rules: [{ name: '自定义', type: 'contains', pattern: 'abc', enabled: true }], builtin_disabled_ids: ['builtin.pelican_bicycle'], builtin_extensions: {} })
  })

  it('edits, removes and adds conditions on a default rule without losing unknown rule settings', async () => {
    getRules.mockResolvedValue({
      rules: [], builtin_disabled_ids: [],
      builtins: [{ id: 'builtin.pelican_bicycle', name: '鹈鹕骑自行车', description: '内置检测', enabled: true }],
      builtin_extensions: { 'builtin.pelican_bicycle': [{ type: 'contains', pattern: '旧场景' }], 'builtin.future': [{ type: 'contains', pattern: '保留' }] },
    })
    saveRules.mockImplementation(async value => ({ ...value, builtins: [{ id: 'builtin.pelican_bicycle', name: '鹈鹕骑自行车', description: '内置检测', enabled: true }] }))
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    const builtin = wrapper.get('[data-testid="builtin-rule"]')
    await builtin.get('[data-testid="builtin-rule-edit"]').trigger('click')
    await builtin.get('[data-testid="builtin-extension-pattern"]').setValue('修改场景')
    await builtin.get('[data-testid="builtin-extension-add"]').trigger('click')
    const conditions = builtin.findAll('[data-testid="builtin-extension"]')
    await conditions[1].get('[data-testid="builtin-extension-type"]').setValue('regex')
    await conditions[1].get('[data-testid="builtin-extension-pattern"]').setValue('人物.*坦克')
    await conditions[0].get('[data-testid="builtin-extension-remove"]').trigger('click')
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    await flushPromises()
    expect(saveRules).toHaveBeenCalledWith({
      rules: [], builtin_disabled_ids: [],
      builtin_extensions: { 'builtin.pelican_bicycle': [{ type: 'regex', pattern: '人物.*坦克' }], 'builtin.future': [{ type: 'contains', pattern: '保留' }] },
    })
  })

  it('blocks saving an empty added default condition', async () => {
    getRules.mockResolvedValue({ rules: [], builtin_disabled_ids: [], builtins: [{ id: 'builtin.pelican_bicycle', name: '鹈鹕', description: '', enabled: true }] })
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    await wrapper.get('[data-testid="builtin-rule-edit"]').trigger('click')
    await wrapper.get('[data-testid="builtin-extension-add"]').trigger('click')
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    expect(wrapper.text()).toContain('admin.settings.intelligenceRules.extensionRequired')
    expect(saveRules).not.toHaveBeenCalled()
  })

  it('persists deletion of the last added default condition', async () => {
    getRules.mockResolvedValue({ rules: [], builtin_disabled_ids: [], builtins: [{ id: 'builtin.pelican_bicycle', name: '鹈鹕', description: '', enabled: true }], builtin_extensions: { 'builtin.pelican_bicycle': [{ type: 'contains', pattern: '旧条件' }] } })
    saveRules.mockImplementation(async value => ({ ...value, builtins: [] }))
    const wrapper = mount(IntelligenceRulesSection)
    await flushPromises()
    const edit = wrapper.get('[data-testid="builtin-rule-edit"]')
    expect(edit.attributes('aria-label')).toContain('鹈鹕')
    await edit.trigger('click')
    await wrapper.get('[data-testid="builtin-extension-remove"]').trigger('click')
    await wrapper.get('[data-testid="intelligence-save"]').trigger('click')
    await flushPromises()
    expect(saveRules).toHaveBeenCalledWith({ rules: [], builtin_disabled_ids: [], builtin_extensions: {} })
  })
})
