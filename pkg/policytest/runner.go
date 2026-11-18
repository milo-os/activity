package policytest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	sigsyaml "sigs.k8s.io/yaml"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// RunTestSuite validates policies from PolicyPaths and test files, runs every
// unit test case, and reports policy and rule coverage.
func RunTestSuite(opts SuiteOptions) (*SuiteResult, error) {
	result := &SuiteResult{
		FailOnUntested:      opts.FailOnUntested,
		FailOnUntestedRules: opts.FailOnUntestedRules,
	}

	// Issues are only ever deduplicated when identical (the same file loaded
	// twice produces the same issue twice); nothing else is dropped.
	seenIssues := make(map[ValidationIssue]bool)
	addIssues := func(issues []ValidationIssue) {
		for _, issue := range issues {
			if seenIssues[issue] {
				continue
			}
			seenIssues[issue] = true
			result.ValidationIssues = append(result.ValidationIssues, issue)
		}
	}

	set := newPolicySet()
	register := func(loaded []LoadedPolicy) []LoadedPolicy {
		canonical, added, dupIssues := set.add(loaded)
		addIssues(dupIssues)
		addIssues(ValidatePolicies(added))
		return canonical
	}

	suiteLoaded, suiteIssues := LoadPoliciesFromPaths(opts.PolicyPaths)
	addIssues(suiteIssues)
	suitePolicies := register(suiteLoaded)

	testFiles, err := DiscoverTestFiles(opts.TestPaths)
	if err != nil {
		return nil, err
	}

	cov := newCoverage()
	for _, tfPath := range testFiles {
		fileRes := runTestFile(tfPath, suitePolicies, set, addIssues, register, cov)
		result.Files = append(result.Files, fileRes)
		for _, tr := range fileRes.Results {
			result.TotalTests++
			if tr.Passed {
				result.PassedTests++
			} else {
				result.FailedTests++
			}
		}
	}

	result.Policies = BuildPolicyReports(set.all, result.ValidationIssues)
	for i, lp := range set.all {
		p := lp.Policy
		report := &result.Policies[i]
		report.TestCount = cov.tests[p.Name]
		if p.Name == "" {
			report.TestCount = 0
		}
		report.Tested = report.TestCount > 0
		if !report.Tested {
			result.UntestedPolicies = append(result.UntestedPolicies, lp.DisplayName())
		}

		ruleSets := []struct {
			ruleType string
			rules    []v1alpha1.ActivityPolicyRule
		}{{"audit", p.Spec.AuditRules}, {"event", p.Spec.EventRules}}
		for _, rs := range ruleSets {
			for _, rule := range rs.rules {
				result.TotalRules++
				if p.Name != "" && cov.matched[p.Name][rs.ruleType+"/"+rule.Name] {
					result.TestedRules++
					continue
				}
				report.UntestedRules = append(report.UntestedRules, rs.ruleType+"/"+rule.Name)
				result.UntestedRules = append(result.UntestedRules, UntestedRule{
					Policy:   lp.DisplayName(),
					RuleType: rs.ruleType,
					Rule:     rule.Name,
				})
			}
		}
	}
	result.TotalPolicies = len(set.all)

	return result, nil
}

// coverage counts, per policy name, the tests that evaluated the policy and
// the rules that matched at least once.
type coverage struct {
	tests   map[string]int
	matched map[string]map[string]bool
}

func newCoverage() *coverage {
	return &coverage{
		tests:   make(map[string]int),
		matched: make(map[string]map[string]bool),
	}
}

func (c *coverage) record(ev *Evaluation) {
	seen := make(map[string]bool)
	for _, name := range ev.Evaluated {
		if !seen[name] {
			seen[name] = true
			c.tests[name]++
		}
	}
	if ev.Matched {
		if c.matched[ev.MatchedPolicy] == nil {
			c.matched[ev.MatchedPolicy] = make(map[string]bool)
		}
		c.matched[ev.MatchedPolicy][ev.MatchedRuleType+"/"+ev.MatchedRule] = true
	}
}

// parseTestDocuments splits a test file into its YAML documents and decodes
// each non-empty one as a test group. Document indexes are 1-based.
func parseTestDocuments(raw []byte) (groups []TestFile, docIndexes []int, errs []string) {
	docs, err := splitYAMLDocuments(raw)
	if err != nil {
		errs = append(errs, fmt.Sprintf("failed to read YAML: %v", err))
	}
	for i, doc := range docs {
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		var probe any
		if err := sigsyaml.Unmarshal(doc, &probe); err == nil && probe == nil {
			continue // comment-only document
		}
		var tf TestFile
		if err := sigsyaml.UnmarshalStrict(doc, &tf); err != nil {
			errs = append(errs, fmt.Sprintf("document %d: failed to parse test file: %v", i+1, err))
			continue
		}
		if len(tf.Tests) == 0 {
			errs = append(errs, fmt.Sprintf("document %d: no tests defined", i+1))
			continue
		}
		groups = append(groups, tf)
		docIndexes = append(docIndexes, i+1)
	}
	return groups, docIndexes, errs
}

func runTestFile(
	tfPath string,
	suitePolicies []LoadedPolicy,
	set *policySet,
	addIssues func([]ValidationIssue),
	register func([]LoadedPolicy) []LoadedPolicy,
	cov *coverage,
) FileResult {
	fileRes := FileResult{File: tfPath}

	raw, err := os.ReadFile(tfPath)
	if err != nil {
		fileRes.Error = fmt.Sprintf("failed to read test file: %v", err)
		return fileRes
	}

	groups, docIndexes, errs := parseTestDocuments(raw)
	if len(groups) == 0 && len(errs) == 0 {
		errs = append(errs, "test file contains no tests")
	}
	multiDoc := len(groups)+len(errs) > 1

	for gi, tf := range groups {
		policyRefPaths := resolveRelativePaths(filepath.Dir(tfPath), tf.AllPolicyFiles())
		loaded, loadIssues := LoadPoliciesFromPaths(policyRefPaths)
		addIssues(loadIssues)
		filePolicies := register(loaded)

		// Prefer policies explicitly listed in the test group; fall back to --policies.
		candidates := filePolicies
		if len(tf.AllPolicyFiles()) == 0 {
			candidates = suitePolicies
		}

		evaluator := NewEvaluator(candidates, EvaluatorOptions{
			ResourceKinds: tf.ResourceKinds,
			KindHints:     set.all,
		})

		for _, tc := range tf.Tests {
			tr := executeTestCase(tc, evaluator, cov)
			if multiDoc {
				tr.Document = docIndexes[gi]
			}
			fileRes.Results = append(fileRes.Results, tr)
		}
	}

	fileRes.Error = strings.Join(errs, "; ")
	return fileRes
}

func resolveRelativePaths(baseDir string, refs []string) []string {
	resolved := make([]string, 0, len(refs))
	for _, ref := range refs {
		if filepath.IsAbs(ref) {
			resolved = append(resolved, ref)
			continue
		}
		resolved = append(resolved, filepath.Join(baseDir, ref))
	}
	return resolved
}

// testResultBuilder accumulates failures in both text and structured form.
type testResultBuilder struct {
	tr *TestResult
}

func (b testResultBuilder) fail(field, expected, actual, message string) {
	b.tr.Failures = append(b.tr.Failures, message)
	b.tr.FailureDetails = append(b.tr.FailureDetails, FailureDetail{
		Field:    field,
		Expected: expected,
		Actual:   actual,
		Message:  message,
	})
}

func (b testResultBuilder) mismatch(field, expected, actual string) {
	b.fail(field, expected, actual, fmt.Sprintf("%s mismatch: exp %q, got %q", field, expected, actual))
}

func executeTestCase(tc TestCase, ev *Evaluator, cov *coverage) TestResult {
	tr := TestResult{
		Name:   tc.Name,
		Policy: tc.Policy,
	}
	b := testResultBuilder{tr: &tr}
	finish := func() TestResult {
		tr.Passed = len(tr.Failures) == 0
		return tr
	}

	if strings.TrimSpace(tc.Name) == "" {
		b.fail("test", "", "", "test case is missing required 'name' field")
		return finish()
	}
	if tc.Audit == nil && len(tc.Event) == 0 {
		b.fail("test", "", "", "test case must specify either 'audit' or 'event'")
		return finish()
	}
	if tc.Audit != nil && len(tc.Event) > 0 {
		b.fail("test", "", "", "test case must not specify both 'audit' and 'event'")
		return finish()
	}

	exp := normalizeExpectations(tc)
	positive := positiveAssertionFields(exp)
	if exp.NoMatch && len(positive) > 0 {
		b.fail("test", "", "", fmt.Sprintf(
			"exp_no_match: true cannot be combined with positive assertions (%s); remove one or the other",
			strings.Join(positive, ", ")))
		return finish()
	}
	if !exp.NoMatch && len(positive) == 0 {
		b.fail("test", "", "", "test case has no assertions (set exp_summary, exp_rule, exp_links, ..., or exp_no_match: true)")
		return finish()
	}

	var summaryRegex *regexp.Regexp
	if exp.SummaryRegex != "" {
		re, err := regexp.Compile(exp.SummaryRegex)
		if err != nil {
			b.fail("summary_regex", exp.SummaryRegex, "", fmt.Sprintf("invalid exp_summary_regex %q: %v", exp.SummaryRegex, err))
			return finish()
		}
		summaryRegex = re
	}

	available := ev.PolicyNames()
	if len(available) == 0 {
		b.fail("routing", "", "", "no policies available (add policy_files to test file or pass --policies)")
		return finish()
	}

	only := ""
	if tc.Policy != "" {
		matches := ev.ResolvePolicy(tc.Policy)
		switch {
		case len(matches) == 0:
			b.fail("routing", tc.Policy, "", fmt.Sprintf("policy %q not found (available: %s)", tc.Policy, strings.Join(available, ", ")))
			return finish()
		case len(matches) > 1:
			names := make([]string, 0, len(matches))
			for _, m := range matches {
				names = append(names, m.Policy.Name)
			}
			b.fail("routing", tc.Policy, "", fmt.Sprintf("policy reference %q is ambiguous (%s); use the policy's metadata.name", tc.Policy, strings.Join(names, ", ")))
			return finish()
		}
		only = matches[0].Policy.Name
		tr.Policy = only
	}

	var res *Evaluation
	var err error
	if tc.Audit != nil {
		res, err = ev.EvaluateAudit(tc.Audit, only)
	} else {
		res, err = ev.EvaluateEvent(tc.Event, only)
	}
	if err != nil {
		b.fail("routing", only, "", err.Error())
		return finish()
	}
	tr.Warnings = append(tr.Warnings, res.Warnings...)

	if res.Ignored != "" || len(res.Candidates) == 0 {
		msg := res.Ignored
		if msg == "" {
			msg = fmt.Sprintf("no loaded policy targets %s (loaded policies: %s); production ignores this input",
				res.Target, strings.Join(available, ", "))
		}
		if exp.NoMatch {
			tr.Warnings = append(tr.Warnings, TestWarning{Type: WarningNoTarget, Message: msg})
		} else {
			b.fail("routing", "", res.Target, msg)
		}
		return finish()
	}

	cov.record(res)
	tr.EvaluatedPolicies = res.Evaluated
	tr.Matched = res.Matched
	tr.MatchedPolicy = res.MatchedPolicy
	tr.MatchedRule = res.MatchedRule
	if tr.Policy == "" {
		tr.Policy = res.MatchedPolicy
	}
	if res.Activity != nil {
		tr.ActualSummary = res.Activity.Spec.Summary
	}

	for _, e := range res.Errors {
		msg := "evaluation error: " + e.String()
		if e.Expression != "" {
			msg += "\nexpression: " + e.Expression
		}
		b.fail("evaluation", "", "", msg)
	}

	verifyExpectations(b, exp, summaryRegex, res)
	return finish()
}

func normalizeExpectations(tc TestCase) ExpectedActivity {
	exp := ExpectedActivity{
		NoMatch:         tc.ExpNoMatch,
		Policy:          tc.ExpPolicy,
		Rule:            tc.ExpRule,
		Summary:         tc.ExpSummary,
		SummaryContains: tc.ExpSummaryContains,
		SummaryRegex:    tc.ExpSummaryRegex,
		ChangeSource:    tc.ExpChangeSource,
		Actor:           tc.ExpActor,
		Resource:        tc.ExpResource,
		Tenant:          tc.ExpTenant,
		Links:           tc.ExpLinks,
	}

	if tc.ExpActivity == nil {
		return exp
	}

	ea := tc.ExpActivity
	if ea.NoMatch {
		exp.NoMatch = true
	}
	if exp.Policy == "" {
		exp.Policy = ea.Policy
	}
	if exp.Rule == "" {
		exp.Rule = ea.Rule
	}
	if exp.Summary == nil {
		exp.Summary = ea.Summary
	}
	if exp.SummaryContains == "" {
		exp.SummaryContains = ea.SummaryContains
	}
	if exp.SummaryRegex == "" {
		exp.SummaryRegex = ea.SummaryRegex
	}
	if exp.ChangeSource == "" {
		exp.ChangeSource = ea.ChangeSource
	}
	if exp.Actor == nil {
		exp.Actor = ea.Actor
	}
	if exp.Resource == nil {
		exp.Resource = ea.Resource
	}
	if exp.Tenant == nil {
		exp.Tenant = ea.Tenant
	}
	if len(exp.Links) == 0 {
		exp.Links = ea.Links
	}

	return exp
}

// positiveAssertionFields lists the match assertions that are set, by their
// top-level key name (the same fields may also come from exp_activity).
func positiveAssertionFields(exp ExpectedActivity) []string {
	var fields []string
	add := func(set bool, name string) {
		if set {
			fields = append(fields, name)
		}
	}
	add(exp.Policy != "", "exp_policy")
	add(exp.Rule != "", "exp_rule")
	add(exp.Summary != nil, "exp_summary")
	add(exp.SummaryContains != "", "exp_summary_contains")
	add(exp.SummaryRegex != "", "exp_summary_regex")
	add(exp.ChangeSource != "", "exp_change_source")
	add(exp.Actor != nil, "exp_actor")
	add(exp.Resource != nil, "exp_resource")
	add(exp.Tenant != nil, "exp_tenant")
	add(exp.Links != nil, "exp_links")
	return fields
}

func verifyExpectations(b testResultBuilder, exp ExpectedActivity, summaryRegex *regexp.Regexp, res *Evaluation) {
	if exp.NoMatch {
		if res.Matched {
			b.fail("match", "no match", fmt.Sprintf("%s/%s", res.MatchedPolicy, res.MatchedRule),
				fmt.Sprintf("expected no rule to match, but policy %q rule %q matched with summary %q",
					res.MatchedPolicy, res.MatchedRule, res.Activity.Spec.Summary))
		}
		return
	}

	if !res.Matched {
		b.fail("match", "a rule to match", "no match",
			fmt.Sprintf("expected a rule to match, but no rule matched (evaluated policies: %s)", strings.Join(res.Evaluated, ", ")))
		return
	}

	act := res.Activity

	if exp.Policy != "" && res.MatchedPolicy != exp.Policy {
		b.fail("policy", exp.Policy, res.MatchedPolicy, fmt.Sprintf("policy mismatch:\n  exp: %q\n  got: %q", exp.Policy, res.MatchedPolicy))
	}

	if exp.Rule != "" && res.MatchedRule != exp.Rule {
		b.fail("rule", exp.Rule, res.MatchedRule, fmt.Sprintf("rule mismatch:\n  exp: %q\n  got: %q", exp.Rule, res.MatchedRule))
	}

	if exp.Summary != nil && act.Spec.Summary != *exp.Summary {
		b.fail("summary", *exp.Summary, act.Spec.Summary, fmt.Sprintf("summary mismatch:\n  exp: %q\n  got: %q", *exp.Summary, act.Spec.Summary))
	}

	if exp.SummaryContains != "" && !strings.Contains(act.Spec.Summary, exp.SummaryContains) {
		b.fail("summary_contains", exp.SummaryContains, act.Spec.Summary,
			fmt.Sprintf("summary missing substring:\n  exp substring: %q\n  got summary:   %q", exp.SummaryContains, act.Spec.Summary))
	}

	if summaryRegex != nil && !summaryRegex.MatchString(act.Spec.Summary) {
		b.fail("summary_regex", exp.SummaryRegex, act.Spec.Summary,
			fmt.Sprintf("summary did not match regex:\n  exp regex:   %q\n  got summary: %q", exp.SummaryRegex, act.Spec.Summary))
	}

	if exp.ChangeSource != "" && act.Spec.ChangeSource != exp.ChangeSource {
		b.mismatch("change_source", exp.ChangeSource, act.Spec.ChangeSource)
	}

	if exp.Actor != nil {
		verifyActor(b, *exp.Actor, act.Spec.Actor)
	}

	if exp.Resource != nil {
		verifyResource(b, "resource", *exp.Resource, act.Spec.Resource)
	}

	if exp.Tenant != nil {
		if exp.Tenant.Type != "" && act.Spec.Tenant.Type != exp.Tenant.Type {
			b.mismatch("tenant.type", exp.Tenant.Type, act.Spec.Tenant.Type)
		}
		if exp.Tenant.Name != nil && act.Spec.Tenant.Name != *exp.Tenant.Name {
			b.mismatch("tenant.name", *exp.Tenant.Name, act.Spec.Tenant.Name)
		}
	}

	if exp.Links != nil {
		verifyLinks(b, exp.Links, act.Spec.Links)
	}
}

func verifyActor(b testResultBuilder, exp ExpectedActor, got v1alpha1.ActivityActor) {
	check := func(field, want, have string) {
		if want != "" && have != want {
			b.mismatch(field, want, have)
		}
	}
	check("actor.type", exp.Type, got.Type)
	check("actor.name", exp.Name, got.Name)
	check("actor.uid", exp.UID, got.UID)
	check("actor.email", exp.Email, got.Email)
}

func verifyResource(b testResultBuilder, label string, exp ExpectedResource, got v1alpha1.ActivityResource) {
	if exp.APIGroup != nil && got.APIGroup != *exp.APIGroup {
		b.mismatch(label+".apiGroup", *exp.APIGroup, got.APIGroup)
	}
	if exp.APIVersion != "" && got.APIVersion != exp.APIVersion {
		b.mismatch(label+".apiVersion", exp.APIVersion, got.APIVersion)
	}
	if exp.Kind != "" && got.Kind != exp.Kind {
		b.mismatch(label+".kind", exp.Kind, got.Kind)
	}
	if exp.Name != "" && got.Name != exp.Name {
		b.mismatch(label+".name", exp.Name, got.Name)
	}
	if exp.Namespace != nil && got.Namespace != *exp.Namespace {
		b.mismatch(label+".namespace", *exp.Namespace, got.Namespace)
	}
	if exp.UID != "" && got.UID != exp.UID {
		b.mismatch(label+".uid", exp.UID, got.UID)
	}
}

func verifyLinks(b testResultBuilder, expLinks []ExpectedLink, gotLinks []v1alpha1.ActivityLink) {
	if len(expLinks) != len(gotLinks) {
		b.fail("links", fmt.Sprintf("%d", len(expLinks)), fmt.Sprintf("%d", len(gotLinks)),
			fmt.Sprintf("links count mismatch: exp %d, got %d (%+v)", len(expLinks), len(gotLinks), gotLinks))
		return
	}
	for i, exp := range expLinks {
		got := gotLinks[i]
		if exp.Marker != "" && got.Marker != exp.Marker {
			b.mismatch(fmt.Sprintf("links[%d].marker", i), exp.Marker, got.Marker)
		}
		verifyResource(b, fmt.Sprintf("links[%d].resource", i), exp.normalizedResource(), got.Resource)
	}
}
