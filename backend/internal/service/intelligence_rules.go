package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
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
	Rules              []IntelligenceRule                        `json:"rules"`
	BuiltinDisabledIDs []string                                  `json:"builtin_disabled_ids"`
	BuiltinExtensions  map[string][]IntelligenceBuiltinExtension `json:"builtin_extensions,omitempty"`
	BuiltinConditions  map[string]IntelligenceCondition          `json:"builtin_conditions,omitempty"`
}

type IntelligenceBuiltinExtension struct {
	Type              string `json:"type"`
	Pattern           string `json:"pattern"`
	compiled          *regexp.Regexp
	normalizedPattern string
}

// MatchAll reports every enabled built-in and custom rule hit.
func (r IntelligenceRules) MatchAll(text string) []IntelligenceRuleMatch {
	return MergeIntelligenceRuleMatches(r.MatchBuiltinExtensions(text), r.MatchBuiltins(text), r.Matches(text))
}

// Extensions add alternatives to a built-in rule without changing its native
// predicate. Disabling the rule disables its extensions as well.
func (r IntelligenceRules) MatchBuiltinExtensions(text string) []IntelligenceRuleMatch {
	if len(r.BuiltinExtensions) == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	text = norm.NFKC.String(text)
	lowerText := strings.ToLower(text)
	disabled := make(map[string]bool, len(r.BuiltinDisabledIDs))
	for _, id := range r.BuiltinDisabledIDs {
		disabled[id] = true
	}
	var matches []IntelligenceRuleMatch
	for _, builtin := range IntelligenceBuiltinRules() {
		if disabled[builtin.ID] {
			continue
		}
		for _, extension := range r.BuiltinExtensions[builtin.ID] {
			rule := IntelligenceRule{Type: extension.Type, Pattern: extension.Pattern, Enabled: true, compiled: extension.compiled, normalizedPattern: extension.normalizedPattern}
			if intelligenceCustomRuleMatches(rule, text, lowerText) {
				matches = append(matches, IntelligenceRuleMatch{ID: builtin.ID, Name: builtin.Name, Type: "builtin", Pattern: extension.Pattern})
				break
			}
		}
	}
	return matches
}

// MatchBuiltins returns enabled built-in matches; older settings that lack
// builtin_disabled_ids preserve the existing default of all enabled.
func (r IntelligenceRules) MatchBuiltins(text string) []IntelligenceRuleMatch {
	all := MatchIntelligenceBuiltins(text)
	if len(r.BuiltinDisabledIDs) == 0 && len(r.BuiltinConditions) == 0 {
		return all
	}
	disabled := make(map[string]bool, len(r.BuiltinDisabledIDs))
	for _, id := range r.BuiltinDisabledIDs {
		disabled[id] = true
	}
	native := make(map[string]IntelligenceRuleMatch, len(all))
	for _, match := range all {
		native[match.ID] = match
	}
	texts := &intelligenceConditionInput{input: text, plain: strings.ToLower(text)}
	var matches []IntelligenceRuleMatch
	for _, builtin := range IntelligenceBuiltinRules() {
		if disabled[builtin.ID] {
			continue
		}
		if condition, overridden := r.BuiltinConditions[builtin.ID]; overridden {
			if matchIntelligenceConditionNode(condition, texts) {
				matches = append(matches, IntelligenceRuleMatch{ID: builtin.ID, Name: builtin.Name, Type: "builtin"})
			}
		} else if match, ok := native[builtin.ID]; ok {
			matches = append(matches, match)
		}
	}
	return matches
}

// Matches captures each matching configured rule for historical attribution.
// The custom ID identifies its position in this snapshot; callers must persist
// the matched name/type so later edits do not rewrite historical attribution.
func (r IntelligenceRules) Matches(text string) []IntelligenceRuleMatch {
	if len(r.Rules) == 0 {
		return nil
	}
	text = norm.NFKC.String(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lowerText := strings.ToLower(text)
	var matches []IntelligenceRuleMatch
	for i, rule := range r.Rules {
		if intelligenceCustomRuleMatches(rule, text, lowerText) {
			matches = append(matches, IntelligenceRuleMatch{ID: "custom:" + strconv.Itoa(i), Name: rule.Name, Type: rule.Type, Pattern: rule.Pattern})
		}
	}
	return matches
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
		if intelligenceCustomRuleMatches(rule, text, lowerText) {
			return true
		}
	}
	return false
}

func intelligenceCustomRuleMatches(rule IntelligenceRule, text, lowerText string) bool {
	if !rule.Enabled || strings.TrimSpace(rule.Pattern) == "" {
		return false
	}
	switch rule.Type {
	case "contains":
		pattern := rule.normalizedPattern
		if pattern == "" {
			pattern = strings.ToLower(norm.NFKC.String(rule.Pattern))
		}
		return strings.Contains(lowerText, pattern)
	case "regex":
		compiled := rule.compiled
		if compiled == nil {
			compiled, _ = regexp.Compile("(?i)" + rule.Pattern)
		}
		return compiled != nil && !compiled.MatchString("") && compiled.MatchString(text)
	}
	return false
}

func prepareIntelligenceRules(value IntelligenceRules) (IntelligenceRules, error) {
	if len(value.Rules) > maxIntelligenceRules {
		return value, fmt.Errorf("%w: at most %d rules", ErrInvalidIntelligenceRules, maxIntelligenceRules)
	}
	if len(value.BuiltinDisabledIDs) > maxIntelligenceRules {
		return value, fmt.Errorf("%w: too many disabled built-in rule IDs", ErrInvalidIntelligenceRules)
	}
	result := IntelligenceRules{Rules: make([]IntelligenceRule, len(value.Rules)), BuiltinDisabledIDs: append([]string{}, value.BuiltinDisabledIDs...)}
	if len(value.BuiltinConditions) > maxIntelligenceRules {
		return value, fmt.Errorf("%w: too many built-in conditions", ErrInvalidIntelligenceRules)
	}
	conditionsJSON, marshalErr := json.Marshal(value.BuiltinConditions)
	if marshalErr != nil || len(conditionsJSON) > 1<<20 {
		return value, fmt.Errorf("%w: built-in conditions are too large", ErrInvalidIntelligenceRules)
	}
	result.BuiltinConditions = make(map[string]IntelligenceCondition, len(value.BuiltinConditions))
	for id, condition := range value.BuiltinConditions {
		if strings.TrimSpace(id) == "" || len(id) > 256 {
			return value, fmt.Errorf("%w: invalid built-in rule ID", ErrInvalidIntelligenceRules)
		}
		count := 0
		prepared, err := prepareIntelligenceCondition(condition, 0, &count)
		if err != nil {
			return value, err
		}
		if matchIntelligenceCondition(prepared, "") {
			return value, fmt.Errorf("%w: built-in condition cannot match empty input", ErrInvalidIntelligenceRules)
		}
		result.BuiltinConditions[id] = prepared
	}
	result.BuiltinExtensions = make(map[string][]IntelligenceBuiltinExtension, len(value.BuiltinExtensions))
	extensionCount := 0
	for id, entries := range value.BuiltinExtensions {
		if strings.TrimSpace(id) == "" || len(id) > 256 {
			return value, fmt.Errorf("%w: invalid built-in rule ID", ErrInvalidIntelligenceRules)
		}
		extensionCount += len(entries)
		if extensionCount > maxIntelligenceRules {
			return value, fmt.Errorf("%w: at most %d built-in extensions", ErrInvalidIntelligenceRules, maxIntelligenceRules)
		}
		copied := make([]IntelligenceBuiltinExtension, len(entries))
		for i, item := range entries {
			if strings.TrimSpace(item.Pattern) == "" || len(item.Pattern) > maxIntelligenceRulePattern {
				return value, fmt.Errorf("%w: built-in extension requires a non-empty pattern", ErrInvalidIntelligenceRules)
			}
			switch item.Type {
			case "contains":
				item.normalizedPattern = strings.ToLower(norm.NFKC.String(item.Pattern))
			case "regex":
				compiled, err := regexp.Compile("(?i)" + item.Pattern)
				if err != nil || compiled.MatchString("") {
					return value, fmt.Errorf("%w: invalid or empty-matching built-in extension", ErrInvalidIntelligenceRules)
				}
				item.compiled = compiled
			default:
				return value, fmt.Errorf("%w: unknown built-in extension type", ErrInvalidIntelligenceRules)
			}
			copied[i] = item
		}
		result.BuiltinExtensions[id] = copied
	}
	for _, id := range result.BuiltinDisabledIDs {
		if strings.TrimSpace(id) == "" || len(id) > 256 {
			return value, fmt.Errorf("%w: invalid built-in rule ID", ErrInvalidIntelligenceRules)
		}
	}
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
	value.BuiltinDisabledIDs = append([]string{}, value.BuiltinDisabledIDs...)
	if value.BuiltinConditions != nil {
		conditions := make(map[string]IntelligenceCondition, len(value.BuiltinConditions))
		for id, condition := range value.BuiltinConditions {
			conditions[id] = cloneIntelligenceCondition(condition)
		}
		value.BuiltinConditions = conditions
	}
	if value.BuiltinExtensions != nil {
		extensions := make(map[string][]IntelligenceBuiltinExtension, len(value.BuiltinExtensions))
		for id, entries := range value.BuiltinExtensions {
			extensions[id] = append([]IntelligenceBuiltinExtension{}, entries...)
		}
		value.BuiltinExtensions = extensions
	}
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
