package reindex

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// Reindexer orchestrates the re-indexing process by querying historical events
// via the Activity API server and publishing regenerated activities to NATS.
type Reindexer struct {
	client      client.Client
	js          nats.JetStreamContext
	rateLimiter *RateLimiter
	publisher   *Publisher

	// policyCache holds pre-compiled policies for efficient evaluation.
	policyCache *activityprocessor.PolicyCache
	// kindResolver resolves plural resource names to Kind.
	kindResolver processor.KindResolver
	// resourceResolver resolves Kind to plural resource name.
	resourceResolver processor.ResourceResolver

	// OnProgress is called after each batch with updated progress information
	OnProgress func(Progress)

	// Enrichment enriches regenerated activities with iam User display names
	// and avatars, and discloses staff actors as Datum Support, the same way
	// the live processor does, so re-indexing backfills both for historical
	// activities.
	Enrichment processor.Enrichment

	// staffActivities returns which of the named activities are already
	// stored with a staff actor in [start, end]. Defaults to an ActivityQuery
	// through the API server; replaceable in tests.
	staffActivities func(ctx context.Context, names []string, start, end time.Time) (map[string]bool, error)
}

// Options configures a re-indexing operation.
type Options struct {
	// StartTime is the beginning of the time range (inclusive)
	StartTime time.Time

	// EndTime is the end of the time range (exclusive)
	EndTime time.Time

	// BatchSize is the number of events to process per batch
	BatchSize int32

	// RateLimit is the maximum events per second to process
	RateLimit int32

	// DryRun previews changes without publishing to NATS
	DryRun bool

	// PolicyNames limits processing to specific policies (nil = all policies)
	PolicyNames []string

	// MatchLabels limits processing to policies with matching labels (nil = all policies)
	MatchLabels map[string]string
}

// Progress tracks the current state of a re-indexing operation.
type Progress struct {
	// TotalEvents is the estimated total events to process
	TotalEvents int64

	// ProcessedEvents is the number of events processed so far
	ProcessedEvents int64

	// ActivitiesGenerated is the number of activities created
	ActivitiesGenerated int64

	// Errors is the count of non-fatal errors encountered
	Errors int64

	// CurrentBatch is the batch number currently being processed
	CurrentBatch int32

	// TotalBatches is the estimated total number of batches
	TotalBatches int32
}

// NewReindexer creates a new Reindexer instance.
// The client is used to query AuditLogQuery and EventQuery resources via the API server,
// and to list ActivityPolicy resources.
func NewReindexer(
	client client.Client,
	js nats.JetStreamContext,
	kindResolver processor.KindResolver,
	resourceResolver processor.ResourceResolver,
) *Reindexer {
	return &Reindexer{
		client:           client,
		js:               js,
		publisher:        NewPublisher(js),
		policyCache:      activityprocessor.NewPolicyCache(),
		kindResolver:     kindResolver,
		resourceResolver: resourceResolver,
	}
}

// Run executes the re-indexing operation with the provided options.
// It processes audit logs first, then Kubernetes events, applying the current
// ActivityPolicy rules to generate activities.
func (r *Reindexer) Run(ctx context.Context, opts Options) error {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		reindexDuration.WithLabelValues(formatTimeRange(opts.StartTime, opts.EndTime)).Observe(duration.Seconds())
	}()

	// Initialize rate limiter if configured
	if opts.RateLimit > 0 {
		r.rateLimiter = NewRateLimiter(int(opts.RateLimit), int(opts.BatchSize))
	}

	// Fetch active policies to apply
	policies, err := r.fetchActivePolicies(ctx, opts.PolicyNames, opts.MatchLabels)
	if err != nil {
		return fmt.Errorf("failed to fetch policies: %w", err)
	}

	if len(policies) == 0 {
		klog.InfoS("No policies to apply, exiting", "policyNames", opts.PolicyNames)
		return nil
	}

	// Compile policies into the cache for efficient evaluation
	for _, policy := range policies {
		resource, err := r.resourceResolver(policy.Spec.Resource.APIGroup, policy.Spec.Resource.Kind)
		if err != nil {
			klog.Warningf("Failed to resolve resource for policy %s (apiGroup=%s, kind=%s): %v",
				policy.Name, policy.Spec.Resource.APIGroup, policy.Spec.Resource.Kind, err)
			continue
		}
		if err := r.policyCache.Add(policy, resource); err != nil {
			klog.Warningf("Failed to compile policy %s: %v", policy.Name, err)
			continue
		}
	}

	klog.InfoS("Starting reindex job",
		"startTime", opts.StartTime,
		"endTime", opts.EndTime,
		"policies", len(policies),
		"compiledPolicies", r.policyCache.Len(),
		"batchSize", opts.BatchSize,
		"rateLimit", opts.RateLimit,
		"dryRun", opts.DryRun,
	)

	// Increment job counter
	reindexJobsTotal.WithLabelValues(
		formatTimeRange(opts.StartTime, opts.EndTime),
		formatPolicyNames(opts.PolicyNames),
	).Inc()

	// Note: We don't estimate total events because the API doesn't have a count endpoint.
	// Progress tracking will be best-effort based on batches processed.
	progress := Progress{
		TotalEvents:  0, // Unknown when using API queries
		TotalBatches: 0, // Unknown until complete
	}

	// Process audit logs first
	if err := r.processAuditLogs(ctx, opts, &progress); err != nil {
		return fmt.Errorf("failed to process audit logs: %w", err)
	}

	// Process Kubernetes events second
	if err := r.processEvents(ctx, opts, &progress); err != nil {
		return fmt.Errorf("failed to process events: %w", err)
	}

	klog.InfoS("Reindex job completed",
		"totalEventsProcessed", progress.ProcessedEvents,
		"totalActivitiesGenerated", progress.ActivitiesGenerated,
		"errors", progress.Errors,
		"duration", time.Since(startTime),
	)

	return nil
}

// fetchActivePolicies retrieves the policies to apply during re-indexing.
// If policyNames is provided, only those policies are fetched.
// If matchLabels is provided, only policies with matching labels are fetched.
// Otherwise, all active policies are fetched.
func (r *Reindexer) fetchActivePolicies(ctx context.Context, policyNames []string, matchLabels map[string]string) ([]*v1alpha1.ActivityPolicy, error) {
	var policyList v1alpha1.ActivityPolicyList
	if err := r.client.List(ctx, &policyList); err != nil {
		return nil, fmt.Errorf("failed to list ActivityPolicy resources: %w", err)
	}

	if len(policyList.Items) == 0 {
		klog.V(2).InfoS("No ActivityPolicy resources found")
		return []*v1alpha1.ActivityPolicy{}, nil
	}

	// If specific policy names are requested, filter the list
	if len(policyNames) > 0 {
		nameSet := make(map[string]bool, len(policyNames))
		for _, name := range policyNames {
			nameSet[name] = true
		}

		filtered := make([]*v1alpha1.ActivityPolicy, 0, len(policyNames))
		for i := range policyList.Items {
			if nameSet[policyList.Items[i].Name] {
				filtered = append(filtered, &policyList.Items[i])
			}
		}

		klog.V(2).InfoS("Filtered ActivityPolicy resources by name",
			"requested", len(policyNames),
			"found", len(filtered),
		)
		return filtered, nil
	}

	// If label selector is provided, filter by labels
	if len(matchLabels) > 0 {
		filtered := make([]*v1alpha1.ActivityPolicy, 0, len(policyList.Items))
		for i := range policyList.Items {
			if matchesLabels(policyList.Items[i].Labels, matchLabels) {
				filtered = append(filtered, &policyList.Items[i])
			}
		}

		klog.V(2).InfoS("Filtered ActivityPolicy resources by labels",
			"matchLabels", matchLabels,
			"found", len(filtered),
		)
		return filtered, nil
	}

	// Convert all items to pointers
	policies := make([]*v1alpha1.ActivityPolicy, len(policyList.Items))
	for i := range policyList.Items {
		policies[i] = &policyList.Items[i]
	}

	klog.V(2).InfoS("Fetched all ActivityPolicy resources", "count", len(policies))
	return policies, nil
}

// matchesLabels returns true if the resource labels contain all the selector labels.
func matchesLabels(resourceLabels, selectorLabels map[string]string) bool {
	if len(selectorLabels) == 0 {
		return true
	}
	if len(resourceLabels) == 0 {
		return false
	}
	for key, value := range selectorLabels {
		if resourceLabels[key] != value {
			return false
		}
	}
	return true
}


// processAuditLogs processes all audit logs in the time range.
func (r *Reindexer) processAuditLogs(ctx context.Context, opts Options, progress *Progress) error {
	klog.InfoS("Processing audit logs", "startTime", opts.StartTime, "endTime", opts.EndTime)

	cursor := ""
	batchNum := int32(0)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		batchStart := time.Now()
		batchNum++
		progress.CurrentBatch = batchNum

		// Fetch batch via AuditLogQuery API
		batch, nextCursor, err := fetchAuditLogBatch(ctx, r.client, opts.StartTime, opts.EndTime, cursor, opts.BatchSize)
		if err != nil {
			reindexErrors.WithLabelValues("query").Inc()
			return fmt.Errorf("failed to fetch audit log batch %d: %w", batchNum, err)
		}

		if len(batch) == 0 {
			klog.V(2).InfoS("No more audit logs to process")
			break
		}

		// Evaluate batch against policies using pre-compiled CEL programs
		activities, err := r.evaluateAuditBatch(ctx, batch)
		if err != nil {
			reindexErrors.WithLabelValues("evaluate").Inc()
			return fmt.Errorf("failed to evaluate audit batch %d: %w", batchNum, err)
		}

		// Publish activities unless in dry-run mode
		if !opts.DryRun && len(activities) > 0 {
			if err := r.publisher.PublishActivities(ctx, activities); err != nil {
				reindexErrors.WithLabelValues("publish").Inc()
				return fmt.Errorf("failed to publish activities for batch %d: %w", batchNum, err)
			}

			// Record published activities metric
			for _, activity := range activities {
				policyName := activity.Labels["activity.miloapis.com/policy-name"]
				reindexActivitiesPublished.WithLabelValues(policyName).Inc()
			}
		}

		// Update progress
		progress.ProcessedEvents += int64(len(batch))
		progress.ActivitiesGenerated += int64(len(activities))

		// Record metrics - events processed is per batch, activities is per activity
		reindexEventsProcessed.WithLabelValues("audit", "all", formatBool(opts.DryRun)).Add(float64(len(batch)))
		for _, activity := range activities {
			policyName := activity.Labels["activity.miloapis.com/policy-name"]
			if policyName == "" {
				policyName = "unknown"
			}
			reindexActivitiesGenerated.WithLabelValues(policyName, formatBool(opts.DryRun)).Inc()
		}

		// Call progress callback if set
		if r.OnProgress != nil {
			r.OnProgress(*progress)
		}

		klog.V(2).InfoS("Processed audit batch",
			"batchNumber", batchNum,
			"eventsProcessed", len(batch),
			"activitiesGenerated", len(activities),
			"totalProcessed", progress.ProcessedEvents,
			"duration", time.Since(batchStart),
		)

		// Record batch duration
		reindexBatchDuration.Observe(time.Since(batchStart).Seconds())

		// Rate limiting
		if r.rateLimiter != nil {
			if err := r.rateLimiter.Wait(ctx, len(batch)); err != nil {
				return err
			}
		}

		// Continue to next batch
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	klog.InfoS("Audit log processing complete",
		"batches", batchNum,
		"eventsProcessed", progress.ProcessedEvents,
		"activitiesGenerated", progress.ActivitiesGenerated,
	)

	return nil
}

// processEvents processes all Kubernetes events in the time range.
func (r *Reindexer) processEvents(ctx context.Context, opts Options, progress *Progress) error {
	klog.InfoS("Processing Kubernetes events", "startTime", opts.StartTime, "endTime", opts.EndTime)

	cursor := ""
	batchNum := int32(0)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		batchStart := time.Now()
		batchNum++
		progress.CurrentBatch = batchNum

		// Fetch batch via EventQuery API
		batch, nextCursor, err := fetchEventBatch(ctx, r.client, opts.StartTime, opts.EndTime, cursor, opts.BatchSize)
		if err != nil {
			reindexErrors.WithLabelValues("query").Inc()
			return fmt.Errorf("failed to fetch event batch %d: %w", batchNum, err)
		}

		if len(batch) == 0 {
			klog.V(2).InfoS("No more events to process")
			break
		}

		// Evaluate batch against policies using pre-compiled CEL programs
		activities, err := r.evaluateEventBatch(ctx, batch)
		if err != nil {
			reindexErrors.WithLabelValues("evaluate").Inc()
			return fmt.Errorf("failed to evaluate event batch %d: %w", batchNum, err)
		}

		// Publish activities unless in dry-run mode
		if !opts.DryRun && len(activities) > 0 {
			if err := r.publisher.PublishActivities(ctx, activities); err != nil {
				reindexErrors.WithLabelValues("publish").Inc()
				return fmt.Errorf("failed to publish activities for batch %d: %w", batchNum, err)
			}

			// Record published activities metric
			for _, activity := range activities {
				policyName := activity.Labels["activity.miloapis.com/policy-name"]
				reindexActivitiesPublished.WithLabelValues(policyName).Inc()
			}
		}

		// Update progress
		progress.ProcessedEvents += int64(len(batch))
		progress.ActivitiesGenerated += int64(len(activities))

		// Record metrics - events processed is per batch, activities is per activity
		reindexEventsProcessed.WithLabelValues("event", "all", formatBool(opts.DryRun)).Add(float64(len(batch)))
		for _, activity := range activities {
			policyName := activity.Labels["activity.miloapis.com/policy-name"]
			if policyName == "" {
				policyName = "unknown"
			}
			reindexActivitiesGenerated.WithLabelValues(policyName, formatBool(opts.DryRun)).Inc()
		}

		// Call progress callback if set
		if r.OnProgress != nil {
			r.OnProgress(*progress)
		}

		klog.V(2).InfoS("Processed event batch",
			"batchNumber", batchNum,
			"eventsProcessed", len(batch),
			"activitiesGenerated", len(activities),
			"totalProcessed", progress.ProcessedEvents,
			"duration", time.Since(batchStart),
		)

		// Record batch duration
		reindexBatchDuration.Observe(time.Since(batchStart).Seconds())

		// Rate limiting
		if r.rateLimiter != nil {
			if err := r.rateLimiter.Wait(ctx, len(batch)); err != nil {
				return err
			}
		}

		// Continue to next batch
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	klog.InfoS("Event processing complete",
		"batches", batchNum,
		"eventsProcessed", progress.ProcessedEvents,
		"activitiesGenerated", progress.ActivitiesGenerated,
	)

	return nil
}

// Helper functions

func formatTimeRange(start, end time.Time) string {
	return fmt.Sprintf("%s_%s", start.Format("20060102"), end.Format("20060102"))
}

func formatPolicyNames(names []string) string {
	if len(names) == 0 {
		return "all"
	}
	if len(names) == 1 {
		return names[0]
	}
	return fmt.Sprintf("%d_policies", len(names))
}

func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
