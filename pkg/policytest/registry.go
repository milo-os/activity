package policytest

import (
	"fmt"
	"reflect"
)

// policySet tracks every policy loaded during a run. The same file reached
// through several paths (e.g. --policies and a test's policy_files) is one
// policy: identity is the resolved source path plus document index. Two
// different sources defining the same metadata.name is an error, because the
// cluster can only hold one of them.
type policySet struct {
	// bySource maps a source document to its canonical policy; nil marks a
	// rejected duplicate so it is reported only once.
	bySource map[string]*LoadedPolicy
	byName   map[string]LoadedPolicy
	all      []LoadedPolicy
}

func newPolicySet() *policySet {
	return &policySet{
		bySource: make(map[string]*LoadedPolicy),
		byName:   make(map[string]LoadedPolicy),
	}
}

func sourceKey(lp LoadedPolicy) string {
	return fmt.Sprintf("%s#%d", absPath(lp.Source), lp.Document)
}

// add registers items. It returns the canonical policies for items (deduped,
// in input order), the policies that were new to the set, and duplicate-name
// issues.
func (s *policySet) add(items []LoadedPolicy) (canonical, added []LoadedPolicy, issues []ValidationIssue) {
	inCanonical := make(map[string]bool)
	appendCanonical := func(lp LoadedPolicy) {
		key := sourceKey(lp)
		if !inCanonical[key] {
			inCanonical[key] = true
			canonical = append(canonical, lp)
		}
	}

	for _, item := range items {
		key := sourceKey(item)
		if canon, ok := s.bySource[key]; ok {
			if canon != nil {
				appendCanonical(*canon)
			}
			continue
		}

		name := item.Policy.Name
		if name != "" {
			if first, ok := s.byName[name]; ok {
				// Identical content reached through different sources (e.g. a
				// kustomization and one of its component directories) is the
				// same object and therefore not a conflict.
				if reflect.DeepEqual(first.Policy.Spec, item.Policy.Spec) {
					s.bySource[key] = &first
					appendCanonical(first)
					continue
				}
				s.bySource[key] = nil
				issues = append(issues, ValidationIssue{
					Source:     item.Source,
					Document:   item.Document,
					PolicyName: name,
					Field:      "metadata.name",
					Message: fmt.Sprintf("duplicate ActivityPolicy name %q: defined in %s and in %s with different content; only one can exist in a cluster",
						name, first.Location(), item.Location()),
					Severity: SeverityError,
				})
				continue
			}
			s.byName[name] = item
		}

		canon := item
		s.bySource[key] = &canon
		s.all = append(s.all, item)
		added = append(added, item)
		appendCanonical(item)
	}
	return canonical, added, issues
}

// DeduplicatePolicies removes policies loaded more than once from the same
// source document and reports metadata.name collisions between different
// sources as errors naming both locations.
func DeduplicatePolicies(policies []LoadedPolicy) ([]LoadedPolicy, []ValidationIssue) {
	s := newPolicySet()
	_, _, issues := s.add(policies)
	return s.all, issues
}

// policyIsInvalid reports whether any error issue applies to lp, matched by
// source document or by policy name.
func policyIsInvalid(lp LoadedPolicy, issues []ValidationIssue) bool {
	for _, issue := range issues {
		if issue.Severity != SeverityError {
			continue
		}
		if issue.Document > 0 && issue.Document == lp.Document && absPath(issue.Source) == absPath(lp.Source) {
			return true
		}
		if issue.PolicyName != "" && lp.Policy != nil && issue.PolicyName == lp.Policy.Name {
			return true
		}
	}
	return false
}

// BuildPolicyReports summarizes policies; Valid is false when any error issue
// applies to the policy.
func BuildPolicyReports(policies []LoadedPolicy, issues []ValidationIssue) []PolicyReport {
	reports := make([]PolicyReport, 0, len(policies))
	for _, lp := range policies {
		p := lp.Policy
		reports = append(reports, PolicyReport{
			Name:       p.Name,
			Source:     lp.Source,
			Document:   lp.Document,
			APIGroup:   p.Spec.Resource.APIGroup,
			Kind:       p.Spec.Resource.Kind,
			AuditRules: len(p.Spec.AuditRules),
			EventRules: len(p.Spec.EventRules),
			Valid:      !policyIsInvalid(lp, issues),
		})
	}
	return reports
}
