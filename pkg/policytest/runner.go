package policytest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gobuffalo/flect"
	sigsyaml "sigs.k8s.io/yaml"

	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// RunTestSuite validates policies from PolicyPaths and TestFiles, runs every
// unit test case, and reports coverage gaps.
func RunTestSuite(opts SuiteOptions) (*SuiteResult, error) {
	result := &SuiteResult{
		FailOnUntested: opts.FailOnUntested,
	}

	suitePolicies, suiteIssues := LoadPoliciesFromPaths(opts.PolicyPaths)
	result.ValidationIssues = append(result.ValidationIssues, suiteIssues...)

	testFiles, err := DiscoverTestFiles(opts.TestPaths)
	if err != nil {
		return nil, err
	}

	// Track all unique policies seen across --policies and test file policy_files.
	allPolicies := make([]LoadedPolicy, 0, len(suitePolicies))
	seenPolicyKeys := make(map[string]struct{})
	addUniquePolicies := func(items []LoadedPolicy) []LoadedPolicy {
		var added []LoadedPolicy
		for _, item := range items {
			key := policyIdentityKey(item)
			if _, ok := seenPolicyKeys[key]; ok {
				continue
			}
			seenPolicyKeys[key] = struct{}{}
			allPolicies = append(allPolicies, item)
			added = append(added, item)
		}
		return added
	}

	addUniquePolicies(suitePolicies)
	result.ValidationIssues = append(result.ValidationIssues, ValidatePolicies(suitePolicies)...)

	testCounts := make(map[string]int)

	for _, tfPath := range testFiles {
		fileRes, filePolicies, fileIssues := runSingleTestFile(tfPath, suitePolicies, testCounts)
		newlyAdded := addUniquePolicies(filePolicies)
		result.ValidationIssues = append(result.ValidationIssues, deduplicateIssues(fileIssues, newlyAdded, filePolicies)...)
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

	invalidPolicies := make(map[string]bool)
	for _, issue := range result.ValidationIssues {
		if issue.Severity == "error" && issue.PolicyName != "" {
			invalidPolicies[issue.PolicyName] = true
		}
	}

	for _, lp := range allPolicies {
		p := lp.Policy
		count := testCounts[policyCoverageKey(p)]
		tested := count > 0
		if !tested {
			result.UntestedPolicies = append(result.UntestedPolicies, p.Name)
		}
		result.TotalRules += len(p.Spec.AuditRules) + len(p.Spec.EventRules)
		result.Policies = append(result.Policies, PolicyReport{
			Name:       p.Name,
			Source:     lp.Source,
			APIGroup:   p.Spec.Resource.APIGroup,
			Kind:       p.Spec.Resource.Kind,
			AuditRules: len(p.Spec.AuditRules),
			EventRules: len(p.Spec.EventRules),
			Tested:     tested,
			TestCount:  count,
			Valid:      !invalidPolicies[p.Name],
		})
	}
	result.TotalPolicies = len(allPolicies)

	return result, nil
}

func runSingleTestFile(
	tfPath string,
	suitePolicies []LoadedPolicy,
	testCounts map[string]int,
) (FileResult, []LoadedPolicy, []ValidationIssue) {
	fileRes := FileResult{File: tfPath}

	raw, err := os.ReadFile(tfPath)
	if err != nil {
		fileRes.Error = fmt.Sprintf("failed to read test file: %v", err)
		return fileRes, nil, nil
	}

	var tf TestFile
	if err := sigsyaml.UnmarshalStrict(raw, &tf); err != nil {
		fileRes.Error = fmt.Sprintf("failed to parse test file: %v", err)
		return fileRes, nil, nil
	}

	policyRefPaths := resolveRelativePaths(filepath.Dir(tfPath), tf.AllPolicyFiles())
	filePolicies, loadIssues := LoadPoliciesFromPaths(policyRefPaths)
	valIssues := append(loadIssues, ValidatePolicies(filePolicies)...)

	// Prefer policies explicitly listed in the test file; fall back to --policies.
	candidatePolicies := filePolicies
	if len(candidatePolicies) == 0 {
		candidatePolicies = suitePolicies
	}

	kindLookupPolicies := make([]LoadedPolicy, 0, len(filePolicies)+len(suitePolicies))
	kindLookupPolicies = append(kindLookupPolicies, filePolicies...)
	kindLookupPolicies = append(kindLookupPolicies, suitePolicies...)
	kindResolver := newOfflineKindResolver(kindLookupPolicies)

	for _, tc := range tf.Tests {
		tr := executeTestCase(tc, candidatePolicies, kindResolver, testCounts)
		fileRes.Results = append(fileRes.Results, tr)
	}

	return fileRes, filePolicies, valIssues
}

// deduplicateIssues avoids repeating policy validation errors when a test file's
// policy_files loads the same policies already loaded from --policies.
func deduplicateIssues(fileIssues []ValidationIssue, newlyAdded, filePolicies []LoadedPolicy) []ValidationIssue {
	if len(newlyAdded) == len(filePolicies) {
		return fileIssues
	}
	allowedNames := make(map[string]struct{}, len(newlyAdded))
	for _, lp := range newlyAdded {
		allowedNames[lp.Policy.Name] = struct{}{}
	}
	var filtered []ValidationIssue
	for _, issue := range fileIssues {
		if issue.PolicyName == "" {
			filtered = append(filtered, issue)
			continue
		}
		if _, ok := allowedNames[issue.PolicyName]; ok {
			filtered = append(filtered, issue)
		}
	}
	return filtered
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

func executeTestCase(
	tc TestCase,
	policies []LoadedPolicy,
	kindResolver processor.KindResolver,
	testCounts map[string]int,
) TestResult {
	tr := TestResult{
		Name:   tc.Name,
		Policy: tc.Policy,
	}

	if strings.TrimSpace(tc.Name) == "" {
		tr.Failures = append(tr.Failures, "test case is missing required 'name' field")
		return tr
	}

	if tc.Audit == nil && len(tc.Event) == 0 {
		tr.Failures = append(tr.Failures, "test case must specify either 'audit' or 'event'")
		return tr
	}
	if tc.Audit != nil && len(tc.Event) > 0 {
		tr.Failures = append(tr.Failures, "test case must not specify both 'audit' and 'event'")
		return tr
	}

	exp := normalizeExpectations(tc)
	if !exp.NoMatch && !hasPositiveAssertion(exp) {
		tr.Failures = append(tr.Failures, "test case has no assertions (set exp_summary, exp_rule, exp_links, ..., or exp_no_match: true)")
		return tr
	}

	target, err := selectTargetPolicy(tc, policies)
	if err != nil {
		tr.Failures = append(tr.Failures, err.Error())
		return tr
	}
	tr.Policy = target.Name
	testCounts[policyCoverageKey(target)]++

	var evalRes *processor.EvaluationResult
	if tc.Audit != nil {
		evalRes, err = processor.EvaluateAuditRules(&target.Spec, tc.Audit, kindResolver)
	} else {
		evalRes, err = processor.EvaluateEventRules(&target.Spec, tc.Event, kindResolver)
	}
	if err != nil {
		tr.Failures = append(tr.Failures, fmt.Sprintf("evaluation error: %v", err))
		return tr
	}

	tr.Matched = evalRes.MatchedRuleIndex >= 0
	tr.MatchedRule = evalRes.MatchedRuleName
	if evalRes.Activity != nil {
		tr.ActualSummary = evalRes.Activity.Spec.Summary
	}

	tr.Failures = verifyExpectations(exp, evalRes)
	tr.Passed = len(tr.Failures) == 0
	return tr
}

func normalizeExpectations(tc TestCase) ExpectedActivity {
	exp := ExpectedActivity{
		NoMatch:         tc.ExpNoMatch,
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

func hasPositiveAssertion(exp ExpectedActivity) bool {
	return exp.Rule != "" ||
		exp.Summary != nil ||
		exp.SummaryContains != "" ||
		exp.SummaryRegex != "" ||
		exp.ChangeSource != "" ||
		exp.Actor != nil ||
		exp.Resource != nil ||
		exp.Tenant != nil ||
		exp.Links != nil
}

func selectTargetPolicy(tc TestCase, policies []LoadedPolicy) (*v1alpha1.ActivityPolicy, error) {
	if len(policies) == 0 {
		return nil, fmt.Errorf("no policies available (add policy_files to test file or pass --policies)")
	}

	if tc.Policy != "" {
		for _, lp := range policies {
			p := lp.Policy
			if p.Name == tc.Policy ||
				formatGroupKind(p.Spec.Resource.APIGroup, p.Spec.Resource.Kind) == tc.Policy ||
				(p.Spec.Resource.APIGroup == "" && p.Spec.Resource.Kind == tc.Policy) {
				return p, nil
			}
		}
		available := make([]string, 0, len(policies))
		for _, lp := range policies {
			available = append(available, lp.Policy.Name)
		}
		return nil, fmt.Errorf("policy %q not found (available: %s)", tc.Policy, strings.Join(available, ", "))
	}

	if len(policies) == 1 {
		return policies[0].Policy, nil
	}

	var matches []*v1alpha1.ActivityPolicy
	if tc.Audit != nil {
		if tc.Audit.ObjectRef == nil || tc.Audit.ObjectRef.Resource == "" {
			return nil, fmt.Errorf("multiple policies loaded; set 'policy' or 'audit.objectRef.resource' to select target policy")
		}
		apiGroup := tc.Audit.ObjectRef.APIGroup
		resName := tc.Audit.ObjectRef.Resource
		for _, lp := range policies {
			p := lp.Policy
			if p.Spec.Resource.APIGroup == apiGroup && matchesResourceKind(p.Spec.Resource.Kind, resName) {
				matches = append(matches, p)
			}
		}
	} else {
		involved := processor.ResolveInvolvedObject(tc.Event)
		if involved == nil {
			return nil, fmt.Errorf("multiple policies loaded; set 'policy' or 'event.regarding' to select target policy")
		}
		kind, _ := involved["kind"].(string)
		apiGroup, _ := involved["apiGroup"].(string)
		if apiGroup == "" {
			if apiVersion, ok := involved["apiVersion"].(string); ok {
				if idx := strings.LastIndex(apiVersion, "/"); idx != -1 {
					apiGroup = apiVersion[:idx]
				}
			}
		}
		if kind == "" {
			return nil, fmt.Errorf("multiple policies loaded; set 'policy' or 'event.regarding.kind' to select target policy")
		}
		for _, lp := range policies {
			p := lp.Policy
			if p.Spec.Resource.APIGroup == apiGroup && strings.EqualFold(p.Spec.Resource.Kind, kind) {
				matches = append(matches, p)
			}
		}
	}

	if len(matches) > 1 {
		var byRuleType []*v1alpha1.ActivityPolicy
		for _, m := range matches {
			if tc.Audit != nil && len(m.Spec.AuditRules) > 0 {
				byRuleType = append(byRuleType, m)
			} else if tc.Event != nil && len(m.Spec.EventRules) > 0 {
				byRuleType = append(byRuleType, m)
			}
		}
		if len(byRuleType) == 1 {
			return byRuleType[0], nil
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.Name)
		}
		return nil, fmt.Errorf("ambiguous target policy (%s); set 'policy' explicitly", strings.Join(names, ", "))
	}

	return nil, fmt.Errorf("no loaded policy matches input resource; set 'policy' explicitly")
}

func formatGroupKind(apiGroup, kind string) string {
	if apiGroup == "" {
		return kind
	}
	return apiGroup + "/" + kind
}

func matchesResourceKind(kind, resource string) bool {
	if kind == "" || resource == "" {
		return false
	}
	lowerKind := strings.ToLower(kind)
	lowerRes := strings.ToLower(resource)
	if lowerKind == lowerRes {
		return true
	}
	if strings.ToLower(flect.Pluralize(lowerKind)) == lowerRes {
		return true
	}
	if strings.ToLower(flect.Singularize(lowerRes)) == lowerKind {
		return true
	}
	// Handle PascalCase compounds (e.g. HTTPProxy -> httpproxies) where flect
	// works better on the original casing before lowercasing.
	if strings.EqualFold(flect.Pluralize(kind), resource) {
		return true
	}
	return false
}

func verifyExpectations(exp ExpectedActivity, evalRes *processor.EvaluationResult) []string {
	matched := evalRes.MatchedRuleIndex >= 0

	if exp.NoMatch {
		if matched {
			summary := ""
			if evalRes.Activity != nil {
				summary = evalRes.Activity.Spec.Summary
			}
			return []string{
				fmt.Sprintf("expected no rule to match, but rule %q matched with summary %q",
					evalRes.MatchedRuleName, summary),
			}
		}
		return nil
	}

	if !matched {
		return []string{"expected a rule to match, but no rule matched"}
	}

	act := evalRes.Activity
	var failures []string

	if exp.Rule != "" && evalRes.MatchedRuleName != exp.Rule {
		failures = append(failures, fmt.Sprintf("rule mismatch:\n  exp: %q\n  got: %q", exp.Rule, evalRes.MatchedRuleName))
	}

	if exp.Summary != nil && act.Spec.Summary != *exp.Summary {
		failures = append(failures, fmt.Sprintf("summary mismatch:\n  exp: %q\n  got: %q", *exp.Summary, act.Spec.Summary))
	}

	if exp.SummaryContains != "" && !strings.Contains(act.Spec.Summary, exp.SummaryContains) {
		failures = append(failures, fmt.Sprintf("summary missing substring:\n  exp substring: %q\n  got summary:   %q", exp.SummaryContains, act.Spec.Summary))
	}

	if exp.SummaryRegex != "" {
		re, err := regexp.Compile(exp.SummaryRegex)
		if err != nil {
			failures = append(failures, fmt.Sprintf("invalid exp_summary_regex %q: %v", exp.SummaryRegex, err))
		} else if !re.MatchString(act.Spec.Summary) {
			failures = append(failures, fmt.Sprintf("summary did not match regex:\n  exp regex:   %q\n  got summary: %q", exp.SummaryRegex, act.Spec.Summary))
		}
	}

	if exp.ChangeSource != "" && act.Spec.ChangeSource != exp.ChangeSource {
		failures = append(failures, fmt.Sprintf("change_source mismatch: exp %q, got %q", exp.ChangeSource, act.Spec.ChangeSource))
	}

	if exp.Actor != nil {
		failures = append(failures, verifyActor(*exp.Actor, act.Spec.Actor)...)
	}

	if exp.Resource != nil {
		failures = append(failures, verifyResource("resource", *exp.Resource, act.Spec.Resource)...)
	}

	if exp.Tenant != nil {
		if exp.Tenant.Type != "" && act.Spec.Tenant.Type != exp.Tenant.Type {
			failures = append(failures, fmt.Sprintf("tenant.type mismatch: exp %q, got %q", exp.Tenant.Type, act.Spec.Tenant.Type))
		}
		if exp.Tenant.Name != nil && act.Spec.Tenant.Name != *exp.Tenant.Name {
			failures = append(failures, fmt.Sprintf("tenant.name mismatch: exp %q, got %q", *exp.Tenant.Name, act.Spec.Tenant.Name))
		}
	}

	if exp.Links != nil {
		failures = append(failures, verifyLinks(exp.Links, act.Spec.Links)...)
	}

	return failures
}

func verifyActor(exp ExpectedActor, got v1alpha1.ActivityActor) []string {
	var failures []string
	if exp.Type != "" && got.Type != exp.Type {
		failures = append(failures, fmt.Sprintf("actor.type mismatch: exp %q, got %q", exp.Type, got.Type))
	}
	if exp.Name != "" && got.Name != exp.Name {
		failures = append(failures, fmt.Sprintf("actor.name mismatch: exp %q, got %q", exp.Name, got.Name))
	}
	if exp.UID != "" && got.UID != exp.UID {
		failures = append(failures, fmt.Sprintf("actor.uid mismatch: exp %q, got %q", exp.UID, got.UID))
	}
	if exp.Email != "" && got.Email != exp.Email {
		failures = append(failures, fmt.Sprintf("actor.email mismatch: exp %q, got %q", exp.Email, got.Email))
	}
	return failures
}

func verifyResource(label string, exp ExpectedResource, got v1alpha1.ActivityResource) []string {
	var failures []string
	if exp.APIGroup != nil && got.APIGroup != *exp.APIGroup {
		failures = append(failures, fmt.Sprintf("%s.apiGroup mismatch: exp %q, got %q", label, *exp.APIGroup, got.APIGroup))
	}
	if exp.APIVersion != "" && got.APIVersion != exp.APIVersion {
		failures = append(failures, fmt.Sprintf("%s.apiVersion mismatch: exp %q, got %q", label, exp.APIVersion, got.APIVersion))
	}
	if exp.Kind != "" && got.Kind != exp.Kind {
		failures = append(failures, fmt.Sprintf("%s.kind mismatch: exp %q, got %q", label, exp.Kind, got.Kind))
	}
	if exp.Name != "" && got.Name != exp.Name {
		failures = append(failures, fmt.Sprintf("%s.name mismatch: exp %q, got %q", label, exp.Name, got.Name))
	}
	if exp.Namespace != nil && got.Namespace != *exp.Namespace {
		failures = append(failures, fmt.Sprintf("%s.namespace mismatch: exp %q, got %q", label, *exp.Namespace, got.Namespace))
	}
	if exp.UID != "" && got.UID != exp.UID {
		failures = append(failures, fmt.Sprintf("%s.uid mismatch: exp %q, got %q", label, exp.UID, got.UID))
	}
	return failures
}

func verifyLinks(expLinks []ExpectedLink, gotLinks []v1alpha1.ActivityLink) []string {
	if len(expLinks) != len(gotLinks) {
		return []string{fmt.Sprintf("links count mismatch: exp %d, got %d (%+v)", len(expLinks), len(gotLinks), gotLinks)}
	}
	var failures []string
	for i, exp := range expLinks {
		got := gotLinks[i]
		if exp.Marker != "" && got.Marker != exp.Marker {
			failures = append(failures, fmt.Sprintf("links[%d].marker mismatch: exp %q, got %q", i, exp.Marker, got.Marker))
		}
		resExp := exp.normalizedResource()
		failures = append(failures, verifyResource(fmt.Sprintf("links[%d].resource", i), resExp, got.Resource)...)
	}
	return failures
}

func newOfflineKindResolver(policies []LoadedPolicy) processor.KindResolver {
	byGroupResource := make(map[string]string, len(policies)*2)
	for _, lp := range policies {
		p := lp.Policy
		if p == nil || p.Spec.Resource.Kind == "" {
			continue
		}
		group := strings.ToLower(p.Spec.Resource.APIGroup)
		kind := p.Spec.Resource.Kind
		plural := strings.ToLower(flect.Pluralize(kind))
		byGroupResource[group+"/"+plural] = kind
		byGroupResource[group+"/"+strings.ToLower(kind)] = kind
	}

	return func(apiGroup, resource string) (string, error) {
		key := strings.ToLower(apiGroup) + "/" + strings.ToLower(resource)
		if kind, ok := byGroupResource[key]; ok {
			return kind, nil
		}
		// Fallback for linked resources not in the loaded policy set (e.g., actorRef or cross-resource links).
		return flect.Pascalize(flect.Singularize(resource)), nil
	}
}

func policyIdentityKey(lp LoadedPolicy) string {
	return policyCoverageKey(lp.Policy)
}

func policyCoverageKey(p *v1alpha1.ActivityPolicy) string {
	if p.Name != "" {
		return p.Name
	}
	return formatGroupKind(p.Spec.Resource.APIGroup, p.Spec.Resource.Kind)
}
