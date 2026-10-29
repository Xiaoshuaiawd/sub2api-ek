package service

import "context"

const IntelligenceRoutingExtraKey = "intelligence_routing_enabled"

type intelligenceRoutingContextKey struct{}

// WithIntelligenceRouting limits this request to accounts marked as capable.
func WithIntelligenceRouting(ctx context.Context) context.Context {
	return context.WithValue(ctx, intelligenceRoutingContextKey{}, true)
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
	return !IsIntelligenceRouting(ctx) || IsIntelligenceRoutingAccount(account)
}
