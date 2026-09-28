package controller

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// ActivityPolicyReconciler reconciles ActivityPolicy resources.
type ActivityPolicyReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	RESTMapper meta.RESTMapper
}

// +kubebuilder:rbac:groups=activity.miloapis.com,resources=activitypolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=activity.miloapis.com,resources=activitypolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=activity.miloapis.com,resources=activitypolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

// Reconcile handles the reconciliation of an ActivityPolicy resource.
func (r *ActivityPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ActivityPolicy
	var policy v1alpha1.ActivityPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "unable to fetch ActivityPolicy")
			return ctrl.Result{}, err
		}
		// Object was deleted, nothing to do
		return ctrl.Result{}, nil
	}

	// Determine Ready condition
	condition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.Now(),
	}

	// Validate that the target resource type exists in the cluster
	if validationErr := r.validateResourceExists(policy.Spec.Resource.APIGroup, policy.Spec.Resource.Kind); validationErr != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ResourceNotFound"
		condition.Message = validationErr.Error()
		logger.V(2).Info("ActivityPolicy targets non-existent resource", "error", validationErr)

		if err := r.updatePolicyStatus(ctx, &policy, condition); err != nil {
			return ctrl.Result{}, fmt.Errorf("error updating policy status: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Validate all CEL expressions in the policy
	if validationErr := r.validatePolicy(&policy); validationErr != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ValidationFailed"
		condition.Message = validationErr.Error()
		logger.V(2).Info("ActivityPolicy failed validation", "error", validationErr)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Valid"
		condition.Message = "All rules validated successfully"
		logger.V(2).Info("ActivityPolicy validated successfully")
	}

	// Update status
	if err := r.updatePolicyStatus(ctx, &policy, condition); err != nil {
		return ctrl.Result{}, fmt.Errorf("error updating policy status: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActivityPolicyReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ActivityPolicy{}).
		// Watch CRDs to re-reconcile policies when new types are registered.
		Watches(
			&apiextensionsv1.CustomResourceDefinition{},
			handler.EnqueueRequestsFromMapFunc(r.mapCRDToActivityPolicies),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		Complete(r)
}

// mapCRDToActivityPolicies finds ActivityPolicies that reference the CRD's Kind.
func (r *ActivityPolicyReconciler) mapCRDToActivityPolicies(ctx context.Context, obj client.Object) []reconcile.Request {
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return nil
	}

	// Extract the Kind and API group from the CRD
	crdKind := crd.Spec.Names.Kind
	crdGroup := crd.Spec.Group

	// List all ActivityPolicies
	var policies v1alpha1.ActivityPolicyList
	if err := r.List(ctx, &policies); err != nil {
		log.FromContext(ctx).Error(err, "Failed to list ActivityPolicies for CRD watch")
		return nil
	}

	// Find policies that reference this CRD's Kind
	var requests []reconcile.Request
	for _, policy := range policies.Items {
		if policy.Spec.Resource.Kind == crdKind && policy.Spec.Resource.APIGroup == crdGroup {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				},
			})
		}
	}

	return requests
}

// updatePolicyStatus updates the status of an ActivityPolicy.
func (r *ActivityPolicyReconciler) updatePolicyStatus(ctx context.Context, policy *v1alpha1.ActivityPolicy, condition metav1.Condition) error {
	logger := log.FromContext(ctx)

	// Check if status already has the same condition to avoid unnecessary updates
	for _, c := range policy.Status.Conditions {
		if c.Type == condition.Type &&
			c.Status == condition.Status &&
			c.Reason == condition.Reason &&
			c.Message == condition.Message &&
			c.ObservedGeneration == condition.ObservedGeneration &&
			policy.Status.ObservedGeneration == policy.Generation {
			// Condition already matches, no update needed
			return nil
		}
	}

	// Update the conditions
	found := false
	for i, c := range policy.Status.Conditions {
		if c.Type == condition.Type {
			// Revalidating a new generation is not a readiness transition.
			if c.Status == condition.Status {
				condition.LastTransitionTime = c.LastTransitionTime
			}
			policy.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		policy.Status.Conditions = append(policy.Status.Conditions, condition)
	}

	// Set observed generation
	policy.Status.ObservedGeneration = policy.Generation

	// Update via the status subresource
	if err := r.Status().Update(ctx, policy); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	logger.V(4).Info("Updated ActivityPolicy status", "ready", condition.Status)
	return nil
}

// validatePolicy validates all CEL expressions in the policy.
func (r *ActivityPolicyReconciler) validatePolicy(policy *v1alpha1.ActivityPolicy) error {
	// Validate audit rules
	for i, rule := range policy.Spec.AuditRules {
		if err := cel.ValidatePolicyExpression(rule.Match, cel.MatchExpression, cel.AuditRule); err != nil {
			return fmt.Errorf("audit rule %d match: %w", i, err)
		}
		if err := cel.ValidatePolicyExpression(rule.Summary, cel.SummaryExpression, cel.AuditRule); err != nil {
			return fmt.Errorf("audit rule %d summary: %w", i, err)
		}
	}

	// Validate event rules
	for i, rule := range policy.Spec.EventRules {
		if err := cel.ValidatePolicyExpression(rule.Match, cel.MatchExpression, cel.EventRule); err != nil {
			return fmt.Errorf("event rule %d match: %w", i, err)
		}
		if err := cel.ValidatePolicyExpression(rule.Summary, cel.SummaryExpression, cel.EventRule); err != nil {
			return fmt.Errorf("event rule %d summary: %w", i, err)
		}
	}

	return nil
}

// validateResourceExists checks if the specified apiGroup/kind exists in the cluster.
func (r *ActivityPolicyReconciler) validateResourceExists(apiGroup, kind string) error {
	if r.RESTMapper == nil {
		return nil // Skip validation if no RESTMapper available
	}

	gk := schema.GroupKind{Group: apiGroup, Kind: kind}
	mapping, err := r.RESTMapper.RESTMapping(gk)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return fmt.Errorf("resource %q not found in API group %q - verify the Kind is spelled correctly (case-sensitive) and the CRD is installed", kind, apiGroup)
		}
		return fmt.Errorf("failed to validate resource %s/%s: %w", apiGroup, kind, err)
	}

	// Verify the kind matches exactly (case-sensitive)
	if mapping.GroupVersionKind.Kind != kind {
		return fmt.Errorf("kind mismatch: specified %q but API server has %q", kind, mapping.GroupVersionKind.Kind)
	}

	return nil
}
