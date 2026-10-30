package service

import (
	"context"
	"errors"
	"time"
)

var ErrIntelligenceContextUnavailable = errors.New("intelligence context storage unavailable")
var ErrIntelligenceContextNotFound = errors.New("intelligence context not found")
var ErrIntelligenceContextAmbiguous = errors.New("multiple intelligence contexts match this usage record")
var ErrIntelligenceContextPriorUsage = errors.New("usage record predates intelligence context")

// IntelligenceRequestContext contains sensitive customer input. Never add it to
// the public usage DTO, logs, request headers, or analytics responses.
type IntelligenceRequestContext struct {
	ID              string                  `json:"id"`
	APIKeyID        int64                   `json:"api_key_id"`
	UserID          int64                   `json:"user_id"`
	UsageRequestID  *string                 `json:"usage_request_id,omitempty"`
	UsageLogID      *int64                  `json:"usage_log_id,omitempty"`
	MatchedRules    []IntelligenceRuleMatch `json:"matched_rules"`
	Body            []byte                  `json:"-"`
	ContentType     string                  `json:"content_type"`
	ContentEncoding string                  `json:"content_encoding"`
	CreatedAt       time.Time               `json:"created_at"`
}

type IntelligenceContextRepository interface {
	Create(context.Context, *IntelligenceRequestContext) error
	Link(context.Context, string, int64, string) error
	FindByUsage(context.Context, int64) (*IntelligenceRequestContext, error)
	FindByID(context.Context, string) (*IntelligenceRequestContext, error)
	Available(context.Context, []int64) (map[int64]bool, error)
	List(context.Context, int, int) ([]IntelligenceRequestContext, int64, error)
}

// SetIntelligenceContextRepository is called during service initialization.
func (s *SettingService) SetIntelligenceContextRepository(repo IntelligenceContextRepository) {
	if s != nil {
		s.intelligenceContexts = repo
	}
}

func (s *SettingService) CreateIntelligenceContext(ctx context.Context, value *IntelligenceRequestContext) error {
	if s == nil || s.intelligenceContexts == nil {
		return ErrIntelligenceContextUnavailable
	}
	return s.intelligenceContexts.Create(ctx, value)
}

func (s *SettingService) LinkIntelligenceContext(ctx context.Context, id string, apiKeyID int64, requestID string) error {
	if s == nil || s.intelligenceContexts == nil {
		return ErrIntelligenceContextUnavailable
	}
	return s.intelligenceContexts.Link(ctx, id, apiKeyID, requestID)
}

func (s *SettingService) IntelligenceContextByUsage(ctx context.Context, id int64) (*IntelligenceRequestContext, error) {
	if s == nil || s.intelligenceContexts == nil {
		return nil, ErrIntelligenceContextUnavailable
	}
	return s.intelligenceContexts.FindByUsage(ctx, id)
}

func (s *SettingService) IntelligenceContextByID(ctx context.Context, id string) (*IntelligenceRequestContext, error) {
	if s == nil || s.intelligenceContexts == nil {
		return nil, ErrIntelligenceContextUnavailable
	}
	return s.intelligenceContexts.FindByID(ctx, id)
}

func (s *SettingService) IntelligenceContextAvailable(ctx context.Context, ids []int64) (map[int64]bool, error) {
	if s == nil || s.intelligenceContexts == nil {
		return nil, ErrIntelligenceContextUnavailable
	}
	return s.intelligenceContexts.Available(ctx, ids)
}

func (s *SettingService) IntelligenceContexts(ctx context.Context, page, pageSize int) ([]IntelligenceRequestContext, int64, error) {
	if s == nil || s.intelligenceContexts == nil {
		return nil, 0, ErrIntelligenceContextUnavailable
	}
	return s.intelligenceContexts.List(ctx, page, pageSize)
}
