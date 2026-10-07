package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

var (
	reindexJobsStartedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "activity_controller",
			Name:      "reindex_jobs_started_total",
			Help:      "Total number of ReindexJob resources started",
		},
	)

	reindexJobsCompletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_controller",
			Name:      "reindex_jobs_completed_total",
			Help:      "Total number of ReindexJob resources completed",
		},
		[]string{"result"}, // result: succeeded, failed
	)

	reindexJobDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "activity_controller",
			Name:      "reindex_job_duration_seconds",
			Help:      "Time spent running ReindexJob operations",
			Buckets:   prometheus.ExponentialBuckets(10, 2, 10), // 10s to ~2.8 hours
		},
	)

	reindexJobsRunning = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "activity_controller",
			Name:      "reindex_jobs_running",
			Help:      "Number of ReindexJob resources currently running",
		},
	)

	reindexJobsTTLDeletedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "activity_controller",
			Name:      "reindex_jobs_ttl_deleted_total",
			Help:      "Total number of ReindexJob resources deleted due to TTL expiration",
		},
	)
)

// registerMetrics registers the ReindexJob controller's collectors. Not done
// in init, so other subcommands in this binary don't serve them.
func registerMetrics(reg prometheus.Registerer) error {
	for _, c := range []prometheus.Collector{
		reindexJobsStartedTotal,
		reindexJobsCompletedTotal,
		reindexJobDuration,
		reindexJobsRunning,
		reindexJobsTTLDeletedTotal,
	} {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// ReindexJobReconciler reconciles ReindexJob resources.
type ReindexJobReconciler struct {
	client.Client
	JobClient client.Client
	Scheme    *runtime.Scheme
	JetStream nats.JetStreamContext
	Recorder  record.EventRecorder

	// Configuration for Kubernetes Jobs
	JobNamespace          string
	ActivityImage         string
	ReindexServiceAccount string
	ReindexMemoryLimit    string
	ReindexCPULimit       string
	MaxConcurrentJobs     int

	// JobTemplate is the PodTemplateSpec used as a base for worker Jobs.
	// This allows operators to configure volumes, security context, etc.
	// If nil, a minimal default template is used.
	JobTemplate *corev1.PodTemplateSpec

	// NATS configuration for worker pods
	NATSURL         string
	NATSTLSEnabled  bool
	NATSTLSCertFile string
	NATSTLSKeyFile  string
	NATSTLSCAFile   string
}

// +kubebuilder:rbac:groups=activity.miloapis.com,resources=reindexjobs,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=activity.miloapis.com,resources=reindexjobs/status,verbs=update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation of a ReindexJob resource.
func (r *ReindexJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := klog.FromContext(ctx)

	// Fetch the ReindexJob
	var job v1alpha1.ReindexJob
	if err := r.Get(ctx, req.NamespacedName, &job); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "unable to fetch ReindexJob")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Handle deletion - cleanup the associated Job
	if !job.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &job)
	}

	// Handle TTL-based cleanup for completed jobs
	if job.Status.Phase == v1alpha1.ReindexJobSucceeded ||
		job.Status.Phase == v1alpha1.ReindexJobFailed {
		return r.handleTTLCleanup(ctx, &job)
	}

	// Check concurrency limit
	runningCount, err := r.countRunningJobs(ctx)
	if err != nil {
		logger.Error(err, "failed to count running jobs")
		return ctrl.Result{}, err
	}

	// Check if we're already running this job
	existingJob, err := r.getJobForReindexJob(ctx, &job)
	if err != nil && client.IgnoreNotFound(err) != nil {
		logger.Error(err, "failed to check for existing Job")
		return ctrl.Result{}, err
	}

	isRunning := existingJob != nil && existingJob.Status.CompletionTime == nil

	// If concurrency limit reached and this job isn't one of the running jobs, queue it
	if runningCount >= r.MaxConcurrentJobs && !isRunning {
		if job.Status.Phase != v1alpha1.ReindexJobPending {
			job.Status.Phase = v1alpha1.ReindexJobPending
			job.Status.Message = fmt.Sprintf("Waiting for slot (running: %d/%d)", runningCount, r.MaxConcurrentJobs)
			meta.SetStatusCondition(&job.Status.Conditions, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "Pending",
				Message:            job.Status.Message,
				ObservedGeneration: job.Generation,
			})
			if err := r.Status().Update(ctx, &job); err != nil {
				logger.Error(err, "failed to update ReindexJob status to Pending")
				return ctrl.Result{}, err
			}
			r.Recorder.Event(&job, "Normal", "Queued", job.Status.Message)
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Start or check job status
	switch job.Status.Phase {
	case "", v1alpha1.ReindexJobPending:
		return r.startJob(ctx, &job)
	case v1alpha1.ReindexJobRunning:
		// Check if Job completed
		return r.checkJobStatus(ctx, &job, existingJob)
	}

	return ctrl.Result{}, nil
}

// handleTTLCleanup checks if a completed job should be deleted based on TTL.
func (r *ReindexJobReconciler) handleTTLCleanup(ctx context.Context, job *v1alpha1.ReindexJob) (ctrl.Result, error) {
	logger := klog.FromContext(ctx)

	// TTL not set - retain indefinitely
	if job.Spec.TTLSecondsAfterFinished == nil {
		logger.V(4).Info("ReindexJob completed, no TTL set", "phase", job.Status.Phase)
		return ctrl.Result{}, nil
	}

	// CompletedAt not set yet - this shouldn't happen for completed jobs, but handle gracefully
	if job.Status.CompletedAt == nil {
		logger.V(4).Info("ReindexJob completed but CompletedAt not set, skipping TTL cleanup", "phase", job.Status.Phase)
		return ctrl.Result{}, nil
	}

	// Calculate expiration time
	ttlDuration := time.Duration(*job.Spec.TTLSecondsAfterFinished) * time.Second
	expirationTime := job.Status.CompletedAt.Add(ttlDuration)
	now := time.Now()

	// Check if TTL has expired
	if now.Before(expirationTime) {
		// Not yet expired - requeue after remaining TTL duration
		remainingTTL := expirationTime.Sub(now)
		// Ensure minimum requeue duration to avoid busy-looping
		if remainingTTL < time.Second {
			remainingTTL = time.Second
		}
		logger.V(4).Info("ReindexJob not yet expired",
			"phase", job.Status.Phase,
			"completedAt", job.Status.CompletedAt.Time,
			"ttlSeconds", *job.Spec.TTLSecondsAfterFinished,
			"remainingTTL", remainingTTL,
		)
		return ctrl.Result{RequeueAfter: remainingTTL}, nil
	}

	// TTL expired - delete the job
	logger.Info("Deleting expired ReindexJob",
		"job", job.Name,
		"phase", job.Status.Phase,
		"completedAt", job.Status.CompletedAt.Time,
		"ttlSeconds", *job.Spec.TTLSecondsAfterFinished,
	)

	// Emit event before deletion
	r.Recorder.Event(job, "Normal", "TTLExpired",
		fmt.Sprintf("ReindexJob deleted after %d seconds TTL", *job.Spec.TTLSecondsAfterFinished))

	// Delete the resource
	if err := r.Delete(ctx, job); err != nil {
		logger.Error(err, "failed to delete expired ReindexJob")
		return ctrl.Result{}, err
	}

	reindexJobsTTLDeletedTotal.Inc()
	logger.Info("Successfully deleted expired ReindexJob", "job", job.Name)
	return ctrl.Result{}, nil
}

// startJob creates a Kubernetes Job to execute the re-indexing work.
func (r *ReindexJobReconciler) startJob(ctx context.Context, job *v1alpha1.ReindexJob) (ctrl.Result, error) {
	logger := klog.FromContext(ctx)

	// Build the Job spec
	k8sJob, err := r.buildJobForReindexJob(job)
	if err != nil {
		logger.Error(err, "failed to build Job spec")
		return ctrl.Result{}, err
	}

	// Note: We do NOT set an OwnerReference here because ReindexJob is cluster-scoped
	// while Jobs are namespaced. Cross-namespace owner references are not allowed in Kubernetes.
	// Cleanup is handled by:
	// 1. handleDeletion() which deletes Jobs by label when ReindexJob is deleted
	// 2. Job TTL (TTLSecondsAfterFinished: 300) for automatic cleanup of completed Jobs

	// Create the Job using JobClient (infrastructure cluster)
	if err := r.JobClient.Create(ctx, k8sJob); err != nil {
		logger.Error(err, "failed to create Job")
		return ctrl.Result{}, err
	}

	// Update status to Running
	job.Status.Phase = v1alpha1.ReindexJobRunning
	now := metav1.Now()
	job.Status.StartedAt = &now
	job.Status.Message = "Job created, waiting for execution"
	meta.SetStatusCondition(&job.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "InProgress",
		Message:            "Re-indexing in progress",
		ObservedGeneration: job.Generation,
	})

	if err := r.Status().Update(ctx, job); err != nil {
		logger.Error(err, "failed to update ReindexJob status to Running")
		return ctrl.Result{}, err
	}

	// Log job details
	batchSize := int32(1000)
	dryRun := false
	if job.Spec.Config != nil {
		batchSize = job.Spec.Config.BatchSize
		dryRun = job.Spec.Config.DryRun
	}
	logger.Info("Created Job for ReindexJob",
		"reindexJob", job.Name,
		"jobName", k8sJob.Name,
		"startTime", job.Spec.TimeRange.StartTime,
		"endTime", job.Spec.TimeRange.EndTime,
		"batchSize", batchSize,
		"dryRun", dryRun,
	)

	// Record metrics
	reindexJobsStartedTotal.Inc()
	reindexJobsRunning.Inc()

	// Emit Started event
	endTimeDisplay := job.Spec.TimeRange.EndTime
	if endTimeDisplay == "" {
		endTimeDisplay = "now"
	}
	r.Recorder.Event(job, "Normal", "Started", fmt.Sprintf("Started re-indexing from %s to %s",
		job.Spec.TimeRange.StartTime, endTimeDisplay))

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// buildJobForReindexJob creates a Kubernetes Job spec for executing the ReindexJob.
func (r *ReindexJobReconciler) buildJobForReindexJob(reindexJob *v1alpha1.ReindexJob) (*batchv1.Job, error) {
	// Build controller-managed arguments (appended after template args)
	controllerArgs := []string{
		"reindex-worker",
		reindexJob.Name,
		"--nats-url=" + r.NATSURL,
		"--logging-format=json",
	}

	if r.NATSTLSEnabled {
		controllerArgs = append(controllerArgs, "--nats-tls-enabled=true")
		if r.NATSTLSCertFile != "" {
			controllerArgs = append(controllerArgs, "--nats-tls-cert-file="+r.NATSTLSCertFile)
		}
		if r.NATSTLSKeyFile != "" {
			controllerArgs = append(controllerArgs, "--nats-tls-key-file="+r.NATSTLSKeyFile)
		}
		if r.NATSTLSCAFile != "" {
			controllerArgs = append(controllerArgs, "--nats-tls-ca-file="+r.NATSTLSCAFile)
		}
	}

	// Build resource requirements
	resourceRequirements := corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{},
		Requests: corev1.ResourceList{},
	}

	if r.ReindexMemoryLimit != "" {
		memQty := resource.MustParse(r.ReindexMemoryLimit)
		resourceRequirements.Limits[corev1.ResourceMemory] = memQty
		resourceRequirements.Requests[corev1.ResourceMemory] = memQty
	}

	if r.ReindexCPULimit != "" {
		cpuQty := resource.MustParse(r.ReindexCPULimit)
		resourceRequirements.Limits[corev1.ResourceCPU] = cpuQty
	}

	// Use configured template or default
	template := r.JobTemplate
	if template == nil {
		template = DefaultJobTemplate()
	}

	// Build and return the Job using the template merge function
	job := MergeJobTemplate(template, JobBuildOptions{
		ReindexJobName:       reindexJob.Name,
		JobNamespace:         r.JobNamespace,
		ActivityImage:        r.ActivityImage,
		ControllerArgs:       controllerArgs,
		ResourceRequirements: resourceRequirements,
		ServiceAccountName:   r.ReindexServiceAccount,
	})

	return job, nil
}

// getJobForReindexJob fetches the Job associated with a ReindexJob.
func (r *ReindexJobReconciler) getJobForReindexJob(ctx context.Context, reindexJob *v1alpha1.ReindexJob) (*batchv1.Job, error) {
	jobName := fmt.Sprintf("%s-job", reindexJob.Name)
	var job batchv1.Job
	err := r.JobClient.Get(ctx, client.ObjectKey{Namespace: r.JobNamespace, Name: jobName}, &job)
	return &job, err
}

// countRunningJobs returns the number of currently running reindex Jobs.
func (r *ReindexJobReconciler) countRunningJobs(ctx context.Context) (int, error) {
	var jobList batchv1.JobList
	if err := r.JobClient.List(ctx, &jobList, client.InNamespace(r.JobNamespace), client.MatchingLabels{
		"app": "activity-reindex",
	}); err != nil {
		return 0, err
	}

	count := 0
	for _, job := range jobList.Items {
		// Count jobs that haven't completed yet
		if job.Status.CompletionTime == nil {
			count++
		}
	}
	return count, nil
}

// checkJobStatus checks the status of the Kubernetes Job and updates the ReindexJob accordingly.
func (r *ReindexJobReconciler) checkJobStatus(ctx context.Context, reindexJob *v1alpha1.ReindexJob, job *batchv1.Job) (ctrl.Result, error) {
	logger := klog.FromContext(ctx)

	if job == nil {
		// Job was deleted or doesn't exist - this is unexpected
		logger.Info("Job not found for running ReindexJob", "reindexJob", reindexJob.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Check if Job completed
	if job.Status.CompletionTime != nil {
		logger.Info("Job completed", "reindexJob", reindexJob.Name, "succeeded", job.Status.Succeeded, "failed", job.Status.Failed)

		// Note: The worker updates the ReindexJob status directly, so we mainly just need to
		// ensure metrics are recorded. The status should already be set by the worker.

		// Record completion metrics
		reindexJobsRunning.Dec()
		if job.Status.Succeeded > 0 {
			reindexJobsCompletedTotal.WithLabelValues("succeeded").Inc()
		} else {
			reindexJobsCompletedTotal.WithLabelValues("failed").Inc()
		}

		return ctrl.Result{}, nil
	}

	// Job still running
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// handleDeletion handles cleanup when a ReindexJob is being deleted.
func (r *ReindexJobReconciler) handleDeletion(ctx context.Context, reindexJob *v1alpha1.ReindexJob) (ctrl.Result, error) {
	logger := klog.FromContext(ctx)

	// Delete the associated Job if it exists
	job, err := r.getJobForReindexJob(ctx, reindexJob)
	if err == nil {
		logger.Info("Deleting Job for deleted ReindexJob", "reindexJob", reindexJob.Name, "job", job.Name)
		if err := r.JobClient.Delete(ctx, job); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ReindexJobReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int) error {
	// Verify NATS ACTIVITIES_REINDEX stream exists at startup
	stream, err := r.JetStream.StreamInfo("ACTIVITIES_REINDEX")
	if err != nil {
		return fmt.Errorf("ACTIVITIES_REINDEX stream not found - run Phase 0 infrastructure setup: %w", err)
	}
	klog.InfoS("NATS stream verified", "stream", stream.Config.Name, "subjects", stream.Config.Subjects)

	// Note: We don't use Owns(&batchv1.Job{}) because Jobs are created in the
	// infrastructure cluster while the manager watches Milo. Instead, we poll
	// for Job status using RequeueAfter in the reconcile loop.
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ReindexJob{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		Complete(r)
}

