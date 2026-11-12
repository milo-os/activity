package cel

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

// PolicyExpressionType indicates whether an expression is a match or summary expression.
type PolicyExpressionType string

const (
	// MatchExpression is a CEL expression that returns a boolean.
	MatchExpression PolicyExpressionType = "match"
	// SummaryExpression is a CEL template expression with {{ }} delimiters.
	SummaryExpression PolicyExpressionType = "summary"
)

// PolicyRuleType indicates whether a rule is for audit or event processing.
type PolicyRuleType string

const (
	// AuditRule is a rule that processes audit log entries.
	AuditRule PolicyRuleType = "audit"
	// EventRule is a rule that processes Kubernetes events.
	EventRule PolicyRuleType = "event"
)

// summaryTemplateRegex matches {{ expression }} patterns in summary templates.
var summaryTemplateRegex = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// auditEnvironment creates a CEL environment for audit rule expressions.
// Wraps NewAuditEnvironment for internal use.
func auditEnvironment(collector *linkCollector) (*cel.Env, error) {
	return NewAuditEnvironment(collector)
}

// eventEnvironment creates a CEL environment for event rule expressions.
// Wraps NewEventEnvironment for internal use.
func eventEnvironment(collector *linkCollector) (*cel.Env, error) {
	return NewEventEnvironment(collector)
}

// ValidatePolicyExpression validates a CEL expression used in an ActivityPolicy rule.
// It checks for syntax errors and type correctness based on the expression and rule types.
func ValidatePolicyExpression(expression string, exprType PolicyExpressionType, ruleType PolicyRuleType) error {
	if expression == "" {
		return fmt.Errorf("expression cannot be empty")
	}

	// Get the appropriate environment based on rule type
	// Pass nil collector since we're just validating, not collecting links
	var env *cel.Env
	var err error

	switch ruleType {
	case AuditRule:
		env, err = auditEnvironment(nil)
	case EventRule:
		env, err = eventEnvironment(nil)
	default:
		return fmt.Errorf("unknown rule type: %s", ruleType)
	}

	if err != nil {
		return fmt.Errorf("failed to create CEL environment: %w", err)
	}

	switch exprType {
	case MatchExpression:
		return validateMatchExpression(env, expression)
	case SummaryExpression:
		return validateSummaryExpression(env, expression)
	default:
		return fmt.Errorf("unknown expression type: %s", exprType)
	}
}

// validateMatchExpression validates a match expression that should return a boolean.
func validateMatchExpression(env *cel.Env, expression string) error {
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return formatPolicyError(issues.Err(), "match")
	}

	// Match expressions must return boolean
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return fmt.Errorf("match expression must return a boolean, got %v", ast.OutputType())
	}

	return nil
}

// validateSummaryExpression validates a summary template expression with {{ }} delimiters.
func validateSummaryExpression(env *cel.Env, expression string) error {
	// Check for balanced delimiters
	openCount := strings.Count(expression, "{{")
	closeCount := strings.Count(expression, "}}")
	if openCount != closeCount {
		return fmt.Errorf("error compiling template: missing closing delimiter")
	}

	// Extract all {{ expression }} blocks from the template
	matches := summaryTemplateRegex.FindAllStringSubmatch(expression, -1)

	if len(matches) == 0 {
		// No template expressions found - this is valid but probably unintended
		// We allow it as the user might want a static summary
		return nil
	}

	// Validate each embedded CEL expression
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		embeddedExpr := strings.TrimSpace(match[1])
		if embeddedExpr == "" {
			return fmt.Errorf("empty expression in template: %s", match[0])
		}

		// Compile the embedded expression
		ast, issues := env.Compile(embeddedExpr)
		if issues != nil && issues.Err() != nil {
			return formatPolicyError(issues.Err(), "summary")
		}

		// Summary template expressions should return a string
		// The link() function returns a string, so check for string type
		if !ast.OutputType().IsExactType(cel.StringType) && !ast.OutputType().IsExactType(cel.DynType) {
			return fmt.Errorf("expression '{{ %s }}' in summary must return a string, got %v", embeddedExpr, ast.OutputType())
		}
	}

	return nil
}

// formatPolicyError formats a CEL error into a user-friendly message.
func formatPolicyError(err error, context string) error {
	errStr := err.Error()

	// Check for common error patterns and provide helpful messages
	if strings.Contains(errStr, "undeclared reference") {
		return fmt.Errorf("invalid %s expression: %s. "+
			"For audit rules: audit.verb, audit.objectRef, audit.user, audit.responseStatus, audit.responseObject, audit.requestObject, actor, actorRef, kind, interface. "+
			"For event rules: event.reason, event.type, event.regarding.name, actor, actorRef, source, interface. "+
			"Also available: link(displayText, resourceRef)", context, errStr)
	}

	if strings.Contains(errStr, "found no matching overload") {
		return fmt.Errorf("invalid %s expression: function call error - %s. "+
			"Available function: link(displayText, resourceRef)", context, errStr)
	}

	return fmt.Errorf("invalid %s expression: %s", context, errStr)
}

// Link represents a link extracted from a summary template.
type Link struct {
	Marker   string
	Resource map[string]interface{}
}

// linkCollector collects links during CEL evaluation.
type linkCollector struct {
	links []Link
}

// addLink records a link. A link identical to one already recorded (same
// marker and same resource) is skipped, so a template that mentions the
// actor both as {{ actor }} and as {{ link(actor, actorRef) }} yields one
// link rather than two.
func (c *linkCollector) addLink(displayText string, resource interface{}) {
	var resourceMap map[string]interface{}
	switch r := resource.(type) {
	case map[string]interface{}:
		resourceMap = r
	case map[ref.Val]ref.Val:
		// Convert CEL map to Go map
		resourceMap = make(map[string]interface{}, len(r))
		for k, v := range r {
			if keyStr, ok := k.Value().(string); ok {
				resourceMap[keyStr] = v.Value()
			}
		}
	default:
		return
	}
	for _, l := range c.links {
		if l.Marker == displayText && reflect.DeepEqual(l.Resource, resourceMap) {
			return
		}
	}
	c.links = append(c.links, Link{Marker: displayText, Resource: resourceMap})
}

// EvaluateAuditMatch evaluates a match expression against an audit log entry.
func EvaluateAuditMatch(expression string, audit interface{}) (bool, error) {
	auditMap, err := toMap(audit)
	if err != nil {
		return false, fmt.Errorf("failed to convert audit to map: %w", err)
	}
	return EvaluateAuditMatchMap(expression, auditMap)
}

// EvaluateAuditMatchMap evaluates a match expression against an audit log entry map.
func EvaluateAuditMatchMap(expression string, auditMap map[string]interface{}) (bool, error) {
	env, err := auditEnvironment(nil) // No link collection for match expressions
	if err != nil {
		return false, fmt.Errorf("failed to create audit environment: %w", err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("failed to compile match expression: %w", issues.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return false, fmt.Errorf("failed to create program: %w", err)
	}

	out, _, err := prg.Eval(BuildAuditVars(auditMap))
	if err != nil {
		return false, fmt.Errorf("failed to evaluate match expression: %w", err)
	}

	result, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("match expression did not return a boolean")
	}

	return result, nil
}

// EvaluateAuditSummary evaluates a summary template against an audit log entry.
func EvaluateAuditSummary(template string, audit interface{}) (string, []Link, error) {
	auditMap, err := toMap(audit)
	if err != nil {
		return "", nil, fmt.Errorf("failed to convert audit to map: %w", err)
	}
	return EvaluateAuditSummaryMap(template, auditMap)
}

// EvaluateAuditSummaryMap evaluates a summary template against an audit log entry map.
func EvaluateAuditSummaryMap(template string, auditMap map[string]interface{}) (string, []Link, error) {
	return EvaluateAuditSummaryVars(template, BuildAuditVars(auditMap))
}

// EvaluateAuditSummaryVars evaluates a summary template against prebuilt
// audit variables (see BuildAuditVars). Callers use it to render with
// variables they have adjusted, for example to hide a staff actor's identity.
func EvaluateAuditSummaryVars(template string, vars map[string]interface{}) (string, []Link, error) {
	collector := &linkCollector{}
	env, err := auditEnvironment(collector)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create audit environment: %w", err)
	}

	result, err := evaluateSummaryTemplate(env, template, vars, collector)
	if err != nil {
		return "", nil, err
	}
	return result, collector.links, nil
}

// EvaluateEventMatch evaluates a match expression against a Kubernetes event.
func EvaluateEventMatch(expression string, event map[string]interface{}) (bool, error) {
	env, err := eventEnvironment(nil) // No link collection for match expressions
	if err != nil {
		return false, fmt.Errorf("failed to create event environment: %w", err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("failed to compile match expression: %w", issues.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return false, fmt.Errorf("failed to create program: %w", err)
	}

	out, _, err := prg.Eval(BuildEventVars(event))
	if err != nil {
		return false, fmt.Errorf("failed to evaluate match expression: %w", err)
	}

	result, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("match expression did not return a boolean")
	}

	return result, nil
}

// EvaluateEventSummary evaluates a summary template against a Kubernetes event.
func EvaluateEventSummary(template string, event map[string]interface{}) (string, []Link, error) {
	collector := &linkCollector{}
	env, err := eventEnvironment(collector)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create event environment: %w", err)
	}

	result, err := evaluateSummaryTemplate(env, template, BuildEventVars(event), collector)
	if err != nil {
		return "", nil, err
	}
	return result, collector.links, nil
}

// evaluateSummaryTemplate evaluates a summary template with the given variables.
// Links are captured by the linkCollector in the CEL environment during evaluation.
// If any CEL expression in the template fails to compile or evaluate, an error is
// returned so the caller can route the event to the dead-letter queue for retry.
//
// A bare {{ actor }} for a human user also registers a link to actorRef, with
// the exact inserted text as its marker, exactly as if the template had said
// {{ link(actor, actorRef) }}. That makes every actor mention a precise link
// that consumers can enrich (for example with a display name) without
// searching the summary text.
func evaluateSummaryTemplate(env *cel.Env, template string, vars map[string]interface{}, collector *linkCollector) (string, error) {
	var evalErrors []error
	result := summaryTemplateRegex.ReplaceAllStringFunc(template, func(match string) string {
		// Extract the expression from {{ expression }}
		submatches := summaryTemplateRegex.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		expr := strings.TrimSpace(submatches[1])

		// Compile and evaluate the expression
		// The link() function in the environment will capture links automatically
		ast, issues := env.Compile(expr)
		if issues != nil && issues.Err() != nil {
			evalErrors = append(evalErrors, fmt.Errorf("compile %q: %w", expr, issues.Err()))
			return fmt.Sprintf("[ERROR: %v]", issues.Err())
		}

		prg, err := env.Program(ast)
		if err != nil {
			evalErrors = append(evalErrors, fmt.Errorf("program %q: %w", expr, err))
			return fmt.Sprintf("[ERROR: %v]", err)
		}

		out, _, err := prg.Eval(vars)
		if err != nil {
			evalErrors = append(evalErrors, fmt.Errorf("eval %q: %w", expr, err))
			return fmt.Sprintf("[ERROR: %v]", err)
		}

		text := fmt.Sprintf("%v", out.Value())
		if expr == "actor" {
			registerActorLink(collector, text, vars)
		}
		return text
	})

	if len(evalErrors) > 0 {
		return result, fmt.Errorf("summary template evaluation failed: %w", errors.Join(evalErrors...))
	}

	return result, nil
}

// registerActorLink records a link from a bare {{ actor }} mention to the
// human actor's actorRef. Non-human actors (system, service accounts,
// controllers) are left as plain text.
func registerActorLink(collector *linkCollector, text string, vars map[string]interface{}) {
	if collector == nil || text == "" {
		return
	}
	actorRef, ok := vars["actorRef"].(map[string]interface{})
	if !ok || actorRef["type"] != "user" {
		return
	}
	collector.addLink(text, actorRef)
}

// toMap converts an interface to a map[string]interface{}.
func toMap(v interface{}) (map[string]interface{}, error) {
	if m, ok := v.(map[string]interface{}); ok {
		return m, nil
	}

	// For structured types, we'd need to use reflection or JSON marshaling
	// For now, return an error for unsupported types
	return nil, fmt.Errorf("unsupported type for map conversion: %T", v)
}
