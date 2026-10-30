import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import IntelligenceContextDialog from '../IntelligenceContextDialog.vue'
import zhResources from '@/i18n/locales/zh/admin/resources'
import enResources from '@/i18n/locales/en/admin/resources'

const mocks = vi.hoisted(() => ({ get: vi.fn(), getById: vi.fn() }))
vi.mock('@/api/admin/intelligenceContext', () => ({ getIntelligenceContext: mocks.get, getIntelligenceContextById: mocks.getById }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const options = { global: { stubs: { BaseDialog: { template: '<section><slot /></section>' } } } }
describe('Intelligence context dialog', () => {
  beforeEach(() => { mocks.get.mockReset(); mocks.getById.mockReset() })
  it('explains that only the first match in each contains-any group is shown', () => {
    expect(zhResources.usage.intelligenceContext.keywordsHint).toContain('每个“包含任一”词组只展示第一个命中词')
    expect(enResources.usage.intelligenceContext.keywordsHint).toContain('Only the first match in each “contains any” group is shown')
  })
  it('loads a matched request only when opened and displays raw body as text', async () => {
    mocks.get.mockResolvedValue({ request_id: 'r42', content_type: 'application/json', content_encoding: 'gzip', request_body: '<img src=x onerror=alert(1)>', matched_rules: [{ id: 'candy', name: '糖果题', type: 'regex', pattern: '<svg onload=alert(1)>' }] })
    const wrapper = mount(IntelligenceContextDialog, { props: { usageId: null }, ...options })
    expect(mocks.get).not.toHaveBeenCalled()
    await wrapper.setProps({ usageId: 42 })
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith(42, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('糖果题')
    expect(wrapper.text()).toContain('<svg onload=alert(1)>')
    expect(wrapper.text()).toContain('gzip')
    expect(wrapper.text()).toContain('admin.usage.intelligenceContext.encodingHint')
    expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('svg').exists()).toBe(false)
  })
  it('reports missing context without showing a previous request body', async () => {
    mocks.get.mockRejectedValue({ status: 404 })
    const wrapper = mount(IntelligenceContextDialog, { props: { usageId: 42 }, ...options })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.intelligenceContext.notFound')
    expect(wrapper.find('[data-testid="intelligence-context-body"]').exists()).toBe(false)
  })
  it('shows actual matched keywords separately from the rule expression, as safe text', async () => {
    mocks.get.mockResolvedValue({ request_id: 'r', content_type: 'application/json', request_body: '{}', matched_rules: [{ id: 'bird', name: '鹈鹕题', type: 'regex', pattern: '鹈鹕.*自行车', matched_keywords: ['鹈鹕', '<svg onload=alert(1)>'] }] })
    const wrapper = mount(IntelligenceContextDialog, { props: { usageId: 42 }, ...options })
    await flushPromises()
    expect(wrapper.get('[data-testid="matched-keywords"]').text()).toContain('鹈鹕')
    expect(wrapper.get('[data-testid="matched-keywords"]').text()).toContain('<svg onload=alert(1)>')
    expect(wrapper.get('[data-testid="matched-keywords"]').text()).toContain('admin.usage.intelligenceContext.keywordsHint')
    expect(wrapper.text()).toContain('admin.usage.intelligenceContext.rulePattern')
    expect(wrapper.find('svg').exists()).toBe(false)
    expect(wrapper.find('[data-testid="matched-keywords-unavailable"]').exists()).toBe(false)
  })
  it('does not infer keywords for a historical record lacking the snapshot', async () => {
    mocks.get.mockResolvedValue({ request_id: 'old', content_type: 'application/json', request_body: '糖果测试', matched_rules: [{ id: 'old', name: '旧规则', type: 'contains', pattern: '糖果' }] })
    const wrapper = mount(IntelligenceContextDialog, { props: { usageId: 42 }, ...options })
    await flushPromises()
    expect(wrapper.get('[data-testid="matched-keywords-unavailable"]').text()).toContain('admin.usage.intelligenceContext.keywordsUnavailable')
    expect(wrapper.find('[data-testid="matched-keywords"]').exists()).toBe(false)
  })
  it('opens unlinked requests by context UUID', async () => {
    mocks.getById.mockResolvedValue({ id: 'ctx-123', request_id: null, content_type: 'application/json', request_body: '{}', matched_rules: [] })
    mount(IntelligenceContextDialog, { props: { contextId: 'ctx-123' }, ...options })
    await flushPromises()
    expect(mocks.getById).toHaveBeenCalledWith('ctx-123', expect.any(AbortSignal))
    expect(mocks.get).not.toHaveBeenCalled()
  })
  it('allows binary request bytes to be downloaded when no UTF-8 body is available', async () => {
    mocks.get.mockResolvedValue({ id: 'ctx-2', request_id: 'r', content_type: 'multipart/form-data', request_body: null, request_body_base64: 'AP8=', matched_rules: [] })
    const create = vi.fn(() => 'blob:test')
    const revoke = vi.fn()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const OriginalURL = globalThis.URL
    vi.stubGlobal('URL', class extends OriginalURL { static createObjectURL = create; static revokeObjectURL = revoke })
    try {
      const wrapper = mount(IntelligenceContextDialog, { props: { usageId: 42 }, ...options })
      await flushPromises()
      expect(wrapper.text()).toContain('admin.usage.intelligenceContext.binaryBody')
      vi.useFakeTimers()
      await wrapper.get('[data-testid="download-intelligence-context"]').trigger('click')
      expect(create).toHaveBeenCalledWith(expect.any(Blob))
      expect(click).toHaveBeenCalledOnce()
      vi.advanceTimersByTime(1000)
      expect(revoke).toHaveBeenCalledWith('blob:test')
    } finally { vi.useRealTimers(); click.mockRestore(); vi.unstubAllGlobals() }
  })
})
