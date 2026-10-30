package service

import (
	"context"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const IntelligenceRoutingExtraKey = "intelligence_routing_enabled"

type intelligenceRoutingContextKey struct{}
type intelligenceContextIDKey struct{}
type intelligenceClassificationFrozenKey struct{}

func WithIntelligenceClassificationFrozen(ctx context.Context) context.Context {
	return context.WithValue(ctx, intelligenceClassificationFrozenKey{}, true)
}

func IntelligenceClassificationFrozen(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	frozen, _ := ctx.Value(intelligenceClassificationFrozenKey{}).(bool)
	return frozen
}

func WithIntelligenceContextID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, intelligenceContextIDKey{}, id)
}

func linkIntelligenceUsage(ctx context.Context, settings *SettingService, apiKeyID int64, requestID string) {
	// Upstream response IDs and stable media task IDs can be reused. Only
	// server-generated per-request UUIDs are safe to associate automatically.
	var suffix string
	switch {
	case strings.HasPrefix(requestID, "client:"):
		suffix = strings.TrimPrefix(requestID, "client:")
	case strings.HasPrefix(requestID, "web_search:"):
		suffix = strings.TrimPrefix(requestID, "web_search:")
	case strings.HasPrefix(requestID, "x_search:"):
		suffix = strings.TrimPrefix(requestID, "x_search:")
	default:
		return
	}
	if _, err := uuid.Parse(suffix); err != nil {
		return
	}
	linkUniqueIntelligenceUsage(ctx, settings, apiKeyID, requestID)
}

func linkUniqueIntelligenceUsage(ctx context.Context, settings *SettingService, apiKeyID int64, requestID string) {
	id := IntelligenceContextID(ctx)
	if id == "" || settings == nil {
		return
	}
	if err := settings.LinkIntelligenceContext(ctx, id, apiKeyID, requestID); err != nil {
		logger.L().Error("intelligence.context_link_failed", zap.String("context_id", id), zap.Error(err))
	}
}

func IntelligenceContextID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(intelligenceContextIDKey{}).(string)
	return id
}

// CaptureIntelligenceRequest persists the original bytes before forwarding.
func CaptureIntelligenceRequest(ctx context.Context, settings *SettingService, key *APIKey, body []byte, contentType string, matches []IntelligenceRuleMatch) (context.Context, error) {
	if len(matches) == 0 {
		return ctx, nil
	}
	ctx = WithIntelligenceRouting(ctx)
	if settings == nil || key == nil {
		return ctx, nil
	}
	if IntelligenceContextID(ctx) != "" {
		return ctx, nil
	}
	encoding := ""
	if original, originalEncoding, ok := pkghttputil.OriginalRequestBody(ctx); ok {
		body, encoding = original, originalEncoding
	}
	id := uuid.NewString()
	value := &IntelligenceRequestContext{ID: id, APIKeyID: key.ID, UserID: key.UserID, MatchedRules: matches, Body: body, ContentType: contentType, ContentEncoding: encoding, CreatedAt: time.Now().UTC()}
	if err := settings.CreateIntelligenceContext(ctx, value); err != nil {
		return ctx, ErrIntelligenceContextUnavailable
	}
	return WithIntelligenceContextID(ctx, id), nil
}

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
