package policytest

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// The evaluator reproduces the activity processor's routing and evaluation.
// Where production logic is exported it is called directly (PolicyCache,
// EvaluateCompiledAuditRules, CompiledRule.EvaluateEventMatch); the policy
// and event-rule loops are mirrored here because production does not expose
// skipped rules or matched rule names. Drift tests in
// internal/processor/policytest_drift_test.go run both sides on the same
// inputs and must be updated together with any production change.

// EvaluatorOptions configures offline resource/kind resolution.
type EvaluatorOptions struct {
	// ResourceKinds are explicit resource->kind mappings per API group.
	ResourceKinds map[string]map[string]string
	// KindHints are extra policies whose spec.resource kinds help resolve
	// link kinds; the evaluated policies are always used as hints.
	KindHints []LoadedPolicy
}

// Evaluator evaluates audit events and Kubernetes events against a set of
// policies the way the activity processor does.
type Evaluator struct {
	cache    *activityprocessor.PolicyCache
	kinds    *kindIndex
	policies map[string]evaluatorPolicy
	order    []string
}

type evaluatorPolicy struct {
	loaded      LoadedPolicy
	resource    string
	resourceHow string
}

// Evaluation is the outcome of evaluating one input.
type Evaluation struct {
	// Ignored explains why production would not evaluate the input at all
	// (no objectRef, no involved object, or no kind).
	Ignored string
	// Target is the apiGroup/resource (audit) or apiGroup/Kind (event) the
	// input routes to.
	Target string
	// Candidates are the policies routed to, in production evaluation order.
	Candidates []string
	// Evaluated are the policies actually evaluated, in order.
	Evaluated []string

	Matched         bool
	MatchedPolicy   string
	MatchedRuleType string
	MatchedRule     string
	Activity        *v1alpha1.Activity

	Warnings []TestWarning
	// Errors are failures production dead-letters (audit rule runtime
	// errors, event summary errors).
	Errors []EvaluationError
}

// EvaluationError describes a rule evaluation failure that production sends
// to the dead-letter queue.
type EvaluationError struct {
	Policy     string `json:"policy"`
	RuleType   string `json:"ruleType"`
	Rule       string `json:"rule,omitempty"`
	Expression string `json:"expression,omitempty"`
	Message    string `json:"message"`
}

func (e EvaluationError) String() string {
	return fmt.Sprintf("policy %q %s rule %q: %s", e.Policy, e.RuleType, e.Rule, e.Message)
}

// NewEvaluator compiles policies into a production PolicyCache. Policies are
// added in metadata.name order, which is the order the processor's informer
// lists them after a restart, so cross-policy "first match wins" behaves the
// same. Unnamed policies and later duplicates of a name are not evaluated;
// both are reported as validation errors elsewhere.
func NewEvaluator(policies []LoadedPolicy, opts EvaluatorOptions) *Evaluator {
	hints := make([]LoadedPolicy, 0, len(policies)+len(opts.KindHints))
	hints = append(hints, policies...)
	hints = append(hints, opts.KindHints...)

	e := &Evaluator{
		cache:    activityprocessor.NewPolicyCache(),
		kinds:    newKindIndex(opts.ResourceKinds, hints),
		policies: make(map[string]evaluatorPolicy),
	}

	sorted := make([]LoadedPolicy, 0, len(policies))
	for _, lp := range policies {
		if lp.Policy == nil || lp.Policy.Name == "" {
			continue
		}
		sorted = append(sorted, lp)
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Policy.Name < sorted[j].Policy.Name })

	for _, lp := range sorted {
		name := lp.Policy.Name
		if _, dup := e.policies[name]; dup {
			continue
		}
		resource, how := e.kinds.resourceForKind(lp.Policy.Spec.Resource.APIGroup, lp.Policy.Spec.Resource.Kind)
		if err := e.cache.Add(lp.Policy, resource); err != nil {
			continue
		}
		e.policies[name] = evaluatorPolicy{loaded: lp, resource: resource, resourceHow: how}
		e.order = append(e.order, name)
	}
	return e
}

// ResolvePolicy finds policies by metadata.name, or by "apiGroup/Kind" or
// Kind. A name match wins; otherwise every policy for that resource is returned.
func (e *Evaluator) ResolvePolicy(ref string) []LoadedPolicy {
	if p, ok := e.policies[ref]; ok {
		return []LoadedPolicy{p.loaded}
	}
	var out []LoadedPolicy
	for _, name := range e.order {
		p := e.policies[name].loaded.Policy
		if formatGroupKind(p.Spec.Resource.APIGroup, p.Spec.Resource.Kind) == ref ||
			(p.Spec.Resource.APIGroup == "" && p.Spec.Resource.Kind == ref) {
			out = append(out, e.policies[name].loaded)
		}
	}
	return out
}

// PolicyNames returns evaluated policy names in production order.
func (e *Evaluator) PolicyNames() []string {
	return append([]string(nil), e.order...)
}

// describeTarget explains which resource a policy routes on.
func (e *Evaluator) describeTarget(name string, audit bool) string {
	p := e.policies[name]
	spec := p.loaded.Policy.Spec.Resource
	if audit {
		return fmt.Sprintf("%s (resource %q, %s)", groupLabel(spec.APIGroup)+"/"+spec.Kind, p.resource, howLabel(p.resourceHow))
	}
	return groupLabel(spec.APIGroup) + "/" + spec.Kind
}

func groupLabel(group string) string {
	if group == "" {
		return "core"
	}
	return group
}

func howLabel(how string) string {
	switch how {
	case resolvedExplicit:
		return "from resource_kinds"
	case resolvedPolicy:
		return "from loaded policies"
	default:
		return "guessed by pluralization; declare resource_kinds if wrong"
	}
}

func compiledNames(policies []*activityprocessor.CompiledPolicy) []string {
	out := make([]string, 0, len(policies))
	for _, p := range policies {
		out = append(out, p.Name)
	}
	return out
}

func filterCompiled(policies []*activityprocessor.CompiledPolicy, name string) []*activityprocessor.CompiledPolicy {
	for _, p := range policies {
		if p.Name == name {
			return []*activityprocessor.CompiledPolicy{p}
		}
	}
	return nil
}

// EvaluateAudit evaluates an audit event. When only is non-empty, just that
// policy (by name) is evaluated; it must be one production routes the input to.
func (e *Evaluator) EvaluateAudit(audit *auditv1.Event, only string) (*Evaluation, error) {
	ev := &Evaluation{}
	if audit.ObjectRef == nil {
		ev.Ignored = "audit event has no objectRef; the processor ignores it"
		return ev, nil
	}
	ref := audit.ObjectRef
	ev.Target = groupLabel(ref.APIGroup) + "/" + ref.Resource

	candidates := e.cache.Get(ref.APIGroup, ref.Resource)
	ev.Candidates = compiledNames(candidates)

	auditMap, err := auditToMap(audit)
	if err != nil {
		return nil, fmt.Errorf("failed to convert audit event: %w", err)
	}

	toEvaluate := candidates
	if only != "" {
		toEvaluate = filterCompiled(candidates, only)
		if len(toEvaluate) == 0 {
			return nil, fmt.Errorf("policy %q targets %s, but the input targets %s; production never evaluates this policy for this input",
				only, e.describeTarget(only, true), ev.Target)
		}
	}

	e.auditChain(ev, toEvaluate, auditMap, audit, true)

	if only != "" && len(candidates) > 1 {
		shadow := &Evaluation{}
		e.auditChain(shadow, candidates, auditMap, audit, false)
		if shadow.Matched && shadow.MatchedPolicy != only {
			ev.Warnings = append(ev.Warnings, shadowWarning(shadow, only))
		}
	}
	return ev, nil
}

func shadowWarning(shadow *Evaluation, only string) TestWarning {
	return TestWarning{
		Type:     WarningShadowed,
		Policy:   shadow.MatchedPolicy,
		RuleType: shadow.MatchedRuleType,
		Rule:     shadow.MatchedRule,
		Message: fmt.Sprintf("in production, policy %q rule %q matches this input before policy %q is reached",
			shadow.MatchedPolicy, shadow.MatchedRule, only),
	}
}

// auditChain mirrors the processor's audit loop: policies in order, first
// match wins, and a policy whose evaluation errors is dead-lettered and
// skipped in favour of the next one.
func (e *Evaluator) auditChain(
	ev *Evaluation,
	policies []*activityprocessor.CompiledPolicy,
	auditMap map[string]any,
	audit *auditv1.Event,
	report bool,
) {
	for _, policy := range policies {
		ev.Evaluated = append(ev.Evaluated, policy.Name)
		if report {
			for i := range policy.AuditRules {
				if !policy.AuditRules[i].Valid {
					ev.Warnings = append(ev.Warnings, compileSkipWarning(policy, "audit", i))
				}
			}
		}

		var guessed []TestWarning
		resolver := func(apiGroup, resource string) (string, error) {
			kind, how := e.kinds.kindForResource(apiGroup, resource)
			if how == resolvedGuessed {
				guessed = append(guessed, TestWarning{
					Type:   WarningKindGuessed,
					Policy: policy.Name,
					Message: fmt.Sprintf("link kind for resource %q in group %q was guessed as %q by pluralization; production uses the cluster's RESTMapper (declare it under resource_kinds to make this exact)",
						resource, groupLabel(apiGroup), kind),
				})
			}
			return kind, nil
		}

		activity, ruleIndex, err := activityprocessor.EvaluateCompiledAuditRules(policy, auditMap, audit, resolver)
		if err != nil {
			if report {
				ev.Errors = append(ev.Errors, auditEvaluationError(policy, ruleIndex, err))
			}
			continue
		}
		if activity == nil {
			continue
		}

		ev.Matched = true
		ev.MatchedPolicy = policy.Name
		ev.MatchedRuleType = "audit"
		ev.MatchedRule = ruleNameAt(policy.OriginalPolicy.Spec.AuditRules, ruleIndex)
		ev.Activity = activity
		if report {
			ev.Warnings = append(ev.Warnings, guessed...)
		}
		return
	}
}

func auditEvaluationError(policy *activityprocessor.CompiledPolicy, ruleIndex int, err error) EvaluationError {
	ee := EvaluationError{
		Policy:   policy.Name,
		RuleType: "audit",
		Message:  fmt.Sprintf("%v (production dead-letters this input for this policy and continues with the next policy)", err),
	}
	if ruleIndex >= 0 && ruleIndex < len(policy.AuditRules) {
		rule := policy.AuditRules[ruleIndex]
		ee.Rule = ruleNameAt(policy.OriginalPolicy.Spec.AuditRules, ruleIndex)
		ee.Expression = rule.Match
		if msg := err.Error(); strings.Contains(msg, " summary: ") || strings.Contains(msg, " build: ") {
			ee.Expression = rule.Summary
		}
	}
	return ee
}

func compileSkipWarning(policy *activityprocessor.CompiledPolicy, ruleType string, i int) TestWarning {
	rule := policy.AuditRules
	specRules := policy.OriginalPolicy.Spec.AuditRules
	if ruleType == "event" {
		rule = policy.EventRules
		specRules = policy.OriginalPolicy.Spec.EventRules
	}
	expr := rule[i].Match
	if strings.HasPrefix(rule[i].CompileError, "summary") {
		expr = rule[i].Summary
	}
	return TestWarning{
		Type:       WarningRuleSkipped,
		Policy:     policy.Name,
		RuleType:   ruleType,
		Rule:       ruleNameAt(specRules, i),
		Expression: expr,
		Message:    fmt.Sprintf("rule skipped: it failed to compile (%s), so production never evaluates it", rule[i].CompileError),
	}
}

func ruleNameAt(rules []v1alpha1.ActivityPolicyRule, i int) string {
	if i < len(rules) && rules[i].Name != "" {
		return rules[i].Name
	}
	return fmt.Sprintf("#%d", i)
}

// EventTarget mirrors EventProcessor.processMessage: the subject is
// "regarding", else "involvedObject"; kind and apiGroup come from it, with
// apiGroup parsed from apiVersion when absent.
func EventTarget(event map[string]any) (involved map[string]any, apiGroup, kind string) {
	involved = processor.ResolveInvolvedObject(event)
	if involved == nil {
		return nil, "", ""
	}
	kind = processor.GetNestedString(involved, "kind")
	apiGroup = processor.GetNestedString(involved, "apiGroup")
	if apiGroup == "" {
		apiVersion := processor.GetNestedString(involved, "apiVersion")
		if idx := strings.LastIndex(apiVersion, "/"); idx != -1 {
			apiGroup = apiVersion[:idx]
		}
	}
	return involved, apiGroup, kind
}

// normalizeEvent mirrors the processor: CEL always sees event.regarding.
func normalizeEvent(event, involved map[string]any) map[string]any {
	if _, ok := event["regarding"].(map[string]any); ok {
		return event
	}
	normalized := make(map[string]any, len(event)+1)
	for k, v := range event {
		normalized[k] = v
	}
	normalized["regarding"] = involved
	return normalized
}

// EvaluateEvent evaluates a Kubernetes event. When only is non-empty, just
// that policy (by name) is evaluated; it must be one production routes the
// input to.
func (e *Evaluator) EvaluateEvent(event map[string]any, only string) (*Evaluation, error) {
	ev := &Evaluation{}
	involved, apiGroup, kind := EventTarget(event)
	if involved == nil {
		ev.Ignored = "event has neither regarding nor involvedObject; the processor ignores it"
		return ev, nil
	}
	if kind == "" {
		ev.Ignored = "event's regarding/involvedObject has no kind; the processor ignores it"
		return ev, nil
	}
	ev.Target = groupLabel(apiGroup) + "/" + kind

	candidates := e.cache.GetByKind(apiGroup, kind)
	ev.Candidates = compiledNames(candidates)
	normalized := normalizeEvent(event, involved)

	toEvaluate := candidates
	if only != "" {
		toEvaluate = filterCompiled(candidates, only)
		if len(toEvaluate) == 0 {
			return nil, fmt.Errorf("policy %q targets %s, but the input's regarding/involvedObject targets %s; production never evaluates this policy for this input",
				only, e.describeTarget(only, false), ev.Target)
		}
	}

	e.eventChain(ev, toEvaluate, event, normalized, true)

	if only != "" && len(candidates) > 1 {
		shadow := &Evaluation{}
		e.eventChain(shadow, candidates, event, normalized, false)
		if shadow.Matched && shadow.MatchedPolicy != only {
			ev.Warnings = append(ev.Warnings, shadowWarning(shadow, only))
		}
	}
	return ev, nil
}

// eventChain mirrors PolicyCache.MatchEvent: policies in order, first match
// wins; rules that failed to compile or whose match expression errors are
// skipped; a summary error aborts evaluation (production dead-letters the event).
func (e *Evaluator) eventChain(
	ev *Evaluation,
	policies []*activityprocessor.CompiledPolicy,
	rawEvent map[string]any,
	normalized map[string]any,
	report bool,
) {
	for _, policy := range policies {
		ev.Evaluated = append(ev.Evaluated, policy.Name)
		specRules := policy.OriginalPolicy.Spec.EventRules

		for i := range policy.EventRules {
			rule := &policy.EventRules[i]
			if !rule.Valid {
				if report {
					ev.Warnings = append(ev.Warnings, compileSkipWarning(policy, "event", i))
				}
				continue
			}

			matched, err := rule.EvaluateEventMatch(normalized)
			if err != nil {
				if report {
					ev.Warnings = append(ev.Warnings, TestWarning{
						Type:       WarningRuleSkipped,
						Policy:     policy.Name,
						RuleType:   "event",
						Rule:       ruleNameAt(specRules, i),
						Expression: rule.Match,
						Message:    fmt.Sprintf("rule skipped: match expression failed (%v); production logs this and continues with the next rule", err),
					})
				}
				continue
			}
			if !matched {
				continue
			}

			summary, links, err := cel.EvaluateEventSummary(rule.Summary, normalized)
			if err != nil {
				if report {
					ev.Errors = append(ev.Errors, EvaluationError{
						Policy:     policy.Name,
						RuleType:   "event",
						Rule:       ruleNameAt(specRules, i),
						Expression: rule.Summary,
						Message:    fmt.Sprintf("failed to evaluate summary: %v (production dead-letters this event and stops evaluating policies)", err),
					})
				}
				return
			}

			builder := &processor.ActivityBuilder{APIGroup: policy.APIGroup, Kind: policy.Kind}
			activity, err := builder.BuildFromEvent(rawEvent, summary, nil, nil)
			if err != nil {
				// Unreachable without links or a resolver; keep the error visible anyway.
				ev.Errors = append(ev.Errors, EvaluationError{Policy: policy.Name, RuleType: "event", Rule: ruleNameAt(specRules, i), Message: err.Error()})
				return
			}
			activity.Spec.Links = productionEventLinks(links)

			ev.Matched = true
			ev.MatchedPolicy = policy.Name
			ev.MatchedRuleType = "event"
			ev.MatchedRule = ruleNameAt(specRules, i)
			ev.Activity = activity
			return
		}
	}
}

// productionEventLinks reproduces EventProcessor.buildActivity's link
// conversion, which copies only apiGroup/kind/name/namespace/uid from the
// link() target. Unlike the audit path (processor.ConvertLinks) it does not
// split apiGroup out of apiVersion or fall back to metadata.name, so e.g.
// link(x, event.regarding) with only apiVersion set yields an empty apiGroup.
//
// TODO(parity-followup): once the live event path uses processor.ConvertLinks,
// replace this with ConvertLinks(links, nil) and update the drift tests.
func productionEventLinks(links []cel.Link) []v1alpha1.ActivityLink {
	var out []v1alpha1.ActivityLink
	for _, link := range links {
		out = append(out, v1alpha1.ActivityLink{
			Marker: link.Marker,
			Resource: v1alpha1.ActivityResource{
				APIGroup:  stringField(link.Resource, "apiGroup"),
				Kind:      stringField(link.Resource, "kind"),
				Name:      stringField(link.Resource, "name"),
				Namespace: stringField(link.Resource, "namespace"),
				UID:       stringField(link.Resource, "uid"),
			},
		})
	}
	return out
}

// auditToMap mirrors the processor's JSON round-trip conversion.
func auditToMap(audit *auditv1.Event) (map[string]any, error) {
	data, err := json.Marshal(audit)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func formatGroupKind(apiGroup, kind string) string {
	if apiGroup == "" {
		return kind
	}
	return apiGroup + "/" + kind
}
