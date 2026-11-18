package policytest

import (
	"fmt"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// Issue severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// LoadedPolicy represents an ActivityPolicy loaded from a file or kustomization.
type LoadedPolicy struct {
	Policy *v1alpha1.ActivityPolicy
	// Source is the file, or the kustomization directory, the policy came from.
	Source string
	// Document is the 1-based YAML document index within Source (for a
	// kustomization, within its rendered output).
	Document int
}

// DisplayName identifies the policy for humans: its metadata.name, or its
// source location when the name is missing.
func (lp LoadedPolicy) DisplayName() string {
	name := ""
	if lp.Policy != nil {
		name = lp.Policy.Name
	}
	return displayName(name, lp.Source, lp.Document)
}

// Location returns "source, document N".
func (lp LoadedPolicy) Location() string {
	return location(lp.Source, lp.Document)
}

func location(source string, document int) string {
	if document > 0 {
		return fmt.Sprintf("%s, document %d", source, document)
	}
	return source
}

func displayName(name, source string, document int) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("<unnamed> (%s)", location(source, document))
}

// ValidationIssue describes a schema or CEL validation problem on a policy.
type ValidationIssue struct {
	Source     string `json:"source"`
	Document   int    `json:"document,omitempty"`
	PolicyName string `json:"policyName,omitempty"`
	RuleType   string `json:"ruleType,omitempty"`
	RuleName   string `json:"ruleName,omitempty"`
	Field      string `json:"field,omitempty"`
	Expression string `json:"expression,omitempty"`
	Message    string `json:"message"`
	Severity   string `json:"severity"`
}

// TestFile represents one declarative ActivityPolicy unit test group. A test
// file may hold several groups separated by "---".
type TestFile struct {
	PolicyFiles []string `json:"policy_files,omitempty"`
	RuleFiles   []string `json:"rule_files,omitempty"`
	// ResourceKinds declares resource-to-kind mappings per API group, e.g.
	// {"networking.datumapis.com": {"httpproxies": "HTTPProxy"}}. Production
	// resolves these with the cluster's RESTMapper; offline the tool otherwise
	// infers them from loaded policies or by English pluralization. Use "" or
	// "core" for the core group.
	ResourceKinds map[string]map[string]string `json:"resource_kinds,omitempty"`
	Tests         []TestCase                   `json:"tests"`
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
	ExpPolicy          string            `json:"exp_policy,omitempty"`
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
	Policy          string            `json:"policy,omitempty"`
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
	TestPaths           []string
	PolicyPaths         []string
	FailOnUntested      bool
	FailOnUntestedRules bool
}

// SuiteResult summarizes validation, test execution, and policy coverage.
type SuiteResult struct {
	ValidationIssues    []ValidationIssue `json:"validationIssues,omitempty"`
	Policies            []PolicyReport    `json:"policies,omitempty"`
	Files               []FileResult      `json:"files,omitempty"`
	UntestedPolicies    []string          `json:"untestedPolicies,omitempty"`
	UntestedRules       []UntestedRule    `json:"untestedRules,omitempty"`
	TotalPolicies       int               `json:"totalPolicies"`
	TotalRules          int               `json:"totalRules"`
	TestedRules         int               `json:"testedRules"`
	TotalTests          int               `json:"totalTests"`
	PassedTests         int               `json:"passedTests"`
	FailedTests         int               `json:"failedTests"`
	FailOnUntested      bool              `json:"failOnUntested,omitempty"`
	FailOnUntestedRules bool              `json:"failOnUntestedRules,omitempty"`
}

// UntestedRule identifies a rule that no test case matched.
type UntestedRule struct {
	Policy   string `json:"policy"`
	RuleType string `json:"ruleType"`
	Rule     string `json:"rule"`
}

// HasErrors reports whether any validation error, test failure, or enforced coverage gap occurred.
func (r *SuiteResult) HasErrors() bool {
	if countErrorIssues(r.ValidationIssues) > 0 {
		return true
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
	if r.FailOnUntestedRules && len(r.UntestedRules) > 0 {
		return true
	}
	return false
}

func countErrorIssues(issues []ValidationIssue) int {
	n := 0
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			n++
		}
	}
	return n
}

// PolicyReport summarizes validation and test coverage for a single policy.
type PolicyReport struct {
	Name          string   `json:"name"`
	Source        string   `json:"source"`
	Document      int      `json:"document,omitempty"`
	APIGroup      string   `json:"apiGroup"`
	Kind          string   `json:"kind"`
	AuditRules    int      `json:"auditRules"`
	EventRules    int      `json:"eventRules"`
	Tested        bool     `json:"tested"`
	TestCount     int      `json:"testCount"`
	UntestedRules []string `json:"untestedRules,omitempty"`
	Valid         bool     `json:"valid"`
}

// DisplayName identifies the report's policy for humans, mirroring LoadedPolicy.DisplayName.
func (p PolicyReport) DisplayName() string {
	return displayName(p.Name, p.Source, p.Document)
}

// FileResult captures the results of all test cases in one test file.
type FileResult struct {
	File    string       `json:"file"`
	Results []TestResult `json:"results,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// TestResult captures the outcome of a single test case.
type TestResult struct {
	Name string `json:"name"`
	// Document is the 1-based YAML document of the test file the test came from.
	Document int `json:"document,omitempty"`
	// Policy is the policy named by the test, or the matched policy.
	Policy string `json:"policy,omitempty"`
	Passed bool   `json:"passed"`
	// Matched reports whether any rule matched the input.
	Matched       bool   `json:"matched"`
	MatchedPolicy string `json:"matchedPolicy,omitempty"`
	MatchedRule   string `json:"matchedRule,omitempty"`
	ActualSummary string `json:"actualSummary,omitempty"`
	// EvaluatedPolicies lists the policies evaluated, in evaluation order.
	EvaluatedPolicies []string `json:"evaluatedPolicies,omitempty"`
	// Failures are human-readable failure messages (kept for compatibility).
	Failures []string `json:"failures,omitempty"`
	// FailureDetails holds a structured entry for each failure.
	FailureDetails []FailureDetail `json:"failureDetails,omitempty"`
	Warnings       []TestWarning   `json:"warnings,omitempty"`
}

// FailureDetail is the structured form of one test failure.
type FailureDetail struct {
	// Field is the asserted field (e.g. "summary", "links[0].resource.kind"),
	// or a category such as "test", "routing" or "evaluation" for test errors.
	Field    string `json:"field"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Message  string `json:"message"`
}

// Warning types reported on TestResult.Warnings.
const (
	// WarningRuleSkipped: a rule was skipped (compile error, or event match
	// expression failed at runtime), exactly as production would skip it.
	WarningRuleSkipped = "rule-skipped"
	// WarningKindGuessed: a link kind was inferred offline by pluralization.
	WarningKindGuessed = "kind-guessed"
	// WarningShadowed: another policy evaluated earlier in production order
	// matches this input first.
	WarningShadowed = "shadowed"
	// WarningNoTarget: no loaded policy targets the input's resource.
	WarningNoTarget = "no-target"
)

// TestWarning is a non-fatal note about how a test case was evaluated.
type TestWarning struct {
	Type       string `json:"type"`
	Policy     string `json:"policy,omitempty"`
	RuleType   string `json:"ruleType,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Expression string `json:"expression,omitempty"`
	Message    string `json:"message"`
}
