package activityprocessor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"

	internalcel "go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// summaryTemplateRegex matches {{ expression }} patterns in summary templates.
var summaryTemplateRegex = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// CompiledRule represents a pre-compiled policy rule ready for execution.
type CompiledRule struct {
	// Match is the original match expression.
	Match string
	// Summary is the original summary template.
	Summary string
	// MatchProgram is the pre-compiled CEL program for match evaluation.
	MatchProgram cel.Program
	// SummaryTemplates contains pre-compiled CEL programs for each template expression.
	SummaryTemplates []compiledTemplate
	// Valid indicates if the rule compiled successfully.
	Valid bool
	// CompileError holds any error from compilation.
	CompileError string
}

// compiledTemplate represents a single {{ expression }} in a summary template.
type compiledTemplate struct {
	// FullMatch is the original {{ expression }} string
	FullMatch string
	// Expression is the CEL expression without {{ }}
	Expression string
	// Program is the pre-compiled CEL program
	Program cel.Program
}

// CompiledPolicy represents a pre-compiled ActivityPolicy ready for execution.
type CompiledPolicy struct {
	// Name is the policy name.
	Name string
	// APIGroup is the target resource's API group.
	APIGroup string
	// Kind is the target resource's kind.
	Kind string
	// Resource is the plural resource name (for audit event matching).
	Resource string
	// AuditRules are the compiled audit rules.
	AuditRules []CompiledRule
	// EventRules are the compiled event rules.
	EventRules []CompiledRule
	// ResourceVersion is the policy's resource version for cache invalidation.
	ResourceVersion string
	// OriginalPolicy is the original policy for metrics and logging.
	OriginalPolicy *v1alpha1.ActivityPolicy
}

// PolicyCache provides thread-safe caching of pre-compiled ActivityPolicy resources.
type PolicyCache struct {
	mu sync.RWMutex

	// policies stores compiled policies indexed by apiGroup/resource (plural)
	// Multiple policies can target the same resource.
	policies map[string][]*CompiledPolicy

	// policiesByKind stores compiled policies indexed by apiGroup/kind
	// for event lookups since events use Kind not Resource.
	policiesByKind map[string][]*CompiledPolicy
}

// NewPolicyCache creates a new policy cache.
func NewPolicyCache() *PolicyCache {
	return &PolicyCache{
		policies:       make(map[string][]*CompiledPolicy),
		policiesByKind: make(map[string][]*CompiledPolicy),
	}
}

// Add compiles and adds a policy to the cache.
func (c *PolicyCache) Add(policy *v1alpha1.ActivityPolicy, resource string) error {
	compiled, err := c.compile(policy, resource)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Index by apiGroup/resource for audit lookups
	key := policyKey(policy.Spec.Resource.APIGroup, resource)
	c.policies[key] = append(c.policies[key], compiled)

	// Index by apiGroup/kind for event lookups
	kindKey := policyKey(policy.Spec.Resource.APIGroup, policy.Spec.Resource.Kind)
	c.policiesByKind[kindKey] = append(c.policiesByKind[kindKey], compiled)

	klog.V(2).InfoS("Added compiled policy to cache",
		"policy", policy.Name,
		"key", key,
		"kindKey", kindKey,
		"auditRules", len(compiled.AuditRules),
		"eventRules", len(compiled.EventRules),
	)

	return nil
}

// Update removes the old policy and adds the new one.
func (c *PolicyCache) Update(oldPolicy, newPolicy *v1alpha1.ActivityPolicy, oldResource, newResource string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove old policy from both indexes
	oldKey := policyKey(oldPolicy.Spec.Resource.APIGroup, oldResource)
	c.removeLocked(oldKey, oldPolicy.Name)
	oldKindKey := policyKey(oldPolicy.Spec.Resource.APIGroup, oldPolicy.Spec.Resource.Kind)
	c.removeKindLocked(oldKindKey, oldPolicy.Name)

	// Compile and add new policy
	compiled, err := c.compile(newPolicy, newResource)
	if err != nil {
		return err
	}

	newKey := policyKey(newPolicy.Spec.Resource.APIGroup, newResource)
	c.policies[newKey] = append(c.policies[newKey], compiled)

	newKindKey := policyKey(newPolicy.Spec.Resource.APIGroup, newPolicy.Spec.Resource.Kind)
	c.policiesByKind[newKindKey] = append(c.policiesByKind[newKindKey], compiled)

	klog.V(2).InfoS("Updated compiled policy in cache",
		"policy", newPolicy.Name,
		"oldKey", oldKey,
		"newKey", newKey,
		"oldKindKey", oldKindKey,
		"newKindKey", newKindKey,
	)

	return nil
}

// Remove removes a policy from the cache.
func (c *PolicyCache) Remove(policy *v1alpha1.ActivityPolicy, resource string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := policyKey(policy.Spec.Resource.APIGroup, resource)
	c.removeLocked(key, policy.Name)

	kindKey := policyKey(policy.Spec.Resource.APIGroup, policy.Spec.Resource.Kind)
	c.removeKindLocked(kindKey, policy.Name)

	klog.V(2).InfoS("Removed policy from cache", "policy", policy.Name, "key", key, "kindKey", kindKey)
}

// removeLocked removes a policy by name from a key. Caller must hold the lock.
func (c *PolicyCache) removeLocked(key, policyName string) {
	policies := c.policies[key]
	for i, p := range policies {
		if p.Name == policyName {
			// O(1) removal: swap with last element and truncate.
			policies[i] = policies[len(policies)-1]
			c.policies[key] = policies[:len(policies)-1]
			break
		}
	}
	if len(c.policies[key]) == 0 {
		delete(c.policies, key)
	}
}

// removeKindLocked removes a policy by name from the kind index. Caller must hold the lock.
func (c *PolicyCache) removeKindLocked(kindKey, policyName string) {
	policies := c.policiesByKind[kindKey]
	for i, p := range policies {
		if p.Name == policyName {
			// O(1) removal: swap with last element and truncate.
			policies[i] = policies[len(policies)-1]
			c.policiesByKind[kindKey] = policies[:len(policies)-1]
			break
		}
	}
	if len(c.policiesByKind[kindKey]) == 0 {
		delete(c.policiesByKind, kindKey)
	}
}

// Get returns compiled policies for a given apiGroup and resource.
func (c *PolicyCache) Get(apiGroup, resource string) []*CompiledPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := policyKey(apiGroup, resource)
	return c.policies[key]
}

// GetByKind returns compiled policies for a given apiGroup and kind.
// Used by event processing since events reference Kind not Resource.
func (c *PolicyCache) GetByKind(apiGroup, kind string) []*CompiledPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := policyKey(apiGroup, kind)
	return c.policiesByKind[key]
}

// Len returns the total number of policies in the cache.
func (c *PolicyCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0
	for _, policies := range c.policies {
		count += len(policies)
	}
	return count
}

// compile compiles an ActivityPolicy into a CompiledPolicy.
func (c *PolicyCache) compile(policy *v1alpha1.ActivityPolicy, resource string) (*CompiledPolicy, error) {
	compiled := &CompiledPolicy{
		Name:            policy.Name,
		APIGroup:        policy.Spec.Resource.APIGroup,
		Kind:            policy.Spec.Resource.Kind,
		Resource:        resource,
		ResourceVersion: policy.ResourceVersion,
		AuditRules:      make([]CompiledRule, len(policy.Spec.AuditRules)),
		EventRules:      make([]CompiledRule, len(policy.Spec.EventRules)),
		OriginalPolicy:  policy.DeepCopy(),
	}

	// Compile audit rules
	for i, rule := range policy.Spec.AuditRules {
		compiledRule := c.compileAuditRule(rule, policy.Name, i)
		compiled.AuditRules[i] = compiledRule
	}

	// Compile event rules
	for i, rule := range policy.Spec.EventRules {
		compiledRule := c.compileEventRule(rule, policy.Name, i)
		compiled.EventRules[i] = compiledRule
	}

	return compiled, nil
}

// compileAuditRule compiles a single audit rule.
func (c *PolicyCache) compileAuditRule(rule v1alpha1.ActivityPolicyRule, policyName string, ruleIndex int) CompiledRule {
	compiled := CompiledRule{
		Match:   rule.Match,
		Summary: rule.Summary,
		Valid:   true,
	}

	// Create audit environment for compilation
	env, err := auditEnvironment()
	if err != nil {
		compiled.Valid = false
		compiled.CompileError = fmt.Sprintf("failed to create CEL environment: %v", err)
		klog.Warningf("Policy %s audit rule %d: %s", policyName, ruleIndex, compiled.CompileError)
		return compiled
	}

	// Compile match expression
	matchAST, issues := env.Compile(rule.Match)
	if issues != nil && issues.Err() != nil {
		compiled.Valid = false
		compiled.CompileError = fmt.Sprintf("match: %v", issues.Err())
		klog.Warningf("Policy %s audit rule %d: %s", policyName, ruleIndex, compiled.CompileError)
		return compiled
	}

	matchProgram, err := env.Program(matchAST)
	if err != nil {
		compiled.Valid = false
		compiled.CompileError = fmt.Sprintf("match program: %v", err)
		klog.Warningf("Policy %s audit rule %d: %s", policyName, ruleIndex, compiled.CompileError)
		return compiled
	}
	compiled.MatchProgram = matchProgram

	// Compile summary template expressions
	templates, err := compileSummaryTemplate(env, rule.Summary)
	if err != nil {
		compiled.Valid = false
		compiled.CompileError = fmt.Sprintf("summary: %v", err)
		klog.Warningf("Policy %s audit rule %d: %s", policyName, ruleIndex, compiled.CompileError)
		return compiled
	}
	compiled.SummaryTemplates = templates

	return compiled
}

// compileEventRule compiles a single event rule.
func (c *PolicyCache) compileEventRule(rule v1alpha1.ActivityPolicyRule, policyName string, ruleIndex int) CompiledRule {
	compiled := CompiledRule{
		Match:   rule.Match,
		Summary: rule.Summary,
		Valid:   true,
	}

	// Create event environment for compilation
	env, err := eventEnvironment()
	if err != nil {
		compiled.Valid = false
		compiled.CompileError = fmt.Sprintf("failed to create CEL environment: %v", err)
		klog.Warningf("Policy %s event rule %d: %s", policyName, ruleIndex, compiled.CompileError)
		return compiled
	}

	// Compile match expression
	matchAST, issues := env.Compile(rule.Match)
	if issues != nil && issues.Err() != nil {
		compiled.Valid = false
		compiled.CompileError = fmt.Sprintf("match: %v", issues.Err())
		klog.Warningf("Policy %s event rule %d: %s", policyName, ruleIndex, compiled.CompileError)
		return compiled
	}

	matchProgram, err := env.Program(matchAST)
	if err != nil {
		compiled.Valid = false
		compiled.CompileError = fmt.Sprintf("match program: %v", err)
		klog.Warningf("Policy %s event rule %d: %s", policyName, ruleIndex, compiled.CompileError)
		return compiled
	}
	compiled.MatchProgram = matchProgram

	// Compile summary template expressions
	templates, err := compileSummaryTemplate(env, rule.Summary)
	if err != nil {
		compiled.Valid = false
		compiled.CompileError = fmt.Sprintf("summary: %v", err)
		klog.Warningf("Policy %s event rule %d: %s", policyName, ruleIndex, compiled.CompileError)
		return compiled
	}
	compiled.SummaryTemplates = templates

	return compiled
}

// compileSummaryTemplate compiles all {{ expression }} blocks in a summary template.
func compileSummaryTemplate(env *cel.Env, template string) ([]compiledTemplate, error) {
	matches := summaryTemplateRegex.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	templates := make([]compiledTemplate, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		expr := strings.TrimSpace(match[1])
		if expr == "" {
			return nil, fmt.Errorf("empty expression in template: %s", match[0])
		}

		ast, issues := env.Compile(expr)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("expression '%s': %w", expr, issues.Err())
		}

		prg, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("expression '%s': %w", expr, err)
		}

		templates = append(templates, compiledTemplate{
			FullMatch:  match[0],
			Expression: expr,
			Program:    prg,
		})
	}

	return templates, nil
}

// auditEnvironment creates a CEL environment for audit rule expressions.
// Uses the shared environment from internal/cel package.
func auditEnvironment() (*cel.Env, error) {
	return internalcel.NewAuditEnvironment(nil)
}

// eventEnvironment creates a CEL environment for event rule expressions.
// Uses the shared environment from internal/cel package.
func eventEnvironment() (*cel.Env, error) {
	return internalcel.NewEventEnvironment(nil)
}

// EvaluateAuditRules evaluates audit rules against an audit event using pre-compiled programs.
// Returns the index of the matching rule, the generated summary, and whether a match was found.
func (r *CompiledRule) EvaluateAuditMatch(auditMap map[string]any) (bool, error) {
	if !r.Valid || r.MatchProgram == nil {
		return false, nil
	}

	vars := internalcel.BuildAuditVars(auditMap)

	out, _, err := r.MatchProgram.Eval(vars)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate match: %w", err)
	}

	result, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("match expression did not return boolean")
	}

	return result, nil
}

// EvaluateSummary evaluates the summary template using pre-compiled programs.
func (r *CompiledRule) EvaluateSummary(vars map[string]any) (string, error) {
	if len(r.SummaryTemplates) == 0 {
		return r.Summary, nil
	}

	result := r.Summary
	for _, tmpl := range r.SummaryTemplates {
		out, _, err := tmpl.Program.Eval(vars)
		if err != nil {
			return "", fmt.Errorf("failed to evaluate summary expression '%s': %w", tmpl.Expression, err)
		}
		result = strings.Replace(result, tmpl.FullMatch, fmt.Sprintf("%v", out.Value()), 1)
	}

	return result, nil
}

// EvaluateEventMatch evaluates the match expression against a Kubernetes event.
func (r *CompiledRule) EvaluateEventMatch(eventMap map[string]any) (bool, error) {
	if !r.Valid || r.MatchProgram == nil {
		return false, nil
	}

	vars := internalcel.BuildEventVars(eventMap)

	out, _, err := r.MatchProgram.Eval(vars)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate match: %w", err)
	}

	result, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("match expression did not return boolean")
	}

	return result, nil
}

// EvaluateCompiledAuditRules evaluates pre-compiled audit rules against an audit event.
// Returns the generated Activity, the matching rule index, and any error.
// Returns (nil, -1, nil) if no rule matched.
//
// Use EvaluateCompiledAuditRulesWithResolver to also enrich the resulting
// activity with user display names; this convenience wrapper passes nil.
func EvaluateCompiledAuditRules(
	policy *CompiledPolicy,
	auditMap map[string]any,
	audit *auditv1.Event,
	resolveKind processor.KindResolver,
) (*v1alpha1.Activity, int, error) {
	return EvaluateCompiledAuditRulesWithResolver(policy, auditMap, audit, resolveKind, nil)
}

// EvaluateCompiledAuditRulesWithResolver is like EvaluateCompiledAuditRules
// but enriches the activity actor and any User-typed link targets with
// display names looked up via resolver.
func EvaluateCompiledAuditRulesWithResolver(
	policy *CompiledPolicy,
	auditMap map[string]any,
	audit *auditv1.Event,
	resolveKind processor.KindResolver,
	resolver processor.UserResolver,
) (*v1alpha1.Activity, int, error) {
	for i := range policy.AuditRules {
		rule := &policy.AuditRules[i]
		if !rule.Valid {
			continue
		}

		matched, err := rule.EvaluateAuditMatch(auditMap)
		if err != nil {
			return nil, i, fmt.Errorf("rule %d match: %w", i, err)
		}

		if matched {
			// Use the interpreted summary evaluation (not pre-compiled EvaluateSummary) because
			// link() calls capture links via a side-channel collector that must be freshly
			// created per evaluation. The pre-compiled programs don't support link collection.
			summary, links, err := internalcel.EvaluateAuditSummaryMap(rule.Summary, auditMap)
			if err != nil {
				return nil, i, fmt.Errorf("rule %d summary: %w", i, err)
			}

			builder := &processor.ActivityBuilder{
				APIGroup:     policy.APIGroup,
				Kind:         policy.Kind,
				UserResolver: resolver,
			}
			activity, err := builder.BuildFromAudit(audit, summary, links, resolveKind)
			if err != nil {
				return nil, i, fmt.Errorf("rule %d build: %w", i, err)
			}

			return activity, i, nil
		}
	}

	return nil, -1, nil
}

// MatchEvent implements processor.EventPolicyLookup.
// It looks up matching event rules for the given apiGroup/kind and evaluates them
// against the provided event map. Returns the first matching result, or nil if no policy matched.
func (c *PolicyCache) MatchEvent(apiGroup, kind string, eventMap map[string]any) (*processor.MatchedPolicy, error) {
	policies := c.GetByKind(apiGroup, kind)
	if len(policies) == 0 {
		return nil, nil
	}

	// First matching policy wins
	for _, policy := range policies {
		for i := range policy.EventRules {
			rule := &policy.EventRules[i]
			if !rule.Valid {
				continue
			}

			// Evaluate match expression
			matched, err := rule.EvaluateEventMatch(eventMap)
			if err != nil {
				eventJSON, _ := json.Marshal(eventMap)
				klog.V(2).InfoS("Failed to evaluate event match",
					"policy", policy.Name,
					"ruleIndex", i,
					"error", err,
					"eventJSON", truncateString(string(eventJSON), 4096),
				)
				continue
			}

			if matched {
				// Evaluate summary using internalcel.EvaluateEventSummary for proper link collection
				summary, links, err := internalcel.EvaluateEventSummary(rule.Summary, eventMap)
				if err != nil {
					return nil, processor.NewPolicyEvaluationError(
						policy.Name, i,
						fmt.Errorf("failed to evaluate summary: %w", err),
					)
				}

				return &processor.MatchedPolicy{
					PolicyName: policy.Name,
					Generation: policy.OriginalPolicy.Generation,
					APIGroup:   policy.APIGroup,
					Kind:       policy.Kind,
					Summary:    summary,
					Links:      links,
				}, nil
			}
		}
	}

	return nil, nil
}
