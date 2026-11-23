package service

import (
	"log/slog"
)

// ActiveSubscriptionCandidateLimit caps how many active subscriptions are loaded
// when resolving one (user, group) pair. A paid plan switch merges group sets, so
// more than one row can reach the same group; the repository orders candidates
// newest-first, and this cap only bounds the data the warn log can report.
const ActiveSubscriptionCandidateLimit = 20

// SelectActiveSubscriptionCandidate resolves which active subscription owns a
// (user, group) pair when several rows reach that group.
//
// The choice must not depend on storage order: the newest StartsAt wins and the
// highest ID breaks ties. Both keys are re-applied here so the result stays
// deterministic even if a caller (or a future query) hands rows over unordered.
// A conflict is reported through a warn log: two subscriptions reaching the same
// group is still a data defect, and billing silently picking one of them is the
// situation this selection exists to make visible.
func SelectActiveSubscriptionCandidate(userID, groupID int64, candidates []UserSubscription) (*UserSubscription, bool) {
	if len(candidates) == 0 {
		return nil, false
	}

	bestIdx := 0
	for i := 1; i < len(candidates); i++ {
		candidate := candidates[i]
		best := candidates[bestIdx]
		if subscriptionBillingPrecedes(&candidate, &best) {
			bestIdx = i
		}
	}

	if len(candidates) > 1 {
		ids := make([]int64, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.ID)
		}
		slog.Warn("multiple active subscriptions match one user group; billing uses the deterministic winner",
			"user_id", userID,
			"group_id", groupID,
			"candidate_subscription_ids", ids,
			"selected_subscription_id", candidates[bestIdx].ID,
		)
	}

	winner := candidates[bestIdx]
	return &winner, true
}

func subscriptionBillingPrecedes(a, b *UserSubscription) bool {
	return a.StartsAt.After(b.StartsAt) || (a.StartsAt.Equal(b.StartsAt) && a.ID > b.ID)
}
