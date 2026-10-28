package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.miloapis.com/activity/internal/controller"
)

// ControllerManagerOptions contains configuration for the controller manager.
type ControllerManagerOptions struct {
	Kubeconfig      string
	JobKubeconfig   string
	MasterURL       string
	Workers         int
	MetricsAddr     string
	HealthProbeAddr string

	// NATS configuration (required)
	NATSURL         string
	NATSTLSEnabled  bool
	NATSTLSCertFile string
	NATSTLSKeyFile  string
	NATSTLSCAFile   string

	// ReindexJob configuration
	ReindexJobNamespace      string
	ReindexServiceAccount    string
	ReindexMemoryLimit       string
	ReindexCPULimit          string
	MaxConcurrentReindexJobs int
	ActivityImage            string

	// JobTemplateConfigMap specifies the ConfigMap containing the Job template.
	// Format: namespace/name. If not set, a default template is used.
	JobTemplateConfigMap string

	Logs *logsapi.LoggingConfiguration
}

// NewControllerManagerOptions creates options with default values.
func NewControllerManagerOptions() *ControllerManagerOptions {
	return &ControllerManagerOptions{
		Logs:                     logsapi.NewLoggingConfiguration(),
		Workers:                  2,
		MetricsAddr:              ":8080",
		HealthProbeAddr:          ":8081",
		ReindexJobNamespace:      "activity-system",
		ReindexServiceAccount:    "activity-reindex-worker",
		ReindexMemoryLimit:       "2Gi",
		ReindexCPULimit:          "1000m",
		MaxConcurrentReindexJobs: 1,
		ActivityImage:            "ghcr.io/datum-cloud/activity:latest",
	}
}

// AddFlags adds controller manager flags to the command.
func (o *ControllerManagerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig,
		"Path to a kubeconfig file for Milo API server. Only required if out-of-cluster.")
	fs.StringVar(&o.JobKubeconfig, "job-kubeconfig", o.JobKubeconfig,
		"Path to a kubeconfig file for infrastructure cluster where Jobs run. If not set, uses --kubeconfig or in-cluster config.")
	fs.StringVar(&o.MasterURL, "master", o.MasterURL,
		"The address of the Kubernetes API server. Overrides any value in kubeconfig.")
	fs.IntVar(&o.Workers, "workers", o.Workers,
		"Number of worker threads for the controller.")
	fs.StringVar(&o.MetricsAddr, "metrics-addr", o.MetricsAddr,
		"The address to bind the metrics endpoint.")
	fs.StringVar(&o.HealthProbeAddr, "health-probe-addr", o.HealthProbeAddr,
		"The address to bind the health probe endpoint.")

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

	// ReindexJob flags
	fs.StringVar(&o.ReindexJobNamespace, "reindex-job-namespace", o.ReindexJobNamespace,
		"Namespace where ReindexJob worker Jobs are created.")
	fs.StringVar(&o.ReindexServiceAccount, "reindex-service-account", o.ReindexServiceAccount,
		"ServiceAccount for ReindexJob worker pods.")
	fs.StringVar(&o.ReindexMemoryLimit, "reindex-memory-limit", o.ReindexMemoryLimit,
		"Memory limit for ReindexJob worker pods (e.g., 2Gi).")
	fs.StringVar(&o.ReindexCPULimit, "reindex-cpu-limit", o.ReindexCPULimit,
		"CPU limit for ReindexJob worker pods (e.g., 1000m).")
	fs.IntVar(&o.MaxConcurrentReindexJobs, "max-concurrent-reindex-jobs", o.MaxConcurrentReindexJobs,
		"Maximum number of concurrent ReindexJobs allowed.")
	fs.StringVar(&o.ActivityImage, "activity-image", o.ActivityImage,
		"Container image for activity binary used by ReindexJob workers.")
	fs.StringVar(&o.JobTemplateConfigMap, "reindex-job-template-configmap", o.JobTemplateConfigMap,
		"ConfigMap containing the Job template for reindex workers (format: namespace/name). "+
			"The ConfigMap should have a 'template.yaml' key with a PodTemplateSpec. "+
			"If not set, a default template is used.")

	logsapi.AddFlags(o.Logs, fs)
}

// NewControllerManagerCommand creates the controller-manager subcommand.
func NewControllerManagerCommand() *cobra.Command {
	options := NewControllerManagerOptions()

	cmd := &cobra.Command{
		Use:   "controller-manager",
		Short: "Run the controller manager",
		Long: `Run the controller manager that watches for changes to Activity resources
and reconciles the desired state. This includes managing ActivityPolicy resources
and ensuring consistent state across the cluster.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := logsapi.ValidateAndApply(options.Logs, utilfeature.DefaultMutableFeatureGate); err != nil {
				return fmt.Errorf("failed to apply logging configuration: %w", err)
			}
			ctrl.SetLogger(klog.NewKlogr())
			return RunControllerManager(options)
		},
	}

	options.AddFlags(cmd.Flags())

	return cmd
}

// RunControllerManager starts the controller manager.
func RunControllerManager(options *ControllerManagerOptions) error {
	// Validate required flags
	if options.NATSURL == "" {
		return fmt.Errorf("--nats-url is required")
	}

	klog.Info("Starting Activity Controller Manager")

	// Build the client configuration for Milo API server (ReindexJob, ActivityPolicy)
	var config *rest.Config
	var err error

	if options.Kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags(options.MasterURL, options.Kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return fmt.Errorf("failed to build config for Milo API server: %w", err)
	}

	// Build the client configuration for infrastructure cluster (Jobs)
	// Priority: --job-kubeconfig > in-cluster config > same as main config
	var jobConfig *rest.Config
	if options.JobKubeconfig != "" {
		// Use explicit kubeconfig for Job operations
		jobConfig, err = clientcmd.BuildConfigFromFlags("", options.JobKubeconfig)
		if err != nil {
			return fmt.Errorf("failed to build config from --job-kubeconfig: %w", err)
		}
		klog.Info("Using explicit kubeconfig for Job operations", "path", options.JobKubeconfig)
	} else if inClusterConfig, inClusterErr := rest.InClusterConfig(); inClusterErr == nil {
		// Running in a cluster - use in-cluster config for Jobs
		// This allows the controller to create Jobs in the infrastructure cluster
		// while connecting to Milo for ReindexJob CRs
		jobConfig = inClusterConfig
		klog.Info("Using in-cluster config for Job operations")
	} else {
		// Not in a cluster and no --job-kubeconfig - use same config as main client
		// This is the typical dev environment scenario
		jobConfig = config
		klog.Info("Using same kubeconfig for Job operations (dev mode)")
	}

	// Create a client for Job operations
	jobClient, err := client.New(jobConfig, client.Options{
		Scheme: controller.Scheme,
	})
	if err != nil {
		return fmt.Errorf("failed to create Job client: %w", err)
	}

	// Load Job template from ConfigMap if specified
	var jobTemplate *corev1.PodTemplateSpec
	if options.JobTemplateConfigMap != "" {
		namespace, name, parseErr := parseNamespacedName(options.JobTemplateConfigMap)
		if parseErr != nil {
			return fmt.Errorf("invalid --reindex-job-template-configmap value: %w", parseErr)
		}

		ctx := context.Background()
		jobTemplate, err = controller.LoadJobTemplate(ctx, jobClient, namespace, name)
		if err != nil {
			return fmt.Errorf("failed to load job template: %w", err)
		}
		klog.Info("Loaded job template from ConfigMap", "namespace", namespace, "name", name)
	}

	managerOpts := controller.ManagerOptions{
		Workers:                  options.Workers,
		MetricsAddr:              options.MetricsAddr,
		HealthProbeAddr:          options.HealthProbeAddr,
		JobClient:                jobClient,
		ReindexJobNamespace:      options.ReindexJobNamespace,
		ReindexServiceAccount:    options.ReindexServiceAccount,
		ReindexMemoryLimit:       options.ReindexMemoryLimit,
		ReindexCPULimit:          options.ReindexCPULimit,
		MaxConcurrentReindexJobs: options.MaxConcurrentReindexJobs,
		ActivityImage:            options.ActivityImage,
		JobTemplate:              jobTemplate,
		NATSURL:                  options.NATSURL,
		NATSTLSEnabled:           options.NATSTLSEnabled,
		NATSTLSCertFile:          options.NATSTLSCertFile,
		NATSTLSKeyFile:           options.NATSTLSKeyFile,
		NATSTLSCAFile:            options.NATSTLSCAFile,
	}

	// Initialize NATS JetStream connection
	klog.Info("Initializing NATS JetStream connection")

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
				return fmt.Errorf("failed to load NATS TLS client cert: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		// Load CA cert if provided
		if options.NATSTLSCAFile != "" {
			caCert, err := os.ReadFile(options.NATSTLSCAFile)
			if err != nil {
				return fmt.Errorf("failed to read NATS TLS CA file: %w", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return fmt.Errorf("failed to parse NATS TLS CA certificate")
			}
			tlsConfig.RootCAs = caCertPool
		}

		natsOpts = append(natsOpts, nats.Secure(tlsConfig))
	}

	natsConn, err := nats.Connect(options.NATSURL, natsOpts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := natsConn.JetStream()
	if err != nil {
		natsConn.Close()
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}
	managerOpts.JetStream = js
	klog.Info("NATS JetStream initialized", "url", options.NATSURL)

	// Create the controller manager
	mgr, err := controller.NewManager(config, managerOpts)
	if err != nil {
		return err
	}

	// Use controller-runtime's signal handler for graceful shutdown
	ctx := ctrl.SetupSignalHandler()

	// Run the controller manager
	runErr := controller.Run(ctx, mgr)

	// Clean up NATS connection on shutdown
	klog.Info("Closing NATS connection")
	natsConn.Close()

	if runErr != nil {
		return runErr
	}

	klog.Info("Controller manager shutdown complete")
	return nil
}

// parseNamespacedName parses a string in the format "namespace/name" into its components.
func parseNamespacedName(s string) (namespace, name string, err error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected format 'namespace/name', got %q", s)
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("namespace and name cannot be empty in %q", s)
	}
	return parts[0], parts[1], nil
}
