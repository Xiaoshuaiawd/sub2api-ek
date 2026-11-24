package service

import (
	"context"
	"strings"

	"github.com/tidwall/gjson"
)

// Contextual rules override only serialization. Keep each adapter's existing
// no-match/default-dispatch behavior instead of mapping a resolved target again.
func httpAccountModelOverride(ctx context.Context, account *Account, model string) (string, bool) {
	if target, resolved := ResolvedAccountModel(ctx, account, model); resolved {
		return target, true
	}
	return ResolveAccountOriginalModelMapping(ctx, account)
}

func resolveHTTPForwardModel(ctx context.Context, account *Account, model, defaultMappedModel string) string {
	if target, matched := httpAccountModelOverride(ctx, account, model); matched {
		return target
	}
	return resolveOpenAIForwardModel(account, model, defaultMappedModel)
}

func resolveHTTPChannelForwardModel(ctx context.Context, account *Account, model, channelMappedModel string) string {
	if target, matched := httpAccountModelOverride(ctx, account, model); matched {
		return target
	}
	return resolveOpenAIChannelForwardModel(account, model, channelMappedModel)
}

func httpModelMappingContext(contexts []context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}

func httpUpstreamRoutingModel(ctx context.Context, account *Account, body []byte, defaultMappedModel string) string {
	if !account.routesByModel() {
		return ""
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	return normalizeOpenAIModelForUpstream(account, resolveHTTPForwardModel(ctx, account, model, defaultMappedModel))
}
