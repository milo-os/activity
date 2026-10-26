package reindexjob

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"go.miloapis.com/activity/internal/timeutil"
	"go.miloapis.com/activity/pkg/apis/activity"
)

// reindexJobStrategy implements behavior for ReindexJob resources.
type reindexJobStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

// reindexJobStatusStrategy implements behavior for ReindexJob status updates.
type reindexJobStatusStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

// NewStrategy creates a new ReindexJob strategy with the given typer.
func NewStrategy(typer runtime.ObjectTyper) reindexJobStrategy {
	return reindexJobStrategy{
		ObjectTyper:   typer,
		NameGenerator: names.SimpleNameGenerator,
	}
}

// NewStatusStrategy creates a new ReindexJob status strategy with the given typer.
func NewStatusStrategy(typer runtime.ObjectTyper) reindexJobStatusStrategy {
	return reindexJobStatusStrategy{
		ObjectTyper:   typer,
		NameGenerator: names.SimpleNameGenerator,
	}
}

// NamespaceScoped returns false because ReindexJob is cluster-scoped.
func (s reindexJobStrategy) NamespaceScoped() bool {
	return false
}

// PrepareForCreate clears status and sets defaults before creation.
func (s reindexJobStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	job := obj.(*activity.ReindexJob)
	// Clear status on creation - it will be set by the controller
	job.Status = activity.ReindexJobStatus{}

	// Default endTime to "now" if not specified
	// This ensures the time range defaults to "up to job start time"
	if job.Spec.TimeRange.EndTime == "" {
		job.Spec.TimeRange.EndTime = "now"
	}

	// Set defaults for config if not provided
	if job.Spec.Config == nil {
		job.Spec.Config = &activity.ReindexConfig{}
	}
	if job.Spec.Config.BatchSize == 0 {
		job.Spec.Config.BatchSize = 1000
	}
	if job.Spec.Config.RateLimit == 0 {
		job.Spec.Config.RateLimit = 100
	}
}

// PrepareForUpdate preserves status when spec is updated.
func (s reindexJobStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	newJob := obj.(*activity.ReindexJob)
	oldJob := old.(*activity.ReindexJob)
	// Preserve status - only the status subresource can update it
	newJob.Status = oldJob.Status
}

// Validate validates a new ReindexJob.
func (s reindexJobStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	job := obj.(*activity.ReindexJob)
	return ValidateReindexJob(job)
}

// WarningsOnCreate returns warnings for the creation of the given object.
func (s reindexJobStrategy) WarningsOnCreate(ctx context.Context, obj runtime.Object) []string {
	job := obj.(*activity.ReindexJob)
	return warningsForJob(job)
}

// AllowCreateOnUpdate returns false because ReindexJob should be created via POST.
func (s reindexJobStrategy) AllowCreateOnUpdate(ctx context.Context) bool {
	return false
}

// AllowUnconditionalUpdate allows unconditional updates to ReindexJob.
func (s reindexJobStrategy) AllowUnconditionalUpdate(ctx context.Context) bool {
	return true
}

// Canonicalize normalizes the object after validation.
func (s reindexJobStrategy) Canonicalize(obj runtime.Object) {
	// No canonicalization needed
}

// ValidateUpdate validates an update to a ReindexJob.
func (s reindexJobStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	job := obj.(*activity.ReindexJob)
	oldJob := old.(*activity.ReindexJob)

	allErrs := ValidateReindexJob(job)

	// Prevent changing spec after job has started
	if oldJob.Status.Phase == activity.ReindexJobRunning ||
		oldJob.Status.Phase == activity.ReindexJobSucceeded ||
		oldJob.Status.Phase == activity.ReindexJobFailed {
		// Spec is immutable once job starts running
		if job.Spec.TimeRange.StartTime != oldJob.Spec.TimeRange.StartTime {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "timeRange", "startTime"),
				job.Spec.TimeRange.StartTime,
				"spec is immutable after job starts",
			))
		}
	}

	return allErrs
}

// WarningsOnUpdate returns warnings for the update of the given object.
func (s reindexJobStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	job := obj.(*activity.ReindexJob)
	return warningsForJob(job)
}

// ValidateReindexJob validates a ReindexJob and returns field errors.
func ValidateReindexJob(job *activity.ReindexJob) field.ErrorList {
	return ValidateReindexJobSpec(&job.Spec, field.NewPath("spec"))
}

// ValidateReindexJobSpec validates a ReindexJobSpec and returns field errors.
func ValidateReindexJobSpec(spec *activity.ReindexJobSpec, specPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	// Use a single reference time for all time parsing to prevent sub-second drift
	now := time.Now()

	// Validate required fields
	timeRangePath := specPath.Child("timeRange")
	if spec.TimeRange.StartTime == "" {
		allErrs = append(allErrs, field.Required(timeRangePath.Child("startTime"),
			"startTime is required"))
	}

	// Parse and validate startTime format
	var startTime time.Time
	if spec.TimeRange.StartTime != "" {
		parsedStart, err := timeutil.ParseFlexibleTime(spec.TimeRange.StartTime, now)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(timeRangePath.Child("startTime"),
				spec.TimeRange.StartTime,
				fmt.Sprintf("invalid time format: %v (use RFC3339 like '2026-02-01T00:00:00Z' or relative like 'now-7d')", err)))
		} else {
			startTime = parsedStart
		}
	}

	// Parse and validate endTime format if specified
	var endTime time.Time
	endTimeSpecified := spec.TimeRange.EndTime != ""
	if endTimeSpecified {
		parsedEnd, err := timeutil.ParseFlexibleTime(spec.TimeRange.EndTime, now)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(timeRangePath.Child("endTime"),
				spec.TimeRange.EndTime,
				fmt.Sprintf("invalid time format: %v (use RFC3339 like '2026-02-01T00:00:00Z' or relative like 'now')", err)))
		} else {
			endTime = parsedEnd
		}
	}

	// Validate time range logic (only if both times parsed successfully)
	if !startTime.IsZero() && endTimeSpecified && !endTime.IsZero() {
		if !startTime.Before(endTime) {
			allErrs = append(allErrs, field.Invalid(timeRangePath, spec.TimeRange,
				"startTime must be before endTime"))
		}
	}

	// NOTE: Retention window validation is deferred to worker execution time.
	// Validating at creation time (with "now" as reference) could pass validation
	// but fail at execution if the job is queued for hours. The worker validates
	// against the retention window at the actual execution time and provides
	// a clear error message if data has been pruned.

	// Validate policySelector (names and matchLabels are mutually exclusive)
	if spec.PolicySelector != nil {
		selectorPath := specPath.Child("policySelector")
		if len(spec.PolicySelector.Names) > 0 && len(spec.PolicySelector.MatchLabels) > 0 {
			allErrs = append(allErrs, field.Invalid(selectorPath, spec.PolicySelector,
				"names and matchLabels are mutually exclusive"))
		}
	}

	// Validate config bounds
	if spec.Config != nil {
		configPath := specPath.Child("config")

		// Validate batchSize bounds (100-10000)
		if spec.Config.BatchSize != 0 {
			if spec.Config.BatchSize < 100 || spec.Config.BatchSize > 10000 {
				allErrs = append(allErrs, field.Invalid(configPath.Child("batchSize"),
					spec.Config.BatchSize,
					"batchSize must be between 100 and 10000"))
			}
		}

		// Validate rateLimit bounds (10-1000)
		if spec.Config.RateLimit != 0 {
			if spec.Config.RateLimit < 10 || spec.Config.RateLimit > 1000 {
				allErrs = append(allErrs, field.Invalid(configPath.Child("rateLimit"),
					spec.Config.RateLimit,
					"rateLimit must be between 10 and 1000"))
			}
		}
	}

	return allErrs
}

// warningsForJob returns warnings for a ReindexJob.
func warningsForJob(job *activity.ReindexJob) []string {
	var warnings []string

	// Use a consistent reference time for parsing
	now := time.Now()

	// Warn if time range is very large
	if job.Spec.TimeRange.StartTime != "" {
		rangeStart, err := timeutil.ParseFlexibleTime(job.Spec.TimeRange.StartTime, now)
		if err == nil {
			// Default endTime to "now" if not specified
			endTimeStr := job.Spec.TimeRange.EndTime
			if endTimeStr == "" {
				endTimeStr = "now"
			}

			rangeEnd, err := timeutil.ParseFlexibleTime(endTimeStr, now)
			if err == nil {
				rangeDuration := rangeEnd.Sub(rangeStart)
				if rangeDuration > 7*24*time.Hour {
					warnings = append(warnings, fmt.Sprintf(
						"time range is large (%s) - consider using smaller batches or running during off-hours",
						rangeDuration.String()))
				}
			}
		}
	}

	// Warn if dry-run mode is enabled
	if job.Spec.Config != nil && job.Spec.Config.DryRun {
		warnings = append(warnings, "dry-run mode enabled - no activities will be written")
	}

	return warnings
}

// GetAttrs returns labels and fields of a given ReindexJob for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	job, ok := obj.(*activity.ReindexJob)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a ReindexJob")
	}
	return job.ObjectMeta.Labels, SelectableFields(job), nil
}

// SelectableFields returns the fields that can be used in field selectors.
func SelectableFields(job *activity.ReindexJob) fields.Set {
	return generic.ObjectMetaFieldsSet(&job.ObjectMeta, false)
}

// MatchReindexJob returns a matcher for ReindexJob resources.
func MatchReindexJob(label labels.Selector, field fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    field,
		GetAttrs: GetAttrs,
	}
}

// Status strategy methods

// NamespaceScoped returns false because ReindexJob is cluster-scoped.
func (s reindexJobStatusStrategy) NamespaceScoped() bool {
	return false
}

// PrepareForUpdate clears fields that are not allowed to be set by end users on status update.
// Only status changes are allowed; spec changes are reverted.
func (s reindexJobStatusStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	newJob := obj.(*activity.ReindexJob)
	oldJob := old.(*activity.ReindexJob)
	// Preserve spec, only allow status changes
	newJob.Spec = oldJob.Spec
	newJob.ObjectMeta.Labels = oldJob.ObjectMeta.Labels
	newJob.ObjectMeta.Annotations = oldJob.ObjectMeta.Annotations
}

// ValidateUpdate validates a status update to a ReindexJob.
func (s reindexJobStatusStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	// Status updates don't need validation
	return nil
}

// WarningsOnUpdate returns warnings for the status update of the given object.
func (s reindexJobStatusStrategy) WarningsOnUpdate(ctx context.Context, obj, old runtime.Object) []string {
	return nil
}

// AllowCreateOnUpdate returns false because ReindexJob should be created via POST.
func (s reindexJobStatusStrategy) AllowCreateOnUpdate(ctx context.Context) bool {
	return false
}

// AllowUnconditionalUpdate allows unconditional updates to ReindexJob status.
func (s reindexJobStatusStrategy) AllowUnconditionalUpdate(ctx context.Context) bool {
	return true
}

// Canonicalize normalizes the object after validation.
func (s reindexJobStatusStrategy) Canonicalize(obj runtime.Object) {
	// No canonicalization needed
}

// GetResetFields returns the fields that should be reset on status update.
func (s reindexJobStatusStrategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	return map[fieldpath.APIVersion]*fieldpath.Set{
		"activity.miloapis.com/v1alpha1": fieldpath.NewSet(
			fieldpath.MakePathOrDie("spec"),
		),
	}
}
