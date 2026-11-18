package policytest

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// ValidatePolicies validates schema constraints and CEL compilation for every
// policy, collecting all issues across all policies in a single pass.
func ValidatePolicies(loaded []LoadedPolicy) []ValidationIssue {
	var issues []ValidationIssue

	for _, item := range loaded {
		issues = append(issues, validateSinglePolicy(item.Policy, item.Source)...)
	}

	return issues
}

func validateSinglePolicy(p *v1alpha1.ActivityPolicy, source string) []ValidationIssue {
	if p == nil {
		return []ValidationIssue{{
			Source:   source,
			Message:  "policy is nil",
			Severity: "error",
		}}
	}

	var issues []ValidationIssue

	if p.Name == "" {
		issues = append(issues, ValidationIssue{
			Source:   source,
			Field:    "metadata.name",
			Message:  "metadata.name is required",
			Severity: "error",
		})
	} else if errs := validation.IsDNS1123Subdomain(p.Name); len(errs) > 0 {
		issues = append(issues, ValidationIssue{
			Source:     source,
			PolicyName: p.Name,
			Field:      "metadata.name",
			Message:    fmt.Sprintf("metadata.name must be a valid DNS-1123 subdomain: %v", errs),
			Severity:   "error",
		})
	}

	if p.Spec.Resource.Kind == "" {
		issues = append(issues, ValidationIssue{
			Source:     source,
			PolicyName: p.Name,
			Field:      "spec.resource.kind",
			Message:    "spec.resource.kind is required (e.g. 'Deployment', 'Group')",
			Severity:   "error",
		})
	}

	issues = append(issues, validateRules(p.Name, source, "audit", "spec.auditRules", cel.AuditRule, p.Spec.AuditRules)...)
	issues = append(issues, validateRules(p.Name, source, "event", "spec.eventRules", cel.EventRule, p.Spec.EventRules)...)

	if len(p.Spec.AuditRules) == 0 && len(p.Spec.EventRules) == 0 {
		issues = append(issues, ValidationIssue{
			Source:     source,
			PolicyName: p.Name,
			Message:    "policy has no auditRules or eventRules defined and will have no effect",
			Severity:   "warning",
		})
	}

	return issues
}

func validateRules(
	policyName string,
	source string,
	ruleTypeLabel string,
	fieldPrefix string,
	ruleType cel.PolicyRuleType,
	rules []v1alpha1.ActivityPolicyRule,
) []ValidationIssue {
	var issues []ValidationIssue
	seenNames := make(map[string]int, len(rules))

	for i, rule := range rules {
		rulePath := fmt.Sprintf("%s[%d]", fieldPrefix, i)

		if rule.Name == "" {
			issues = append(issues, ValidationIssue{
				Source:     source,
				PolicyName: policyName,
				RuleType:   ruleTypeLabel,
				Field:      rulePath + ".name",
				Message:    "rule name is required",
				Severity:   "error",
			})
		} else {
			if errs := validation.IsDNS1123Subdomain(rule.Name); len(errs) > 0 {
				issues = append(issues, ValidationIssue{
					Source:     source,
					PolicyName: policyName,
					RuleType:   ruleTypeLabel,
					RuleName:   rule.Name,
					Field:      rulePath + ".name",
					Message:    fmt.Sprintf("rule name must be a valid DNS-1123 subdomain: %v", errs),
					Severity:   "error",
				})
			}
			if firstIdx, exists := seenNames[rule.Name]; exists {
				issues = append(issues, ValidationIssue{
					Source:     source,
					PolicyName: policyName,
					RuleType:   ruleTypeLabel,
					RuleName:   rule.Name,
					Field:      rulePath + ".name",
					Message:    fmt.Sprintf("duplicate rule name %q (conflicts with %s[%d])", rule.Name, fieldPrefix, firstIdx),
					Severity:   "error",
				})
			} else {
				seenNames[rule.Name] = i
			}
		}

		if rule.Match == "" {
			issues = append(issues, ValidationIssue{
				Source:     source,
				PolicyName: policyName,
				RuleType:   ruleTypeLabel,
				RuleName:   rule.Name,
				Field:      rulePath + ".match",
				Message:    "match expression is required",
				Severity:   "error",
			})
		} else if err := cel.ValidatePolicyExpression(rule.Match, cel.MatchExpression, ruleType); err != nil {
			issues = append(issues, ValidationIssue{
				Source:     source,
				PolicyName: policyName,
				RuleType:   ruleTypeLabel,
				RuleName:   rule.Name,
				Field:      rulePath + ".match",
				Expression: rule.Match,
				Message:    err.Error(),
				Severity:   "error",
			})
		}

		if rule.Summary == "" {
			issues = append(issues, ValidationIssue{
				Source:     source,
				PolicyName: policyName,
				RuleType:   ruleTypeLabel,
				RuleName:   rule.Name,
				Field:      rulePath + ".summary",
				Message:    "summary template is required",
				Severity:   "error",
			})
		} else if err := cel.ValidatePolicyExpression(rule.Summary, cel.SummaryExpression, ruleType); err != nil {
			issues = append(issues, ValidationIssue{
				Source:     source,
				PolicyName: policyName,
				RuleType:   ruleTypeLabel,
				RuleName:   rule.Name,
				Field:      rulePath + ".summary",
				Expression: rule.Summary,
				Message:    err.Error(),
				Severity:   "error",
			})
		}
	}

	return issues
}
