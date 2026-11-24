package service

import "maps"

// CopySchedulerSnapshot keeps the decoded master private. Fork candidate
// filters may update nested credentials/Extra and allowed-model slices, so a
// plain struct copy is insufficient even after the model-limit COW fix.
func (a *Account) CopySchedulerSnapshot() Account {
	copy := *a
	copy.schedulingCandidate = false
	copy.schedulingPrimaryRead = false
	copy.Credentials = cloneSchedulerSnapshotMap(a.Credentials)
	copy.Extra = cloneSchedulerSnapshotMap(a.Extra)
	copy.GroupIDs = append([]int64(nil), a.GroupIDs...)
	copy.AccountGroups = append([]AccountGroup(nil), a.AccountGroups...)
	for i := range copy.AccountGroups {
		copy.AccountGroups[i].AllowedModels = append([]string(nil), a.AccountGroups[i].AllowedModels...)
	}
	copy.modelMappingCache = maps.Clone(a.modelMappingCache)
	copy.modelMappingCacheShared = false
	// Reuse the prepared mapping, but retain the fork's content-signature check
	// for request-local in-place changes. No JSON decode is repeated on memo hits.
	copy.modelMappingCacheCredentialsPtr = mapPtr(copy.Credentials)
	raw, _ := copy.Credentials["model_mapping"].(map[string]any)
	copy.modelMappingCacheRawPtr = mapPtr(raw)
	return copy
}

// CopySchedulingCandidate gives the scheduler its own value fields and lazy
// caches while sharing immutable configuration with a prepared snapshot.
// Credentials, Extra and model/group slices must only be read; use existing
// copy-on-write helpers for local limit updates. Hydrate the selected account
// before it reaches forwarding or any other mutable account consumer.
func (a *Account) CopySchedulingCandidate() Account {
	copy := *a
	copy.schedulingCandidate = true
	copy.schedulingPrimaryRead = false
	copy.PrepareSharedSnapshot()
	return copy
}

// IsSchedulingCandidate reports an internal configuration-only view. The flag
// is not serialized and full-account hydration always returns a mutable object.
func (a *Account) IsSchedulingCandidate() bool {
	return a != nil && a.schedulingCandidate
}

// schedulingCandidatesOwnValues permits in-place filtering only for the
// dedicated reader's independently allocated value slice, never public input.
func schedulingCandidatesOwnValues(accounts []Account) bool {
	if len(accounts) == 0 {
		return false
	}
	for i := range accounts {
		if !accounts[i].IsSchedulingCandidate() {
			return false
		}
	}
	return true
}

func cloneSchedulerSnapshotMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = cloneSchedulerSnapshotValue(value)
	}
	return copy
}

// Snapshot maps have already passed JSON decoding; only JSON maps/slices need
// recursive copying. Scalars are immutable and safe to reuse.
func cloneSchedulerSnapshotValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneSchedulerSnapshotMap(v)
	case []any:
		copy := make([]any, len(v))
		for i, child := range v {
			copy[i] = cloneSchedulerSnapshotValue(child)
		}
		return copy
	default:
		return value
	}
}
