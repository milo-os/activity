package policytest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.miloapis.com/activity/pkg/apis/activity"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func policyYAML(name, group, kind, rules string) string {
	return `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: ` + name + `
spec:
  resource:
    apiGroup: ` + group + `
    kind: ` + kind + `
` + rules
}

func issuesContaining(issues []ValidationIssue, severity, substr string) []ValidationIssue {
	var out []ValidationIssue
	for _, issue := range issues {
		if issue.Severity == severity && strings.Contains(issue.Message, substr) {
			out = append(out, issue)
		}
	}
	return out
}

// routingPolicies has two policies for the same resource (a-groups is
// evaluated first and shadows b-groups) plus an unrelated Role policy.
var routingPolicies = map[string]string{
	"policies/a-groups.yaml": policyYAML("a-groups", "iam.miloapis.com", "Group", `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "A {{ audit.objectRef.name }}"
`),
	"policies/b-groups.yaml": policyYAML("b-groups", "iam.miloapis.com", "Group", `  auditRules:
    - name: create-admins
      match: "audit.verb == 'create' && audit.objectRef.name == 'admins'"
      summary: "B {{ audit.objectRef.name }}"
`),
	"policies/roles.yaml": policyYAML("roles", "iam.miloapis.com", "Role", `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "role {{ audit.objectRef.name }}"
  eventRules:
    - name: ready
      match: "event.reason == 'Ready'"
      summary: "role {{ event.regarding.name }} ready"
`),
}

const groupAudit = `    audit:
      verb: create
      user:
        username: alice
      objectRef:
        apiGroup: iam.miloapis.com
        resource: groups
        name: admins
`

func runSingleCase(t *testing.T, policies map[string]string, header, testCase string) (*SuiteResult, TestResult) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, policies)
	writeFiles(t, dir, map[string]string{"case.test.yaml": header + "tests:\n" + testCase})
	res, err := RunTestSuite(SuiteOptions{TestPaths: []string{filepath.Join(dir, "case.test.yaml")}})
	if err != nil {
		t.Fatalf("RunTestSuite: %v", err)
	}
	if len(res.Files) != 1 || res.Files[0].Error != "" || len(res.Files[0].Results) != 1 {
		t.Fatalf("unexpected file results: %+v", res.Files)
	}
	return res, res.Files[0].Results[0]
}

func TestRoutingAndExpectations(t *testing.T) {
	const header = "policy_files:\n  - policies\n"

	tests := []struct {
		name         string
		testCase     string
		wantPass     bool
		wantPolicy   string
		wantRule     string
		wantFailure  string
		wantField    string
		wantWarning  string
		wantEvalured []string
	}{
		{
			name: "cross-policy shadowing: first policy in name order wins",
			testCase: `  - name: t
` + groupAudit + `    exp_policy: b-groups
`,
			wantPolicy:   "a-groups",
			wantRule:     "create",
			wantFailure:  `exp: "b-groups"`,
			wantField:    "policy",
			wantEvalured: []string{"a-groups"},
		},
		{
			name: "exp_policy passes for the policy production picks",
			testCase: `  - name: t
` + groupAudit + `    exp_policy: a-groups
    exp_summary: "A admins"
`,
			wantPass:   true,
			wantPolicy: "a-groups",
			wantRule:   "create",
		},
		{
			name: "explicit policy evaluates only that policy and warns about shadowing",
			testCase: `  - name: t
    policy: b-groups
` + groupAudit + `    exp_rule: create-admins
`,
			wantPass:     true,
			wantPolicy:   "b-groups",
			wantRule:     "create-admins",
			wantWarning:  WarningShadowed,
			wantEvalured: []string{"b-groups"},
		},
		{
			name: "explicit policy that does not target the audit resource fails",
			testCase: `  - name: t
    policy: roles
` + groupAudit + `    exp_no_match: true
`,
			wantFailure: "production never evaluates this policy",
			wantField:   "routing",
		},
		{
			name: "explicit policy that does not target the event subject fails",
			testCase: `  - name: t
    policy: a-groups
    event:
      reason: Ready
      regarding:
        apiVersion: iam.miloapis.com/v1alpha1
        kind: Role
        name: viewer
    exp_no_match: true
`,
			wantFailure: "regarding/involvedObject targets iam.miloapis.com/Role",
			wantField:   "routing",
		},
		{
			name: "missing target policy fails a positive test",
			testCase: `  - name: t
    audit:
      verb: create
      objectRef:
        apiGroup: iam.miloapis.com
        resource: widgets
    exp_rule: create
`,
			wantFailure: "no loaded policy targets iam.miloapis.com/widgets",
			wantField:   "routing",
		},
		{
			name: "missing target policy passes exp_no_match with a warning",
			testCase: `  - name: t
    audit:
      verb: create
      objectRef:
        apiGroup: iam.miloapis.com
        resource: widgets
    exp_no_match: true
`,
			wantPass:    true,
			wantWarning: WarningNoTarget,
		},
		{
			name: "ambiguous policy reference fails",
			testCase: `  - name: t
    policy: iam.miloapis.com/Group
` + groupAudit + `    exp_rule: create
`,
			wantFailure: "is ambiguous (a-groups, b-groups)",
			wantField:   "routing",
		},
		{
			name: "unknown policy reference fails",
			testCase: `  - name: t
    policy: nope
` + groupAudit + `    exp_rule: create
`,
			wantFailure: `policy "nope" not found`,
			wantField:   "routing",
		},
		{
			name: "exp_no_match combined with exp_rule is a test error",
			testCase: `  - name: t
` + groupAudit + `    exp_no_match: true
    exp_rule: create
`,
			wantFailure: "exp_no_match: true cannot be combined with positive assertions (exp_rule)",
			wantField:   "test",
		},
		{
			name: "exp_activity.no_match combined with nested summary is a test error",
			testCase: `  - name: t
` + groupAudit + `    exp_activity:
      no_match: true
      summary: "A admins"
`,
			wantFailure: "cannot be combined with positive assertions (exp_summary)",
			wantField:   "test",
		},
		{
			name: "invalid exp_summary_regex is reported even when nothing matches",
			testCase: `  - name: t
    audit:
      verb: get
      objectRef:
        apiGroup: iam.miloapis.com
        resource: groups
    exp_summary_regex: "A ("
`,
			wantFailure: "invalid exp_summary_regex",
			wantField:   "summary_regex",
		},
		{
			name: "audit event without objectRef is ignored by production",
			testCase: `  - name: t
    audit:
      verb: create
    exp_rule: create
`,
			wantFailure: "no objectRef",
			wantField:   "routing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, tr := runSingleCase(t, routingPolicies, header, tt.testCase)
			if tr.Passed != tt.wantPass {
				t.Fatalf("Passed = %v, want %v; failures=%v warnings=%+v", tr.Passed, tt.wantPass, tr.Failures, tr.Warnings)
			}
			if tt.wantPolicy != "" && tr.MatchedPolicy != tt.wantPolicy {
				t.Errorf("MatchedPolicy = %q, want %q", tr.MatchedPolicy, tt.wantPolicy)
			}
			if tt.wantRule != "" && tr.MatchedRule != tt.wantRule {
				t.Errorf("MatchedRule = %q, want %q", tr.MatchedRule, tt.wantRule)
			}
			if tt.wantFailure != "" {
				if !strings.Contains(strings.Join(tr.Failures, "\n"), tt.wantFailure) {
					t.Errorf("failures %q do not contain %q", tr.Failures, tt.wantFailure)
				}
				if len(tr.FailureDetails) != len(tr.Failures) {
					t.Errorf("FailureDetails (%d) not aligned with Failures (%d)", len(tr.FailureDetails), len(tr.Failures))
				} else if tr.FailureDetails[0].Field != tt.wantField {
					t.Errorf("FailureDetails[0].Field = %q, want %q", tr.FailureDetails[0].Field, tt.wantField)
				}
			}
			if tt.wantWarning != "" {
				found := false
				for _, w := range tr.Warnings {
					found = found || w.Type == tt.wantWarning
				}
				if !found {
					t.Errorf("expected %s warning, got %+v", tt.wantWarning, tr.Warnings)
				}
			}
			if tt.wantEvalured != nil && !reflect.DeepEqual(tr.EvaluatedPolicies, tt.wantEvalured) {
				t.Errorf("EvaluatedPolicies = %v, want %v", tr.EvaluatedPolicies, tt.wantEvalured)
			}
		})
	}
}

func TestStructuredFailureDetails(t *testing.T) {
	_, tr := runSingleCase(t, routingPolicies, "policy_files:\n  - policies\n", `  - name: t
`+groupAudit+`    exp_summary: "nope"
`)
	want := FailureDetail{Field: "summary", Expected: "nope", Actual: "A admins"}
	if len(tr.FailureDetails) != 1 {
		t.Fatalf("FailureDetails = %+v", tr.FailureDetails)
	}
	got := tr.FailureDetails[0]
	got.Message = ""
	if got != want {
		t.Errorf("FailureDetails[0] = %+v, want %+v", got, want)
	}
}

func TestRuleErrorsFollowProductionSemantics(t *testing.T) {
	policies := map[string]string{
		"policies/events.yaml": policyYAML("pods", "", "Pod", `  eventRules:
    - name: by-label
      match: "event.regarding.labels.tier == 'web'"
      summary: "labelled {{ event.regarding.name }}"
    - name: scheduled
      match: "event.reason == 'Scheduled'"
      summary: "{{ event.regarding.name }} scheduled"
`),
		"policies/a-audit.yaml": policyYAML("a-services", "", "Service", `  auditRules:
    - name: by-response
      match: "audit.responseObject.metadata.name == 'x'"
      summary: "never"
`),
		"policies/b-audit.yaml": policyYAML("b-services", "", "Service", `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "created {{ audit.objectRef.name }}"
`),
	}

	t.Run("event match error skips the rule with a warning", func(t *testing.T) {
		_, tr := runSingleCase(t, policies, "policy_files:\n  - policies\n", `  - name: t
    event:
      reason: Scheduled
      regarding:
        apiVersion: v1
        kind: Pod
        name: web-0
    exp_rule: scheduled
    exp_summary: "web-0 scheduled"
`)
		if !tr.Passed {
			t.Fatalf("expected pass, failures=%v", tr.Failures)
		}
		if len(tr.Warnings) != 1 {
			t.Fatalf("expected 1 warning, got %+v", tr.Warnings)
		}
		w := tr.Warnings[0]
		if w.Type != WarningRuleSkipped || w.Policy != "pods" || w.Rule != "by-label" || w.RuleType != "event" ||
			w.Expression != "event.regarding.labels.tier == 'web'" {
			t.Errorf("unexpected warning %+v", w)
		}
	})

	t.Run("audit runtime error is a failure and evaluation continues with the next policy", func(t *testing.T) {
		_, tr := runSingleCase(t, policies, "policy_files:\n  - policies\n", `  - name: t
    audit:
      verb: create
      objectRef:
        resource: services
        name: web
    exp_policy: b-services
    exp_summary: "created web"
`)
		if tr.Passed || tr.MatchedPolicy != "b-services" {
			t.Fatalf("expected failure with b-services matched, got passed=%v matched=%q", tr.Passed, tr.MatchedPolicy)
		}
		msg := strings.Join(tr.Failures, "\n")
		if !strings.Contains(msg, `policy "a-services" audit rule "by-response"`) ||
			!strings.Contains(msg, "audit.responseObject.metadata.name == 'x'") {
			t.Errorf("failure should name policy, rule and expression: %s", msg)
		}
	})
}

func TestKindResolution(t *testing.T) {
	policies := map[string]string{
		"policies/proxy.yaml": policyYAML("proxies", "example.com", "Proxy", `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ link(audit.objectRef.name, audit.objectRef) }} {{ link('w', {'apiGroup': 'example.com', 'resource': 'widgets', 'name': 'w'}) }}"
`),
	}
	testCase := `  - name: t
    audit:
      verb: create
      objectRef:
        apiGroup: example.com
        resource: proxyz
        name: p1
    exp_links:
      - marker: p1
        kind: Proxy
      - marker: w
        kind: Widget
`

	t.Run("irregular resource is unroutable without resource_kinds", func(t *testing.T) {
		_, tr := runSingleCase(t, policies, "policy_files:\n  - policies\n", testCase)
		if tr.Passed || !strings.Contains(strings.Join(tr.Failures, "\n"), "no loaded policy targets example.com/proxyz") {
			t.Fatalf("expected routing failure, got %+v", tr)
		}
	})

	t.Run("resource_kinds routes the policy and resolves link kinds; guesses warn", func(t *testing.T) {
		header := "policy_files:\n  - policies\nresource_kinds:\n  example.com:\n    proxyz: Proxy\n"
		_, tr := runSingleCase(t, policies, header, testCase)
		if !tr.Passed {
			t.Fatalf("expected pass, failures=%v", tr.Failures)
		}
		var guessed []TestWarning
		for _, w := range tr.Warnings {
			if w.Type == WarningKindGuessed {
				guessed = append(guessed, w)
			}
		}
		if len(guessed) != 1 || !strings.Contains(guessed[0].Message, `"widgets"`) {
			t.Errorf("expected exactly one kind-guessed warning for widgets, got %+v", tr.Warnings)
		}
	})
}

func TestDuplicatePolicies(t *testing.T) {
	const rules = `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "created {{ audit.objectRef.name }}"
`
	const altRules = `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "made {{ audit.objectRef.name }}"
`
	const testFile = `policy_files:
  - %s
tests:
  - name: t
` + groupAudit + `    exp_rule: create
`

	tests := []struct {
		name        string
		files       map[string]string
		policyPaths []string
		testRef     string
		wantErr     bool
		wantTotal   int
	}{
		{
			name: "same name in two files is an error naming both",
			files: map[string]string{
				"one/p.yaml": policyYAML("groups", "iam.miloapis.com", "Group", rules),
				"two/p.yaml": policyYAML("groups", "iam.miloapis.com", "Group", altRules),
			},
			policyPaths: []string{"one"},
			testRef:     "two",
			wantErr:     true,
			wantTotal:   1,
		},
		{
			name: "same file via --policies and policy_files is not a duplicate",
			files: map[string]string{
				"one/p.yaml": policyYAML("groups", "iam.miloapis.com", "Group", rules),
			},
			policyPaths: []string{"one"},
			testRef:     "one/p.yaml",
			wantTotal:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tt.files)
			writeFiles(t, dir, map[string]string{"d.test.yaml": strings.Replace(testFile, "%s", tt.testRef, 1)})
			var policyPaths []string
			for _, p := range tt.policyPaths {
				policyPaths = append(policyPaths, filepath.Join(dir, p))
			}
			res, err := RunTestSuite(SuiteOptions{
				TestPaths:   []string{filepath.Join(dir, "d.test.yaml")},
				PolicyPaths: policyPaths,
			})
			if err != nil {
				t.Fatal(err)
			}
			dups := issuesContaining(res.ValidationIssues, SeverityError, "duplicate ActivityPolicy name")
			if tt.wantErr {
				if len(dups) != 1 || !strings.Contains(dups[0].Message, filepath.Join("one", "p.yaml")) ||
					!strings.Contains(dups[0].Message, filepath.Join("two", "p.yaml")) {
					t.Fatalf("expected one duplicate error naming both files, got %+v", res.ValidationIssues)
				}
				if !res.HasErrors() {
					t.Error("expected HasErrors")
				}
			} else if len(dups) != 0 || res.HasErrors() {
				t.Fatalf("unexpected errors: %+v files=%+v", res.ValidationIssues, res.Files)
			}
			if res.TotalPolicies != tt.wantTotal {
				t.Errorf("TotalPolicies = %d, want %d", res.TotalPolicies, tt.wantTotal)
			}
		})
	}

	t.Run("DeduplicatePolicies for check", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"a.yaml": policyYAML("groups", "iam.miloapis.com", "Group", rules),
			"b.yaml": policyYAML("groups", "iam.miloapis.com", "Group", altRules),
		})
		loaded, _ := LoadPoliciesFromPaths([]string{dir, filepath.Join(dir, "a.yaml")})
		policies, issues := DeduplicatePolicies(loaded)
		if len(policies) != 1 || len(issuesContaining(issues, SeverityError, "duplicate ActivityPolicy name")) != 1 {
			t.Fatalf("policies=%d issues=%+v", len(policies), issues)
		}
	})
}

func TestBrokenPolicyIssuesAreNeverDropped(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"good/groups.yaml": policyYAML("groups", "iam.miloapis.com", "Group", `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "created"
`),
		"broken.yaml": policyYAML("broken", "iam.miloapis.com", "Group", `  unknownField: true
`),
		"drop.test.yaml": `policy_files:
  - good
  - broken.yaml
tests:
  - name: t
` + groupAudit + `    exp_rule: create
`,
	})
	res, err := RunTestSuite(SuiteOptions{
		TestPaths:   []string{filepath.Join(dir, "drop.test.yaml")},
		PolicyPaths: []string{filepath.Join(dir, "good")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(issuesContaining(res.ValidationIssues, SeverityError, "invalid ActivityPolicy schema")) != 1 {
		t.Fatalf("schema error for broken.yaml was dropped: %+v", res.ValidationIssues)
	}
	if !res.HasErrors() {
		t.Error("expected HasErrors")
	}
}

func TestMultiDocumentTestFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, routingPolicies)
	writeFiles(t, dir, map[string]string{
		"roles-only/roles.yaml": routingPolicies["policies/roles.yaml"],
		"multi.test.yaml": `policy_files:
  - policies
tests:
  - name: first group passes
` + groupAudit + `    exp_policy: a-groups
---
# second group uses a different policy set
policy_files:
  - roles-only
tests:
  - name: second group fails
` + groupAudit + `    exp_rule: create
`,
	})
	res, err := RunTestSuite(SuiteOptions{TestPaths: []string{filepath.Join(dir, "multi.test.yaml")}})
	if err != nil {
		t.Fatal(err)
	}
	f := res.Files[0]
	if f.Error != "" || len(f.Results) != 2 {
		t.Fatalf("expected 2 results from 2 documents, got %+v", f)
	}
	if !f.Results[0].Passed || f.Results[0].Document != 1 {
		t.Errorf("first result: %+v", f.Results[0])
	}
	if f.Results[1].Passed || f.Results[1].Document != 2 {
		t.Errorf("second result should fail (no groups policy in its group): %+v", f.Results[1])
	}
	if res.TotalTests != 2 || res.FailedTests != 1 {
		t.Errorf("counts total=%d failed=%d", res.TotalTests, res.FailedTests)
	}
}

func TestLoaderKustomizeAndKinds(t *testing.T) {
	const good = `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "created"
`
	tests := []struct {
		name         string
		files        map[string]string
		paths        []string
		wantPolicies int
		wantSeverity string
		wantMessage  string
	}{
		{
			name: "kustomize build error is reported",
			files: map[string]string{
				"k/kustomization.yaml": "resources:\n  - missing.yaml\n",
			},
			paths:        []string{"k"},
			wantSeverity: SeverityError,
			wantMessage:  `resource "missing.yaml" not found`,
		},
		{
			name: "remote bases fail offline",
			files: map[string]string{
				"k/kustomization.yaml": "resources:\n  - https://github.com/example/repo//policies?ref=main\n",
			},
			paths:        []string{"k"},
			wantSeverity: SeverityError,
			wantMessage:  "offline: remote bases not supported",
		},
		{
			name: "files outside the kustomization root are rejected",
			files: map[string]string{
				"outside.yaml":         policyYAML("outside", "iam.miloapis.com", "Group", good),
				"k/kustomization.yaml": "resources:\n  - ../outside.yaml\n",
			},
			paths:        []string{"k"},
			wantSeverity: SeverityError,
			wantMessage:  "failed to build kustomization",
		},
		{
			name: "patch file used by a rendered kustomization is not loaded standalone",
			files: map[string]string{
				"k/kustomization.yaml": "resources:\n  - groups.yaml\npatches:\n  - path: patch.yaml\n",
				"k/groups.yaml":        policyYAML("groups", "iam.miloapis.com", "Group", good),
				"k/patch.yaml": `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: groups
spec:
  resource:
    kind: Group
`,
			},
			paths:        []string{"k", "k/patch.yaml"},
			wantPolicies: 1,
			wantSeverity: SeverityWarning,
			wantMessage:  "kustomize patch",
		},
		{
			name: "typo'd kind in the activity group warns",
			files: map[string]string{
				"p.yaml": strings.Replace(policyYAML("groups", "iam.miloapis.com", "Group", good), "kind: ActivityPolicy", "kind: ActivtyPolicy", 1),
			},
			paths:        []string{"p.yaml"},
			wantSeverity: SeverityWarning,
			wantMessage:  `unexpected kind "ActivtyPolicy"`,
		},
		{
			name: "wrong-case kind warns",
			files: map[string]string{
				"p.yaml": strings.Replace(policyYAML("groups", "iam.miloapis.com", "Group", good), "kind: ActivityPolicy", "kind: activitypolicy", 1),
			},
			paths:        []string{"p.yaml"},
			wantSeverity: SeverityWarning,
			wantMessage:  "case-sensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tt.files)
			var paths []string
			for _, p := range tt.paths {
				paths = append(paths, filepath.Join(dir, p))
			}
			policies, issues := LoadPoliciesFromPaths(paths)
			if len(policies) != tt.wantPolicies {
				t.Errorf("loaded %d policies, want %d", len(policies), tt.wantPolicies)
			}
			if len(issuesContaining(issues, tt.wantSeverity, tt.wantMessage)) == 0 {
				t.Errorf("expected %s issue containing %q, got %+v", tt.wantSeverity, tt.wantMessage, issues)
			}
		})
	}
}

func TestUnnamedPolicyIsInvalidAndIdentifiedBySource(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"p.yaml": "---\n" + strings.Replace(policyYAML("x", "iam.miloapis.com", "Group", `  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "created"
`), "  name: x\n", "  labels: {}\n", 1),
	})
	policies, issues := LoadPoliciesFromPaths([]string{dir})
	issues = append(issues, ValidatePolicies(policies)...)
	reports := BuildPolicyReports(policies, issues)
	if len(reports) != 1 {
		t.Fatalf("reports = %+v", reports)
	}
	if reports[0].Valid {
		t.Error("unnamed policy must not be reported valid")
	}
	if got := reports[0].DisplayName(); !strings.Contains(got, "p.yaml, document 1") {
		t.Errorf("DisplayName = %q, want source and document", got)
	}
}

func TestCoverage(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, routingPolicies)
	writeFiles(t, dir, map[string]string{
		"c.test.yaml": `policy_files:
  - policies
tests:
  - name: evaluated
` + groupAudit + `    exp_rule: create
  - name: errored before evaluation counts for nothing
    policy: roles
` + groupAudit + `    exp_no_match: true
    exp_rule: create
`,
	})
	res, err := RunTestSuite(SuiteOptions{
		TestPaths:           []string{filepath.Join(dir, "c.test.yaml")},
		FailOnUntestedRules: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	tested := map[string]bool{}
	for _, p := range res.Policies {
		tested[p.Name] = p.Tested
	}
	if !tested["a-groups"] || tested["b-groups"] || tested["roles"] {
		t.Errorf("tested = %v; want only a-groups (b-groups is never reached, roles' only test errored)", tested)
	}
	var untested []string
	for _, r := range res.UntestedRules {
		untested = append(untested, r.Policy+":"+r.RuleType+"/"+r.Rule)
	}
	want := []string{"b-groups:audit/create-admins", "roles:audit/create", "roles:event/ready"}
	if !reflect.DeepEqual(untested, want) {
		t.Errorf("UntestedRules = %v, want %v", untested, want)
	}
	if res.TotalRules != 4 || res.TestedRules != 1 {
		t.Errorf("TotalRules=%d TestedRules=%d", res.TotalRules, res.TestedRules)
	}
	if !res.HasErrors() {
		t.Error("FailOnUntestedRules should make the suite fail")
	}
}

// TestToInternalSpecCoversAllFields guards toInternalSpec, which feeds the
// API server's ValidateActivityPolicySpec: if either API type gains a field,
// the conversion must be updated.
func TestToInternalSpecCoversAllFields(t *testing.T) {
	pairs := []struct {
		versioned, internal any
		fields              int
	}{
		{v1alpha1.ActivityPolicySpec{}, activity.ActivityPolicySpec{}, 3},
		{v1alpha1.ActivityPolicyResource{}, activity.ActivityPolicyResource{}, 2},
		{v1alpha1.ActivityPolicyRule{}, activity.ActivityPolicyRule{}, 4},
	}
	for _, p := range pairs {
		vt, it := reflect.TypeOf(p.versioned), reflect.TypeOf(p.internal)
		if vt.NumField() != p.fields || it.NumField() != p.fields {
			t.Errorf("%s has %d fields and internal %s has %d; update toInternalSpec and this test",
				vt.Name(), vt.NumField(), it.Name(), it.NumField())
		}
	}

	in := &v1alpha1.ActivityPolicySpec{
		Resource:   v1alpha1.ActivityPolicyResource{APIGroup: "g", Kind: "K"},
		AuditRules: []v1alpha1.ActivityPolicyRule{{Name: "a", Description: "d", Match: "m", Summary: "s"}},
		EventRules: []v1alpha1.ActivityPolicyRule{{Name: "e", Description: "d2", Match: "m2", Summary: "s2"}},
	}
	out := toInternalSpec(in)
	if out.Resource.APIGroup != "g" || out.Resource.Kind != "K" ||
		out.AuditRules[0] != (activity.ActivityPolicyRule{Name: "a", Description: "d", Match: "m", Summary: "s"}) ||
		out.EventRules[0] != (activity.ActivityPolicyRule{Name: "e", Description: "d2", Match: "m2", Summary: "s2"}) {
		t.Errorf("conversion lost data: %+v", out)
	}
}
