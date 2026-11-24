package service

import (
	"context"
	"strings"
)

type accountModelMappingKey struct{}
type resolvedAccountModelKey struct{}

type accountModelMappingBasis struct {
	source  string
	channel string
}

type resolvedAccountModel struct {
	accountID int64
	model     string
}

// WithAccountModelMapping preserves the input and output of channel mapping.
// The input is after existing API-key/composite routing, not necessarily the
// public client alias. Decisions are made separately for each fresh account.
func WithAccountModelMapping(ctx context.Context, sourceModel, channelModel string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, accountModelMappingKey{}, accountModelMappingBasis{
		source: strings.TrimSpace(sourceModel), channel: strings.TrimSpace(channelModel),
	})
	return context.WithValue(ctx, resolvedAccountModelKey{}, resolvedAccountModel{})
}

func AccountModelMappingModels(ctx context.Context) (sourceModel, channelModel string, ok bool) {
	if ctx == nil {
		return "", "", false
	}
	basis, ok := ctx.Value(accountModelMappingKey{}).(accountModelMappingBasis)
	return basis.source, basis.channel, ok && basis.source != ""
}

func configuredModelMappingAccount(account *Account) *Account {
	if account != nil && account.gatewayProtocolSource != nil {
		return account.gatewayProtocolSource
	}
	return account
}

// ResolveConfiguredModelMapping matches only administrator-supplied rules.
// Provider defaults and protocol projections must not suppress channel rules.
// Identity targets count as matches, and wildcards retain longest-first order.
func (a *Account) ResolveConfiguredModelMapping(requestedModel string) (string, bool) {
	a = configuredModelMappingAccount(a)
	if a == nil || a.Credentials == nil || requestedModel == "" {
		return "", false
	}
	raw := a.Credentials["model_mapping"]
	if target, matched := resolveConfiguredMapping(raw, requestedModel); matched {
		return target, true
	}
	if normalized := normalizeRequestedModelForLookup(a.Platform, requestedModel); normalized != requestedModel {
		return resolveConfiguredMapping(raw, normalized)
	}
	return "", false
}

func resolveConfiguredMapping(raw any, requestedModel string) (string, bool) {
	var bestPattern, bestTarget string
	consider := func(pattern, target string) {
		target = strings.TrimSpace(target)
		if target == "" || !matchWildcard(pattern, requestedModel) {
			return
		}
		if bestTarget == "" || len(pattern) > len(bestPattern) || len(pattern) == len(bestPattern) && pattern < bestPattern {
			bestPattern, bestTarget = pattern, target
		}
	}
	switch mapping := raw.(type) {
	case map[string]any:
		if target, ok := mapping[requestedModel].(string); ok && strings.TrimSpace(target) != "" {
			return strings.TrimSpace(target), true
		}
		for pattern, value := range mapping {
			if target, ok := value.(string); ok {
				consider(pattern, target)
			}
		}
	case map[string]string:
		if target := strings.TrimSpace(mapping[requestedModel]); target != "" {
			return target, true
		}
		for pattern, target := range mapping {
			consider(pattern, target)
		}
	}
	return bestTarget, bestTarget != ""
}

func ResolveAccountOriginalModelMapping(ctx context.Context, account *Account) (string, bool) {
	source, _, ok := AccountModelMappingModels(ctx)
	if !ok {
		return "", false
	}
	return account.ResolveConfiguredModelMapping(source)
}

// AccountModelMappingInput is the source-key identity used by account model
// boundaries. It must not be replaced by an upstream target and mapped twice.
func AccountModelMappingInput(ctx context.Context, account *Account, fallbackModel string, requireCompact bool) string {
	source, channel, ok := AccountModelMappingModels(ctx)
	if !ok || account == nil {
		return fallbackModel
	}
	owner := configuredModelMappingAccount(account)
	if requireCompact {
		if target, matched := owner.ResolveCompactMappedModel(source); matched && strings.TrimSpace(target) != "" {
			return source
		}
	}
	if _, matched := account.ResolveConfiguredModelMapping(source); matched {
		return source
	}
	// A serialized WS/bridge payload already contains its wire target. When
	// rechecking a fresh account, recover the channel input instead of using
	// that target as a second account-mapping source.
	if serialized, marked := ctx.Value(resolvedAccountModelKey{}).(resolvedAccountModel); marked && serialized.model != "" && serialized.model == strings.TrimSpace(fallbackModel) && channel != "" {
		return channel
	}
	return fallbackModel
}

// AccountMappedModelForRequest is for serialization, not authorization. Fresh
// admission must recompute from the preserved source/channel basis.
func AccountMappedModelForRequest(ctx context.Context, account *Account, fallbackModel string) string {
	if resolved, ok := ResolvedAccountModel(ctx, account, fallbackModel); ok {
		return resolved
	}
	if target, matched := ResolveAccountOriginalModelMapping(ctx, account); matched {
		return target
	}
	if account == nil {
		return fallbackModel
	}
	return account.GetMappedModel(AccountModelMappingInput(ctx, account, fallbackModel, false))
}

func WithResolvedAccountModel(ctx context.Context, accountID int64, model string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, resolvedAccountModelKey{}, resolvedAccountModel{accountID: accountID, model: strings.TrimSpace(model)})
}

func ResolvedAccountModel(ctx context.Context, account *Account, model string) (string, bool) {
	if ctx == nil || account == nil {
		return "", false
	}
	resolved, ok := ctx.Value(resolvedAccountModelKey{}).(resolvedAccountModel)
	if !ok || resolved.accountID != account.ID || resolved.model == "" || resolved.model != strings.TrimSpace(model) {
		return "", false
	}
	return resolved.model, true
}

// ForAccount describes actual forwarding independently of the configured
// channel pricing target. A winning original-source account rule skips A→B.
func (r ChannelMappingResult) ForAccount(account *Account, sourceModel string, requireCompact bool) ChannelMappingResult {
	_, matched := account.ResolveConfiguredModelMapping(sourceModel)
	if requireCompact && account != nil {
		if target, compact := configuredModelMappingAccount(account).ResolveCompactMappedModel(sourceModel); compact && strings.TrimSpace(target) != "" {
			matched = true
		}
	}
	if !matched {
		return r
	}
	if r.Mapped && r.PricingMappedModel == "" {
		r.PricingMappedModel = r.MappedModel
	}
	r.MappedModel = sourceModel
	r.Mapped = false
	return r
}
