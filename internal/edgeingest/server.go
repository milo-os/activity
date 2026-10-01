package edgeingest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/metrics"
)

// AuditPath is the endpoint edge shippers post to. The payload is an
// audit.k8s.io/v1 EventList, which is what a kube-apiserver audit webhook
// backend already sends, so an edge control plane needs no shipper of its own.
const AuditPath = "/v1/audit"

// ServerConfig configures the edge audit ingest listener.
type ServerConfig struct {
	Address string

	// TLSCertFile and TLSKeyFile are the server's own certificate.
	TLSCertFile string
	TLSKeyFile  string

	// ClientCAFile is the CA that signs edge shipper certificates. It is
	// required: cluster identity comes from the client certificate, so a
	// listener that does not verify one has no identity to work from.
	ClientCAFile string

	// MaxRequestBytes bounds a single batch.
	MaxRequestBytes int64

	ReadHeaderTimeout time.Duration
}

// Server receives audit batches from edge control planes.
type Server struct {
	config   ServerConfig
	registry *ClusterRegistry
	pipeline *Pipeline
	resolver UpstreamNamespaceResolver

	http *http.Server
}

// NewServer builds the ingest listener.
func NewServer(config ServerConfig, registry *ClusterRegistry, pipeline *Pipeline, resolver UpstreamNamespaceResolver) (*Server, error) {
	if config.ClientCAFile == "" {
		return nil, fmt.Errorf("--client-ca-file is required: edge cluster identity comes from the client certificate")
	}
	if config.TLSCertFile == "" || config.TLSKeyFile == "" {
		return nil, fmt.Errorf("--tls-cert-file and --tls-key-file are required")
	}

	tlsConfig, err := serverTLSConfig(config)
	if err != nil {
		return nil, err
	}

	server := &Server{
		config:   config,
		registry: registry,
		pipeline: pipeline,
		resolver: resolver,
	}

	mux := http.NewServeMux()
	mux.HandleFunc(AuditPath, server.handleAudit)
	mux.HandleFunc("/healthz", server.handleHealthz)
	mux.HandleFunc("/readyz", server.handleReadyz)

	readHeaderTimeout := config.ReadHeaderTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = 10 * time.Second
	}

	server.http = &http.Server{
		Addr:              config.Address,
		Handler:           mux,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	return server, nil
}

func serverTLSConfig(config ServerConfig) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed loading serving certificate: %w", err)
	}

	caCert, err := os.ReadFile(config.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("failed reading client CA: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("client CA %s contains no certificates", config.ClientCAFile)
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}, nil
}

// Start serves until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.Address)
	if err != nil {
		return fmt.Errorf("failed listening on %s: %w", s.config.Address, err)
	}

	return s.Serve(ctx, listener)
}

// Serve serves on an existing listener until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
	}()

	klog.InfoS("Edge audit ingest listening", "address", listener.Addr().String(), "path", AuditPath)

	if err := s.http.ServeTLS(listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleReadyz gates readiness on namespace cache sync rather than on the
// listener being bound. A listener that accepts traffic against a cold cache
// misfiles every record it takes in that window.
func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if !s.resolver.HasSynced() {
		http.Error(w, "upstream namespace caches have not synced", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// ingestResult reports what happened to one batch.
type ingestResult struct {
	Received int `json:"received"`
	Emitted  int `json:"emitted"`
	Dropped  int `json:"dropped"`
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	identity, err := s.registry.Identify(r.TLS.VerifiedChains)
	if err != nil {
		metrics.EdgeAuditUnauthenticatedRequests.Inc()
		klog.V(2).ErrorS(err, "Rejected edge audit batch")
		http.Error(w, "client identity is not registered", http.StatusForbidden)
		return
	}

	maxBytes := s.config.MaxRequestBytes
	if maxBytes <= 0 {
		maxBytes = 32 << 20
	}

	var list auditv1.EventList
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes)).Decode(&list); err != nil {
		http.Error(w, "request body is not an audit.k8s.io/v1 EventList", http.StatusBadRequest)
		return
	}

	metrics.EdgeAuditEventsReceived.WithLabelValues(identity.Name).Add(float64(len(list.Items)))

	result := ingestResult{Received: len(list.Items)}

	for i := range list.Items {
		event := &list.Items[i]

		switch err := s.pipeline.Process(r.Context(), identity, event); {
		case err == nil:
			result.Emitted++

		case errors.Is(err, ErrCacheNotSynced):
			metrics.EdgeAuditEventsRetried.WithLabelValues(identity.Name).Add(float64(len(list.Items)))
			klog.V(2).ErrorS(err, "Asking edge shipper to resend batch", "cluster", identity.Name)
			w.Header().Set("Retry-After", "5")
			http.Error(w, "namespace caches are cold, retry", http.StatusServiceUnavailable)
			return

		default:
			result.Dropped++
			metrics.EdgeAuditEventsDropped.WithLabelValues(identity.Name, dropReason(err)).Inc()
			klog.ErrorS(err, "Dropped edge audit event",
				"cluster", identity.Name,
				"auditID", event.AuditID,
			)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}

// dropReason classifies a terminal failure for metrics. The audit event itself
// is never logged or exported on this path: the reason a record is dropped is
// usually that it still names something a customer must not see.
func dropReason(err error) string {
	switch {
	case errors.Is(err, ErrUpstreamNamespaceUnknown):
		return "namespace_unresolved"
	case errors.Is(err, ErrAmbiguousProject):
		return "ambiguous_project"
	case errors.Is(err, ErrDownstreamNamespaceLeak):
		return "namespace_leak"
	default:
		return "error"
	}
}
