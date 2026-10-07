package httpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	activityclient "go.miloapis.com/activity/pkg/client/clientset/versioned/typed/activity/v1alpha1"
	"go.miloapis.com/activity/pkg/mcp/tools"
)

const (
	// ProjectHeader names the project whose activity a request reads. It is
	// set by the already-authenticated client calling this server (Patch),
	// never by the model: a model that could name its own project would be one
	// prompt injection away from another tenant's history.
	ProjectHeader = "X-Datum-Project"

	// misconfiguredClientNote closes every error a request can fail with
	// before a read is attempted. All of them are the calling client's
	// configuration, so the sentence names that actor and rules out the
	// advice an assistant would otherwise relay (re-authenticate).
	misconfiguredClientNote = "The person who asked did nothing wrong and re-authenticating will not " +
		"help: this is a configuration problem for whoever operates that client"

	// defaultRequestTimeout bounds a single Activity API call when the base
	// configuration sets no timeout of its own.
	defaultRequestTimeout = 60 * time.Second
)

// LoadBaseConfig loads the configuration that supplies the control plane
// endpoint and CA for HTTP mode. It reads kubeconfig (or, when empty, the
// standard KUBECONFIG / ~/.kube/config locations) and never falls back to
// in-cluster configuration: this server reads Datum project control planes
// through Milo, and the cluster it happens to run in is never that.
func LoadBaseConfig(kubeconfig, kubeContext string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	raw, err := rules.Load()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	// NewDefaultClientConfig, unlike the deferred loader, has no in-cluster
	// fallback: an empty kubeconfig is an error here, not a silent switch to
	// the pod's own service account.
	cfg, err := clientcmd.NewDefaultClientConfig(*raw, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building configuration from kubeconfig (HTTP mode requires a kubeconfig "+
			"naming the Milo API endpoint and CA; pass --kubeconfig or set KUBECONFIG): %w", err)
	}
	return cfg, nil
}

// CheckBaseConfig refuses a base configuration this server cannot use
// safely. It is called at boot so a deployment mistake fails there, rather
// than as a stream of tool errors that read like the caller's fault.
//
//   - The host must carry no path. Each request appends a project
//     control-plane path to it; a base that already has one would produce a
//     URL that is neither the project's control plane nor an error.
//   - The host must carry no userinfo, query or fragment. Userinfo in
//     particular would be sent as Basic auth in place of the caller's token.
//   - As a best-effort guard, the host must not be the literal address
//     in-cluster config resolves to (https://$KUBERNETES_SERVICE_HOST:$PORT).
//     That catches the in-cluster fallback, not every way of naming the local
//     API server (https://kubernetes.default.svc passes). It is a
//     misconfiguration check, not a security boundary: the server's own
//     credentials are stripped from every read regardless.
func CheckBaseConfig(cfg *rest.Config) error {
	if cfg == nil || cfg.Host == "" {
		return fmt.Errorf("control plane endpoint is empty: the kubeconfig must name the Milo API server")
	}
	host, err := parseHost(cfg.Host)
	if err != nil {
		return err
	}
	if host.User != nil {
		return fmt.Errorf("control plane endpoint carries credentials in its URL: the kubeconfig must name the " +
			"bare Milo API server address; HTTP mode reads only with each caller's own token")
	}
	if host.RawQuery != "" || host.ForceQuery || host.Fragment != "" {
		return fmt.Errorf("control plane endpoint %s has a query or fragment: the kubeconfig must name the "+
			"bare Milo API server address", cfg.Host)
	}
	if p := strings.TrimSuffix(host.Path, "/"); p != "" {
		return fmt.Errorf("control plane endpoint %s already has a path (%q): HTTP mode appends each "+
			"caller's project control-plane path itself, so the kubeconfig must name the bare Milo API "+
			"server address", cfg.Host, host.Path)
	}
	if local := localClusterEndpoint(); local != "" && sameEndpoint(cfg.Host, local) {
		return fmt.Errorf("control plane endpoint %s is this cluster's own API server: the activity MCP "+
			"server reads Datum project control planes through Milo, not the cluster it runs in, so the "+
			"in-cluster fallback is never correct. Mount a kubeconfig naming the Milo API server address "+
			"and CA, and point KUBECONFIG (or --kubeconfig) at it", cfg.Host)
	}
	return nil
}

// localClusterEndpoint returns the API server address in-cluster config
// resolves to, or "" outside a pod. It mirrors rest.InClusterConfig's own
// derivation so the check holds even when no service account token is mounted.
func localClusterEndpoint() string {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return ""
	}
	return "https://" + net.JoinHostPort(host, port)
}

// sameEndpoint compares two API server addresses, ignoring a trailing slash.
func sameEndpoint(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

// parseHost parses a rest.Config host, which may omit the scheme.
func parseHost(host string) (*url.URL, error) {
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("parsing control plane host %q: %w", host, err)
	}
	return u, nil
}

// ProjectControlPlanePath returns the Milo API path for a project's control
// plane. Milo authenticates the caller there and forwards the request to the
// Activity API server with the project recorded as the caller's parent, which
// is what scopes every query to that project.
func ProjectControlPlanePath(project string) string {
	return fmt.Sprintf("/apis/resourcemanager.miloapis.com/v1alpha1/projects/%s/control-plane", project)
}

// ClientConfig derives the REST config one request reads through: the
// caller's token, pointed at their project's control plane. The base config
// supplies the endpoint and CA only; every credential it might carry (client
// certificate, token, exec or auth provider, impersonation) is dropped, so the
// server's own identity can never leak into a caller's read.
//
// The project path is mandatory. Without it the request would reach Activity
// with no parent recorded, and Activity treats that as a platform-wide query.
func ClientConfig(base *rest.Config, token, project string) (*rest.Config, error) {
	// The project arrives in a header and is interpolated into a URL path, so
	// it is validated before it can reshape that path into another route.
	if errs := validation.IsDNS1123Subdomain(project); len(errs) > 0 {
		return nil, fmt.Errorf("invalid project %q on the %s header sent by the client that called "+
			"this tool: %s. %s", project, ProjectHeader, strings.Join(errs, "; "), misconfiguredClientNote)
	}

	cfg := rest.AnonymousClientConfig(rest.CopyConfig(base))
	cfg.BearerToken = token
	cfg.BearerTokenFile = ""
	// A rate limiter on the base would be shared by every caller.
	cfg.RateLimiter = nil
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultRequestTimeout
	}

	host, err := parseHost(cfg.Host)
	if err != nil {
		return nil, err
	}
	// Only scheme and host:port survive from the base. Userinfo especially:
	// client-go would send it as Basic auth instead of the caller's token.
	host.User = nil
	host.RawQuery = ""
	host.ForceQuery = false
	host.Fragment = ""
	host.RawFragment = ""
	host.Path = ProjectControlPlanePath(project)
	host.RawPath = ""
	cfg.Host = host.String()

	return cfg, nil
}

// bearerToken extracts a bearer token from the Authorization header.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}

// ResolverFromRequest returns a client resolver bound to the caller and
// project on r. Resolution is deferred to tool-call time, so a request missing
// either fails as a tool error the model can relay, and no API request is
// made. The client is built at most once per request.
func ResolverFromRequest(r *http.Request, base *rest.Config) tools.ClientResolver {
	token := bearerToken(r)
	project := strings.TrimSpace(r.Header.Get(ProjectHeader))

	var (
		once   sync.Once
		client activityclient.ActivityV1alpha1Interface
		err    error
	)
	return func(context.Context) (activityclient.ActivityV1alpha1Interface, error) {
		once.Do(func() {
			client, err = clientFor(base, token, project)
		})
		return client, err
	}
}

// clientFor builds an Activity client that reads project's control plane as
// the bearer of token.
func clientFor(base *rest.Config, token, project string) (activityclient.ActivityV1alpha1Interface, error) {
	if token == "" {
		return nil, fmt.Errorf("no credentials on this request: the client that called this tool did not "+
			"forward the user's identity. %s", misconfiguredClientNote)
	}
	if project == "" {
		return nil, fmt.Errorf("no project on this request: the client that called this tool did not set "+
			"the %s header. %s", ProjectHeader, misconfiguredClientNote)
	}

	cfg, err := ClientConfig(base, token, project)
	if err != nil {
		return nil, err
	}
	c, err := activityclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building activity client: %w", err)
	}
	return c, nil
}
