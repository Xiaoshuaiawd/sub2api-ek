package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrIntelligenceRulesConflict = errors.New("intelligence rules changed; reload and retry")

// IntelligenceRulePatch changes one rule against a caller-supplied revision.
// Indexes are only valid for that revision and are never stable identifiers.
type IntelligenceRulePatch struct {
	Revision       string                          `json:"revision"`
	Operation      string                          `json:"operation"`
	Index          *int                            `json:"index,omitempty"`
	Rule           *IntelligenceRule               `json:"rule,omitempty"`
	BuiltinID      string                          `json:"builtin_id,omitempty"`
	Enabled        *bool                           `json:"enabled,omitempty"`
	Condition      *IntelligenceCondition          `json:"condition,omitempty"`
	ResetCondition bool                            `json:"reset_condition,omitempty"`
	Extensions     *[]IntelligenceBuiltinExtension `json:"extensions,omitempty"`
	Extension      *IntelligenceBuiltinExtension   `json:"extension,omitempty"`
}

// IntelligenceRulesReplacement supports versioned whole-form updates while
// preserving fields omitted by older clients.
type IntelligenceRulesReplacement struct {
	Revision           string
	Rules              []IntelligenceRule
	BuiltinDisabledIDs *[]string
	BuiltinExtensions  *map[string][]IntelligenceBuiltinExtension
	BuiltinConditions  *map[string]IntelligenceCondition
}

type intelligenceRulesCASWriter interface {
	CompareAndSetIntelligenceRules(context.Context, string, string, string, bool) (bool, error)
}

func (s *SettingService) loadVersionedIntelligenceRules(ctx context.Context, revision string) (IntelligenceRules, string, bool, intelligenceRulesCASWriter, error) {
	if s == nil || s.settingRepo == nil {
		return IntelligenceRules{}, "", false, nil, errors.New("settings service unavailable")
	}
	_, revisionErr := hex.DecodeString(revision)
	if len(revision) != 64 || revisionErr != nil {
		return IntelligenceRules{}, "", false, nil, fmt.Errorf("%w: revision is required", ErrInvalidIntelligenceRules)
	}
	writer, ok := s.settingRepo.(intelligenceRulesCASWriter)
	if !ok {
		return IntelligenceRules{}, "", false, nil, errors.New("atomic settings update unavailable")
	}
	previous, err := s.settingRepo.GetValue(ctx, intelligenceRulesSettingKey)
	exists := err == nil
	if errors.Is(err, ErrSettingNotFound) {
		previous, err = "", nil
	}
	if err != nil {
		return IntelligenceRules{}, "", false, nil, err
	}
	current := IntelligenceRules{Rules: []IntelligenceRule{}}
	if previous != "" {
		if err := json.Unmarshal([]byte(previous), &current); err != nil {
			return IntelligenceRules{}, "", false, nil, errors.New("invalid saved intelligence routing rules")
		}
		current, err = prepareIntelligenceRules(current)
		if err != nil {
			return IntelligenceRules{}, "", false, nil, err
		}
	}
	if IntelligenceRulesRevision(current) != revision {
		return IntelligenceRules{}, "", false, nil, ErrIntelligenceRulesConflict
	}
	return current, previous, exists, writer, nil
}

func (s *SettingService) commitVersionedIntelligenceRules(ctx context.Context, updated IntelligenceRules, previous string, exists bool, writer intelligenceRulesCASWriter) (IntelligenceRules, error) {
	prepared, err := prepareIntelligenceRules(updated)
	if err != nil {
		return IntelligenceRules{}, err
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		return IntelligenceRules{}, err
	}
	changed, err := writer.CompareAndSetIntelligenceRules(ctx, intelligenceRulesSettingKey, previous, string(raw), exists)
	if err != nil {
		return IntelligenceRules{}, err
	}
	if !changed {
		return IntelligenceRules{}, ErrIntelligenceRulesConflict
	}
	s.intelligenceRulesCache = &prepared
	s.intelligenceRulesExpires = time.Now().Add(5 * time.Second)
	return cloneIntelligenceRules(prepared), nil
}

// ReplaceIntelligenceRulesIfRevision is used by the current admin UI. Legacy
// whole-form requests without a revision retain their previous behavior.
func (s *SettingService) ReplaceIntelligenceRulesIfRevision(ctx context.Context, replacement IntelligenceRulesReplacement) (IntelligenceRules, error) {
	if s == nil || s.settingRepo == nil {
		return IntelligenceRules{}, errors.New("settings service unavailable")
	}
	s.intelligenceRulesMu.Lock()
	defer s.intelligenceRulesMu.Unlock()
	current, previous, exists, writer, err := s.loadVersionedIntelligenceRules(ctx, replacement.Revision)
	if err != nil {
		return IntelligenceRules{}, err
	}
	updated := cloneIntelligenceRules(current)
	updated.Rules = replacement.Rules
	if replacement.BuiltinDisabledIDs != nil {
		updated.BuiltinDisabledIDs = *replacement.BuiltinDisabledIDs
	}
	if replacement.BuiltinExtensions != nil {
		updated.BuiltinExtensions = *replacement.BuiltinExtensions
	}
	if replacement.BuiltinConditions != nil {
		updated.BuiltinConditions = *replacement.BuiltinConditions
	}
	return s.commitVersionedIntelligenceRules(ctx, updated, previous, exists, writer)
}

func IntelligenceRulesRevision(value IntelligenceRules) string {
	// Default predicates are part of the editable rule set. An application
	// upgrade changing them must also invalidate a previously read revision.
	raw, _ := json.Marshal(struct {
		Settings IntelligenceRules                `json:"settings"`
		Builtins []IntelligenceBuiltinRule        `json:"builtins"`
		Defaults map[string]IntelligenceCondition `json:"defaults"`
	}{value, IntelligenceBuiltinRules(), DefaultIntelligenceBuiltinConditions()})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validateIntelligenceRulePatchFields(patch IntelligenceRulePatch) error {
	invalid := func() error { return fmt.Errorf("%w: fields do not match operation", ErrInvalidIntelligenceRules) }
	builtinFields := patch.BuiltinID != "" || patch.Enabled != nil || patch.Condition != nil || patch.ResetCondition || patch.Extensions != nil || patch.Extension != nil
	switch patch.Operation {
	case "add_rule":
		if patch.Rule == nil || patch.Index != nil || builtinFields {
			return invalid()
		}
	case "update_rule":
		if patch.Rule == nil || patch.Index == nil || builtinFields {
			return invalid()
		}
	case "delete_rule":
		if patch.Rule != nil || patch.Index == nil || builtinFields {
			return invalid()
		}
	case "update_builtin":
		if patch.Rule != nil || patch.Index != nil || patch.Extension != nil {
			return invalid()
		}
	case "add_builtin_extension":
		if patch.Rule != nil || patch.Index != nil || patch.Extension == nil || patch.Enabled != nil || patch.Condition != nil || patch.ResetCondition || patch.Extensions != nil {
			return invalid()
		}
	case "update_builtin_extension":
		if patch.Rule != nil || patch.Index == nil || patch.Extension == nil || patch.Enabled != nil || patch.Condition != nil || patch.ResetCondition || patch.Extensions != nil {
			return invalid()
		}
	case "delete_builtin_extension":
		if patch.Rule != nil || patch.Index == nil || patch.Extension != nil || patch.Enabled != nil || patch.Condition != nil || patch.ResetCondition || patch.Extensions != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

// PatchIntelligenceRules atomically applies a single operation. The storage
// compare-and-set protects changes made by another process or the admin UI.
func (s *SettingService) PatchIntelligenceRules(ctx context.Context, patch IntelligenceRulePatch) (IntelligenceRules, error) {
	if s == nil || s.settingRepo == nil {
		return IntelligenceRules{}, errors.New("settings service unavailable")
	}
	if err := validateIntelligenceRulePatchFields(patch); err != nil {
		return IntelligenceRules{}, err
	}
	s.intelligenceRulesMu.Lock()
	defer s.intelligenceRulesMu.Unlock()
	current, previous, exists, writer, err := s.loadVersionedIntelligenceRules(ctx, patch.Revision)
	if err != nil {
		return IntelligenceRules{}, err
	}
	updated := cloneIntelligenceRules(current)
	switch patch.Operation {
	case "add_rule":
		if patch.Rule == nil {
			return IntelligenceRules{}, fmt.Errorf("%w: rule is required", ErrInvalidIntelligenceRules)
		}
		updated.Rules = append(updated.Rules, *patch.Rule)
	case "update_rule", "delete_rule":
		if patch.Index == nil || *patch.Index < 0 || *patch.Index >= len(updated.Rules) {
			return IntelligenceRules{}, fmt.Errorf("%w: index is out of range", ErrInvalidIntelligenceRules)
		}
		if patch.Operation == "update_rule" {
			if patch.Rule == nil {
				return IntelligenceRules{}, fmt.Errorf("%w: rule is required", ErrInvalidIntelligenceRules)
			}
			updated.Rules[*patch.Index] = *patch.Rule
		} else {
			updated.Rules = append(updated.Rules[:*patch.Index], updated.Rules[*patch.Index+1:]...)
		}
	case "update_builtin":
		known := false
		for _, rule := range IntelligenceBuiltinRules() {
			if rule.ID == patch.BuiltinID {
				known = true
				break
			}
		}
		if !known {
			return IntelligenceRules{}, fmt.Errorf("%w: unknown built-in rule", ErrInvalidIntelligenceRules)
		}
		if patch.Enabled == nil && patch.Condition == nil && !patch.ResetCondition && patch.Extensions == nil {
			return IntelligenceRules{}, fmt.Errorf("%w: no built-in changes", ErrInvalidIntelligenceRules)
		}
		if patch.Condition != nil && patch.ResetCondition {
			return IntelligenceRules{}, fmt.Errorf("%w: condition and reset cannot be combined", ErrInvalidIntelligenceRules)
		}
		if patch.Enabled != nil {
			var ids []string
			for _, id := range updated.BuiltinDisabledIDs {
				if id != patch.BuiltinID {
					ids = append(ids, id)
				}
			}
			if !*patch.Enabled {
				ids = append(ids, patch.BuiltinID)
			}
			updated.BuiltinDisabledIDs = ids
		}
		if patch.ResetCondition {
			delete(updated.BuiltinConditions, patch.BuiltinID)
		}
		if patch.Condition != nil {
			if updated.BuiltinConditions == nil {
				updated.BuiltinConditions = make(map[string]IntelligenceCondition)
			}
			updated.BuiltinConditions[patch.BuiltinID] = *patch.Condition
		}
		if patch.Extensions != nil {
			if updated.BuiltinExtensions == nil {
				updated.BuiltinExtensions = make(map[string][]IntelligenceBuiltinExtension)
			}
			updated.BuiltinExtensions[patch.BuiltinID] = *patch.Extensions
		}
	case "add_builtin_extension", "update_builtin_extension", "delete_builtin_extension":
		known := false
		for _, rule := range IntelligenceBuiltinRules() {
			if rule.ID == patch.BuiltinID {
				known = true
				break
			}
		}
		if !known {
			return IntelligenceRules{}, fmt.Errorf("%w: unknown built-in rule", ErrInvalidIntelligenceRules)
		}
		if updated.BuiltinExtensions == nil {
			updated.BuiltinExtensions = make(map[string][]IntelligenceBuiltinExtension)
		}
		items := updated.BuiltinExtensions[patch.BuiltinID]
		if patch.Operation == "add_builtin_extension" {
			if patch.Extension == nil {
				return IntelligenceRules{}, fmt.Errorf("%w: extension is required", ErrInvalidIntelligenceRules)
			}
			items = append(items, *patch.Extension)
		} else {
			if patch.Index == nil || *patch.Index < 0 || *patch.Index >= len(items) {
				return IntelligenceRules{}, fmt.Errorf("%w: index is out of range", ErrInvalidIntelligenceRules)
			}
			if patch.Operation == "update_builtin_extension" {
				if patch.Extension == nil {
					return IntelligenceRules{}, fmt.Errorf("%w: extension is required", ErrInvalidIntelligenceRules)
				}
				items[*patch.Index] = *patch.Extension
			} else {
				items = append(items[:*patch.Index], items[*patch.Index+1:]...)
			}
		}
		if len(items) == 0 {
			delete(updated.BuiltinExtensions, patch.BuiltinID)
		} else {
			updated.BuiltinExtensions[patch.BuiltinID] = items
		}
	default:
		return IntelligenceRules{}, fmt.Errorf("%w: unknown operation", ErrInvalidIntelligenceRules)
	}
	return s.commitVersionedIntelligenceRules(ctx, updated, previous, exists, writer)
}
