import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UsageTable from '../UsageTable.vue'
import { observerUsageContext } from '../observerUsageContext'

vi.mock('vue-i18n', async importOriginal => ({ ...(await importOriginal<typeof import('vue-i18n')>()), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {} }) }))
vi.mock('@/utils/ipGeoLookup', () => ({ getEntry: vi.fn(), fetchBatch: vi.fn() }))

const data = [{ id: 11, model: 'gpt', intelligence_context_available: true }]
const render = (observer: boolean, rows = data) => mount(UsageTable, {
  props: { data: rows as never, columns: [] },
  global: { provide: { [observerUsageContext as symbol]: observer }, stubs: {
    DataTable: { props: ['data'], template: '<div><slot name="cell-intelligence_context" :row="data[0]" /></div>' },
    Icon: true, EmptyState: true, UsageTimingDialog: true,
  } },
})

describe('intelligence context visibility', () => {
  it('shows a button for matched admin usage and emits its ID', async () => {
    const wrapper = render(false)
    await wrapper.get('[data-testid="view-intelligence-context"]').trigger('click')
    expect(wrapper.emitted('intelligenceContext')).toEqual([[11]])
  })
  it('does not show a button for ordinary usage or observers', () => {
    expect(render(false, [{ id: 12, model: 'gpt', intelligence_context_available: false }]).find('[data-testid="view-intelligence-context"]').exists()).toBe(false)
    expect(render(true).find('[data-testid="view-intelligence-context"]').exists()).toBe(false)
  })
})
