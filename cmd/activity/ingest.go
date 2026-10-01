package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/edgeingest"
	"go.miloapis.com/activity/internal/metrics"
)

// IngestOptions contains configuration for the edge audit ingest listener.
type IngestOptions struct {
	Address           string
	TLSCertFile       string
	TLSKeyFile        string
	ClientCAFile      string
	MaxRequestBytes   int64
	ReadHeaderTimeout time.Duration

	ClusterRegistryFile string

	UpstreamKubeconfig      string
	NamespaceResync         time.Duration
	NamespaceIndexFile      string
	NamespaceIndexSaveEvery time.Duration

	ResolveParkTimeout      time.Duration
	ResolveParkPollInterval time.Duration

	NATSURL           string
	NATSStreamName    string
	NATSSubjectPrefix string
	NATSTLSEnabled    bool
	NATSTLSCertFile   string
	NATSTLSKeyFile    string
	NATSTLSCAFile     string

	Logs *logsapi.LoggingConfiguration
}

// NewIngestOptions creates options with default values.
func NewIngestOptions() *IngestOptions {
	return &IngestOptions{
		Logs:                    logsapi.NewLoggingConfiguration(),
		Address:                 ":8443",
		MaxRequestBytes:         32 << 20,
		ReadHeaderTimeout:       10 * time.Second,
		NamespaceResync:         30 * time.Minute,
		NamespaceIndexSaveEvery: 1 * time.Minute,
		ResolveParkTimeout:      30 * time.Second,
		ResolveParkPollInterval: 250 * time.Millisecond,
		NATSStreamName:          "AUDIT_EVENTS",
		NATSSubjectPrefix:       "audit.k8s.edge",
	}
}

// AddFlags adds ingest flags to the command.
func (o *IngestOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Address, "address", o.Address,
		"Address the audit ingest listener binds to.")
	fs.StringVar(&o.TLSCertFile, "tls-cert-file", o.TLSCertFile,
		"Path to the serving certificate.")
	fs.StringVar(&o.TLSKeyFile, "tls-key-file", o.TLSKeyFile,
		"Path to the serving private key.")
	fs.StringVar(&o.ClientCAFile, "client-ca-file", o.ClientCAFile,
		"Path to the CA that signs edge shipper client certificates. Required: cluster identity is taken from the client certificate.")
	fs.Int64Var(&o.MaxRequestBytes, "max-request-bytes", o.MaxRequestBytes,
		"Maximum size of a single audit batch.")
	fs.DurationVar(&o.ReadHeaderTimeout, "read-header-timeout", o.ReadHeaderTimeout,
		"Maximum time allowed to read request headers.")

	fs.StringVar(&o.ClusterRegistryFile, "cluster-registry-file", o.ClusterRegistryFile,
		"Path to the YAML file mapping edge client certificate common names to cluster names and locations.")

	fs.StringVar(&o.UpstreamKubeconfig, "upstream-kubeconfig", o.UpstreamKubeconfig,
		"Path to a kubeconfig with one context per upstream project control plane. Namespaces observed there drive the reverse ns-<uid> lookup.")
	fs.DurationVar(&o.NamespaceResync, "namespace-resync-period", o.NamespaceResync,
		"Resync period for upstream namespace informers.")
	fs.StringVar(&o.NamespaceIndexFile, "namespace-index-file", o.NamespaceIndexFile,
		"Path the retained downstream namespace index is persisted to. Without it, mappings for namespaces deleted while the process was down are lost and records about them stop resolving.")
	fs.DurationVar(&o.NamespaceIndexSaveEvery, "namespace-index-save-interval", o.NamespaceIndexSaveEvery,
		"How often the namespace index is written to disk.")

	fs.DurationVar(&o.ResolveParkTimeout, "resolve-park-timeout", o.ResolveParkTimeout,
		"How long a record waits for a cold namespace cache before the shipper is asked to retry.")
	fs.DurationVar(&o.ResolveParkPollInterval, "resolve-park-poll-interval", o.ResolveParkPollInterval,
		"How often a parked record rechecks the namespace cache.")

	fs.StringVar(&o.NATSURL, "nats-url", o.NATSURL,
		"NATS server URL.")
	fs.StringVar(&o.NATSStreamName, "nats-stream", o.NATSStreamName,
		"NATS JetStream stream rewritten audit events are published to.")
	fs.StringVar(&o.NATSSubjectPrefix, "nats-subject-prefix", o.NATSSubjectPrefix,
		"Subject prefix for published audit events. The cluster name is appended.")
	fs.BoolVar(&o.NATSTLSEnabled, "nats-tls-enabled", o.NATSTLSEnabled,
		"Enable TLS for the NATS connection.")
	fs.StringVar(&o.NATSTLSCertFile, "nats-tls-cert-file", o.NATSTLSCertFile,
		"Path to client certificate file for NATS mTLS.")
	fs.StringVar(&o.NATSTLSKeyFile, "nats-tls-key-file", o.NATSTLSKeyFile,
		"Path to client private key file for NATS mTLS.")
	fs.StringVar(&o.NATSTLSCAFile, "nats-tls-ca-file", o.NATSTLSCAFile,
		"Path to CA certificate file for NATS server verification.")

	logsapi.AddFlags(o.Logs, fs)
}

// Validate ensures required configuration is provided.
func (o *IngestOptions) Validate() error {
	var errs []error

	if o.ClusterRegistryFile == "" {
		errs = append(errs, fmt.Errorf("--cluster-registry-file is required"))
	}
	if o.ClientCAFile == "" {
		errs = append(errs, fmt.Errorf("--client-ca-file is required"))
	}
	if o.TLSCertFile == "" || o.TLSKeyFile == "" {
		errs = append(errs, fmt.Errorf("--tls-cert-file and --tls-key-file are required"))
	}
	if o.UpstreamKubeconfig == "" {
		errs = append(errs, fmt.Errorf("--upstream-kubeconfig is required"))
	}
	if o.NATSURL == "" {
		errs = append(errs, fmt.Errorf("--nats-url is required"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation errors: %v", errs)
	}
	return nil
}

// NewIngestCommand creates the ingest subcommand.
func NewIngestCommand() *cobra.Command {
	options := NewIngestOptions()

	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest audit events from edge control planes",
		Long: `Receive kube-apiserver audit events from edge control planes and republish them
onto the audit stream the core control plane pipeline already uses.

Edge control planes are downstream clusters, so their audit records refer to
projected ns-<upstream-namespace-uid> namespaces that mean nothing to a customer
and must never be shown to one. This command reverses that mapping, attributes
each record to the project that owns it, stamps the location it came from, and
publishes it for Vector and the activity processor to pick up unchanged.

The cluster a record belongs to is taken from the authenticated client
certificate and looked up in the cluster registry. Nothing in a request body is
trusted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := logsapi.ValidateAndApply(options.Logs, utilfeature.DefaultMutableFeatureGate); err != nil {
				return fmt.Errorf("failed to apply logging configuration: %w", err)
			}
			if err := options.Validate(); err != nil {
				return err
			}
			return RunIngest(cmd.Context(), options)
		},
	}

	options.AddFlags(cmd.Flags())

	return cmd
}

// RunIngest starts the edge audit ingest listener.
func RunIngest(ctx context.Context, options *IngestOptions) error {
	klog.Info("Starting Activity edge audit ingest")

	registry, err := edgeingest.LoadClusterRegistry(options.ClusterRegistryFile)
	if err != nil {
		return err
	}
	klog.InfoS("Loaded edge cluster registry", "clusters", registry.Len())

	index := edgeingest.NewNamespaceIndex()
	if err := index.Load(options.NamespaceIndexFile); err != nil {
		return err
	}

	upstream := edgeingest.UpstreamClusters{
		KubeconfigPath: options.UpstreamKubeconfig,
		ResyncPeriod:   options.NamespaceResync,
	}
	if err := upstream.Start(ctx, index); err != nil {
		return err
	}

	emitter, err := edgeingest.NewNATSEmitter(edgeingest.NATSEmitterConfig{
		URL:           options.NATSURL,
		StreamName:    options.NATSStreamName,
		SubjectPrefix: options.NATSSubjectPrefix,
		TLSEnabled:    options.NATSTLSEnabled,
		TLSCertFile:   options.NATSTLSCertFile,
		TLSKeyFile:    options.NATSTLSKeyFile,
		TLSCAFile:     options.NATSTLSCAFile,
	})
	if err != nil {
		return err
	}
	defer emitter.Close()

	pipeline := &edgeingest.Pipeline{
		Resolver:         index,
		Emitter:          emitter,
		ParkTimeout:      options.ResolveParkTimeout,
		ParkPollInterval: options.ResolveParkPollInterval,
	}

	server, err := edgeingest.NewServer(edgeingest.ServerConfig{
		Address:           options.Address,
		TLSCertFile:       options.TLSCertFile,
		TLSKeyFile:        options.TLSKeyFile,
		ClientCAFile:      options.ClientCAFile,
		MaxRequestBytes:   options.MaxRequestBytes,
		ReadHeaderTimeout: options.ReadHeaderTimeout,
	}, registry, pipeline, index)
	if err != nil {
		return err
	}

	go persistNamespaceIndex(ctx, index, options.NamespaceIndexFile, options.NamespaceIndexSaveEvery)

	return server.Start(ctx)
}

// persistNamespaceIndex keeps the retained namespace index on disk so mappings
// for namespaces deleted while the process was down survive a restart.
func persistNamespaceIndex(ctx context.Context, index *edgeingest.NamespaceIndex, path string, interval time.Duration) {
	if path == "" || interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := index.Save(path); err != nil {
				klog.ErrorS(err, "Failed saving namespace index on shutdown")
			}
			return
		case <-ticker.C:
			metrics.EdgeAuditNamespaceIndexSize.Set(float64(index.Len()))
			if err := index.Save(path); err != nil {
				klog.ErrorS(err, "Failed saving namespace index")
			}
		}
	}
}
