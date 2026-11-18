package policytest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePolicies_ValidAndInvalid(t *testing.T) {
	dir := t.TempDir()

	validPath := filepath.Join(dir, "valid.yaml")
	validYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: valid-group
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Group
  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ actor }} created group {{ link(audit.objectRef.name, audit.objectRef) }}"
  eventRules:
    - name: ready
      match: "event.reason == 'Ready'"
      summary: "Group {{ event.regarding.name }} is ready"
`
	if err := os.WriteFile(validPath, []byte(validYAML), 0644); err != nil {
		t.Fatal(err)
	}

	invalidPath := filepath.Join(dir, "invalid.yaml")
	invalidYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: invalid-policy
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Group
  auditRules:
    - name: bad-match
      match: "event.reason == 'Ready'"
      summary: "{{ actor }} did something"
    - name: bad-match
      match: "audit.verb == 'delete'"
      summary: "{{ actor }} deleted {{ unclosed"
`
	if err := os.WriteFile(invalidPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}

	policies, loadIssues := LoadPoliciesFromPaths([]string{validPath, invalidPath})
	if len(loadIssues) != 0 {
		t.Fatalf("unexpected load issues: %+v", loadIssues)
	}
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(policies))
	}

	issues := ValidatePolicies(policies)
	var errs []ValidationIssue
	for _, issue := range issues {
		if issue.Severity == "error" {
			errs = append(errs, issue)
		}
	}
	if len(errs) != 3 {
		t.Fatalf("expected 3 validation errors (bad audit match var, duplicate rule name, unclosed summary), got %d: %+v", len(errs), errs)
	}

	var sawMatchErr, sawDupErr, sawSummaryErr bool
	for _, e := range errs {
		if e.PolicyName != "invalid-policy" {
			t.Errorf("unexpected policyName %q", e.PolicyName)
		}
		if e.RuleName == "bad-match" && strings.Contains(e.Field, ".match") && strings.Contains(e.Message, "undeclared reference") {
			sawMatchErr = true
		}
		if e.RuleName == "bad-match" && strings.Contains(e.Field, ".name") && strings.Contains(e.Message, "duplicate rule name") {
			sawDupErr = true
		}
		if e.RuleName == "bad-match" && strings.Contains(e.Field, ".summary") && strings.Contains(e.Message, "missing closing delimiter") {
			sawSummaryErr = true
		}
	}
	if !sawMatchErr || !sawDupErr || !sawSummaryErr {
		t.Errorf("missing expected error kinds: match=%v dup=%v summary=%v; errs=%+v", sawMatchErr, sawDupErr, sawSummaryErr, errs)
	}
}

func TestRunTestSuite_AuditAndEventRules(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "project-policy.yaml")
	policyYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: resourcemanager.miloapis.com-project
spec:
  resource:
    apiGroup: resourcemanager.miloapis.com
    kind: Project
  auditRules:
    - name: create
      match: "!audit.user.username.startsWith('system:') && audit.verb == 'create' && audit.responseStatus.code >= 200 && audit.responseStatus.code < 300"
      summary: "{{ actor }} created project {{ has(audit.responseObject.metadata.name) ? link(audit.responseObject.metadata.name, audit.responseObject) : link(audit.objectRef.name, audit.objectRef) }}"
    - name: update
      match: "!audit.user.username.startsWith('system:') && audit.verb in ['update', 'patch'] && !has(audit.objectRef.subresource)"
      summary: "{{ actor }} updated project {{ link(audit.objectRef.name, audit.objectRef) }}"
  eventRules:
    - name: suspended
      match: "event.reason == 'Suspended'"
      summary: "Project {{ link(event.regarding.name, event.regarding) }} was suspended"
`
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	testPath := filepath.Join(dir, "project.test.yaml")
	testYAML := `policy_files:
  - project-policy.yaml
tests:
  - name: human create uses responseObject metadata name and links Project
    audit:
      verb: create
      user:
        username: alice@example.com
      objectRef:
        apiGroup: resourcemanager.miloapis.com
        resource: projects
      responseStatus:
        code: 201
      responseObject:
        apiVersion: resourcemanager.miloapis.com/v1alpha1
        kind: Project
        metadata:
          name: prod-app
          uid: proj-123
    exp_rule: create
    exp_summary: "alice@example.com created project prod-app"
    exp_change_source: human
    exp_actor:
      type: user
      name: alice@example.com
      email: alice@example.com
    exp_links:
      - marker: prod-app
        apiGroup: resourcemanager.miloapis.com
        kind: Project
        name: prod-app

  - name: system actor create is ignored
    audit:
      verb: create
      user:
        username: system:serviceaccount:kube-system:controller
      objectRef:
        apiGroup: resourcemanager.miloapis.com
        resource: projects
        name: prod-app
      responseStatus:
        code: 201
    exp_no_match: true

  - name: status subresource update is ignored
    audit:
      verb: update
      user:
        username: alice@example.com
      objectRef:
        apiGroup: resourcemanager.miloapis.com
        resource: projects
        name: prod-app
        subresource: status
    exp_no_match: true

  - name: legacy involvedObject event normalizes to regarding
    event:
      reason: Suspended
      type: Warning
      source:
        component: project-controller
      involvedObject:
        apiVersion: resourcemanager.miloapis.com/v1alpha1
        kind: Project
        name: prod-app
    exp_rule: suspended
    exp_summary: "Project prod-app was suspended"
    exp_change_source: system
    exp_actor:
      type: controller
      name: project-controller
    exp_links:
      - marker: prod-app
        resource:
          apiGroup: resourcemanager.miloapis.com
          kind: Project
          name: prod-app
`
	if err := os.WriteFile(testPath, []byte(testYAML), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := RunTestSuite(SuiteOptions{
		TestPaths:      []string{testPath},
		PolicyPaths:    []string{policyPath},
		FailOnUntested: true,
	})
	if err != nil {
		t.Fatalf("RunTestSuite failed: %v", err)
	}
	if res.HasErrors() {
		t.Fatalf("expected passing suite, got errors: validation=%+v files=%+v untested=%+v",
			res.ValidationIssues, res.Files, res.UntestedPolicies)
	}
	if res.TotalTests != 4 || res.PassedTests != 4 || res.FailedTests != 0 {
		t.Fatalf("unexpected counts: total=%d passed=%d failed=%d", res.TotalTests, res.PassedTests, res.FailedTests)
	}
	if len(res.UntestedPolicies) != 0 {
		t.Errorf("expected 0 untested policies, got %+v", res.UntestedPolicies)
	}
}

func TestRunTestSuite_DetectsSummaryAndRuleMismatchesAndUntested(t *testing.T) {
	dir := t.TempDir()

	kustomization := `apiVersion: kustomize.config.k8s.io/v1alpha1
kind: Component
resources:
  - group-policy.yaml
  - untested-policy.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(kustomization), 0644); err != nil {
		t.Fatal(err)
	}

	groupYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: iam.miloapis.com-group
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Group
  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ actor }} created group {{ audit.objectRef.name }}"
    - name: specific-create
      match: "audit.verb == 'create' && audit.objectRef.name == 'admins'"
      summary: "{{ actor }} created admin group"
`
	if err := os.WriteFile(filepath.Join(dir, "group-policy.yaml"), []byte(groupYAML), 0644); err != nil {
		t.Fatal(err)
	}

	untestedYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: iam.miloapis.com-role
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Role
  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ actor }} created role"
`
	if err := os.WriteFile(filepath.Join(dir, "untested-policy.yaml"), []byte(untestedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	testPath := filepath.Join(dir, "group.test.yaml")
	testYAML := `policy_files:
  - .
tests:
  - name: shadowed rule and wrong summary fail clearly
    audit:
      verb: create
      user:
        username: bob
      objectRef:
        apiGroup: iam.miloapis.com
        resource: groups
        name: admins
    exp_rule: specific-create
    exp_summary: "bob created admin group"
`
	if err := os.WriteFile(testPath, []byte(testYAML), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := RunTestSuite(SuiteOptions{
		TestPaths:      []string{testPath},
		PolicyPaths:    []string{dir},
		FailOnUntested: true,
	})
	if err != nil {
		t.Fatalf("RunTestSuite failed: %v", err)
	}
	if !res.HasErrors() {
		t.Fatal("expected suite to fail due to shadowed rule, summary diff, and untested policy")
	}
	if res.FailedTests != 1 {
		t.Fatalf("expected 1 failed test, got %d", res.FailedTests)
	}
	failures := strings.Join(res.Files[0].Results[0].Failures, "\n")
	if !strings.Contains(failures, `exp: "specific-create"`) || !strings.Contains(failures, `got: "create"`) {
		t.Errorf("expected rule mismatch diff in failures, got:\n%s", failures)
	}
	if !strings.Contains(failures, `exp: "bob created admin group"`) || !strings.Contains(failures, `got: "bob created group admins"`) {
		t.Errorf("expected summary mismatch diff in failures, got:\n%s", failures)
	}
	if len(res.UntestedPolicies) != 1 || res.UntestedPolicies[0] != "iam.miloapis.com-role" {
		t.Errorf("expected iam.miloapis.com-role to be untested, got %+v", res.UntestedPolicies)
	}
	if res.TotalPolicies != 2 {
		t.Errorf("expected 2 deduplicated policies, got %d", res.TotalPolicies)
	}
}

func TestRunTestSuite_DeduplicatesPoliciesAcrossDifferentSources(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	rootKust := `apiVersion: kustomize.config.k8s.io/v1alpha1
kind: Component
components:
  - sub
`
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(rootKust), 0644); err != nil {
		t.Fatal(err)
	}

	subKust := `apiVersion: kustomize.config.k8s.io/v1alpha1
kind: Component
resources:
  - group-policy.yaml
`
	if err := os.WriteFile(filepath.Join(subDir, "kustomization.yaml"), []byte(subKust), 0644); err != nil {
		t.Fatal(err)
	}

	groupYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: iam.miloapis.com-group
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Group
  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ actor }} created group {{ audit.objectRef.name }}"
`
	if err := os.WriteFile(filepath.Join(subDir, "group-policy.yaml"), []byte(groupYAML), 0644); err != nil {
		t.Fatal(err)
	}

	testPath := filepath.Join(dir, "group.test.yaml")
	testYAML := `policy_files:
  - sub
tests:
  - name: create group
    audit:
      verb: create
      user:
        username: alice
      objectRef:
        apiGroup: iam.miloapis.com
        resource: groups
        name: devs
    exp_rule: create
    exp_summary: "alice created group devs"
`
	if err := os.WriteFile(testPath, []byte(testYAML), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := RunTestSuite(SuiteOptions{
		TestPaths:   []string{testPath},
		PolicyPaths: []string{dir},
	})
	if err != nil {
		t.Fatalf("RunTestSuite failed: %v", err)
	}
	if res.TotalPolicies != 1 {
		t.Fatalf("expected TotalPolicies=1 (deduplicated across dir and sub), got %d", res.TotalPolicies)
	}
	if len(res.UntestedPolicies) != 0 {
		t.Errorf("expected 0 untested policies, got %+v", res.UntestedPolicies)
	}
}
