package service

import "context"

const IntelligenceRoutingExtraKey = "intelligence_routing_enabled"

type intelligenceRoutingContextKey struct{}

// WithIntelligenceRouting limits this request to accounts marked as capable.
func WithIntelligenceRouting(ctx context.Context) context.Context {
	return WithIntelligenceRoutingRequired(ctx, true)
}

// WithIntelligenceRoutingRequired sets the routing class for this request or turn.
func WithIntelligenceRoutingRequired(ctx context.Context, required bool) context.Context {
	return context.WithValue(ctx, intelligenceRoutingContextKey{}, required)
}

// IsIntelligenceRoutingAccount reports whether the account is enabled for intelligence tests.
func IsIntelligenceRoutingAccount(account *Account) bool {
	if account == nil {
		return false
	}
	enabled, _ := account.Extra[IntelligenceRoutingExtraKey].(bool)
	return enabled
}

// IsIntelligenceRouting reports whether the request requires a designated account.
func IsIntelligenceRouting(ctx context.Context) bool {
	required, _ := ctx.Value(intelligenceRoutingContextKey{}).(bool)
	return required
}

func intelligenceRoutingAllowsAccount(ctx context.Context, account *Account) bool {
	return account != nil && IsIntelligenceRouting(ctx) == IsIntelligenceRoutingAccount(account)
}

func filterIntelligenceRoutingAccounts(ctx context.Context, accounts []Account) []Account {
	filtered := make([]Account, 0, len(accounts))
	for i := range accounts {
		if intelligenceRoutingAllowsAccount(ctx, &accounts[i]) {
			filtered = append(filtered, accounts[i])
		}
	}
	return filtered
}
