package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	logsapi "k8s.io/component-base/logs/api/v1"

	"go.miloapis.com/activity/internal/eventexporter"
)

// EventExporterOptions contains configuration for the event exporter.
type EventExporterOptions struct {
	NATSUrl         string
	SubjectPrefix   string
	ScopeType       string
	ScopeName       string
	Kubeconfig      string
	ResyncPeriod    time.Duration
	HealthProbeAddr string

	NATSTLSEnabled  bool
	NATSTLSCertFile string
	NATSTLSKeyFile  string
	NATSTLSCAFile   string

	Logs *logsapi.LoggingConfiguration
}

// NewEventExporterOptions creates options with default values.
func NewEventExporterOptions() *EventExporterOptions {
	return &EventExporterOptions{
		Logs:            logsapi.NewLoggingConfiguration(),
		NATSUrl:         getEnvOrDefault("NATS_URL", "nats://nats.nats-system.svc.cluster.local:4222"),
		SubjectPrefix:   getEnvOrDefault("SUBJECT_PREFIX", "events.k8s"),
		ScopeType:       getEnvOrDefault("SCOPE_TYPE", "organization"),
		ScopeName:       getEnvOrDefault("SCOPE_NAME", "dev-org"),
		Kubeconfig:      os.Getenv("KUBECONFIG"),
		ResyncPeriod:    30 * time.Minute,
		HealthProbeAddr: getEnvOrDefault("HEALTH_PROBE_ADDR", ":8081"),
		NATSTLSCertFile: getEnvOrDefault("NATS_TLS_CERT_FILE", ""),
		NATSTLSKeyFile:  getEnvOrDefault("NATS_TLS_KEY_FILE", ""),
		NATSTLSCAFile:   getEnvOrDefault("NATS_TLS_CA_FILE", ""),
	}
}

// AddFlags adds event exporter flags to the flag set.
func (o *EventExporterOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.NATSUrl, "nats-url", o.NATSUrl, "NATS server URL")
	fs.StringVar(&o.SubjectPrefix, "subject-prefix", o.SubjectPrefix, "NATS subject prefix")
	fs.StringVar(&o.ScopeType, "scope-type", o.ScopeType, "Scope type annotation value")
	fs.StringVar(&o.ScopeName, "scope-name", o.ScopeName, "Scope name annotation value")
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig, "Path to kubeconfig (empty for in-cluster)")
	fs.DurationVar(&o.ResyncPeriod, "resync-period", o.ResyncPeriod, "Informer resync period")
	fs.StringVar(&o.HealthProbeAddr, "health-probe-addr", o.HealthProbeAddr, "Health probe server bind address")
	fs.BoolVar(&o.NATSTLSEnabled, "nats-tls-enabled", o.NATSTLSEnabled, "Enable TLS for NATS connection")
	fs.StringVar(&o.NATSTLSCertFile, "nats-tls-cert-file", o.NATSTLSCertFile, "Path to client certificate file for NATS TLS")
	fs.StringVar(&o.NATSTLSKeyFile, "nats-tls-key-file", o.NATSTLSKeyFile, "Path to client private key file for NATS TLS")
	fs.StringVar(&o.NATSTLSCAFile, "nats-tls-ca-file", o.NATSTLSCAFile, "Path to CA certificate file for NATS TLS")
	logsapi.AddFlags(o.Logs, fs)
}

// NewEventExporterCommand creates the event-exporter subcommand.
func NewEventExporterCommand() *cobra.Command {
	options := NewEventExporterOptions()

	cmd := &cobra.Command{
		Use:   "event-exporter",
		Short: "Export Kubernetes Events to NATS JetStream",
		Long: `Watch Kubernetes Events and publish them to NATS JetStream for ingestion
into ClickHouse. This exporter uses events.k8s.io/v1 Event format for
consistency with the EventRecord API and ClickHouse schema.

Events are published with scope annotations for multi-tenant isolation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := logsapi.ValidateAndApply(options.Logs, utilfeature.DefaultMutableFeatureGate); err != nil {
				return fmt.Errorf("failed to apply logging configuration: %w", err)
			}
			cfg := eventexporter.Config{
				NATSUrl:         options.NATSUrl,
				SubjectPrefix:   options.SubjectPrefix,
				ScopeType:       options.ScopeType,
				ScopeName:       options.ScopeName,
				Kubeconfig:      options.Kubeconfig,
				ResyncPeriod:    options.ResyncPeriod,
				HealthProbeAddr: options.HealthProbeAddr,
				NATSTLSEnabled:  options.NATSTLSEnabled,
				NATSTLSCertFile: options.NATSTLSCertFile,
				NATSTLSKeyFile:  options.NATSTLSKeyFile,
				NATSTLSCAFile:   options.NATSTLSCAFile,
			}
			return eventexporter.Run(cmd.Context(), cfg)
		},
	}

	options.AddFlags(cmd.Flags())

	return cmd
}

// getEnvOrDefault returns the environment variable value or a default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
