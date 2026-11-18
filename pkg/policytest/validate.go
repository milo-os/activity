package policytest

import (
	"fmt"
	"regexp"
	"strconv"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"go.miloapis.com/activity/internal/registry/activity/policy"
	"go.miloapis.com/activity/pkg/apis/activity"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// rulePathRegex extracts the rule list and index from a field path such as
// "spec.auditRules[2].match".
var rulePathRegex = regexp.MustCompile(`^spec\.(auditRules|eventRules)\[(\d+)\]\.(\w+)$`)

// ValidatePolicies validates every policy with the same validation the API
// server runs on create/update, collecting all issues across all policies in
// a single pass.
func ValidatePolicies(loaded []LoadedPolicy) []ValidationIssue {
	var issues []ValidationIssue
	for _, item := range loaded {
		issues = append(issues, validateSinglePolicy(item)...)
	}
	return issues
}

func validateSinglePolicy(lp LoadedPolicy) []ValidationIssue {
	p := lp.Policy
	if p == nil {
		return []ValidationIssue{{
			Source:   lp.Source,
			Document: lp.Document,
			Message:  "policy is nil",
			Severity: SeverityError,
		}}
	}

	base := ValidationIssue{
		Source:     lp.Source,
		Document:   lp.Document,
		PolicyName: p.Name,
	}

	var issues []ValidationIssue

	// The API server checks object names generically; the tool checks them
	// here because there is no API server.
	if p.Name == "" {
		issue := base
		issue.Field = "metadata.name"
		issue.Message = "metadata.name is required"
		issue.Severity = SeverityError
		issues = append(issues, issue)
	} else if errs := validation.IsDNS1123Subdomain(p.Name); len(errs) > 0 {
		issue := base
		issue.Field = "metadata.name"
		issue.Message = fmt.Sprintf("metadata.name must be a valid DNS-1123 subdomain: %v", errs)
		issue.Severity = SeverityError
		issues = append(issues, issue)
	}

	internalSpec := toInternalSpec(&p.Spec)
	for _, fe := range policy.ValidateActivityPolicySpec(internalSpec, field.NewPath("spec")) {
		issue := base
		issue.Severity = SeverityError
		issue.Field = fe.Field
		issue.Message = fieldErrorMessage(fe)

		if m := rulePathRegex.FindStringSubmatch(fe.Field); m != nil {
			idx, _ := strconv.Atoi(m[2])
			rules := p.Spec.AuditRules
			issue.RuleType = "audit"
			if m[1] == "eventRules" {
				rules = p.Spec.EventRules
				issue.RuleType = "event"
			}
			if idx < len(rules) {
				issue.RuleName = rules[idx].Name
				switch m[3] {
				case "match":
					issue.Expression = rules[idx].Match
				case "summary":
					issue.Expression = rules[idx].Summary
				}
			}
		}
		issues = append(issues, issue)
	}

	// Mirrors the API server's create/update warning (warningsForPolicy in
	// internal/registry/activity/policy/strategy.go, which is unexported).
	if len(p.Spec.AuditRules) == 0 && len(p.Spec.EventRules) == 0 {
		issue := base
		issue.Severity = SeverityWarning
		issue.Message = "policy has no rules defined and will have no effect"
		issues = append(issues, issue)
	}

	return issues
}

// toInternalSpec converts to the internal API type so the API server's
// exported ValidateActivityPolicySpec can be called directly. A drift test
// (TestToInternalSpecCoversAllFields) fails if either type gains a field.
func toInternalSpec(in *v1alpha1.ActivityPolicySpec) *activity.ActivityPolicySpec {
	convert := func(rules []v1alpha1.ActivityPolicyRule) []activity.ActivityPolicyRule {
		out := make([]activity.ActivityPolicyRule, len(rules))
		for i, r := range rules {
			out[i] = activity.ActivityPolicyRule{
				Name:        r.Name,
				Description: r.Description,
				Match:       r.Match,
				Summary:     r.Summary,
			}
		}
		return out
	}
	return &activity.ActivityPolicySpec{
		Resource: activity.ActivityPolicyResource{
			APIGroup: in.Resource.APIGroup,
			Kind:     in.Resource.Kind,
		},
		AuditRules: convert(in.AuditRules),
		EventRules: convert(in.EventRules),
	}
}

// fieldErrorMessage renders a field error without repeating the bad value,
// which is reported separately as the issue's expression.
func fieldErrorMessage(fe *field.Error) string {
	switch fe.Type {
	case field.ErrorTypeInvalid:
		if fe.Detail != "" {
			return fe.Detail
		}
	case field.ErrorTypeDuplicate:
		return fmt.Sprintf("duplicate rule name %v", fe.BadValue)
	}
	return fe.ErrorBody()
}
