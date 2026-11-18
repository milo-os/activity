package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/version"
	"go.miloapis.com/activity/pkg/policytest"
)

func main() {
	silenceKlog()
	os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr))
}

// silenceKlog stops production packages (policy compilation) from logging to
// stderr; everything relevant is reported through the tool's own output.
func silenceKlog() {
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	_ = fs.Set("logtostderr", "false")
	_ = fs.Set("alsologtostderr", "false")
	_ = fs.Set("stderrthreshold", "FATAL")
	klog.SetOutput(io.Discard)
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error {
	return e.err
}

func execute(args []string, stdout, stderr io.Writer) int {
	cmd := newRootCommand(stdout, stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", ee.err)
			}
			return ee.code
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	return 0
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	var showVersion bool

	rootCmd := &cobra.Command{
		Use:           "activitytool",
		Short:         "Validate and unit-test ActivityPolicy resources offline",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `activitytool validates ActivityPolicy manifests and runs declarative unit tests
against sample Kubernetes audit events and Events without requiring a cluster.

Examples:
  activitytool check config/services/activity/policies
  activitytool test --policies config/services/activity/policies test/activity-policies`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				fmt.Fprintf(stdout, "activitytool %s\n", version.Get().Version)
				return nil
			}
			return cmd.Help()
		},
	}

	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.Flags().BoolVar(&showVersion, "version", false, "Print version information and exit")

	rootCmd.AddCommand(newVersionCommand(stdout))
	rootCmd.AddCommand(newCheckCommand(stdout, stderr))
	rootCmd.AddCommand(newTestCommand(stdout, stderr))

	return rootCmd
}

func newVersionCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			info := version.Get()
			fmt.Fprintf(stdout, "activitytool %s (commit: %s, built: %s, go: %s, platform: %s)\n",
				info.Version, info.GitCommit, info.BuildDate, info.GoVersion, info.Platform)
		},
	}
}

type checkOutput struct {
	Passed           bool                         `json:"passed"`
	TotalPolicies    int                          `json:"totalPolicies"`
	TotalRules       int                          `json:"totalRules"`
	Policies         []policytest.PolicyReport    `json:"policies,omitempty"`
	ValidationIssues []policytest.ValidationIssue `json:"validationIssues,omitempty"`
}

func newCheckCommand(stdout, stderr io.Writer) *cobra.Command {
	var outputFormat string
	var allowEmpty bool

	cmd := &cobra.Command{
		Use:   "check [policies|rules] <path>...",
		Short: "Validate ActivityPolicy syntax and compile all CEL expressions",
		Long: `Load ActivityPolicy manifests from files, directories, or Kustomize
directories and verify schema rules and CEL expression compilation.

Examples:
  activitytool check examples/basic-kubernetes
  activitytool check policies config/services/activity/policies
  activitytool check -o json path/to/policy.yaml`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if outputFormat != "text" && outputFormat != "json" {
				return &exitError{code: 2, err: fmt.Errorf("unsupported output format %q (expected text or json)", outputFormat)}
			}

			paths := stripOptionalNoun(args)
			if len(paths) == 0 {
				return &exitError{code: 2, err: fmt.Errorf("at least one policy path is required")}
			}

			loaded, loadIssues := policytest.LoadPoliciesFromPaths(paths)
			policies, dupIssues := policytest.DeduplicatePolicies(loaded)
			allIssues := append(loadIssues, dupIssues...)
			allIssues = append(allIssues, policytest.ValidatePolicies(policies)...)

			hasErrors := countErrors(allIssues) > 0
			reports := policytest.BuildPolicyReports(policies, allIssues)
			totalRules := 0
			for _, r := range reports {
				totalRules += r.AuditRules + r.EventRules
			}

			if len(policies) == 0 && !hasErrors && !allowEmpty {
				hasErrors = true
				allIssues = append(allIssues, policytest.ValidationIssue{
					Source:   strings.Join(paths, ", "),
					Message:  "no ActivityPolicy resources found",
					Severity: "error",
				})
			}

			out := checkOutput{
				Passed:           !hasErrors,
				TotalPolicies:    len(policies),
				TotalRules:       totalRules,
				Policies:         reports,
				ValidationIssues: allIssues,
			}

			if outputFormat == "json" {
				if err := writeJSON(stdout, out); err != nil {
					return &exitError{code: 2, err: err}
				}
			} else {
				writeCheckText(stdout, paths, out, allowEmpty)
			}

			if hasErrors {
				return &exitError{code: 1}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&allowEmpty, "allow-empty", false, "Exit 0 with a warning if no policies are found")

	return cmd
}

type testOutput struct {
	Passed bool `json:"passed"`
	*policytest.SuiteResult
}

func newTestCommand(stdout, stderr io.Writer) *cobra.Command {
	var policyPaths []string
	var failOnUntested bool
	var failOnUntestedRules bool
	var allowEmpty bool
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "test [policies|rules] [test-path...]",
		Short: "Validate policies and run ActivityPolicy unit tests",
		Long: `Run declarative unit tests (*test.yaml) against ActivityPolicy definitions
and verify matched rules, generated English summaries, actors, and links.

Examples:
  activitytool test test/policies/basic-kubernetes.test.yaml
  activitytool test --policies config/services/activity/policies test/activity-policies
  activitytool test --policies config/services/activity/policies --fail-on-untested test/activity-policies`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if outputFormat != "text" && outputFormat != "json" {
				return &exitError{code: 2, err: fmt.Errorf("unsupported output format %q (expected text or json)", outputFormat)}
			}

			testPaths := stripOptionalNoun(args)
			if len(testPaths) == 0 && len(policyPaths) == 0 {
				if allowEmpty {
					if outputFormat == "json" {
						return writeJSON(stdout, testOutput{Passed: true, SuiteResult: &policytest.SuiteResult{}})
					}
					fmt.Fprintln(stdout, "WARNING: no test paths or --policies provided")
					return nil
				}
				return &exitError{code: 2, err: fmt.Errorf("provide at least one test file/directory or --policies path")}
			}

			res, err := policytest.RunTestSuite(policytest.SuiteOptions{
				TestPaths:           testPaths,
				PolicyPaths:         policyPaths,
				FailOnUntested:      failOnUntested,
				FailOnUntestedRules: failOnUntestedRules,
			})
			if err != nil {
				return &exitError{code: 2, err: err}
			}

			emptyRun := res.TotalPolicies == 0 && res.TotalTests == 0 && len(res.ValidationIssues) == 0 && len(res.Files) == 0
			hasErrors := res.HasErrors()
			if emptyRun && !allowEmpty {
				hasErrors = true
				res.ValidationIssues = append(res.ValidationIssues, policytest.ValidationIssue{
					Source:   strings.Join(append(append([]string{}, policyPaths...), testPaths...), ", "),
					Message:  "no ActivityPolicy resources or test files found",
					Severity: "error",
				})
			}

			out := testOutput{
				Passed:      !hasErrors,
				SuiteResult: res,
			}

			if outputFormat == "json" {
				if err := writeJSON(stdout, out); err != nil {
					return &exitError{code: 2, err: err}
				}
			} else {
				writeTestText(stdout, out, emptyRun && allowEmpty)
			}

			if hasErrors {
				return &exitError{code: 1}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&policyPaths, "policies", nil, "Policy files, directories, or kustomizations to validate and include in coverage")
	cmd.Flags().BoolVar(&failOnUntested, "fail-on-untested", false, "Fail if any loaded policy has no unit tests")
	cmd.Flags().BoolVar(&failOnUntestedRules, "fail-on-untested-rules", false, "Fail if any rule of a loaded policy is never matched by a unit test")
	cmd.Flags().BoolVar(&allowEmpty, "allow-empty", false, "Exit 0 when no policies or tests are found")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format: text or json")

	return cmd
}

// stripOptionalNoun drops a leading "policies"/"rules" noun (promtool style),
// unless a file or directory with that name exists, in which case it is a path.
func stripOptionalNoun(args []string) []string {
	if len(args) > 1 && (args[0] == "policies" || args[0] == "rules") {
		if _, err := os.Stat(args[0]); err != nil {
			return args[1:]
		}
	}
	return args
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeCheckText(w io.Writer, paths []string, out checkOutput, allowEmpty bool) {
	fmt.Fprintf(w, "Checking %s\n", strings.Join(paths, ", "))
	for _, p := range out.Policies {
		status := "OK"
		if !p.Valid {
			status = "FAIL"
		}
		group := p.APIGroup
		if group == "" {
			group = "core"
		}
		source := p.Source
		if p.Document > 0 {
			source = fmt.Sprintf("%s, document %d", p.Source, p.Document)
		}
		fmt.Fprintf(w, "  %s: %s (%s/%s: %d audit, %d event) [%s]\n",
			status, p.DisplayName(), group, p.Kind, p.AuditRules, p.EventRules, source)
	}

	writeValidationIssues(w, out.ValidationIssues)

	if out.TotalPolicies == 0 && out.Passed && allowEmpty {
		fmt.Fprintln(w, "  WARNING: no ActivityPolicy resources found (--allow-empty)")
		return
	}

	if out.Passed {
		fmt.Fprintf(w, "  SUCCESS: %d policies, %d rules validated\n", out.TotalPolicies, out.TotalRules)
	} else {
		fmt.Fprintf(w, "  FAILED: %d validation issue(s) across %d policies\n", countErrors(out.ValidationIssues), out.TotalPolicies)
	}
}

func writeTestText(w io.Writer, out testOutput, emptyAllowed bool) {
	res := out.SuiteResult

	if len(res.ValidationIssues) > 0 {
		writeValidationIssues(w, res.ValidationIssues)
	}

	for _, file := range res.Files {
		fmt.Fprintf(w, "Unit Testing: %s\n", file.File)
		if file.Error != "" {
			fmt.Fprintf(w, "  FAILED: %s\n", file.Error)
			continue
		}

		for _, tr := range file.Results {
			name := tr.Name
			if tr.Document > 0 {
				name = fmt.Sprintf("%s [document %d]", tr.Name, tr.Document)
			}
			if tr.Passed {
				outcome := "no-match"
				if tr.Matched {
					outcome = fmt.Sprintf("rule=%s", tr.MatchedRule)
				}
				fmt.Fprintf(w, "  PASS: %s (%s, %s)\n", name, tr.Policy, outcome)
			} else {
				fmt.Fprintf(w, "  FAIL: %s (%s)\n", name, describeOutcome(tr))
				for _, failure := range tr.Failures {
					for _, line := range strings.Split(failure, "\n") {
						fmt.Fprintf(w, "    %s\n", line)
					}
				}
			}
			for _, warning := range tr.Warnings {
				fmt.Fprintf(w, "    WARNING: %s\n", formatTestWarning(warning))
			}
		}
	}

	if len(res.UntestedPolicies) > 0 {
		label := "WARNING"
		if res.FailOnUntested {
			label = "FAILED"
		}
		fmt.Fprintf(w, "\n%s: %d policy(s) validated but untested:\n", label, len(res.UntestedPolicies))
		for _, name := range res.UntestedPolicies {
			fmt.Fprintf(w, "  - %s\n", name)
		}
	}

	if len(res.UntestedRules) > 0 {
		label := "WARNING"
		if res.FailOnUntestedRules {
			label = "FAILED"
		}
		fmt.Fprintf(w, "\n%s: %d rule(s) never matched by a test:\n", label, len(res.UntestedRules))
		for _, r := range res.UntestedRules {
			fmt.Fprintf(w, "  - %s: %s/%s\n", r.Policy, r.RuleType, r.Rule)
		}
	}

	if emptyAllowed {
		fmt.Fprintln(w, "WARNING: no ActivityPolicy resources or test files found (--allow-empty)")
		return
	}

	status := "SUCCESS"
	if !out.Passed {
		status = "FAILED"
	}
	fmt.Fprintf(w, "\n%s: %d/%d tests passed, %d policies validated (%d untested), %d/%d rules matched by tests\n",
		status, res.PassedTests, res.TotalTests, res.TotalPolicies, len(res.UntestedPolicies), res.TestedRules, res.TotalRules)
}

// describeOutcome summarizes what actually happened for a failed test.
func describeOutcome(tr policytest.TestResult) string {
	switch {
	case tr.Matched:
		return fmt.Sprintf("matched policy=%s rule=%s", tr.MatchedPolicy, tr.MatchedRule)
	case len(tr.EvaluatedPolicies) > 0:
		return fmt.Sprintf("no match; evaluated %s", strings.Join(tr.EvaluatedPolicies, ", "))
	case tr.Policy != "":
		return fmt.Sprintf("policy=%s, not evaluated", tr.Policy)
	default:
		return "not evaluated"
	}
}

func formatTestWarning(w policytest.TestWarning) string {
	var parts []string
	if w.Policy != "" {
		parts = append(parts, "policy="+w.Policy)
	}
	if w.Rule != "" {
		parts = append(parts, fmt.Sprintf("rule=%s:%s", w.RuleType, w.Rule))
	}
	msg := w.Message
	if len(parts) > 0 {
		msg = strings.Join(parts, " ") + ": " + msg
	}
	if w.Expression != "" {
		msg += "\n      expression: " + w.Expression
	}
	return msg
}

func writeValidationIssues(w io.Writer, issues []policytest.ValidationIssue) {
	for _, issue := range issues {
		loc := issue.Source
		if issue.Document > 0 {
			loc = fmt.Sprintf("%s, document %d", issue.Source, issue.Document)
		}
		if issue.PolicyName != "" {
			loc = fmt.Sprintf("%s (%s)", issue.PolicyName, loc)
		}
		rulePart := ""
		if issue.RuleType != "" || issue.RuleName != "" {
			rulePart = fmt.Sprintf(" rule=%s:%s", issue.RuleType, issue.RuleName)
		}
		fieldPart := ""
		if issue.Field != "" {
			fieldPart = fmt.Sprintf(" field=%s", issue.Field)
		}
		fmt.Fprintf(w, "  %s: %s%s%s: %s\n",
			strings.ToUpper(issue.Severity), loc, rulePart, fieldPart, issue.Message)
		if issue.Expression != "" {
			fmt.Fprintf(w, "    expression: %s\n", issue.Expression)
		}
	}
}

func countErrors(issues []policytest.ValidationIssue) int {
	n := 0
	for _, issue := range issues {
		if issue.Severity == "error" {
			n++
		}
	}
	return n
}
