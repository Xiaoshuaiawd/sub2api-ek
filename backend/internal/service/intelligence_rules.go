package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

const intelligenceRulesSettingKey = "intelligence_routing_rules_v1"
const maxIntelligenceRules = 200
const maxIntelligenceRulePattern = 4096

var ErrInvalidIntelligenceRules = errors.New("invalid intelligence routing rules")
var ErrIntelligenceRulesUnavailable = errors.New("intelligence routing rules unavailable; please retry")

type IntelligenceRule struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	Pattern           string `json:"pattern"`
	Enabled           bool   `json:"enabled"`
	compiled          *regexp.Regexp
	normalizedPattern string
}

type IntelligenceRules struct {
	Rules []IntelligenceRule `json:"rules"`
}

// Match checks compiled rules. Text and contains patterns are normalized
// consistently; regex uses Go's linear-time RE2 engine and Unicode case-folding.
func (r IntelligenceRules) Match(text string) bool {
	if len(r.Rules) == 0 {
		return false
	}
	text = norm.NFKC.String(text)
	if strings.TrimSpace(text) == "" {
		return false
	}
	lowerText := strings.ToLower(text)
	for _, rule := range r.Rules {
		if !rule.Enabled || strings.TrimSpace(rule.Pattern) == "" {
			continue
		}
		switch rule.Type {
		case "contains":
			pattern := rule.normalizedPattern
			if pattern == "" {
				pattern = strings.ToLower(norm.NFKC.String(rule.Pattern))
			}
			if strings.Contains(lowerText, pattern) {
				return true
			}
		case "regex":
			compiled := rule.compiled
			if compiled == nil {
				compiled, _ = regexp.Compile("(?i)" + rule.Pattern)
			}
			if compiled != nil && !compiled.MatchString("") && compiled.MatchString(text) {
				return true
			}
		}
	}
	return false
}

func prepareIntelligenceRules(value IntelligenceRules) (IntelligenceRules, error) {
	if len(value.Rules) > maxIntelligenceRules {
		return value, fmt.Errorf("%w: at most %d rules", ErrInvalidIntelligenceRules, maxIntelligenceRules)
	}
	result := IntelligenceRules{Rules: make([]IntelligenceRule, len(value.Rules))}
	for i, rule := range value.Rules {
		rule.Name = strings.TrimSpace(rule.Name)
		if rule.Name == "" || len(rule.Name) > 256 || strings.TrimSpace(rule.Pattern) == "" || len(rule.Pattern) > maxIntelligenceRulePattern {
			return value, fmt.Errorf("%w: rule %d requires a name and non-empty pattern", ErrInvalidIntelligenceRules, i+1)
		}
		switch rule.Type {
		case "contains":
			rule.normalizedPattern = strings.ToLower(norm.NFKC.String(rule.Pattern))
		case "regex":
			compiled, err := regexp.Compile("(?i)" + rule.Pattern)
			if err != nil || compiled.MatchString("") {
				return value, fmt.Errorf("%w: rule %d has an invalid or empty-matching regular expression", ErrInvalidIntelligenceRules, i+1)
			}
			rule.compiled = compiled
		default:
			return value, fmt.Errorf("%w: rule %d has an unknown type", ErrInvalidIntelligenceRules, i+1)
		}
		result.Rules[i] = rule
	}
	return result, nil
}

func (s *SettingService) GetIntelligenceRules(ctx context.Context) (IntelligenceRules, error) {
	if s == nil || s.settingRepo == nil {
		return IntelligenceRules{Rules: []IntelligenceRule{}}, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, intelligenceRulesSettingKey)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && raw == "") {
		return IntelligenceRules{Rules: []IntelligenceRule{}}, nil
	}
	if err != nil {
		return IntelligenceRules{}, err
	}
	var value IntelligenceRules
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return IntelligenceRules{}, fmt.Errorf("invalid saved intelligence routing rules")
	}
	return prepareIntelligenceRules(value)
}

func (s *SettingService) SetIntelligenceRules(ctx context.Context, input IntelligenceRules) (IntelligenceRules, error) {
	prepared, err := prepareIntelligenceRules(input)
	if err != nil {
		return IntelligenceRules{}, err
	}
	if s == nil || s.settingRepo == nil {
		return IntelligenceRules{}, errors.New("settings service unavailable")
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		return IntelligenceRules{}, err
	}
	s.intelligenceRulesMu.Lock()
	defer s.intelligenceRulesMu.Unlock()
	if err := s.settingRepo.Set(ctx, intelligenceRulesSettingKey, string(raw)); err != nil {
		return IntelligenceRules{}, err
	}
	s.intelligenceRulesCache = &prepared
	s.intelligenceRulesExpires = time.Now().Add(5 * time.Second)
	return cloneIntelligenceRules(prepared), nil
}

func cloneIntelligenceRules(value IntelligenceRules) IntelligenceRules {
	value.Rules = append([]IntelligenceRule{}, value.Rules...)
	return value
}

// RuntimeIntelligenceRules refreshes from shared storage every 5 seconds.
// A temporary storage failure retains the last known good configuration.
func (s *SettingService) RuntimeIntelligenceRules(ctx context.Context) (IntelligenceRules, error) {
	if s == nil || s.settingRepo == nil {
		return IntelligenceRules{Rules: []IntelligenceRule{}}, nil
	}
	s.intelligenceRulesMu.Lock()
	defer s.intelligenceRulesMu.Unlock()
	if s.intelligenceRulesCache != nil && time.Now().Before(s.intelligenceRulesExpires) {
		return cloneIntelligenceRules(*s.intelligenceRulesCache), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := s.GetIntelligenceRules(ctx)
	if err != nil {
		if s.intelligenceRulesCache != nil {
			s.intelligenceRulesExpires = time.Now().Add(5 * time.Second)
			return cloneIntelligenceRules(*s.intelligenceRulesCache), nil
		}
		return IntelligenceRules{}, err
	}
	s.intelligenceRulesCache = &value
	s.intelligenceRulesExpires = time.Now().Add(5 * time.Second)
	return cloneIntelligenceRules(value), nil
}
