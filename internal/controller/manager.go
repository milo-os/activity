package controller

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

var (
	// Scheme defines the runtime type system for API object serialization.
	Scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(Scheme))
	utilruntime.Must(v1alpha1.AddToScheme(Scheme))
}

// ManagerOptions contains configuration for the controller manager.
type ManagerOptions struct {
	// Workers is the number of worker threads for processing items.
	Workers int
	// MetricsAddr is the address to bind the metrics endpoint.
	MetricsAddr string
	// HealthProbeAddr is the address to bind the health probe endpoint.
	HealthProbeAddr string
	// JetStream is the NATS JetStream context for publishing activities (required)
	JetStream nats.JetStreamContext
	// JobClient is the Kubernetes client for Job operations in the infrastructure cluster
	JobClient client.Client

	// ReindexJob configuration
	ReindexJobNamespace      string
	ReindexServiceAccount    string
	ReindexMemoryLimit       string
	ReindexCPULimit          string
	MaxConcurrentReindexJobs int
	ActivityImage            string
	NATSURL                  string
	NATSTLSEnabled           bool
	NATSTLSCertFile          string
	NATSTLSKeyFile           string
	NATSTLSCAFile            string

	// JobTemplate is the PodTemplateSpec for reindex worker Jobs.
	// If nil, a default template is used.
	JobTemplate *corev1.PodTemplateSpec
}

// ActivityPolicyGVR is the GroupVersionResource for ActivityPolicy.
var ActivityPolicyGVR = schema.GroupVersionResource{
	Group:    v1alpha1.GroupName,
	Version:  "v1alpha1",
	Resource: "activitypolicies",
}

// NewManager creates a new controller manager using controller-runtime.
func NewManager(config *rest.Config, options ManagerOptions) (ctrl.Manager, error) {
	if err := registerMetrics(ctrlmetrics.Registry); err != nil {
		return nil, fmt.Errorf("failed to register controller metrics: %w", err)
	}

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 Scheme,
		HealthProbeBindAddress: options.HealthProbeAddr,
		Metrics: metricsserver.Options{
			BindAddress: options.MetricsAddr,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	// Add health and readiness checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("failed to add healthz check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("failed to add readyz check: %w", err)
	}

	// Create and register the ActivityPolicy reconciler
	policyReconciler := &ActivityPolicyReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		RESTMapper: mgr.GetRESTMapper(),
	}

	if err := policyReconciler.SetupWithManager(mgr, options.Workers); err != nil {
		return nil, fmt.Errorf("failed to create ActivityPolicy controller: %w", err)
	}

	// Create and register the ReindexJob reconciler
	reindexReconciler := &ReindexJobReconciler{
		Client:                mgr.GetClient(),
		JobClient:             options.JobClient,
		Scheme:                mgr.GetScheme(),
		JetStream:             options.JetStream,
		Recorder:              mgr.GetEventRecorderFor("reindexjob-controller"),
		JobNamespace:          options.ReindexJobNamespace,
		ActivityImage:         options.ActivityImage,
		ReindexServiceAccount: options.ReindexServiceAccount,
		ReindexMemoryLimit:    options.ReindexMemoryLimit,
		ReindexCPULimit:       options.ReindexCPULimit,
		MaxConcurrentJobs:     options.MaxConcurrentReindexJobs,
		JobTemplate:           options.JobTemplate,
		NATSURL:               options.NATSURL,
		NATSTLSEnabled:        options.NATSTLSEnabled,
		NATSTLSCertFile:       options.NATSTLSCertFile,
		NATSTLSKeyFile:        options.NATSTLSKeyFile,
		NATSTLSCAFile:         options.NATSTLSCAFile,
	}

	if err := reindexReconciler.SetupWithManager(mgr, options.Workers); err != nil {
		return nil, fmt.Errorf("failed to create ReindexJob controller: %w", err)
	}

	return mgr, nil
}

// Run starts the controller manager and blocks until the context is cancelled.
func Run(ctx context.Context, mgr ctrl.Manager) error {
	klog.Info("Starting controller manager")
	return mgr.Start(ctx)
}
