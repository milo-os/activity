package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLoadJobTemplate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	t.Run("loads valid template", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-template",
				Namespace: "activity-system",
			},
			Data: map[string]string{
				"template.yaml": `
spec:
  serviceAccountName: custom-sa
  volumes:
    - name: kubeconfig
      secret:
        secretName: kubeconfig-secret
  containers:
    - name: reindex
      args:
        - --kubeconfig=/etc/kubernetes/kubeconfig
      volumeMounts:
        - name: kubeconfig
          mountPath: /etc/kubernetes
          readOnly: true
`,
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cm).
			Build()

		template, err := LoadJobTemplate(context.Background(), fakeClient, "activity-system", "test-template")
		require.NoError(t, err)
		assert.Equal(t, "custom-sa", template.Spec.ServiceAccountName)
		require.Len(t, template.Spec.Volumes, 1)
		assert.Equal(t, "kubeconfig", template.Spec.Volumes[0].Name)
		require.Len(t, template.Spec.Containers, 1)
		assert.Equal(t, "reindex", template.Spec.Containers[0].Name)
	})

	t.Run("error when ConfigMap not found", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		_, err := LoadJobTemplate(context.Background(), fakeClient, "activity-system", "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get job template ConfigMap")
	})

	t.Run("error when template.yaml key missing", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-template",
				Namespace: "activity-system",
			},
			Data: map[string]string{
				"other-key": "some data",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cm).
			Build()

		_, err := LoadJobTemplate(context.Background(), fakeClient, "activity-system", "test-template")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not contain key")
	})

	t.Run("error when template is invalid YAML", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-template",
				Namespace: "activity-system",
			},
			Data: map[string]string{
				"template.yaml": "not: valid: yaml: [",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cm).
			Build()

		_, err := LoadJobTemplate(context.Background(), fakeClient, "activity-system", "test-template")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse job template")
	})
}

func TestDefaultJobTemplate(t *testing.T) {
	t.Parallel()

	template := DefaultJobTemplate()

	// Verify restart policy
	assert.Equal(t, corev1.RestartPolicyOnFailure, template.Spec.RestartPolicy)

	// Verify security context
	require.NotNil(t, template.Spec.SecurityContext)
	assert.Equal(t, ptr.To(true), template.Spec.SecurityContext.RunAsNonRoot)
	assert.Equal(t, ptr.To(int64(65532)), template.Spec.SecurityContext.RunAsUser)

	// Verify container
	require.Len(t, template.Spec.Containers, 1)
	assert.Equal(t, ReindexContainerName, template.Spec.Containers[0].Name)
}

func TestMergeJobTemplate(t *testing.T) {
	t.Parallel()

	t.Run("basic merge with default template", func(t *testing.T) {
		template := DefaultJobTemplate()
		opts := JobBuildOptions{
			ReindexJobName: "test-reindex",
			JobNamespace:   "activity-system",
			ActivityImage:  "ghcr.io/datum-cloud/activity:test",
			ControllerArgs: []string{"reindex-worker", "test-reindex", "--nats-url=nats://localhost:4222"},
		}

		job := MergeJobTemplate(template, opts)

		// Verify Job metadata
		assert.Equal(t, "test-reindex-job", job.Name)
		assert.Equal(t, "activity-system", job.Namespace)
		assert.Equal(t, "activity-reindex", job.Labels["app"])
		assert.Equal(t, "test-reindex", job.Labels["reindex.activity.miloapis.com/job"])

		// Verify Job spec
		assert.Equal(t, ptr.To(int32(3)), job.Spec.BackoffLimit)
		assert.Equal(t, ptr.To(int32(300)), job.Spec.TTLSecondsAfterFinished)

		// Verify container
		require.Len(t, job.Spec.Template.Spec.Containers, 1)
		container := job.Spec.Template.Spec.Containers[0]
		assert.Equal(t, "reindex", container.Name)
		assert.Equal(t, "ghcr.io/datum-cloud/activity:test", container.Image)
		assert.Equal(t, []string{"reindex-worker", "test-reindex", "--nats-url=nats://localhost:4222"}, container.Args)
	})

	t.Run("merge with template args prepended", func(t *testing.T) {
		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "reindex",
						Args: []string{"--kubeconfig=/etc/kubernetes/kubeconfig"},
					},
				},
			},
		}

		opts := JobBuildOptions{
			ReindexJobName: "test-reindex",
			JobNamespace:   "activity-system",
			ActivityImage:  "ghcr.io/datum-cloud/activity:test",
			ControllerArgs: []string{"reindex-worker", "test-reindex", "--nats-url=nats://localhost:4222"},
		}

		job := MergeJobTemplate(template, opts)

		container := job.Spec.Template.Spec.Containers[0]
		// Template args come first, controller args appended
		expectedArgs := []string{
			"--kubeconfig=/etc/kubernetes/kubeconfig",
			"reindex-worker",
			"test-reindex",
			"--nats-url=nats://localhost:4222",
		}
		assert.Equal(t, expectedArgs, container.Args)
	})

	t.Run("preserves template volumes and mounts", func(t *testing.T) {
		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "kubeconfig",
						VolumeSource: corev1.VolumeSource{
							CSI: &corev1.CSIVolumeSource{
								Driver: "cert-manager.io/csi",
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name: "reindex",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "kubeconfig",
								MountPath: "/etc/kubernetes",
								ReadOnly:  true,
							},
						},
					},
				},
			},
		}

		opts := JobBuildOptions{
			ReindexJobName: "test-reindex",
			JobNamespace:   "activity-system",
			ActivityImage:  "ghcr.io/datum-cloud/activity:test",
			ControllerArgs: []string{"reindex-worker", "test-reindex"},
		}

		job := MergeJobTemplate(template, opts)

		// Verify volumes are preserved
		require.Len(t, job.Spec.Template.Spec.Volumes, 1)
		assert.Equal(t, "kubeconfig", job.Spec.Template.Spec.Volumes[0].Name)
		assert.NotNil(t, job.Spec.Template.Spec.Volumes[0].CSI)

		// Verify volume mounts are preserved
		container := job.Spec.Template.Spec.Containers[0]
		require.Len(t, container.VolumeMounts, 1)
		assert.Equal(t, "kubeconfig", container.VolumeMounts[0].Name)
		assert.Equal(t, "/etc/kubernetes", container.VolumeMounts[0].MountPath)
	})

	t.Run("merges resource requirements", func(t *testing.T) {
		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "reindex",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("100m"),
							},
						},
					},
				},
			},
		}

		opts := JobBuildOptions{
			ReindexJobName: "test-reindex",
			JobNamespace:   "activity-system",
			ActivityImage:  "ghcr.io/datum-cloud/activity:test",
			ControllerArgs: []string{"reindex-worker", "test-reindex"},
			ResourceRequirements: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
		}

		job := MergeJobTemplate(template, opts)

		container := job.Spec.Template.Spec.Containers[0]
		// Controller resources are merged (take precedence)
		assert.Equal(t, resource.MustParse("2Gi"), container.Resources.Limits[corev1.ResourceMemory])
		assert.Equal(t, resource.MustParse("2Gi"), container.Resources.Requests[corev1.ResourceMemory])
		// Template CPU request is also present
		assert.Equal(t, resource.MustParse("100m"), container.Resources.Requests[corev1.ResourceCPU])
	})

	t.Run("sets service account from options", func(t *testing.T) {
		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				ServiceAccountName: "template-sa",
				Containers: []corev1.Container{
					{Name: "reindex"},
				},
			},
		}

		opts := JobBuildOptions{
			ReindexJobName:     "test-reindex",
			JobNamespace:       "activity-system",
			ActivityImage:      "ghcr.io/datum-cloud/activity:test",
			ControllerArgs:     []string{"reindex-worker", "test-reindex"},
			ServiceAccountName: "controller-sa",
		}

		job := MergeJobTemplate(template, opts)

		// Controller service account takes precedence
		assert.Equal(t, "controller-sa", job.Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("uses template service account when not set in options", func(t *testing.T) {
		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				ServiceAccountName: "template-sa",
				Containers: []corev1.Container{
					{Name: "reindex"},
				},
			},
		}

		opts := JobBuildOptions{
			ReindexJobName: "test-reindex",
			JobNamespace:   "activity-system",
			ActivityImage:  "ghcr.io/datum-cloud/activity:test",
			ControllerArgs: []string{"reindex-worker", "test-reindex"},
			// No ServiceAccountName set
		}

		job := MergeJobTemplate(template, opts)

		// Template service account is preserved
		assert.Equal(t, "template-sa", job.Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("creates reindex container if not in template", func(t *testing.T) {
		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "sidecar", Image: "sidecar:latest"},
				},
			},
		}

		opts := JobBuildOptions{
			ReindexJobName: "test-reindex",
			JobNamespace:   "activity-system",
			ActivityImage:  "ghcr.io/datum-cloud/activity:test",
			ControllerArgs: []string{"reindex-worker", "test-reindex"},
		}

		job := MergeJobTemplate(template, opts)

		// Should have both sidecar and reindex containers
		require.Len(t, job.Spec.Template.Spec.Containers, 2)

		// Find reindex container
		var reindexContainer *corev1.Container
		for i := range job.Spec.Template.Spec.Containers {
			if job.Spec.Template.Spec.Containers[i].Name == "reindex" {
				reindexContainer = &job.Spec.Template.Spec.Containers[i]
				break
			}
		}
		require.NotNil(t, reindexContainer)
		assert.Equal(t, "ghcr.io/datum-cloud/activity:test", reindexContainer.Image)
	})

	t.Run("merges labels from template", func(t *testing.T) {
		template := &corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"custom-label": "custom-value",
					"app":          "should-be-overridden",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "reindex"},
				},
			},
		}

		opts := JobBuildOptions{
			ReindexJobName: "test-reindex",
			JobNamespace:   "activity-system",
			ActivityImage:  "ghcr.io/datum-cloud/activity:test",
			ControllerArgs: []string{"reindex-worker", "test-reindex"},
		}

		job := MergeJobTemplate(template, opts)

		// Custom label is preserved
		assert.Equal(t, "custom-value", job.Labels["custom-label"])
		// Controller-managed labels take precedence
		assert.Equal(t, "activity-reindex", job.Labels["app"])
		assert.Equal(t, "test-reindex", job.Labels["reindex.activity.miloapis.com/job"])
	})
}
