package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

const cliGroupPolicy = `apiVersion: activity.miloapis.com/v1alpha1
kind: ActivityPolicy
metadata:
  name: %s
spec:
  resource:
    apiGroup: iam.miloapis.com
    kind: Group
  auditRules:
    - name: create
      match: "audit.verb == 'create'"
      summary: "%s"
    - name: delete
      match: "audit.verb == 'delete'"
      summary: "deleted"
`

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStripOptionalNoun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir("rules", 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		args []string
		want []string
	}{
		{[]string{"policies", "other"}, []string{"other"}},
		{[]string{"rules", "other"}, []string{"rules", "other"}}, // ./rules exists: it is a path
		{[]string{"policies"}, []string{"policies"}},
		{[]string{"a", "b"}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		if got := stripOptionalNoun(tt.args); strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("stripOptionalNoun(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestCheckCLI_PoliciesDirectoryNameIsAPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeCLIFile(t, filepath.Join("policies", "groups.yaml"), fmt.Sprintf(cliGroupPolicy, "groups", "created"))
	writeCLIFile(t, filepath.Join("other", "roles.yaml"), strings.Replace(fmt.Sprintf(cliGroupPolicy, "roles", "created"), "kind: Group", "kind: Role", 1))

	var stdout, stderr bytes.Buffer
	if code := execute([]string{"check", "policies", "other"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "SUCCESS: 2 policies") {
		t.Errorf("expected both directories to be checked:\n%s", stdout.String())
	}
}

func TestCheckCLI_DuplicateAndUnnamedPolicies(t *testing.T) {
	dir := t.TempDir()
	writeCLIFile(t, filepath.Join(dir, "a.yaml"), fmt.Sprintf(cliGroupPolicy, "groups", "created"))
	writeCLIFile(t, filepath.Join(dir, "b.yaml"), fmt.Sprintf(cliGroupPolicy, "groups", "made"))
	writeCLIFile(t, filepath.Join(dir, "c.yaml"), strings.Replace(fmt.Sprintf(cliGroupPolicy, "x", "created"), "  name: x\n", "  labels: {}\n", 1))

	var stdout, stderr bytes.Buffer
	code := execute([]string{"check", dir}, &stdout, &stderr)
	out := stdout.String()
	if code != 1 {
		t.Fatalf("expected exit 1, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "duplicate ActivityPolicy name") || !strings.Contains(out, "a.yaml") || !strings.Contains(out, "b.yaml") {
		t.Errorf("expected duplicate error naming both files:\n%s", out)
	}
	if strings.Contains(out, "OK:  (") || !strings.Contains(out, "FAIL: <unnamed> (") {
		t.Errorf("unnamed policy must be shown as FAIL with its source:\n%s", out)
	}
}

func TestTestCLI_FailureOutputAndJSON(t *testing.T) {
	dir := t.TempDir()
	writeCLIFile(t, filepath.Join(dir, "p.yaml"), fmt.Sprintf(cliGroupPolicy, "groups", "created"))
	writeCLIFile(t, filepath.Join(dir, "g.test.yaml"), `policy_files:
  - p.yaml
tests:
  - name: wrong summary
    audit:
      verb: create
      objectRef:
        apiGroup: iam.miloapis.com
        resource: groups
    exp_summary: "nope"
`)

	var stdout, stderr bytes.Buffer
	code := execute([]string{"test", "--fail-on-untested-rules", filepath.Join(dir, "g.test.yaml")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	out := stdout.String()
	for _, want := range []string{
		"FAIL: wrong summary (matched policy=groups rule=create)",
		"FAILED: 1 rule(s) never matched by a test",
		"groups: audit/delete",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}

	stdout.Reset()
	execute([]string{"test", "-o", "json", filepath.Join(dir, "g.test.yaml")}, &stdout, &stderr)
	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	result := parsed["files"].([]any)[0].(map[string]any)["results"].([]any)[0].(map[string]any)
	for _, key := range []string{"failures", "failureDetails", "actualSummary", "matchedRule", "matchedPolicy"} {
		if _, ok := result[key]; !ok {
			t.Errorf("JSON result missing %q: %v", key, result)
		}
	}
	detail := result["failureDetails"].([]any)[0].(map[string]any)
	if detail["field"] != "summary" || detail["expected"] != "nope" || detail["actual"] != "created" {
		t.Errorf("unexpected failure detail %v", detail)
	}
	if _, ok := parsed["untestedRules"]; !ok {
		t.Errorf("JSON missing untestedRules: %s", stdout.String())
	}
}
