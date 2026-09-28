package policy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity"
)

// activityPolicyStrategy implements behavior for ActivityPolicy resources.
type activityPolicyStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

// activityPolicyStatusStrategy implements behavior for ActivityPolicy status updates.
type activityPolicyStatusStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

// NewStrategy creates a new ActivityPolicy strategy with the given typer.
func NewStrategy(typer runtime.ObjectTyper) activityPolicyStrategy {
	return activityPolicyStrategy{
		ObjectTyper:   typer,
		NameGenerator: names.SimpleNameGenerator,
	}
}

// NewStatusStrategy creates a new ActivityPolicy status strategy with the given typer.
func NewStatusStrategy(typer runtime.ObjectTyper) activityPolicyStatusStrategy {
	return activityPolicyStatusStrategy{
		ObjectTyper:   typer,
		NameGenerator: names.SimpleNameGenerator,
	}
}

// NamespaceScoped returns false because ActivityPolicy is cluster-scoped.
func (s activityPolicyStrategy) NamespaceScoped() bool {
	return false
}

// PrepareForCreate clears status and sets defaults before creation.
func (s activityPolicyStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	policy := obj.(*activity.ActivityPolicy)
	// Clear status on creation - it will be set by the controller
	policy.Status = activity.ActivityPolicyStatus{}
	policy.Generation = 1
}

// PrepareForUpdate preserves status when spec is updated.
func (s activityPolicyStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	newPolicy := obj.(*activity.ActivityPolicy)
	oldPolicy := old.(*activity.ActivityPolicy)
	// Preserve status - only the status subresource can update it
	newPolicy.Status = oldPolicy.Status
	newPolicy.Generation = oldPolicy.Generation
	if !equality.Semantic.DeepEqual(newPolicy.Spec, oldPolicy.Spec) {
		newPolicy.Generation++
	}
}

// Validate validates a new ActivityPolicy.
func (s activityPolicyStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	policy := obj.(*activity.ActivityPolicy)
	return ValidateActivityPolicy(policy)
}

// WarningsOnCreate returns warnings for the creation of the given object.
func (s activityPolicyStrategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string {
	policy := obj.(*activity.ActivityPolicy)
	return warningsForPolicy(policy)
}

// AllowCreateOnUpdate returns false because ActivityPolicy should be created via POST.
func (s activityPolicyStrategy) AllowCreateOnUpdate() bool {
	return false
}

// AllowUnconditionalUpdate allows unconditional updates to ActivityPolicy.
func (s activityPolicyStrategy) AllowUnconditionalUpdate() bool {
	return true
}

// Canonicalize normalizes the object after validation.
func (s activityPolicyStrategy) Canonicalize(obj runtime.Object) {
	// No canonicalization needed
}

// ValidateUpdate validates an update to an ActivityPolicy.
func (s activityPolicyStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	policy := obj.(*activity.ActivityPolicy)
	oldPolicy := old.(*activity.ActivityPolicy)

	allErrs := ValidateActivityPolicy(policy)

	// Prevent changing the target resource (apiGroup + kind) after creation
	if policy.Spec.Resource.APIGroup != oldPolicy.Spec.Resource.APIGroup ||
		policy.Spec.Resource.Kind != oldPolicy.Spec.Resource.Kind {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "resource"),
			policy.Spec.Resource,
			"resource (apiGroup and kind) is immutable after creation",
		))
	}

	return allErrs
}

// WarningsOnUpdate returns warnings for the update of the given object.
func (s activityPolicyStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	policy := obj.(*activity.ActivityPolicy)
	return warningsForPolicy(policy)
}

// ValidateActivityPolicy validates an ActivityPolicy and returns field errors.
func ValidateActivityPolicy(policy *activity.ActivityPolicy) field.ErrorList {
	return ValidateActivityPolicySpec(&policy.Spec, field.NewPath("spec"))
}

// ValidateActivityPolicySpec validates an ActivityPolicySpec and returns field errors.
// The specPath parameter allows customizing the field path for error messages.
func ValidateActivityPolicySpec(spec *activity.ActivityPolicySpec, specPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	// Validate required resource fields
	resourcePath := specPath.Child("resource")
	// Note: apiGroup can be empty string for core API resources (v1)
	// e.g., pods, services, configmaps, secrets, namespaces
	if spec.Resource.Kind == "" {
		allErrs = append(allErrs, field.Required(resourcePath.Child("kind"),
			"specify the kind of resource this policy targets (e.g., 'Deployment', 'Service')"))
	}

	// Validate audit rules
	auditRulesPath := specPath.Child("auditRules")
	auditRuleNames := make(map[string]int)
	for i, rule := range spec.AuditRules {
		rulePath := auditRulesPath.Index(i)
		allErrs = append(allErrs, validatePolicyRule(rule, rulePath, cel.AuditRule)...)

		// Check for duplicate names within auditRules
		if rule.Name != "" {
			if firstIdx, exists := auditRuleNames[rule.Name]; exists {
				allErrs = append(allErrs, field.Duplicate(rulePath.Child("name"),
					fmt.Sprintf("%s (conflicts with auditRules[%d])", rule.Name, firstIdx)))
			} else {
				auditRuleNames[rule.Name] = i
			}
		}
	}

	// Validate event rules
	eventRulesPath := specPath.Child("eventRules")
	eventRuleNames := make(map[string]int)
	for i, rule := range spec.EventRules {
		rulePath := eventRulesPath.Index(i)
		allErrs = append(allErrs, validatePolicyRule(rule, rulePath, cel.EventRule)...)

		// Check for duplicate names within eventRules
		if rule.Name != "" {
			if firstIdx, exists := eventRuleNames[rule.Name]; exists {
				allErrs = append(allErrs, field.Duplicate(rulePath.Child("name"),
					fmt.Sprintf("%s (conflicts with eventRules[%d])", rule.Name, firstIdx)))
			} else {
				eventRuleNames[rule.Name] = i
			}
		}
	}

	return allErrs
}

// warningsForPolicy returns warnings for an ActivityPolicy.
func warningsForPolicy(policy *activity.ActivityPolicy) []string {
	var warnings []string
	if len(policy.Spec.AuditRules) == 0 && len(policy.Spec.EventRules) == 0 {
		warnings = append(warnings, "policy has no rules defined and will have no effect")
	}
	return warnings
}

// validatePolicyRule validates a single ActivityPolicyRule.
func validatePolicyRule(rule activity.ActivityPolicyRule, path *field.Path, ruleType cel.PolicyRuleType) field.ErrorList {
	allErrs := field.ErrorList{}

	// Validate name field - required
	if rule.Name == "" {
		allErrs = append(allErrs, field.Required(path.Child("name"),
			"provide a unique name for this rule (e.g., 'create', 'update-status')"))
	} else {
		if errs := validation.IsDNS1123Subdomain(rule.Name); len(errs) > 0 {
			allErrs = append(allErrs, field.Invalid(path.Child("name"), rule.Name,
				fmt.Sprintf("must be a valid DNS subdomain (lowercase alphanumeric and hyphens, start/end with alphanumeric): %v", errs)))
		}
	}

	// Validate match expression
	if rule.Match == "" {
		allErrs = append(allErrs, field.Required(path.Child("match"),
			"provide a CEL expression that determines when this rule applies (e.g., 'verb == \"create\"')"))
	} else {
		if err := cel.ValidatePolicyExpression(rule.Match, cel.MatchExpression, ruleType); err != nil {
			allErrs = append(allErrs, field.Invalid(path.Child("match"), rule.Match, err.Error()))
		}
	}

	// Validate summary template
	if rule.Summary == "" {
		allErrs = append(allErrs, field.Required(path.Child("summary"),
			"provide a template for the activity summary (e.g., '{{ actor }} created {{ kind }}')"))
	} else {
		if err := cel.ValidatePolicyExpression(rule.Summary, cel.SummaryExpression, ruleType); err != nil {
			allErrs = append(allErrs, field.Invalid(path.Child("summary"), rule.Summary, err.Error()))
		}
	}

	return allErrs
}

// GetAttrs returns labels and fields of a given ActivityPolicy for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	policy, ok := obj.(*activity.ActivityPolicy)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not an ActivityPolicy")
	}
	return policy.ObjectMeta.Labels, SelectableFields(policy), nil
}

// SelectableFields returns the fields that can be used in field selectors.
func SelectableFields(policy *activity.ActivityPolicy) fields.Set {
	return generic.ObjectMetaFieldsSet(&policy.ObjectMeta, false)
}

// MatchActivityPolicy returns a matcher for ActivityPolicy resources.
func MatchActivityPolicy(label labels.Selector, field fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    field,
		GetAttrs: GetAttrs,
	}
}

// Status strategy methods

// NamespaceScoped returns false because ActivityPolicy is cluster-scoped.
func (s activityPolicyStatusStrategy) NamespaceScoped() bool {
	return false
}

// PrepareForUpdate clears fields that are not allowed to be set by end users on status update.
// Only status changes are allowed; spec changes are reverted.
func (s activityPolicyStatusStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	newPolicy := obj.(*activity.ActivityPolicy)
	oldPolicy := old.(*activity.ActivityPolicy)
	// Preserve spec, only allow status changes
	newPolicy.Spec = oldPolicy.Spec
	newPolicy.Generation = oldPolicy.Generation
	newPolicy.ObjectMeta.Labels = oldPolicy.ObjectMeta.Labels
	newPolicy.ObjectMeta.Annotations = oldPolicy.ObjectMeta.Annotations
}

// ValidateUpdate validates a status update to an ActivityPolicy.
func (s activityPolicyStatusStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	// Status updates don't need validation
	return nil
}

// WarningsOnUpdate returns warnings for the status update of the given object.
func (s activityPolicyStatusStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}

// AllowCreateOnUpdate returns false because ActivityPolicy should be created via POST.
func (s activityPolicyStatusStrategy) AllowCreateOnUpdate() bool {
	return false
}

// AllowUnconditionalUpdate allows unconditional updates to ActivityPolicy status.
func (s activityPolicyStatusStrategy) AllowUnconditionalUpdate() bool {
	return true
}

// Canonicalize normalizes the object after validation.
func (s activityPolicyStatusStrategy) Canonicalize(obj runtime.Object) {
	// No canonicalization needed
}

// GetResetFields returns the fields that should be reset on status update.
func (s activityPolicyStatusStrategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	return map[fieldpath.APIVersion]*fieldpath.Set{
		"activity.miloapis.com/v1alpha1": fieldpath.NewSet(
			fieldpath.MakePathOrDie("spec"),
		),
	}
}
