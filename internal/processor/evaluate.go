package processor

import (
	"encoding/json"
	"fmt"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// EvaluationResult contains the result of evaluating policy rules against an input.
type EvaluationResult struct {
	// Activity is the generated activity, or nil if no rule matched
	Activity *v1alpha1.Activity

	// MatchedRuleIndex is the index of the rule that matched, or -1 if none matched
	MatchedRuleIndex int

	// MatchedRuleType is "audit" or "event" depending on which rule matched
	MatchedRuleType string

	// MatchedRuleName is the name of the matched rule from the policy spec
	MatchedRuleName string
}

// EvaluateAuditRules evaluates audit rules against an audit log input.
// Returns the generated Activity if a rule matches, or nil if no rule matched.
// If resolveKind is provided, it will be used to resolve resource names to Kind in links.
//
// Use EvaluateAuditRulesWithResolver to also enrich activities with user
// display names; this convenience wrapper forwards a nil resolver.
func EvaluateAuditRules(
	spec *v1alpha1.ActivityPolicySpec,
	audit *auditv1.Event,
	resolveKind KindResolver,
) (*EvaluationResult, error) {
	return EvaluateAuditRulesWithResolver(spec, audit, resolveKind, nil)
}

// EvaluateAuditRulesWithResolver is like EvaluateAuditRules but additionally
// enriches the resulting Activity with display names looked up via resolver.
func EvaluateAuditRulesWithResolver(
	spec *v1alpha1.ActivityPolicySpec,
	audit *auditv1.Event,
	resolveKind KindResolver,
	resolver UserResolver,
) (*EvaluationResult, error) {
	// Convert to map for CEL evaluation
	auditMap, err := toMap(audit)
	if err != nil {
		return nil, fmt.Errorf("failed to convert audit data: %w", err)
	}

	// Create activity builder
	builder := &ActivityBuilder{
		APIGroup:     spec.Resource.APIGroup,
		Kind:         spec.Resource.Kind,
		UserResolver: resolver,
	}

	// Try each audit rule in order
	for i, rule := range spec.AuditRules {
		matched, err := cel.EvaluateAuditMatchMap(rule.Match, auditMap)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate rule %d match: %w", i, err)
		}

		if matched {
			// Generate summary
			summary, links, err := cel.EvaluateAuditSummaryMap(rule.Summary, auditMap)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate rule %d summary: %w", i, err)
			}

			// Build the Activity
			activity, err := builder.BuildFromAudit(audit, summary, links, resolveKind)
			if err != nil {
				return nil, fmt.Errorf("failed to build activity for rule %d: %w", i, err)
			}

			return &EvaluationResult{
				Activity:         activity,
				MatchedRuleIndex: i,
				MatchedRuleType:  "audit",
				MatchedRuleName:  rule.Name,
			}, nil
		}
	}

	// No rule matched
	return &EvaluationResult{
		MatchedRuleIndex: -1,
	}, nil
}

// EvaluateEventRules evaluates event rules against a Kubernetes event input.
// Returns the generated Activity if a rule matches, or nil if no rule matched.
// If resolveKind is provided, it will be used to resolve resource names to Kind in links.
//
// Use EvaluateEventRulesWithResolver to also enrich activities with user
// display names; this convenience wrapper forwards a nil resolver.
func EvaluateEventRules(
	spec *v1alpha1.ActivityPolicySpec,
	eventData interface{},
	resolveKind KindResolver,
) (*EvaluationResult, error) {
	return EvaluateEventRulesWithResolver(spec, eventData, resolveKind, nil)
}

// EvaluateEventRulesWithResolver is like EvaluateEventRules but additionally
// enriches User-typed link targets with display names looked up via resolver.
func EvaluateEventRulesWithResolver(
	spec *v1alpha1.ActivityPolicySpec,
	eventData interface{},
	resolveKind KindResolver,
	resolver UserResolver,
) (*EvaluationResult, error) {
	// Convert event data to map if needed
	eventMap, err := toMap(eventData)
	if err != nil {
		return nil, fmt.Errorf("failed to convert event data: %w", err)
	}

	// Create activity builder
	builder := &ActivityBuilder{
		APIGroup:     spec.Resource.APIGroup,
		Kind:         spec.Resource.Kind,
		UserResolver: resolver,
	}

	// Try each event rule in order
	for i, rule := range spec.EventRules {
		matched, err := cel.EvaluateEventMatch(rule.Match, eventMap)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate rule %d match: %w", i, err)
		}

		if matched {
			// Generate summary
			summary, links, err := cel.EvaluateEventSummary(rule.Summary, eventMap)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate rule %d summary: %w", i, err)
			}

			// Build the Activity
			activity, err := builder.BuildFromEvent(eventMap, summary, links, resolveKind)
			if err != nil {
				return nil, fmt.Errorf("failed to build activity for rule %d: %w", i, err)
			}

			return &EvaluationResult{
				Activity:         activity,
				MatchedRuleIndex: i,
				MatchedRuleType:  "event",
				MatchedRuleName:  rule.Name,
			}, nil
		}
	}

	// No rule matched
	return &EvaluationResult{
		MatchedRuleIndex: -1,
	}, nil
}

// toMap converts various input types to map[string]interface{}.
func toMap(data interface{}) (map[string]interface{}, error) {
	switch v := data.(type) {
	case map[string]interface{}:
		return v, nil
	case *map[string]interface{}:
		if v == nil {
			return nil, fmt.Errorf("nil map pointer")
		}
		return *v, nil
	default:
		// Try JSON marshaling as a fallback
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal data: %w", err)
		}

		var m map[string]interface{}
		if err := json.Unmarshal(jsonData, &m); err != nil {
			return nil, fmt.Errorf("failed to unmarshal to map: %w", err)
		}
		return m, nil
	}
}
