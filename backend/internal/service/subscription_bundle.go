package service

import (
	"fmt"
)

// normalizePlanGroupIDs accepts the old single-group request and the new
// ordered group list. The first ID is the legacy primary group ID.
func normalizePlanGroupIDs(groupID int64, ids []int64) ([]int64, error) {
	if ids == nil && groupID > 0 {
		ids = []int64{groupID}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one subscription group is required")
	}
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("subscription group IDs must be positive")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *UserSubscription) CanUseGroup(groupID int64) bool {
	if s == nil || groupID <= 0 {
		return false
	}
	if s.GroupID == groupID {
		return true
	}
	for _, id := range s.GroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

func (s *UserSubscription) AccessibleGroupIDs() []int64 {
	if s == nil {
		return nil
	}
	for _, id := range s.GroupIDs {
		if id == s.GroupID {
			return s.GroupIDs
		}
	}
	if s.GroupID <= 0 {
		return s.GroupIDs
	}
	return append([]int64{s.GroupID}, s.GroupIDs...)
}

func (s *UserSubscription) DailyLimit(group *Group) *float64 {
	if s != nil && s.PlanID != nil {
		return s.DailyLimitUSD
	}
	if group != nil {
		return group.DailyLimitUSD
	}
	return nil
}

func (s *UserSubscription) WeeklyLimit(group *Group) *float64 {
	if s != nil && s.PlanID != nil {
		return s.WeeklyLimitUSD
	}
	if group != nil {
		return group.WeeklyLimitUSD
	}
	return nil
}

func (s *UserSubscription) MonthlyLimit(group *Group) *float64 {
	if s != nil && s.PlanID != nil {
		return s.MonthlyLimitUSD
	}
	if group != nil {
		return group.MonthlyLimitUSD
	}
	return nil
}

func applyPurchasedBundle(sub, previous *UserSubscription, input *AssignSubscriptionInput, previousActive bool) {
	if sub == nil || input == nil || input.PlanID == nil {
		return
	}
	groupIDs := input.GroupIDs
	daily, weekly, monthly := input.DailyLimitUSD, input.WeeklyLimitUSD, input.MonthlyLimitUSD
	if previousActive && previous != nil {
		groupIDs, _ = normalizePlanGroupIDs(previous.GroupID, append(append([]int64{}, previous.AccessibleGroupIDs()...), input.GroupIDs...))
		daily = lessRestrictiveSubscriptionLimit(previous.DailyLimit(previous.Group), daily)
		weekly = lessRestrictiveSubscriptionLimit(previous.WeeklyLimit(previous.Group), weekly)
		monthly = lessRestrictiveSubscriptionLimit(previous.MonthlyLimit(previous.Group), monthly)
	}
	sub.GroupIDs = groupIDs
	sub.PlanID = input.PlanID
	sub.DailyLimitUSD = daily
	sub.WeeklyLimitUSD = weekly
	sub.MonthlyLimitUSD = monthly
}

func lessRestrictiveSubscriptionLimit(previous, purchased *float64) *float64 {
	if previous == nil || purchased == nil {
		return nil
	}
	if *previous >= *purchased {
		return previous
	}
	return purchased
}

// planLimitTightened 判断购买后的额度是否比当前窗口生效的额度更紧。
//   - 新额度 nil（不限）永远不算收紧；
//   - 旧额度 nil（原来不限/没有快照）时，任何有限额度都算收紧；
//   - 其余按数值比较，更小就是收紧。
func planLimitTightened(previous, purchased *float64) bool {
	if purchased == nil {
		return false
	}
	if previous == nil {
		return true
	}
	return *purchased < *previous
}

// deferTightenedPlanLimits 把本次购买中「额度收紧」的窗口回退成当前周期的旧额度，
// 并把新额度记到 pending_*，交给该窗口下一次滚动落地（降级口径，issue #6）。
// 放宽或持平的窗口保留新额度，同时清掉上一次遗留的 pending 值，避免旧值盖回去。
// 返回是否仍有窗口处于待生效状态。
func deferTightenedPlanLimits(sub *UserSubscription, input *AssignSubscriptionInput, oldDaily, oldWeekly, oldMonthly *float64) bool {
	if sub == nil || input == nil {
		return false
	}
	pending := false
	if planLimitTightened(oldDaily, input.DailyLimitUSD) {
		sub.DailyLimitUSD = oldDaily
		sub.PendingDailyLimitUSD = input.DailyLimitUSD
		pending = true
	} else {
		sub.PendingDailyLimitUSD = nil
	}
	if planLimitTightened(oldWeekly, input.WeeklyLimitUSD) {
		sub.WeeklyLimitUSD = oldWeekly
		sub.PendingWeeklyLimitUSD = input.WeeklyLimitUSD
		pending = true
	} else {
		sub.PendingWeeklyLimitUSD = nil
	}
	if planLimitTightened(oldMonthly, input.MonthlyLimitUSD) {
		sub.MonthlyLimitUSD = oldMonthly
		sub.PendingMonthlyLimitUSD = input.MonthlyLimitUSD
		pending = true
	} else {
		sub.PendingMonthlyLimitUSD = nil
	}
	if pending {
		sub.PendingPlanID = input.PlanID
	} else {
		sub.PendingPlanID = nil
	}
	return pending
}

// applySwitchedPlanBundle overwrites a subscription with the plan a paid order
// purchased. It is used by the paid fulfillment path only: a customer who buys
// another plan expects that plan's entitlements, not the previous plan's.
//
// The subscription keeps its primary group (the row is keyed by it). Groups the
// subscription could already reach are kept only while that term was still
// running, so switching a plan never revokes access the customer paid for but
// also never hands over groups of an already expired term.
func applySwitchedPlanBundle(sub, previous *UserSubscription, previousActive bool, input *AssignSubscriptionInput) error {
	if sub == nil || input == nil || input.PlanID == nil {
		return nil
	}
	purchasedGroupIDs := input.GroupIDs
	if len(purchasedGroupIDs) == 0 {
		purchasedGroupIDs = []int64{input.GroupID}
	}
	groupIDs, err := normalizePlanGroupIDs(input.GroupID, purchasedGroupIDs)
	if err != nil {
		return err
	}
	if previous != nil {
		kept := []int64{previous.GroupID}
		if previousActive {
			kept = previous.AccessibleGroupIDs()
		}
		merged, err := normalizePlanGroupIDs(previous.GroupID, append(append([]int64{}, kept...), groupIDs...))
		if err != nil {
			return err
		}
		groupIDs = merged
	}
	sub.GroupIDs = groupIDs
	sub.PlanID = input.PlanID
	sub.DailyLimitUSD = input.DailyLimitUSD
	sub.WeeklyLimitUSD = input.WeeklyLimitUSD
	sub.MonthlyLimitUSD = input.MonthlyLimitUSD
	return nil
}
