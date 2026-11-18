package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAndTestCLI(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	policyYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: test-groups
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Group
  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ actor }} created group {{ link(audit.objectRef.name, audit.objectRef) }}"
`
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	testPath := filepath.Join(dir, "groups.test.yaml")
	testYAML := `policy_files:
  - policy.yaml
tests:
  - name: create group renders expected english summary
    audit:
      verb: create
      user:
        username: alice@example.com
      objectRef:
        apiGroup: iam.miloapis.com
        resource: groups
        name: platform-admins
    exp_rule: create
    exp_summary: "alice@example.com created group platform-admins"
`
	if err := os.WriteFile(testPath, []byte(testYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// check policies text
	var stdout, stderr bytes.Buffer
	code := execute([]string{"check", "policies", policyPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("check failed with code %d:\nstdout=%s\nstderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "SUCCESS: 1 policies, 1 rules validated") {
		t.Errorf("unexpected check output:\n%s", stdout.String())
	}

	// test json
	stdout.Reset()
	stderr.Reset()
	code = execute([]string{"test", "--policies", policyPath, "--fail-on-untested", "-o", "json", testPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("test failed with code %d:\nstdout=%s\nstderr=%s", code, stdout.String(), stderr.String())
	}

	var parsed testOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid json output: %v\n%s", err, stdout.String())
	}
	if !parsed.Passed || parsed.PassedTests != 1 || len(parsed.UntestedPolicies) != 0 {
		t.Errorf("unexpected parsed test JSON: %+v", parsed)
	}
}

func TestCheckCLI_ReportsInvalidPolicy(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "bad.yaml")
	policyYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: broken-policy
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Group
  auditRules:
    - name: create
      match: "audit.verb"
      summary: "{{ actor }} created group"
`
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := execute([]string{"check", policyPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid policy, got %d", code)
	}
	if !strings.Contains(stdout.String(), "broken-policy") || !strings.Contains(stdout.String(), "must return a boolean") {
		t.Errorf("expected detailed validation output, got:\n%s", stdout.String())
	}
}

func TestTestCLI_UntestedPolicyHandling(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	policyYAML := `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: only-validated
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Group
  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "{{ actor }} created group"
`
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := execute([]string{"test", "--policies", policyPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 without --fail-on-untested, got %d:\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "untested") {
		t.Errorf("expected untested warning in output:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = execute([]string{"test", "--policies", policyPath, "--fail-on-untested"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 with --fail-on-untested, got %d", code)
	}
}
