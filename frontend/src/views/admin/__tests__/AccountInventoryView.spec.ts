import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountInventoryView from '../AccountInventoryView.vue'

const { getStatus, saveConfig, importLines, listRows } = vi.hoisted(() => ({
  getStatus: vi.fn().mockResolvedValue({ target_count: 0, enabled: false, available: 12, pending: 3, activating: 0, active: 0, failed: 0, retired: 0, duplicate: 0 }),
  saveConfig: vi.fn().mockResolvedValue({ target_count: 400, enabled: true, available: 12, pending: 3, activating: 0, active: 0, failed: 0, retired: 0, duplicate: 0 }),
  importLines: vi.fn().mockResolvedValue({ total: 1, imported: 1, skipped: 0 }),
  listRows: vi.fn().mockResolvedValue({ items: [{ id: 1, email: 'u***@example.com', state: 'pending', attempts: 0, created_at: '2026-11-09T00:00:00Z' }], total: 1, page: 1, page_size: 50 })
}))
vi.mock('@/api/admin/accountInventory', () => ({ getStatus, saveConfig, importLines, listRows }))
vi.mock('vue-i18n', async () => {
  const original = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...original, useI18n: () => ({ t: (key: string) => key }) }
})

function view() {
  return mount(AccountInventoryView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, 'router-link': { template: '<a><slot /></a>' } } } })
}

describe('OpenAI account inventory', () => {
  it('saves the custom target and imports credential lines without showing secrets', async () => {
    const wrapper = view()
    await flushPromises()
    await wrapper.get('[data-testid="inventory-target"]').setValue('400')
    await wrapper.get('[data-testid="inventory-enabled"]').setValue(true)
    await wrapper.get('[data-testid="inventory-save"]').trigger('click')
    await flushPromises()
    expect(saveConfig).toHaveBeenCalledWith(400, true)

    await wrapper.get('[data-testid="inventory-content"]').setValue('user@example.com----password----MFASECRET')
    await wrapper.get('[data-testid="inventory-import"]').trigger('click')
    await flushPromises()
    expect(importLines).toHaveBeenCalledWith('user@example.com----password----MFASECRET')
    expect(wrapper.text()).not.toContain('password')
    expect(wrapper.text()).not.toContain('MFASECRET')
    expect(wrapper.text()).toContain('u***@example.com')
  })
})
