package service

import (
	"context"
	"strings"
	"time"
)

func (s *defaultOpenAIAccountScheduler) isAccountTransportCompatibleForRequest(ctx context.Context, account *Account, req OpenAIAccountScheduleRequest) bool {
	if s == nil || s.service == nil {
		return false
	}
	return s.service.isOpenAIAccountTransportCompatibleForRequest(ctx, account, req.RequiredTransport, req.RequestedModel, req.RequireCompact)
}

func (s *OpenAIGatewayService) isOpenAIAccountTransportCompatibleForRequest(ctx context.Context, account *Account, transport OpenAIUpstreamTransport, model string, compact bool) bool {
	return s.isOpenAIAccountTransportCompatible(account, transport, AccountModelMappingInput(ctx, account, model, compact))
}

func (s *OpenAIGatewayService) isExcelBPSCoolingDownForRequest(ctx context.Context, account *Account, model string, compact bool) bool {
	return s.isExcelBPSCoolingDown(account, AccountModelMappingInput(ctx, account, model, compact))
}

func (s *OpenAIGatewayService) isExcelBPSModelCoolingDownForRequest(ctx context.Context, account *Account, model string, compact bool) bool {
	return s.isExcelBPSModelCoolingDown(account, AccountModelMappingInput(ctx, account, model, compact))
}

func isExcelBPSEnabledForRequest(ctx context.Context, account *Account, model string, compact bool) bool {
	return account.IsExcelBPSEnabledForModel(AccountModelMappingInput(ctx, account, model, compact))
}

func canonicalOpenAIAccountSchedulingModelForRequest(ctx context.Context, account *Account, model string, compact bool) string {
	if account == nil {
		return strings.TrimSpace(model)
	}
	if account.IsOpenAI() {
		return resolveOpenAIAccountModelForScheduling(ctx, account, model, compact)
	}
	if target, matched := ResolveAccountOriginalModelMapping(ctx, account); matched {
		return target
	}
	return canonicalOpenAIAccountSchedulingModel(account, AccountModelMappingInput(ctx, account, model, compact))
}

func accountMappingTargetAllowedForRequest(ctx context.Context, account *Account, model string, compact bool) bool {
	if account == nil {
		return false
	}
	if _, _, contextual := AccountModelMappingModels(ctx); !contextual {
		return true
	}
	target := canonicalOpenAIAccountSchedulingModelForRequest(ctx, account, model, compact)
	return account.oauthInitialSelectionAllows(target) && !isRetiredOpenAISubscriptionModel(account, target)
}

func (s *OpenAIGatewayService) openAICodexTicketOutboundModelForRequest(ctx context.Context, account *Account, model string, compact bool) string {
	if _, _, contextual := AccountModelMappingModels(ctx); !contextual {
		return s.openAICodexTicketOutboundModel(account, model, compact)
	}
	input := AccountModelMappingInput(ctx, account, model, compact)
	upstream := canonicalOpenAIAccountSchedulingModelForRequest(ctx, account, model, compact)
	if compact && account != nil && account.IsOpenAI() {
		if fallback := strings.TrimSpace(s.resolveOpenAICompactFallbackModel(account, input)); fallback != "" {
			return fallback
		}
	}
	return upstream
}

// Read current account authority from the immutable source/channel basis. A
// wire marker from a previous send never authorizes a retired/blocked target.
func (s *OpenAIGatewayService) isOpenAIAccountRequestRuntimeBlockedForRequest(ctx context.Context, account *Account, model string, compact bool) bool {
	if _, _, contextual := AccountModelMappingModels(ctx); !contextual {
		return s.isOpenAIAccountRequestRuntimeBlocked(account, model, compact)
	}
	if s == nil || account == nil {
		return false
	}
	input := AccountModelMappingInput(ctx, account, model, compact)
	outbound := s.openAICodexTicketOutboundModelForRequest(ctx, account, model, compact)
	blocks, _ := account.Extra[OAuth429ModelBlocksKey].(map[string]any)
	if blocks[outbound] != nil || s.isExcelBPSCoolingDown(account, input) || s.isExcelBPSModelCoolingDown(account, input) || s.openAICodexTicketBlocksAccount(account, outbound) {
		return true
	}
	snapshot := s.peekOpenAIAccountRuntimeBlock(account)
	if snapshot.blocked {
		if accountPersistedSchedulingCooldownActive(account) {
			return true
		}
		s.clearOpenAIAccountRuntimeBlockIfUnchanged(account.ID, snapshot)
	}
	state := s.getOpenAIAccountModelTransientState()
	return state != nil && state.isBlocked(account.ID, openAIAccountModelTransientModel(outbound), time.Now())
}

func filterGrokSchedulingModelsForRequest(ctx context.Context, accounts []Account, model string, compact, quota bool, now time.Time) []Account {
	if _, _, contextual := AccountModelMappingModels(ctx); !contextual {
		if quota {
			return filterGrokModelQuotaBlockedAccounts(accounts, model, now)
		}
		return filterGrokTeamModelRateLimitedAccounts(accounts, model, now)
	}
	out := accounts[:0]
	for i := range accounts {
		upstream := canonicalOpenAIAccountSchedulingModelForRequest(ctx, &accounts[i], model, compact)
		if quota && isGrokModelQuotaBlocked(accounts[i].ID, upstream, now) || !quota && isGrokTeamModelRateLimited(&accounts[i], upstream, now) {
			continue
		}
		out = append(out, accounts[i])
	}
	clear(accounts[len(out):])
	return out
}
