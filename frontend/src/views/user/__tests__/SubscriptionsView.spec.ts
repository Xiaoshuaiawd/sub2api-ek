import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import SubscriptionsView from '../SubscriptionsView.vue'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription } from '@/types'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  app: {
    cachedPublicSettings: { payment_enabled: false },
    fetchPublicSettings: vi.fn(),
    showInfo: vi.fn(),
    showError: vi.fn(),
  },
}))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('@/api/subscriptions', () => ({
  default: {
    getMySubscriptions: vi.fn().mockResolvedValue([
      { id: 1, group_id: 7, status: 'active', group: { name: 'Test', platform: 'openai' } },
    ]),
  },
}))
vi.mock('@/api/groups', () => ({ default: { getAvailable: vi.fn().mockResolvedValue([]) } }))

describe('subscription renewal navigation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.app.cachedPublicSettings = { payment_enabled: false }
    mocks.app.fetchPublicSettings.mockResolvedValue(null)
  })

  async function clickRenew() {
    const wrapper = shallowMount(SubscriptionsView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    wrapper.unmount()
  }

  it('stays on subscriptions and explains when payment is disabled', async () => {
    await clickRenew()
    expect(mocks.app.fetchPublicSettings).toHaveBeenCalledWith(true)
    expect(mocks.push).not.toHaveBeenCalled()
    expect(mocks.app.showInfo).toHaveBeenCalledWith('purchase.notEnabledDesc')
  })

  it('uses refreshed settings and opens the matching subscription group', async () => {
    mocks.app.fetchPublicSettings.mockImplementation(async () => {
      mocks.app.cachedPublicSettings = { payment_enabled: true }
    })
    await clickRenew()
    expect(mocks.push).toHaveBeenCalledWith({
      path: '/purchase', query: { tab: 'subscription', group: '7' },
    })
    expect(mocks.app.showInfo).not.toHaveBeenCalled()
  })
})

// 降级 + extend 口径（issue #6）：当期保留旧额度，收窄后的额度下个周期才生效。
// 前端要把它显式写出来，否则用户会以为「我买的套餐没生效」。
describe('pending plan switch hint', () => {
  const baseSub = {
    id: 1,
    user_id: 1,
    group_id: 7,
    plan_id: 3,
    daily_limit_usd: 5,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    daily_usage_usd: 4.5,
    weekly_usage_usd: 0,
    monthly_usage_usd: 0,
    starts_at: '2026-01-01T00:00:00Z',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    expires_at: null,
    status: 'active',
    group: { name: 'Test', platform: 'openai' },
  } as unknown as UserSubscription

  beforeEach(() => {
    vi.clearAllMocks()
    mocks.app.cachedPublicSettings = { payment_enabled: false }
  })

  async function mountWith(sub: UserSubscription) {
    vi.mocked(subscriptionsAPI.getMySubscriptions).mockResolvedValueOnce([sub])
    const wrapper = shallowMount(SubscriptionsView, {
      global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    return wrapper
  }

  it('shows the next-cycle limits while keeping the current cycle on the old limit', async () => {
    const wrapper = await mountWith({
      ...baseSub,
      pending_plan_id: 4,
      pending_plan_name: 'Lite',
      pending_daily_limit_usd: 2,
      pending_weekly_limit_usd: null,
    })
    const hint = wrapper.get('[data-test="pending-plan-switch"]')
    const text = hint.text()
    expect(text).toContain('userSubscriptions.pendingSwitch.title')
    expect(text).toContain('Lite')
    expect(text).toContain('userSubscriptions.daily $2.00')
    // null = 待生效为「不限额」，不能被当成「没有待生效值」而漏掉
    expect(text).toContain('userSubscriptions.weekly userSubscriptions.unlimited')
    // 没有值的窗口不渲染，避免出现「Monthly undefined」
    expect(text).not.toContain('userSubscriptions.monthly')
    // 当期额度不变：日额度仍是旧的 5.00
    expect(wrapper.text()).toContain('$4.50 / $5.00')
    wrapper.unmount()
  })

  it('renders nothing when the subscription has no pending switch', async () => {
    const wrapper = await mountWith({ ...baseSub })
    expect(wrapper.find('[data-test="pending-plan-switch"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('userSubscriptions.pendingSwitch.title')
    wrapper.unmount()
  })
})
