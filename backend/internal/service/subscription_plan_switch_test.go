package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func newPlanSwitchService(t *testing.T, subRepo *subscriptionUserSubRepoStub, groupIDs ...int64) *SubscriptionService {
	t.Helper()
	groups := make(map[int64]*Group, len(groupIDs))
	for _, id := range groupIDs {
		groups[id] = &Group{ID: id, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}
	}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{groups: groups}, subRepo, nil, nil, nil)
	t.Cleanup(svc.Stop)
	return svc
}

func seededExhaustedPlan1(subID int64) *UserSubscription {
	now := time.Now()
	plan1, daily1 := int64(1), 5.0
	return &UserSubscription{
		ID:              subID,
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          &plan1,
		DailyLimitUSD:   &daily1,
		WeeklyLimitUSD:  &daily1,
		MonthlyLimitUSD: &daily1,
		DailyUsageUSD:   5,
		WeeklyUsageUSD:  5,
		MonthlyUsageUSD: 5,
		StartsAt:        now.Add(-10 * 24 * time.Hour),
		ExpiresAt:       now.Add(20 * 24 * time.Hour),
		Status:          SubscriptionStatusActive,
	}
}

func purchasedPlan2() (*int64, *float64, *float64, *float64) {
	plan2, daily2, weekly2, monthly2 := int64(2), 20.0, 60.0, 120.0
	return &plan2, &daily2, &weekly2, &monthly2
}

// purchasedDowngradePlan3 是比套餐1（日 5 / 周 60 / 月 120）三个窗口都紧的套餐：
// 日 2 / 周 10 / 月 40。
func purchasedDowngradePlan3() (*int64, *float64, *float64, *float64) {
	plan3, daily3, weekly3, monthly3 := int64(3), 2.0, 10.0, 40.0
	return &plan3, &daily3, &weekly3, &monthly3
}

// seededRunningPlan1Sub 构造仍在运行的套餐1 订阅：
// 日 5/周 60/月 120，今日已用 3、本周已用 40、本月已用 90。
// 日、周、月在购买时都还没到下一个重置点。
func seededRunningPlan1Sub(now time.Time) *UserSubscription {
	plan1 := int64(1)
	daily, weekly, monthly := 5.0, 60.0, 120.0
	today := timezone.StartOfDay(now)
	weeklyStart := now.Add(-6 * 24 * time.Hour)
	tenDaysAgo := now.AddDate(0, 0, -10)
	return &UserSubscription{
		ID:                 1,
		UserID:             42,
		GroupID:            7,
		GroupIDs:           []int64{7},
		PlanID:             &plan1,
		DailyLimitUSD:      &daily,
		WeeklyLimitUSD:     &weekly,
		MonthlyLimitUSD:    &monthly,
		StartsAt:           tenDaysAgo,
		ExpiresAt:          now.AddDate(0, 0, 20),
		Status:             SubscriptionStatusActive,
		DailyWindowStart:   &today,
		WeeklyWindowStart:  &weeklyStart,
		MonthlyWindowStart: &tenDaysAgo,
		DailyUsageUSD:      3,
		WeeklyUsageUSD:     40,
		MonthlyUsageUSD:    90,
	}
}

// 平台1 额度耗尽的用户购买套餐2（同一分组，重开周期）后，订阅必须变成套餐2 的额度。
func TestPaidPlanSwitchSameGroupAppliesPurchasedPlan(t *testing.T) {
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(seededExhaustedPlan1(1))
	svc := newPlanSwitchService(t, subRepo, 7)
	plan2, daily2, weekly2, monthly2 := purchasedPlan2()
	before := time.Now()

	sub, renewed, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          plan2,
		DailyLimitUSD:   daily2,
		WeeklyLimitUSD:  weekly2,
		MonthlyLimitUSD: monthly2,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeRestart, false)
	require.NoError(t, err)
	require.True(t, renewed)
	require.Equal(t, 0, subRepo.createCalls, "同分组换套餐不应新增订阅")
	require.Equal(t, 1, len(subRepo.byID))
	require.Equal(t, *plan2, *sub.PlanID)
	require.Equal(t, *daily2, *sub.DailyLimitUSD)
	require.Equal(t, *weekly2, *sub.WeeklyLimitUSD)
	require.Equal(t, *monthly2, *sub.MonthlyLimitUSD)
	// restart：周期重算、用量清零
	require.InDelta(t, 0.0, sub.DailyUsageUSD, 1e-9)
	require.WithinDuration(t, before.AddDate(0, 0, 30), sub.ExpiresAt, time.Minute)
}

// 延长周期模式下换套餐：时长叠加、已用用量保留，但额度必须是套餐2 的。
func TestPaidPlanSwitchExtendKeepsUsageAndAppliesPurchasedPlan(t *testing.T) {
	subRepo := newSubscriptionUserSubRepoStub()
	seeded := seededExhaustedPlan1(1)
	originalExpiresAt := seeded.ExpiresAt
	subRepo.seed(seeded)
	svc := newPlanSwitchService(t, subRepo, 7)
	plan2, daily2, weekly2, monthly2 := purchasedPlan2()

	sub, renewed, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          plan2,
		DailyLimitUSD:   daily2,
		WeeklyLimitUSD:  weekly2,
		MonthlyLimitUSD: monthly2,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeExtend, false)
	require.NoError(t, err)
	require.True(t, renewed)
	require.Equal(t, *plan2, *sub.PlanID)
	require.Equal(t, *daily2, *sub.DailyLimitUSD)
	require.InDelta(t, 5.0, sub.DailyUsageUSD, 1e-9, "extend 不应重置已用用量")
	require.WithinDuration(t, originalExpiresAt.AddDate(0, 0, 30), sub.ExpiresAt, time.Minute)
}

// 套餐2 主分组不同但分组集合与现有订阅重叠时，合并进现有订阅，不产生第二条订阅。
func TestPaidPlanSwitchOverlappingGroupsMergesIntoExistingSubscription(t *testing.T) {
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(seededExhaustedPlan1(1))
	svc := newPlanSwitchService(t, subRepo, 7, 9)
	plan2, daily2, weekly2, monthly2 := purchasedPlan2()

	sub, renewed, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         9,
		GroupIDs:        []int64{9, 7},
		PlanID:          plan2,
		DailyLimitUSD:   daily2,
		WeeklyLimitUSD:  weekly2,
		MonthlyLimitUSD: monthly2,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeRestart, false)
	require.NoError(t, err)
	require.True(t, renewed)
	require.Equal(t, 0, subRepo.createCalls, "分组重叠时必须合并，不能出现两条订阅")
	require.Equal(t, 1, len(subRepo.byID))
	require.Equal(t, int64(1), sub.ID)
	require.Equal(t, int64(7), sub.GroupID, "主分组保持不变")
	require.ElementsMatch(t, []int64{7, 9}, sub.AccessibleGroupIDs())
	require.Equal(t, *plan2, *sub.PlanID)
	require.Equal(t, *daily2, *sub.DailyLimitUSD)
}

// 没有套餐快照的订单（历史订单 / 仅含时长的续费）必须保持原有行为。
func TestPaidRenewalWithoutPlanSnapshotKeepsExistingPlan(t *testing.T) {
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(seededExhaustedPlan1(1))
	svc := newPlanSwitchService(t, subRepo, 7)

	sub, renewed, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       42,
		GroupID:      7,
		GroupIDs:     []int64{7},
		ValidityDays: 30,
		Notes:        "payment order note",
	}, SubscriptionRenewalModeExtend, false)
	require.NoError(t, err)
	require.True(t, renewed)
	require.Equal(t, int64(1), *sub.PlanID)
	require.Equal(t, 5.0, *sub.DailyLimitUSD)
	require.InDelta(t, 5.0, sub.DailyUsageUSD, 1e-9)
}

func TestPaidRenewalWithoutSnapshotPreservesLimitsWhenRestarting(t *testing.T) {
	for _, mode := range []SubscriptionRenewalMode{SubscriptionRenewalModeRestart, SubscriptionRenewalModeExtend} {
		t.Run(string(mode), func(t *testing.T) {
			repo := newSubscriptionUserSubRepoStub()
			old := seededExhaustedPlan1(1)
			if mode == SubscriptionRenewalModeExtend {
				old.ExpiresAt = time.Now().Add(-time.Hour)
			}
			repo.seed(old)
			svc := newPlanSwitchService(t, repo, 7)
			got, _, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
				UserID: 42, GroupID: 7, ValidityDays: 30,
			}, mode, true)
			require.NoError(t, err)
			require.Equal(t, old.PlanID, got.PlanID)
			require.Equal(t, old.DailyLimitUSD, got.DailyLimitUSD)
			require.Equal(t, old.WeeklyLimitUSD, got.WeeklyLimitUSD)
			require.Equal(t, old.MonthlyLimitUSD, got.MonthlyLimitUSD)
			require.Zero(t, got.DailyUsageUSD)
		})
	}
}

func TestPaidPlanSwitchUsesNewestBillingSubscriptionAcrossPurchasedGroups(t *testing.T) {
	repo := newSubscriptionUserSubRepoStub()
	old := seededExhaustedPlan1(1)
	repo.seed(old)
	newer := seededExhaustedPlan1(2)
	newer.GroupID, newer.GroupIDs = 9, []int64{9}
	newer.StartsAt = old.StartsAt.Add(time.Hour)
	newer.ExpiresAt = old.ExpiresAt.Add(-time.Hour)
	repo.seed(newer)
	svc := newPlanSwitchService(t, repo, 7, 9)
	plan, daily, weekly, monthly := purchasedPlan2()
	got, _, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID: 42, GroupID: 7, GroupIDs: []int64{7, 9}, PlanID: plan,
		DailyLimitUSD: daily, WeeklyLimitUSD: weekly, MonthlyLimitUSD: monthly, ValidityDays: 30,
	}, SubscriptionRenewalModeExtend, true)
	require.NoError(t, err)
	require.Equal(t, newer.ID, got.ID)
	require.ElementsMatch(t, []int64{7, 9}, got.AccessibleGroupIDs())
	require.Equal(t, daily, got.DailyLimitUSD)
}

// 重叠订阅查询失败时必须报错，不能静默当成「没有订阅」去新建第二条。
type planSwitchFailingListRepo struct {
	*subscriptionUserSubRepoStub
}

func (r *planSwitchFailingListRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, errPlanSwitchListFailed
}

var errPlanSwitchListFailed = errors.New("list active subscriptions failed")

func TestPaidPlanSwitchFailsWhenOverlapLookupFails(t *testing.T) {
	base := newSubscriptionUserSubRepoStub()
	base.seed(seededExhaustedPlan1(1))
	repo := &planSwitchFailingListRepo{subscriptionUserSubRepoStub: base}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{groups: map[int64]*Group{
		7: {ID: 7, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		9: {ID: 9, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
	}}, repo, nil, nil, nil)
	t.Cleanup(svc.Stop)
	plan2, daily2, weekly2, monthly2 := purchasedPlan2()

	_, _, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         9,
		GroupIDs:        []int64{9, 7},
		PlanID:          plan2,
		DailyLimitUSD:   daily2,
		WeeklyLimitUSD:  weekly2,
		MonthlyLimitUSD: monthly2,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeRestart, false)
	require.ErrorIs(t, err, errPlanSwitchListFailed)
	require.Equal(t, 0, base.createCalls, "查询失败时不得新建订阅")
}

// 已过期订阅换套餐时不能顺带保留旧套餐的其它分组（不白送未购买分组的访问权）。
func TestPaidPlanSwitchDoesNotKeepGroupsOfExpiredTerm(t *testing.T) {
	subRepo := newSubscriptionUserSubRepoStub()
	now := time.Now()
	plan1, daily1 := int64(1), 5.0
	subRepo.seed(&UserSubscription{
		ID:            1,
		UserID:        42,
		GroupID:       7,
		GroupIDs:      []int64{7, 8},
		PlanID:        &plan1,
		DailyLimitUSD: &daily1,
		StartsAt:      now.Add(-40 * 24 * time.Hour),
		ExpiresAt:     now.Add(-10 * 24 * time.Hour),
		Status:        SubscriptionStatusExpired,
	})
	svc := newPlanSwitchService(t, subRepo, 7, 9)
	plan2, daily2, weekly2, monthly2 := purchasedPlan2()

	sub, renewed, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7, 9},
		PlanID:          plan2,
		DailyLimitUSD:   daily2,
		WeeklyLimitUSD:  weekly2,
		MonthlyLimitUSD: monthly2,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeExtend, false)
	require.NoError(t, err)
	require.True(t, renewed)
	require.Equal(t, 0, subRepo.createCalls)
	require.ElementsMatch(t, []int64{7, 9}, sub.AccessibleGroupIDs(), "过期周期不得保留未购买的 8")
	require.NotContains(t, sub.AccessibleGroupIDs(), int64(8))
	require.Equal(t, *plan2, *sub.PlanID)
}

// #6 降级 + extend：已用量超过新套餐额度时，当前窗口仍按旧额度可用。
// 断言的是具体数值，不是「有没有报错」。
func TestPaidPlanSwitchExtendDowngradeKeepsCurrentWindowQuota(t *testing.T) {
	now := time.Now()
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(seededRunningPlan1Sub(now))
	svc := newPlanSwitchService(t, subRepo, 7)
	plan3, daily3, weekly3, monthly3 := purchasedDowngradePlan3()

	sub, renewed, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          plan3,
		DailyLimitUSD:   daily3,
		WeeklyLimitUSD:  weekly3,
		MonthlyLimitUSD: monthly3,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeExtend, false)
	require.NoError(t, err)
	require.True(t, renewed)
	require.Equal(t, 0, subRepo.createCalls, "同分组降级不应新增订阅")

	// 套餐身份当场切换成买的那一个。
	require.Equal(t, int64(3), *sub.PlanID)
	// 当前窗口保留套餐1 的旧额度，新额度只挂在 pending 上等窗口滚动。
	require.InDelta(t, 5.0, *sub.DailyLimitUSD, 1e-9, "当窗日额度应保留旧值 5")
	require.InDelta(t, 60.0, *sub.WeeklyLimitUSD, 1e-9, "当窗周额度应保留旧值 60")
	require.InDelta(t, 120.0, *sub.MonthlyLimitUSD, 1e-9, "当月额度应保留旧值 120")
	require.InDelta(t, 2.0, *sub.PendingDailyLimitUSD, 1e-9)
	require.InDelta(t, 10.0, *sub.PendingWeeklyLimitUSD, 1e-9)
	require.InDelta(t, 40.0, *sub.PendingMonthlyLimitUSD, 1e-9)
	require.Equal(t, int64(3), *sub.PendingPlanID)

	// 已用量原样保留（extend 只叠时长）。
	require.InDelta(t, 3.0, sub.DailyUsageUSD, 1e-9)
	require.InDelta(t, 40.0, sub.WeeklyUsageUSD, 1e-9)
	require.InDelta(t, 90.0, sub.MonthlyUsageUSD, 1e-9)

	require.InDelta(t, 2.0, *sub.DailyLimitUSD-sub.DailyUsageUSD, 1e-9)
	require.InDelta(t, 20.0, *sub.WeeklyLimitUSD-sub.WeeklyUsageUSD, 1e-9)
	require.InDelta(t, 30.0, *sub.MonthlyLimitUSD-sub.MonthlyUsageUSD, 1e-9)
	require.WithinDuration(t, now.AddDate(0, 0, 20).AddDate(0, 0, 30), sub.ExpiresAt, time.Minute)
}

// #6 下个周期按新额度：日窗口滚动后日额度变 2，周窗口滚动后周额度变 10，
// 月窗口还没滚动则继续用 120，直到它自己滚动才变 40。
func TestPaidPlanSwitchDowngradeAppliesNewLimitsOnNextWindow(t *testing.T) {
	now := time.Now()
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(seededRunningPlan1Sub(now))
	svc := newPlanSwitchService(t, subRepo, 7)
	svc.now = func() time.Time { return now }
	plan3, daily3, weekly3, monthly3 := purchasedDowngradePlan3()

	sub, _, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          plan3,
		DailyLimitUSD:   daily3,
		WeeklyLimitUSD:  weekly3,
		MonthlyLimitUSD: monthly3,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeExtend, false)
	require.NoError(t, err)

	svc.now = func() time.Time { return now.Add(25 * time.Hour) }
	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))

	// 日窗口滚到今天 0 点：额度 5 -> 2，用量清零，pending 消费掉。
	require.InDelta(t, 2.0, *sub.DailyLimitUSD, 1e-9)
	require.Nil(t, sub.PendingDailyLimitUSD)
	require.InDelta(t, 0.0, sub.DailyUsageUSD, 1e-9)
	require.True(t, sub.CheckDailyLimit(nil, 2), "新日额度 2 应可再花满")
	require.False(t, sub.CheckDailyLimit(nil, 2.01), "超过新日额度 2 必须拒绝")

	// 周窗口滚到下一个锚点：额度 60 -> 10。
	require.InDelta(t, 10.0, *sub.WeeklyLimitUSD, 1e-9)
	require.Nil(t, sub.PendingWeeklyLimitUSD)
	require.InDelta(t, 0.0, sub.WeeklyUsageUSD, 1e-9)

	// 月窗口没滚：还是旧额度 120、旧用量 90，pending 留着等它自己滚。
	require.InDelta(t, 120.0, *sub.MonthlyLimitUSD, 1e-9)
	require.InDelta(t, 40.0, *sub.PendingMonthlyLimitUSD, 1e-9)
	require.InDelta(t, 90.0, sub.MonthlyUsageUSD, 1e-9)
	require.Equal(t, int64(3), *sub.PendingPlanID, "还有窗口待生效，pending 套餐 id 不能清")

	// 21 天后月窗口滚动：额度变 40，pending 全部清空。
	later := now.AddDate(0, 0, 21)
	svc.now = func() time.Time { return later }
	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	require.InDelta(t, 40.0, *sub.MonthlyLimitUSD, 1e-9)
	require.Nil(t, sub.PendingMonthlyLimitUSD)
	require.Nil(t, sub.PendingPlanID)
	require.False(t, sub.HasPendingPlanLimit())
	require.InDelta(t, 0.0, sub.MonthlyUsageUSD, 1e-9)
	require.Equal(t, []SubscriptionQuotaWindow{
		SubscriptionQuotaWindowDaily,
		SubscriptionQuotaWindowWeekly,
		SubscriptionQuotaWindowMonthly,
	}, subRepo.appliedPendingWindows, "每个窗口只落一次")
}

// 升级（额度放宽）在 extend 下立即生效，并清掉上一次降级遗留的待生效值。
func TestPaidPlanSwitchExtendUpgradeAppliesImmediatelyAndClearsPending(t *testing.T) {
	now := time.Now()
	subRepo := newSubscriptionUserSubRepoStub()
	seeded := seededRunningPlan1Sub(now)
	stalePlan, staleDaily, staleWeekly, staleMonthly := int64(3), 1.0, 2.0, 3.0
	seeded.PendingPlanID = &stalePlan
	seeded.PendingDailyLimitUSD = &staleDaily
	seeded.PendingWeeklyLimitUSD = &staleWeekly
	seeded.PendingMonthlyLimitUSD = &staleMonthly
	subRepo.seed(seeded)
	svc := newPlanSwitchService(t, subRepo, 7)
	plan2, daily2, weekly2, monthly2 := purchasedPlan2()

	sub, _, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          plan2,
		DailyLimitUSD:   daily2,
		WeeklyLimitUSD:  weekly2,
		MonthlyLimitUSD: monthly2,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeExtend, false)
	require.NoError(t, err)
	require.InDelta(t, 20.0, *sub.DailyLimitUSD, 1e-9)
	require.InDelta(t, 60.0, *sub.WeeklyLimitUSD, 1e-9)
	require.InDelta(t, 120.0, *sub.MonthlyLimitUSD, 1e-9)
	require.Nil(t, sub.PendingDailyLimitUSD)
	require.Nil(t, sub.PendingWeeklyLimitUSD)
	require.Nil(t, sub.PendingMonthlyLimitUSD)
	require.Nil(t, sub.PendingPlanID)
}

// restart 会重开窗口、清零用量，降级当场生效即可，不需要延后。
func TestPaidPlanSwitchRestartDowngradeAppliesImmediately(t *testing.T) {
	now := time.Now()
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(seededRunningPlan1Sub(now))
	svc := newPlanSwitchService(t, subRepo, 7)
	plan3, daily3, weekly3, monthly3 := purchasedDowngradePlan3()

	sub, _, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          plan3,
		DailyLimitUSD:   daily3,
		WeeklyLimitUSD:  weekly3,
		MonthlyLimitUSD: monthly3,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeRestart, false)
	require.NoError(t, err)
	require.InDelta(t, 2.0, *sub.DailyLimitUSD, 1e-9)
	require.InDelta(t, 10.0, *sub.WeeklyLimitUSD, 1e-9)
	require.InDelta(t, 40.0, *sub.MonthlyLimitUSD, 1e-9)
	require.Nil(t, sub.PendingDailyLimitUSD)
	require.Nil(t, sub.PendingWeeklyLimitUSD)
	require.Nil(t, sub.PendingMonthlyLimitUSD)
	require.InDelta(t, 0.0, sub.DailyUsageUSD, 1e-9, "restart 清空用量，降级不会把用户锁在窗外")
}

// 期限已过期时 extend 会整体重开期限，视为新周期，降级额度和用量一起重算。
func TestPaidPlanSwitchExtendDowngradeOnExpiredTermAppliesImmediately(t *testing.T) {
	now := time.Now()
	plan1, daily1, weekly1, monthly1 := int64(1), 5.0, 60.0, 120.0
	expiredWindow := now.AddDate(0, 0, -40)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 1,
		UserID:             42,
		GroupID:            7,
		GroupIDs:           []int64{7},
		PlanID:             &plan1,
		DailyLimitUSD:      &daily1,
		WeeklyLimitUSD:     &weekly1,
		MonthlyLimitUSD:    &monthly1,
		StartsAt:           now.AddDate(0, 0, -40),
		ExpiresAt:          now.AddDate(0, 0, -10),
		Status:             SubscriptionStatusExpired,
		DailyWindowStart:   &expiredWindow,
		WeeklyWindowStart:  &expiredWindow,
		MonthlyWindowStart: &expiredWindow,
		DailyUsageUSD:      5,
		WeeklyUsageUSD:     40,
		MonthlyUsageUSD:    90,
	})
	svc := newPlanSwitchService(t, subRepo, 7)
	plan3, daily3, weekly3, monthly3 := purchasedDowngradePlan3()

	sub, _, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          plan3,
		DailyLimitUSD:   daily3,
		WeeklyLimitUSD:  weekly3,
		MonthlyLimitUSD: monthly3,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeExtend, false)
	require.NoError(t, err)
	require.InDelta(t, 2.0, *sub.DailyLimitUSD, 1e-9)
	require.Nil(t, sub.PendingDailyLimitUSD)
	require.Nil(t, sub.PendingPlanID)
	require.InDelta(t, 0.0, sub.DailyUsageUSD, 1e-9)
}

// 没有套餐快照的按分组订阅（管理员直开）也要保住当窗分组额度，不能被降级立刻收紧。
func TestPaidPlanSwitchExtendDowngradeKeepsGroupQuotaForGroupSubscription(t *testing.T) {
	now := time.Now()
	groupDaily, groupWeekly, groupMonthly := 5.0, 60.0, 120.0
	groupRepo := &subscriptionGroupRepoStub{groups: map[int64]*Group{
		7: {
			ID: 7, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription,
			DailyLimitUSD: &groupDaily, WeeklyLimitUSD: &groupWeekly, MonthlyLimitUSD: &groupMonthly,
		},
	}}
	start := now.AddDate(0, 0, -10)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:              1,
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		StartsAt:        start,
		ExpiresAt:       now.AddDate(0, 0, 20),
		Status:          SubscriptionStatusActive,
		DailyUsageUSD:   5,
		WeeklyUsageUSD:  40,
		MonthlyUsageUSD: 90,
	})
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	t.Cleanup(svc.Stop)
	plan3, daily3, weekly3, monthly3 := purchasedDowngradePlan3()

	sub, _, err := svc.assignOrRenewPaidSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:          42,
		GroupID:         7,
		GroupIDs:        []int64{7},
		PlanID:          plan3,
		DailyLimitUSD:   daily3,
		WeeklyLimitUSD:  weekly3,
		MonthlyLimitUSD: monthly3,
		ValidityDays:    30,
		Notes:           "payment order note",
	}, SubscriptionRenewalModeExtend, false)
	require.NoError(t, err)
	require.InDelta(t, 5.0, *sub.DailyLimitUSD, 1e-9, "当窗沿用分组额度 5")
	require.InDelta(t, 60.0, *sub.WeeklyLimitUSD, 1e-9)
	require.InDelta(t, 120.0, *sub.MonthlyLimitUSD, 1e-9)
	require.InDelta(t, 2.0, *sub.PendingDailyLimitUSD, 1e-9)
	require.True(t, sub.CheckDailyLimit(nil, 0), "分组订阅降级后当窗仍可用")
}
