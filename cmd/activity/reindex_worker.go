package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/controller"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/internal/reindex"
	"go.miloapis.com/activity/internal/timeutil"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

const (
	retentionWindow = 60 * 24 * time.Hour

	// reindexUserIndexSyncTimeout bounds how long a re-index waits for the iam
	// User index to sync. Unless unenriched re-indexing is allowed, the job
	// fails if the index has not synced by then.
	reindexUserIndexSyncTimeout = 60 * time.Second
)

// ReindexWorkerOptions contains configuration for the reindex worker.
type ReindexWorkerOptions struct {
	// The ReindexJob resource name
	JobName string

	// Kubeconfig for connecting to the API server where ReindexJob resources live.
	// If empty, uses in-cluster config.
	Kubeconfig string

	// NATS configuration (required for publishing activities)
	NATSURL         string
	NATSTLSEnabled  bool
	NATSTLSCertFile string
	NATSTLSKeyFile  string
	NATSTLSCAFile   string

	// AllowUnenriched lets the job run without actor display names when the
	// iam User index cannot sync. Off by default, so a re-index never
	// silently replaces enriched history with unenriched activities. Set by
	// the ACTIVITY_ALLOW_UNENRICHED environment variable or the root-level
	// --allow-unenriched flag (see addAllowUnenrichedFlag).
	AllowUnenriched bool

	// StaffEmailDomains are treated as Datum staff while staff membership
	// cannot be checked (root --staff-email-domains flag).
	StaffEmailDomains []string

	// StaffRedactionMode is "required" or "domain-only" (root
	// --staff-redaction-mode flag).
	StaffRedactionMode string

	Logs *logsapi.LoggingConfiguration
}

// NewReindexWorkerOptions creates options with default values.
func NewReindexWorkerOptions() *ReindexWorkerOptions {
	allow, _ := strconv.ParseBool(os.Getenv(allowUnenrichedEnv))
	return &ReindexWorkerOptions{
		AllowUnenriched: allow,
		Logs:            logsapi.NewLoggingConfiguration(),
	}
}

const (
	// allowUnenrichedFlag and allowUnenrichedEnv opt a re-index into running
	// without actor display names.
	allowUnenrichedFlag = "allow-unenriched"
	allowUnenrichedEnv  = "ACTIVITY_ALLOW_UNENRICHED"
)

// addAllowUnenrichedFlag registers --allow-unenriched as a persistent flag on
// the root command. The controller places job template args before the
// reindex-worker subcommand, and cobra can only tell that a bare flag in that
// position takes no value if the root command knows it is a bool; registered
// on the subcommand alone, "--allow-unenriched reindex-worker" would consume
// "reindex-worker" as the flag's value.
func addAllowUnenrichedFlag(root *cobra.Command) {
	root.PersistentFlags().Bool(allowUnenrichedFlag, false,
		"reindex-worker only: re-index without actor display names if the iam User index cannot sync. "+
			"By default the job fails instead, so enriched history is never overwritten with unenriched activities. "+
			"Also settable with "+allowUnenrichedEnv+"=true.")
}

// runReindexWorker is RunReindexWorker, replaceable in tests.
var runReindexWorker = RunReindexWorker

// AddFlags adds reindex worker flags to the command.
func (o *ReindexWorkerOptions) AddFlags(fs *pflag.FlagSet) {
	// Kubernetes API flags
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig,
		"Path to kubeconfig file for connecting to the API server. If empty, uses in-cluster config.")

	// NATS flags (required)
	fs.StringVar(&o.NATSURL, "nats-url", o.NATSURL,
		"NATS server URL (e.g., nats://localhost:4222). Required.")
	fs.BoolVar(&o.NATSTLSEnabled, "nats-tls-enabled", o.NATSTLSEnabled,
		"Enable TLS for NATS connection.")
	fs.StringVar(&o.NATSTLSCertFile, "nats-tls-cert-file", o.NATSTLSCertFile,
		"Path to client certificate file for NATS TLS.")
	fs.StringVar(&o.NATSTLSKeyFile, "nats-tls-key-file", o.NATSTLSKeyFile,
		"Path to client private key file for NATS TLS.")
	fs.StringVar(&o.NATSTLSCAFile, "nats-tls-ca-file", o.NATSTLSCAFile,
		"Path to CA certificate file for NATS TLS.")

	logsapi.AddFlags(o.Logs, fs)
}

// NewReindexWorkerCommand creates the reindex-worker subcommand.
func NewReindexWorkerCommand() *cobra.Command {
	options := NewReindexWorkerOptions()

	cmd := &cobra.Command{
		Use:   "reindex-worker <reindexjob-name>",
		Short: "Run a single ReindexJob worker",
		Long: `Run the reindex worker for a specific ReindexJob resource.
This is executed by Kubernetes Jobs created by the controller-manager.
It should not be run manually.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.JobName = args[0]
			if f := cmd.Flags().Lookup(allowUnenrichedFlag); f != nil && f.Changed {
				v, err := strconv.ParseBool(f.Value.String())
				if err != nil {
					return fmt.Errorf("invalid --%s: %w", allowUnenrichedFlag, err)
				}
				options.AllowUnenriched = v
			}
			options.StaffEmailDomains = staffEmailDomains(cmd)
			mode, err := staffRedactionMode(cmd)
			if err != nil {
				return err
			}
			options.StaffRedactionMode = mode
			return runReindexWorker(cmd.Context(), options)
		},
	}

	options.AddFlags(cmd.Flags())

	return cmd
}

// RunReindexWorker executes the reindex job worker.
func RunReindexWorker(ctx context.Context, options *ReindexWorkerOptions) error {
	if err := logsapi.ValidateAndApply(options.Logs, utilfeature.DefaultMutableFeatureGate); err != nil {
		return fmt.Errorf("failed to apply logging configuration: %w", err)
	}

	// Validate required flags
	if options.NATSURL == "" {
		return fmt.Errorf("--nats-url is required")
	}

	if options.JobName == "" {
		return fmt.Errorf("reindexjob name is required")
	}

	klog.InfoS("Starting reindex worker", "job", options.JobName)

	// Build Kubernetes client config
	var config *rest.Config
	var err error
	if options.Kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", options.Kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
		klog.InfoS("Using kubeconfig", "path", options.Kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to get in-cluster config: %w", err)
		}
		klog.InfoS("Using in-cluster config")
	}

	// Create Kubernetes client
	scheme := controller.Scheme
	cl, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Fetch the ReindexJob resource
	loaded, finished, err := fetchActiveReindexJob(ctx, cl, options.JobName)
	if err != nil {
		return err
	}
	if finished {
		return nil
	}
	job := *loaded

	// Start the enrichment indexes now so they sync while NATS connects.
	// Re-indexed activities get the same actor display names as live ones,
	// taken from each user's current User record, and staff membership is
	// evaluated as it is now.
	var enrichment processor.Enrichment
	var indexes enrichmentWaiters
	if userIndex, err := activityprocessor.NewUserIndex(config); err != nil {
		indexes.users = failedIndex{err: err}
	} else {
		userIndex.Start(ctx)
		enrichment.Users = userIndex
		indexes.users = userIndex
	}
	var staffIndex *activityprocessor.StaffIndex
	if options.StaffRedactionMode == activityprocessor.StaffRedactionDomainOnly {
		activityprocessor.WarnDomainOnlyStaffRedaction("reindex-worker", activityprocessor.ResolveStaffEmailDomains(options.StaffEmailDomains))
		staffIndex = activityprocessor.NewDomainOnlyStaffIndex(options.StaffEmailDomains)
		indexes.staff = staffIndex
	} else if staffIndex, err = activityprocessor.NewStaffIndex(config, options.StaffEmailDomains); err != nil {
		staffIndex = activityprocessor.NewUnavailableStaffIndex(options.StaffEmailDomains)
		indexes.staff = failedIndex{err: err}
	} else {
		staffIndex.Start(ctx)
		indexes.staff = staffIndex
	}
	// Even unsynced, the staff index fails closed by email domain.
	enrichment.Staff = staffIndex

	// Initialize NATS JetStream connection
	klog.InfoS("Connecting to NATS", "url", options.NATSURL)
	natsOpts, err := buildNATSOptions(options)
	if err != nil {
		return fmt.Errorf("failed to build NATS options: %w", err)
	}

	natsConn, err := nats.Connect(options.NATSURL, natsOpts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer natsConn.Close()

	js, err := natsConn.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	// Activities are replaced by name, so re-indexing without the User index
	// would strip display names from history (allowed only with the opt-out),
	// and re-indexing without the staff indexes could disclose staff (never
	// allowed).
	if err := enforceEnrichment(ctx, cl, &job, indexes, reindexUserIndexSyncTimeout, options.AllowUnenriched); err != nil {
		return err
	}

	// Update status to Running
	if err := updateJobPhase(ctx, cl, &job, v1alpha1.ReindexJobRunning, "Re-indexing in progress"); err != nil {
		return fmt.Errorf("failed to update status to Running: %w", err)
	}

	// Build REST mapper for kind/resource resolution
	mapper, err := processor.NewRESTMapperFromConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create REST mapper: %w", err)
	}

	kindResolver := processor.NewKindResolver(mapper)
	resourceResolver := processor.NewResourceResolver(mapper)

	// Create reindexer
	reindexer := reindex.NewReindexer(cl, js, kindResolver, resourceResolver)

	reindexer.Enrichment = enrichment

	// Set up progress callback to update ReindexJob status
	reindexer.OnProgress = func(progress reindex.Progress) {
		if err := updateJobProgress(ctx, cl, &job, progress); err != nil {
			klog.V(2).InfoS("failed to update progress (will retry on next batch)", "error", err)
		}
	}

	// Parse time range
	now := time.Now()
	startTime, err := timeutil.ParseFlexibleTime(job.Spec.TimeRange.StartTime, now)
	if err != nil {
		updateJobPhase(ctx, cl, &job, v1alpha1.ReindexJobFailed, fmt.Sprintf("Invalid startTime: %v", err))
		return fmt.Errorf("invalid startTime: %w", err)
	}

	endTimeStr := job.Spec.TimeRange.EndTime
	if endTimeStr == "" {
		endTimeStr = "now"
	}
	endTime, err := timeutil.ParseFlexibleTime(endTimeStr, now)
	if err != nil {
		updateJobPhase(ctx, cl, &job, v1alpha1.ReindexJobFailed, fmt.Sprintf("Invalid endTime: %v", err))
		return fmt.Errorf("invalid endTime: %w", err)
	}

	// Validate retention window
	timeSinceStart := time.Since(startTime)
	if timeSinceStart > retentionWindow {
		msg := fmt.Sprintf(
			"startTime exceeds ClickHouse retention window: data from %s is beyond the %d-day retention period (age: %dd)",
			startTime.Format(time.RFC3339),
			int(retentionWindow.Hours()/24),
			int(timeSinceStart.Hours()/24),
		)
		updateJobPhase(ctx, cl, &job, v1alpha1.ReindexJobFailed, msg)
		return fmt.Errorf("retention window exceeded")
	}

	// Build reindex options
	batchSize := int32(1000)
	if job.Spec.Config != nil && job.Spec.Config.BatchSize > 0 {
		batchSize = job.Spec.Config.BatchSize
	}

	rateLimit := int32(100)
	if job.Spec.Config != nil && job.Spec.Config.RateLimit > 0 {
		rateLimit = job.Spec.Config.RateLimit
	}

	dryRun := false
	if job.Spec.Config != nil {
		dryRun = job.Spec.Config.DryRun
	}

	var policyNames []string
	var matchLabels map[string]string
	if job.Spec.PolicySelector != nil {
		if len(job.Spec.PolicySelector.Names) > 0 {
			policyNames = job.Spec.PolicySelector.Names
		}
		if len(job.Spec.PolicySelector.MatchLabels) > 0 {
			matchLabels = job.Spec.PolicySelector.MatchLabels
		}
	}

	opts := reindex.Options{
		StartTime:   startTime,
		EndTime:     endTime,
		BatchSize:   batchSize,
		RateLimit:   rateLimit,
		DryRun:      dryRun,
		PolicyNames: policyNames,
		MatchLabels: matchLabels,
	}

	klog.InfoS("Starting reindex operation",
		"startTime", opts.StartTime,
		"endTime", opts.EndTime,
		"batchSize", opts.BatchSize,
		"rateLimit", opts.RateLimit,
		"dryRun", opts.DryRun,
	)

	// Run the reindexer
	runErr := reindexer.Run(ctx, opts)

	// Update final status
	if runErr != nil {
		msg := fmt.Sprintf("Re-indexing failed: %v", runErr)
		if err := updateJobPhase(ctx, cl, &job, v1alpha1.ReindexJobFailed, msg); err != nil {
			klog.ErrorS(err, "failed to update status to Failed")
		}
		return runErr
	}

	// Success
	var finalJob v1alpha1.ReindexJob
	if err := cl.Get(ctx, types.NamespacedName{Name: job.Name}, &finalJob); err == nil {
		activitiesGenerated := int64(0)
		if finalJob.Status.Progress != nil {
			activitiesGenerated = finalJob.Status.Progress.ActivitiesGenerated
		}

		var msg string
		if dryRun {
			msg = fmt.Sprintf("Dry-run complete: %d activities would be generated", activitiesGenerated)
		} else {
			msg = fmt.Sprintf("Completed: %d activities generated", activitiesGenerated)
		}

		if err := updateJobPhase(ctx, cl, &finalJob, v1alpha1.ReindexJobSucceeded, msg); err != nil {
			klog.ErrorS(err, "failed to update status to Succeeded")
		}

		klog.InfoS("Reindex job completed successfully",
			"activitiesGenerated", activitiesGenerated,
		)
	}

	return nil
}

// fetchActiveReindexJob loads the ReindexJob. finished is true when the job
// has already Succeeded or Failed: the Job may restart this pod
// (BackoffLimit, RestartPolicyOnFailure) after the worker finished, and a
// terminal ReindexJob is final, so the worker must exit cleanly without
// touching its status. That keeps a Failed job from flipping back to Running.
func fetchActiveReindexJob(ctx context.Context, cl client.Client, name string) (job *v1alpha1.ReindexJob, finished bool, err error) {
	job = &v1alpha1.ReindexJob{}
	if err := cl.Get(ctx, types.NamespacedName{Name: name}, job); err != nil {
		return nil, false, fmt.Errorf("failed to fetch ReindexJob %s: %w", name, err)
	}
	klog.InfoS("Loaded ReindexJob", "name", job.Name, "generation", job.Generation)
	if isTerminalReindexJob(job) {
		klog.InfoS("ReindexJob already finished; exiting without changes", "name", job.Name, "phase", job.Status.Phase)
		return job, true, nil
	}
	return job, false, nil
}

// isTerminalReindexJob reports whether the job has already Succeeded or Failed.
func isTerminalReindexJob(job *v1alpha1.ReindexJob) bool {
	return job.Status.Phase == v1alpha1.ReindexJobSucceeded || job.Status.Phase == v1alpha1.ReindexJobFailed
}

// indexWaiter is the part of the enrichment indexes the reindex worker
// needs; an interface so the decision logic can be tested.
type indexWaiter interface {
	WaitForSync(ctx context.Context, timeout time.Duration) error
}

// enrichmentWaiters are the indexes a re-index waits for.
type enrichmentWaiters struct {
	// users is the iam User index (display names). Unenriched re-indexing
	// may skip it.
	users indexWaiter
	// staff are the staff disclosure indexes. They are privacy-critical: a
	// re-index always waits for them and fails if they do not sync.
	staff indexWaiter
}

// waitForEnrichment waits for both index sets concurrently, each for up to
// timeout. A staff index failure is always returned. A User index failure
// is returned unless allowUnenriched is set, in which case activities are
// re-indexed with whatever display names the User index provides.
func waitForEnrichment(ctx context.Context, w enrichmentWaiters, timeout time.Duration, allowUnenriched bool) error {
	var userErr, staffErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); userErr = w.users.WaitForSync(ctx, timeout) }()
	go func() { defer wg.Done(); staffErr = w.staff.WaitForSync(ctx, timeout) }()
	wg.Wait()

	if staffErr != nil {
		return fmt.Errorf("staff memberships are unavailable, and re-indexing cannot proceed without them even "+
			"with unenriched re-indexing allowed: %w", staffErr)
	}
	if userErr != nil {
		if allowUnenriched {
			klog.InfoS("Re-indexing without actor display names (unenriched re-indexing allowed)", "reason", userErr.Error())
			return nil
		}
		return fmt.Errorf("actor display names are unavailable: %w", userErr)
	}
	return nil
}

// enforceEnrichment waits for the enrichment indexes. When one cannot sync
// (and, for the User index, unenriched re-indexing is not allowed), it marks
// the ReindexJob Failed with a message explaining why and returns an error.
func enforceEnrichment(ctx context.Context, cl client.Client, job *v1alpha1.ReindexJob, w enrichmentWaiters, timeout time.Duration, allowUnenriched bool) error {
	err := waitForEnrichment(ctx, w, timeout, allowUnenriched)
	if err == nil {
		return nil
	}
	msg := unenrichedFailureMessage(err)
	if uerr := updateJobPhase(ctx, cl, job, v1alpha1.ReindexJobFailed, msg); uerr != nil {
		klog.ErrorS(uerr, "failed to update status to Failed")
	}
	return errors.New(msg)
}

// failedIndex stands in for an index that could not be created.
type failedIndex struct {
	err error
}

func (f failedIndex) WaitForSync(context.Context, time.Duration) error {
	return fmt.Errorf("%w: %v", activityprocessor.ErrIndexUnavailable, f.err)
}

// unenrichedFailureMessage is the ReindexJob status message for a job that
// stopped because actor display names were unavailable.
func unenrichedFailureMessage(err error) string {
	return fmt.Sprintf("The job was stopped because %v. Grant get, list and watch on users.iam.miloapis.com, "+
		"groupmemberships.iam.miloapis.com, organizationmemberships.resourcemanager.miloapis.com and "+
		"projects.resourcemanager.miloapis.com to the reindex worker. If only display names are unavailable, setting "+
		allowUnenrichedEnv+"=true on the reindex container in the activity-reindex-job-template ConfigMap lets the job "+
		"run without them; staff memberships are always required.", err)
}

// updateJobPhase updates the ReindexJob phase and message, setting CompletedAt for terminal phases.
func updateJobPhase(ctx context.Context, cl client.Client, job *v1alpha1.ReindexJob, phase v1alpha1.ReindexJobPhase, message string) error {
	// Fetch latest version
	var latest v1alpha1.ReindexJob
	if err := cl.Get(ctx, types.NamespacedName{Name: job.Name}, &latest); err != nil {
		return err
	}

	latest.Status.Phase = phase
	latest.Status.Message = message

	// Set CompletedAt for terminal phases
	if phase == v1alpha1.ReindexJobSucceeded || phase == v1alpha1.ReindexJobFailed {
		now := metav1.Now()
		latest.Status.CompletedAt = &now
	}

	// Set StartedAt for Running phase if not already set
	if phase == v1alpha1.ReindexJobRunning && latest.Status.StartedAt == nil {
		now := metav1.Now()
		latest.Status.StartedAt = &now
	}

	// Update conditions
	var conditionStatus metav1.ConditionStatus
	var reason string
	switch phase {
	case v1alpha1.ReindexJobPending:
		conditionStatus = metav1.ConditionFalse
		reason = "Pending"
	case v1alpha1.ReindexJobRunning:
		conditionStatus = metav1.ConditionFalse
		reason = "InProgress"
	case v1alpha1.ReindexJobSucceeded:
		conditionStatus = metav1.ConditionTrue
		reason = "Succeeded"
	case v1alpha1.ReindexJobFailed:
		conditionStatus = metav1.ConditionFalse
		reason = "Failed"
	}

	meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             conditionStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: latest.Generation,
	})

	return cl.Status().Update(ctx, &latest)
}

// updateJobProgress updates the ReindexJob progress information.
func updateJobProgress(ctx context.Context, cl client.Client, job *v1alpha1.ReindexJob, progress reindex.Progress) error {
	// Fetch latest version
	var latest v1alpha1.ReindexJob
	if err := cl.Get(ctx, types.NamespacedName{Name: job.Name}, &latest); err != nil {
		return err
	}

	latest.Status.Progress = &v1alpha1.ReindexProgress{
		TotalEvents:         progress.TotalEvents,
		ProcessedEvents:     progress.ProcessedEvents,
		ActivitiesGenerated: progress.ActivitiesGenerated,
		Errors:              progress.Errors,
		CurrentBatch:        progress.CurrentBatch,
		TotalBatches:        progress.TotalBatches,
	}

	latest.Status.Message = fmt.Sprintf("Processing: %d events processed, %d activities generated",
		progress.ProcessedEvents, progress.ActivitiesGenerated)

	return cl.Status().Update(ctx, &latest)
}

// buildNATSOptions constructs NATS connection options from configuration.
func buildNATSOptions(options *ReindexWorkerOptions) ([]nats.Option, error) {
	var natsOpts []nats.Option

	// Configure TLS if enabled
	if options.NATSTLSEnabled {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}

		// Load client cert/key if provided
		if options.NATSTLSCertFile != "" && options.NATSTLSKeyFile != "" {
			cert, err := tls.LoadX509KeyPair(options.NATSTLSCertFile, options.NATSTLSKeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load NATS TLS client cert: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		// Load CA cert if provided
		if options.NATSTLSCAFile != "" {
			caCert, err := os.ReadFile(options.NATSTLSCAFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read NATS TLS CA file: %w", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse NATS TLS CA certificate")
			}
			tlsConfig.RootCAs = caCertPool
		}

		natsOpts = append(natsOpts, nats.Secure(tlsConfig))
	}

	return natsOpts, nil
}
