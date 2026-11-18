package policytest

import (
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// LoadedPolicy represents an ActivityPolicy loaded from a file or kustomization.
type LoadedPolicy struct {
	Policy *v1alpha1.ActivityPolicy
	Source string
}

// ValidationIssue describes a schema or CEL validation problem on a policy.
type ValidationIssue struct {
	Source     string `json:"source"`
	PolicyName string `json:"policyName,omitempty"`
	RuleType   string `json:"ruleType,omitempty"`
	RuleName   string `json:"ruleName,omitempty"`
	Field      string `json:"field,omitempty"`
	Expression string `json:"expression,omitempty"`
	Message    string `json:"message"`
	Severity   string `json:"severity"`
}

// TestFile represents a declarative ActivityPolicy unit test file.
type TestFile struct {
	PolicyFiles []string   `json:"policy_files,omitempty"`
	RuleFiles   []string   `json:"rule_files,omitempty"`
	Tests       []TestCase `json:"tests"`
}

// AllPolicyFiles returns the combined list of policy_files and rule_files.
func (tf *TestFile) AllPolicyFiles() []string {
	out := make([]string, 0, len(tf.PolicyFiles)+len(tf.RuleFiles))
	out = append(out, tf.PolicyFiles...)
	out = append(out, tf.RuleFiles...)
	return out
}

// TestCase defines a single input and expected Activity outcome.
type TestCase struct {
	Name   string         `json:"name"`
	Policy string         `json:"policy,omitempty"`
	Audit  *auditv1.Event `json:"audit,omitempty"`
	Event  map[string]any `json:"event,omitempty"`

	ExpNoMatch         bool              `json:"exp_no_match,omitempty"`
	ExpRule            string            `json:"exp_rule,omitempty"`
	ExpSummary         *string           `json:"exp_summary,omitempty"`
	ExpSummaryContains string            `json:"exp_summary_contains,omitempty"`
	ExpSummaryRegex    string            `json:"exp_summary_regex,omitempty"`
	ExpChangeSource    string            `json:"exp_change_source,omitempty"`
	ExpActor           *ExpectedActor    `json:"exp_actor,omitempty"`
	ExpResource        *ExpectedResource `json:"exp_resource,omitempty"`
	ExpTenant          *ExpectedTenant   `json:"exp_tenant,omitempty"`
	ExpLinks           []ExpectedLink    `json:"exp_links,omitempty"`
	ExpActivity        *ExpectedActivity `json:"exp_activity,omitempty"`
}

// ExpectedActor asserts fields on ActivitySpec.Actor.
type ExpectedActor struct {
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	UID   string `json:"uid,omitempty"`
	Email string `json:"email,omitempty"`
}

// ExpectedResource asserts fields on an ActivityResource.
// Pointer fields allow distinguishing omitted checks from explicit empty values.
type ExpectedResource struct {
	APIGroup   *string `json:"apiGroup,omitempty"`
	APIVersion string  `json:"apiVersion,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	Name       string  `json:"name,omitempty"`
	Namespace  *string `json:"namespace,omitempty"`
	UID        string  `json:"uid,omitempty"`
}

// ExpectedTenant asserts fields on ActivitySpec.Tenant.
type ExpectedTenant struct {
	Type string  `json:"type,omitempty"`
	Name *string `json:"name,omitempty"`
}

// ExpectedLink asserts a link entry on ActivitySpec.Links.
// Resource fields may be specified either nested under resource or directly as top-level shorthand.
type ExpectedLink struct {
	Marker     string           `json:"marker"`
	Resource   ExpectedResource `json:"resource,omitempty"`
	APIGroup   *string          `json:"apiGroup,omitempty"`
	APIVersion string           `json:"apiVersion,omitempty"`
	Kind       string           `json:"kind,omitempty"`
	Name       string           `json:"name,omitempty"`
	Namespace  *string          `json:"namespace,omitempty"`
	UID        string           `json:"uid,omitempty"`
}

// normalizedResource returns Resource merged with any top-level shorthand fields.
func (l ExpectedLink) normalizedResource() ExpectedResource {
	res := l.Resource
	if res.APIGroup == nil {
		res.APIGroup = l.APIGroup
	}
	if res.APIVersion == "" {
		res.APIVersion = l.APIVersion
	}
	if res.Kind == "" {
		res.Kind = l.Kind
	}
	if res.Name == "" {
		res.Name = l.Name
	}
	if res.Namespace == nil {
		res.Namespace = l.Namespace
	}
	if res.UID == "" {
		res.UID = l.UID
	}
	return res
}

// ExpectedActivity provides an optional grouped form of TestCase expectations.
type ExpectedActivity struct {
	NoMatch         bool              `json:"no_match,omitempty"`
	Rule            string            `json:"rule,omitempty"`
	Summary         *string           `json:"summary,omitempty"`
	SummaryContains string            `json:"summary_contains,omitempty"`
	SummaryRegex    string            `json:"summary_regex,omitempty"`
	ChangeSource    string            `json:"change_source,omitempty"`
	Actor           *ExpectedActor    `json:"actor,omitempty"`
	Resource        *ExpectedResource `json:"resource,omitempty"`
	Tenant          *ExpectedTenant   `json:"tenant,omitempty"`
	Links           []ExpectedLink    `json:"links,omitempty"`
}

// SuiteOptions configures a validation and unit-test run.
type SuiteOptions struct {
	TestPaths      []string
	PolicyPaths    []string
	FailOnUntested bool
}

// SuiteResult summarizes validation, test execution, and policy coverage.
type SuiteResult struct {
	ValidationIssues []ValidationIssue `json:"validationIssues,omitempty"`
	Policies         []PolicyReport    `json:"policies,omitempty"`
	Files            []FileResult      `json:"files,omitempty"`
	UntestedPolicies []string          `json:"untestedPolicies,omitempty"`
	TotalPolicies    int               `json:"totalPolicies"`
	TotalRules       int               `json:"totalRules"`
	TotalTests       int               `json:"totalTests"`
	PassedTests      int               `json:"passedTests"`
	FailedTests      int               `json:"failedTests"`
	FailOnUntested   bool              `json:"failOnUntested,omitempty"`
}

// HasErrors reports whether any validation error, test failure, or enforced coverage gap occurred.
func (r *SuiteResult) HasErrors() bool {
	for _, issue := range r.ValidationIssues {
		if issue.Severity == "error" {
			return true
		}
	}
	for _, file := range r.Files {
		if file.Error != "" {
			return true
		}
	}
	if r.FailedTests > 0 {
		return true
	}
	if r.FailOnUntested && len(r.UntestedPolicies) > 0 {
		return true
	}
	return false
}

// PolicyReport summarizes validation and test coverage for a single policy.
type PolicyReport struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	APIGroup   string `json:"apiGroup"`
	Kind       string `json:"kind"`
	AuditRules int    `json:"auditRules"`
	EventRules int    `json:"eventRules"`
	Tested     bool   `json:"tested"`
	TestCount  int    `json:"testCount"`
	Valid      bool   `json:"valid"`
}

// FileResult captures the results of all test cases in one test file.
type FileResult struct {
	File    string       `json:"file"`
	Results []TestResult `json:"results,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// TestResult captures the outcome of a single test case.
type TestResult struct {
	Name          string   `json:"name"`
	Policy        string   `json:"policy,omitempty"`
	Passed        bool     `json:"passed"`
	Matched       bool     `json:"matched"`
	MatchedRule   string   `json:"matchedRule,omitempty"`
	ActualSummary string   `json:"actualSummary,omitempty"`
	Failures      []string `json:"failures,omitempty"`
}
