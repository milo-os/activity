package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func TestBuildJobForReindexJob(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))

	reconciler := &ReindexJobReconciler{
		JobNamespace:          "activity-system",
		ActivityImage:         "ghcr.io/datum-cloud/activity:test",
		ReindexServiceAccount: "activity-reindex-worker",
		ReindexMemoryLimit:    "2Gi",
		ReindexCPULimit:       "1000m",
		NATSURL:               "nats://nats.activity-system.svc:4222",
		NATSTLSEnabled:        false,
	}

	reindexJob := &v1alpha1.ReindexJob{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-reindex",
		},
		Spec: v1alpha1.ReindexJobSpec{
			TimeRange: v1alpha1.ReindexTimeRange{
				StartTime: "now-7d",
				EndTime:   "now",
			},
		},
	}

	job, err := reconciler.buildJobForReindexJob(reindexJob)
	require.NoError(t, err)

	// Verify Job metadata
	assert.Equal(t, "test-reindex-job", job.Name)
	assert.Equal(t, "activity-system", job.Namespace)
	assert.Equal(t, "activity-reindex", job.Labels["app"])
	assert.Equal(t, "test-reindex", job.Labels["reindex.activity.miloapis.com/job"])

	// Verify Job spec
	assert.Equal(t, ptr.To(int32(3)), job.Spec.BackoffLimit)
	assert.Equal(t, ptr.To(int32(300)), job.Spec.TTLSecondsAfterFinished)
	assert.Equal(t, corev1.RestartPolicyOnFailure, job.Spec.Template.Spec.RestartPolicy)
	assert.Equal(t, "activity-reindex-worker", job.Spec.Template.Spec.ServiceAccountName)

	// Verify container
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	container := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "reindex", container.Name)
	assert.Equal(t, "ghcr.io/datum-cloud/activity:test", container.Image)

	// Verify args
	expectedArgs := []string{
		"reindex-worker",
		"test-reindex",
		"--nats-url=nats://nats.activity-system.svc:4222",
		"--logging-format=json",
	}
	assert.Equal(t, expectedArgs, container.Args)

	// Verify resource limits
	memLimit := resource.MustParse("2Gi")
	cpuLimit := resource.MustParse("1000m")
	assert.Equal(t, memLimit, container.Resources.Limits[corev1.ResourceMemory])
	assert.Equal(t, memLimit, container.Resources.Requests[corev1.ResourceMemory])
	assert.Equal(t, cpuLimit, container.Resources.Limits[corev1.ResourceCPU])

	// Verify Pod SecurityContext
	require.NotNil(t, job.Spec.Template.Spec.SecurityContext)
	podSecurityContext := job.Spec.Template.Spec.SecurityContext
	assert.Equal(t, ptr.To(true), podSecurityContext.RunAsNonRoot)
	assert.Equal(t, ptr.To(int64(65532)), podSecurityContext.RunAsUser)
	assert.Equal(t, ptr.To(int64(65532)), podSecurityContext.RunAsGroup)
	assert.Equal(t, ptr.To(int64(65532)), podSecurityContext.FSGroup)
	require.NotNil(t, podSecurityContext.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, podSecurityContext.SeccompProfile.Type)

	// Verify Container SecurityContext
	require.NotNil(t, container.SecurityContext)
	containerSecurityContext := container.SecurityContext
	assert.Equal(t, ptr.To(false), containerSecurityContext.AllowPrivilegeEscalation)
	assert.Equal(t, ptr.To(true), containerSecurityContext.ReadOnlyRootFilesystem)
	assert.Equal(t, ptr.To(true), containerSecurityContext.RunAsNonRoot)
	require.NotNil(t, containerSecurityContext.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, containerSecurityContext.Capabilities.Drop)
}

func TestBuildJobForReindexJob_WithTLS(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))

	reconciler := &ReindexJobReconciler{
		JobNamespace:          "activity-system",
		ActivityImage:         "ghcr.io/datum-cloud/activity:test",
		ReindexServiceAccount: "activity-reindex-worker",
		ReindexMemoryLimit:    "2Gi",
		ReindexCPULimit:       "1000m",
		NATSURL:               "nats://nats.activity-system.svc:4222",
		NATSTLSEnabled:        true,
		NATSTLSCertFile:       "/certs/tls.crt",
		NATSTLSKeyFile:        "/certs/tls.key",
		NATSTLSCAFile:         "/certs/ca.crt",
	}

	reindexJob := &v1alpha1.ReindexJob{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-reindex-tls",
		},
		Spec: v1alpha1.ReindexJobSpec{
			TimeRange: v1alpha1.ReindexTimeRange{
				StartTime: "now-7d",
			},
		},
	}

	job, err := reconciler.buildJobForReindexJob(reindexJob)
	require.NoError(t, err)

	// Verify TLS args are included
	container := job.Spec.Template.Spec.Containers[0]
	expectedArgs := []string{
		"reindex-worker",
		"test-reindex-tls",
		"--nats-url=nats://nats.activity-system.svc:4222",
		"--logging-format=json",
		"--nats-tls-enabled=true",
		"--nats-tls-cert-file=/certs/tls.crt",
		"--nats-tls-key-file=/certs/tls.key",
		"--nats-tls-ca-file=/certs/ca.crt",
	}
	assert.Equal(t, expectedArgs, container.Args)
}

func TestCountRunningJobs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))

	now := metav1.Now()

	// Create test jobs
	runningJob1 := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-1",
			Namespace: "activity-system",
			Labels: map[string]string{
				"app": "activity-reindex",
			},
		},
		Status: batchv1.JobStatus{
			// No completion time - still running
		},
	}

	runningJob2 := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-2",
			Namespace: "activity-system",
			Labels: map[string]string{
				"app": "activity-reindex",
			},
		},
		Status: batchv1.JobStatus{
			// No completion time - still running
		},
	}

	completedJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-3",
			Namespace: "activity-system",
			Labels: map[string]string{
				"app": "activity-reindex",
			},
		},
		Status: batchv1.JobStatus{
			CompletionTime: &now,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(runningJob1, runningJob2, completedJob).
		Build()

	reconciler := &ReindexJobReconciler{
		Client:       fakeClient,
		JobClient:    fakeClient,
		JobNamespace: "activity-system",
	}

	count, err := reconciler.countRunningJobs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should count only running jobs (without completion time)")
}

func TestGetJobForReindexJob(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-reindex-job",
			Namespace: "activity-system",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(job).
		Build()

	reconciler := &ReindexJobReconciler{
		Client:       fakeClient,
		JobClient:    fakeClient,
		JobNamespace: "activity-system",
	}

	reindexJob := &v1alpha1.ReindexJob{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-reindex",
		},
	}

	foundJob, err := reconciler.getJobForReindexJob(context.Background(), reindexJob)
	require.NoError(t, err)
	assert.Equal(t, "test-reindex-job", foundJob.Name)
}

func TestCheckJobStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	recorder := record.NewFakeRecorder(10)

	reconciler := &ReindexJobReconciler{
		Client:    fakeClient,
		JobClient: fakeClient,
		Recorder:  recorder,
	}

	t.Run("job still running", func(t *testing.T) {
		reindexJob := &v1alpha1.ReindexJob{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-reindex",
			},
			Status: v1alpha1.ReindexJobStatus{
				Phase: v1alpha1.ReindexJobRunning,
			},
		}

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-reindex-job",
			},
			Status: batchv1.JobStatus{
				// No completion time - still running
			},
		}

		result, err := reconciler.checkJobStatus(context.Background(), reindexJob, job)
		require.NoError(t, err)
		assert.Greater(t, result.RequeueAfter.Seconds(), float64(0), "should requeue to check again later")
	})

	t.Run("job completed successfully", func(t *testing.T) {
		reindexJob := &v1alpha1.ReindexJob{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-reindex",
			},
			Status: v1alpha1.ReindexJobStatus{
				Phase: v1alpha1.ReindexJobRunning,
			},
		}

		now := metav1.Now()
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-reindex-job",
			},
			Status: batchv1.JobStatus{
				Succeeded:      1,
				CompletionTime: &now,
			},
		}

		result, err := reconciler.checkJobStatus(context.Background(), reindexJob, job)
		require.NoError(t, err)
		assert.Equal(t, int64(0), result.RequeueAfter.Nanoseconds(), "should not requeue for completed job")
	})

	t.Run("job failed", func(t *testing.T) {
		reindexJob := &v1alpha1.ReindexJob{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-reindex",
			},
			Status: v1alpha1.ReindexJobStatus{
				Phase: v1alpha1.ReindexJobRunning,
			},
		}

		now := metav1.Now()
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-reindex-job",
			},
			Status: batchv1.JobStatus{
				Failed:         1,
				CompletionTime: &now,
			},
		}

		result, err := reconciler.checkJobStatus(context.Background(), reindexJob, job)
		require.NoError(t, err)
		assert.Equal(t, int64(0), result.RequeueAfter.Nanoseconds(), "should not requeue for completed job")
	})
}
