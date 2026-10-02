package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/natsconn"
)

// ProcessorOptions contains configuration for the activity processor.
type ProcessorOptions struct {
	// Kubernetes configuration
	Kubeconfig string
	MasterURL  string

	// NATS configuration
	Input          natsconn.Endpoint
	NATSStreamName string
	ConsumerName   string

	// NATS event stream configuration
	NATSEventStream   string
	NATSEventConsumer string

	// Output NATS stream
	OutputStreamName    string
	OutputSubjectPrefix string

	// Output NATS broker (empty URL = publish on the input connection, empty
	// TLS fields = inherit the input values)
	Output natsconn.Endpoint

	// Dead-letter queue configuration
	DLQEnabled       bool
	DLQStreamName    string
	DLQSubjectPrefix string

	// DLQ retry configuration
	DLQRetryEnabled           bool
	DLQRetryInterval          time.Duration
	DLQRetryBatchSize         int
	DLQRetryBackoffBase       time.Duration
	DLQRetryBackoffMultiplier float64
	DLQRetryBackoffMax        time.Duration
	DLQRetryAlertThreshold    int
	DLQRetryAuditSubject      string
	DLQRetryEventSubject      string

	// Processing configuration
	Workers   int
	BatchSize int
	AckWait   time.Duration

	// Health probe configuration
	HealthProbeAddr string

	Logs *logsapi.LoggingConfiguration
}

// NewProcessorOptions creates options with default values.
func NewProcessorOptions() *ProcessorOptions {
	return &ProcessorOptions{
		Logs:                      logsapi.NewLoggingConfiguration(),
		Input:                     natsconn.Endpoint{URL: "nats://localhost:4222"},
		NATSStreamName:            "AUDIT_EVENTS",
		ConsumerName:              "activity-processor@activity.miloapis.com",
		NATSEventStream:           "EVENTS",
		NATSEventConsumer:         "activity-event-processor",
		OutputStreamName:          "ACTIVITIES",
		OutputSubjectPrefix:       "activities",
		DLQEnabled:                true,
		DLQStreamName:             "ACTIVITY_DEAD_LETTER",
		DLQSubjectPrefix:          "activity.dlq",
		DLQRetryEnabled:           true,
		DLQRetryInterval:          5 * time.Minute,
		DLQRetryBatchSize:         100,
		DLQRetryBackoffBase:       1 * time.Minute,
		DLQRetryBackoffMultiplier: 2.0,
		DLQRetryBackoffMax:        24 * time.Hour,
		DLQRetryAlertThreshold:    10,
		DLQRetryAuditSubject:      "audit.k8s.retry",
		DLQRetryEventSubject:      "events.retry",
		Workers:                   4,
		BatchSize:                 100,
		AckWait:                   30 * time.Second,
		HealthProbeAddr:           ":8081",
	}
}

// AddFlags adds processor flags to the command.
func (o *ProcessorOptions) AddFlags(fs *pflag.FlagSet) {
	// Kubernetes flags
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig,
		"Path to a kubeconfig file. Only required if out-of-cluster.")
	fs.StringVar(&o.MasterURL, "master", o.MasterURL,
		"The address of the Kubernetes API server. Overrides any value in kubeconfig.")

	// NATS flags
	fs.StringVar(&o.Input.URL, "nats-url", o.Input.URL,
		"NATS server URL.")
	fs.StringVar(&o.NATSStreamName, "nats-stream", o.NATSStreamName,
		"NATS JetStream stream name for audit events. Empty disables audit event consumption.")
	fs.StringVar(&o.ConsumerName, "consumer-name", o.ConsumerName,
		"Durable consumer name for the audit log processor.")
	fs.StringVar(&o.NATSEventStream, "nats-event-stream", o.NATSEventStream,
		"NATS JetStream stream name for Kubernetes events.")
	fs.StringVar(&o.NATSEventConsumer, "nats-event-consumer", o.NATSEventConsumer,
		"Durable consumer name for the event processor.")
	fs.StringVar(&o.OutputStreamName, "output-stream", o.OutputStreamName,
		"NATS JetStream stream name for generated activities.")
	fs.StringVar(&o.OutputSubjectPrefix, "output-subject-prefix", o.OutputSubjectPrefix,
		"Subject prefix for published activities.")
	fs.StringVar(&o.Output.URL, "output-nats-url", o.Output.URL,
		"NATS server URL to publish activities to. Empty publishes on the --nats-url connection.")

	// NATS TLS/mTLS flags
	fs.BoolVar(&o.Input.TLS.Enabled, "nats-tls-enabled", o.Input.TLS.Enabled,
		"Enable TLS for NATS connection.")
	fs.StringVar(&o.Input.TLS.CertFile, "nats-tls-cert-file", o.Input.TLS.CertFile,
		"Path to client certificate file for mTLS authentication.")
	fs.StringVar(&o.Input.TLS.KeyFile, "nats-tls-key-file", o.Input.TLS.KeyFile,
		"Path to client private key file for mTLS authentication.")
	fs.StringVar(&o.Input.TLS.CAFile, "nats-tls-ca-file", o.Input.TLS.CAFile,
		"Path to CA certificate file for server verification.")

	// Output NATS TLS/mTLS flags
	fs.BoolVar(&o.Output.TLS.Enabled, "output-nats-tls-enabled", o.Output.TLS.Enabled,
		"Enable TLS for the --output-nats-url connection. Inherits --nats-tls-enabled when no output TLS is set.")
	fs.StringVar(&o.Output.TLS.CertFile, "output-nats-tls-cert-file", o.Output.TLS.CertFile,
		"Path to client certificate file for the output connection. Empty inherits --nats-tls-cert-file.")
	fs.StringVar(&o.Output.TLS.KeyFile, "output-nats-tls-key-file", o.Output.TLS.KeyFile,
		"Path to client private key file for the output connection. Empty inherits --nats-tls-key-file.")
	fs.StringVar(&o.Output.TLS.CAFile, "output-nats-tls-ca-file", o.Output.TLS.CAFile,
		"Path to CA certificate file for the output connection. Empty inherits --nats-tls-ca-file.")

	// Dead-letter queue flags
	fs.BoolVar(&o.DLQEnabled, "dlq-enabled", o.DLQEnabled,
		"Enable dead-letter queue for failed events.")
	fs.StringVar(&o.DLQStreamName, "dlq-stream", o.DLQStreamName,
		"NATS JetStream stream name for dead-letter queue.")
	fs.StringVar(&o.DLQSubjectPrefix, "dlq-subject-prefix", o.DLQSubjectPrefix,
		"Subject prefix for dead-letter queue messages.")

	// DLQ retry flags
	fs.BoolVar(&o.DLQRetryEnabled, "dlq-retry-enabled", o.DLQRetryEnabled,
		"Enable automatic retry of DLQ events.")
	fs.DurationVar(&o.DLQRetryInterval, "dlq-retry-interval", o.DLQRetryInterval,
		"Interval between DLQ retry batches.")
	fs.IntVar(&o.DLQRetryBatchSize, "dlq-retry-batch-size", o.DLQRetryBatchSize,
		"Number of DLQ events to process per retry batch.")
	fs.DurationVar(&o.DLQRetryBackoffBase, "dlq-retry-backoff-base", o.DLQRetryBackoffBase,
		"Initial backoff duration for DLQ retries.")
	fs.Float64Var(&o.DLQRetryBackoffMultiplier, "dlq-retry-backoff-multiplier", o.DLQRetryBackoffMultiplier,
		"Exponential backoff multiplier for DLQ retries.")
	fs.DurationVar(&o.DLQRetryBackoffMax, "dlq-retry-backoff-max", o.DLQRetryBackoffMax,
		"Maximum backoff duration for DLQ retries.")
	fs.IntVar(&o.DLQRetryAlertThreshold, "dlq-retry-alert-threshold", o.DLQRetryAlertThreshold,
		"Retry count threshold that triggers alerting metrics.")
	fs.StringVar(&o.DLQRetryAuditSubject, "dlq-retry-audit-subject", o.DLQRetryAuditSubject,
		"NATS subject for republishing audit events from DLQ.")
	fs.StringVar(&o.DLQRetryEventSubject, "dlq-retry-event-subject", o.DLQRetryEventSubject,
		"NATS subject for republishing Kubernetes events from DLQ.")

	// Processing flags
	fs.IntVar(&o.Workers, "workers", o.Workers,
		"Number of worker goroutines for processing.")
	fs.IntVar(&o.BatchSize, "batch-size", o.BatchSize,
		"Number of messages to fetch per batch.")
	fs.DurationVar(&o.AckWait, "ack-wait", o.AckWait,
		"Time to wait before message redelivery.")

	// Health probe flags
	fs.StringVar(&o.HealthProbeAddr, "health-probe-addr", o.HealthProbeAddr,
		"Address for health probe server (e.g., :8081). Set to empty to disable.")

	logsapi.AddFlags(o.Logs, fs)
}

// NewProcessorCommand creates the processor subcommand.
func NewProcessorCommand() *cobra.Command {
	options := NewProcessorOptions()

	cmd := &cobra.Command{
		Use:   "processor",
		Short: "Run the activity processor",
		Long: `Run the activity processor that consumes audit events and Kubernetes events from NATS,
evaluates ActivityPolicy rules, and generates human-readable Activity resources.

The processor:
- Connects to NATS JetStream to consume Kubernetes audit events (AUDIT_EVENTS stream)
- Connects to NATS JetStream to consume Kubernetes events (EVENTS stream)
- Watches ActivityPolicy resources to know which rules to apply
- Evaluates CEL expressions to match and transform events
- Publishes activities to NATS for downstream consumption (Vector writes to ClickHouse)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := logsapi.ValidateAndApply(options.Logs, utilfeature.DefaultMutableFeatureGate); err != nil {
				return fmt.Errorf("failed to apply logging configuration: %w", err)
			}
			ctrl.SetLogger(klog.NewKlogr())
			return RunProcessor(options)
		},
	}

	options.AddFlags(cmd.Flags())

	return cmd
}

// RunProcessor starts the activity processor.
func RunProcessor(options *ProcessorOptions) error {
	klog.Info("Starting Activity Processor")

	// Build Kubernetes client configuration
	var restConfig *rest.Config
	var err error

	if options.Kubeconfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags(options.MasterURL, options.Kubeconfig)
	} else {
		restConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	// Create processor
	processorConfig := activityprocessor.Config{
		Input:                     options.Input,
		NATSStreamName:            options.NATSStreamName,
		ConsumerName:              options.ConsumerName,
		NATSEventStream:           options.NATSEventStream,
		NATSEventConsumer:         options.NATSEventConsumer,
		OutputStreamName:          options.OutputStreamName,
		OutputSubjectPrefix:       options.OutputSubjectPrefix,
		Output:                    options.Output,
		DLQEnabled:                options.DLQEnabled,
		DLQStreamName:             options.DLQStreamName,
		DLQSubjectPrefix:          options.DLQSubjectPrefix,
		DLQRetryEnabled:           options.DLQRetryEnabled,
		DLQRetryInterval:          options.DLQRetryInterval,
		DLQRetryBatchSize:         options.DLQRetryBatchSize,
		DLQRetryBackoffBase:       options.DLQRetryBackoffBase,
		DLQRetryBackoffMultiplier: options.DLQRetryBackoffMultiplier,
		DLQRetryBackoffMax:        options.DLQRetryBackoffMax,
		DLQRetryAlertThreshold:    options.DLQRetryAlertThreshold,
		DLQRetryAuditSubject:      options.DLQRetryAuditSubject,
		DLQRetryEventSubject:      options.DLQRetryEventSubject,
		Workers:                   options.Workers,
		BatchSize:                 options.BatchSize,
		AckWait:                   options.AckWait,
		MaxDeliver:                5,
		HealthProbeAddr:           options.HealthProbeAddr,
	}

	proc, err := activityprocessor.New(processorConfig, restConfig)
	if err != nil {
		return fmt.Errorf("failed to create processor: %w", err)
	}

	// Use controller-runtime's signal handler for graceful shutdown
	ctx := ctrl.SetupSignalHandler()

	// Start the processor
	if err := proc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start processor: %w", err)
	}

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	proc.Stop()

	klog.Info("Activity processor shutdown complete")
	return nil
}
