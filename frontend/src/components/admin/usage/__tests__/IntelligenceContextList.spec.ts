import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import IntelligenceContextList from '../IntelligenceContextList.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/admin/intelligenceContext', () => ({ listIntelligenceContexts: mocks.list }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
describe('Intelligence context list', () => {
  beforeEach(() => { mocks.list.mockReset() })
  it('shows a record even if no usage log is linked and opens it', async () => {
    mocks.list.mockResolvedValue({ items: [{ id: 'ctx-8', request_id: 'r8', created_at: '2026-10-30', matched_rules: [{ id: 'candy', name: '糖果题', type: 'builtin' }], usage_log_id: null }], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mount(IntelligenceContextList, { global: { stubs: { Pagination: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('糖果题')
    expect(wrapper.text()).toContain('admin.usage.intelligenceContext.noUsageLog')
    await wrapper.get('[data-testid="open-intelligence-context"]').trigger('click')
    expect(wrapper.emitted('open')).toEqual([['ctx-8']])
  })
})
