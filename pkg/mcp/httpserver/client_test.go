package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const testToken = "caller-token"

// baseConfig is shaped like a config a deployment might hand the server by
// mistake: an endpoint and CA, plus every kind of server identity, none of
// which may survive into a caller's read.
func baseConfig() *rest.Config {
	return &rest.Config{
		Host:            "https://api.datum.example",
		BearerToken:     "server-service-account-token",
		BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
		Username:        "server-user",
		Password:        "server-password",
		Impersonate:     rest.ImpersonationConfig{UserName: "system:admin", Groups: []string{"system:masters"}},
		AuthProvider:    &clientcmdapi.AuthProviderConfig{Name: "oidc"},
		ExecProvider:    &clientcmdapi.ExecConfig{Command: "get-token"},
		TLSClientConfig: rest.TLSClientConfig{
			CAFile:   "/var/run/secrets/ca.crt",
			CAData:   []byte("ca-data"),
			CertFile: "/var/run/secrets/tls.crt",
			KeyFile:  "/var/run/secrets/tls.key",
			CertData: []byte("cert-data"),
			KeyData:  []byte("key-data"),
		},
	}
}

func TestClientConfigAddressesProjectControlPlane(t *testing.T) {
	for _, host := range []string{
		"https://api.datum.example",
		"https://api.datum.example/",
		"api.datum.example",
	} {
		base := baseConfig()
		base.Host = host
		cfg, err := ClientConfig(base, testToken, "my-project")
		if err != nil {
			t.Fatalf("ClientConfig(%q): %v", host, err)
		}
		want := "https://api.datum.example/apis/resourcemanager.miloapis.com/v1alpha1/projects/my-project/control-plane"
		if cfg.Host != want {
			t.Errorf("Host for base %q = %q, want %q", host, cfg.Host, want)
		}
	}
}

// TestClientConfigDropsURLCredentials: userinfo in the base host survives
// AnonymousClientConfig, and client-go would send it as Basic auth in place
// of the caller's bearer token. The rewrite must drop it, with any query or
// fragment.
func TestClientConfigDropsURLCredentials(t *testing.T) {
	base := baseConfig()
	base.Host = "https://user:pass@api.datum.example?x=1#frag"
	cfg, err := ClientConfig(base, testToken, "my-project")
	if err != nil {
		t.Fatalf("ClientConfig: %v", err)
	}
	want := "https://api.datum.example/apis/resourcemanager.miloapis.com/v1alpha1/projects/my-project/control-plane"
	if cfg.Host != want {
		t.Errorf("Host = %q, want %q", cfg.Host, want)
	}
}

// TestClientConfigCarriesOnlyTheCallerCredential is the security property the
// design rests on: the server must never read as itself.
func TestClientConfigCarriesOnlyTheCallerCredential(t *testing.T) {
	base := baseConfig()
	cfg, err := ClientConfig(base, testToken, "my-project")
	if err != nil {
		t.Fatalf("ClientConfig: %v", err)
	}

	if cfg.BearerToken != testToken {
		t.Errorf("BearerToken = %q, want the caller's token", cfg.BearerToken)
	}
	if cfg.BearerTokenFile != "" {
		t.Errorf("BearerTokenFile = %q, want it cleared", cfg.BearerTokenFile)
	}
	if cfg.Username != "" || cfg.Password != "" {
		t.Errorf("basic auth survived: %q/%q", cfg.Username, cfg.Password)
	}
	if cfg.Impersonate.UserName != "" || len(cfg.Impersonate.Groups) != 0 {
		t.Errorf("Impersonate = %+v, want none", cfg.Impersonate)
	}
	if cfg.AuthProvider != nil {
		t.Errorf("AuthProvider = %+v, want nil", cfg.AuthProvider)
	}
	if cfg.ExecProvider != nil {
		t.Errorf("ExecProvider = %+v, want nil", cfg.ExecProvider)
	}
	if cfg.CertFile != "" || cfg.KeyFile != "" || len(cfg.CertData) != 0 || len(cfg.KeyData) != 0 {
		t.Error("client certificate survived into the caller's config")
	}
	if cfg.CAFile != base.CAFile || string(cfg.CAData) != string(base.CAData) {
		t.Error("CA was not carried over from the base config")
	}
	// The base is shared by every request and must be left alone.
	if base.BearerToken != "server-service-account-token" || base.Host != "https://api.datum.example" {
		t.Error("ClientConfig mutated the shared base config")
	}
}

// TestClientConfigRejectsHostileProject: the project comes off a header and
// is interpolated into a URL path, so anything that could reshape that path
// into another route is refused rather than escaped.
func TestClientConfigRejectsHostileProject(t *testing.T) {
	for _, project := range []string{
		"../../../api/v1/nodes",
		"a/b",
		"proj/../other",
		"proj%2f..",
		"UPPER",
		"has space",
		"..",
		"",
		"proj?x=1",
		"proj#frag",
	} {
		if _, err := ClientConfig(baseConfig(), testToken, project); err == nil {
			t.Errorf("ClientConfig(%q) succeeded, want a rejection", project)
		}
	}
}

func TestResolverRequiresCredentialsAndProject(t *testing.T) {
	tests := []struct {
		name    string
		auth    string
		project string
		want    string
	}{
		{name: "no token", project: "my-project", want: "no credentials"},
		{name: "wrong scheme", auth: "Basic abc", project: "my-project", want: "no credentials"},
		{name: "empty bearer", auth: "Bearer ", project: "my-project", want: "no credentials"},
		{name: "no project", auth: "Bearer " + testToken, want: "no project"},
		{name: "blank project", auth: "Bearer " + testToken, project: "   ", want: "no project"},
		{name: "invalid project", auth: "Bearer " + testToken, project: "a/b", want: "invalid project"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			if tc.project != "" {
				r.Header.Set(ProjectHeader, tc.project)
			}

			_, err := ResolverFromRequest(r, baseConfig())(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("error leaks the bearer token: %q", err)
			}
		})
	}
}

// TestCredentialErrorsBlameTheClientNotTheUser: these errors reach a person
// through an assistant, so they must not tell that person to do something
// they cannot (re-authenticate), and must name the actual actor.
func TestCredentialErrorsBlameTheClientNotTheUser(t *testing.T) {
	secondPerson := regexp.MustCompile(`(?i)\byou(r|rs)?\b`)

	errs := map[string]error{}
	for name, h := range map[string]struct{ auth, project string }{
		"no-token":        {project: "acme-prod"},
		"no-project":      {auth: "Bearer " + testToken},
		"invalid-project": {auth: "Bearer " + testToken, project: "Not A Project"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if h.auth != "" {
			r.Header.Set("Authorization", h.auth)
		}
		if h.project != "" {
			r.Header.Set(ProjectHeader, h.project)
		}
		_, err := ResolverFromRequest(r, baseConfig())(context.Background())
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		errs[name] = err
	}

	for name, err := range errs {
		msg := err.Error()
		if !strings.Contains(msg, "re-authenticating will not help") {
			t.Errorf("%s: error = %q, want it to rule out re-authenticating", name, msg)
		}
		if !strings.Contains(msg, "client that called this tool") || !strings.Contains(msg, "whoever operates that client") {
			t.Errorf("%s: error = %q, want it to name the client as the actor", name, msg)
		}
		if m := secondPerson.FindString(msg); m != "" {
			t.Errorf("%s: error = %q addresses the reader as %q", name, msg, m)
		}
	}
}

// TestProjectComesFromHeaderOnly guards the prompt-injection defense: a query
// parameter the model could influence must not reach the addressing.
func TestProjectComesFromHeaderOnly(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/mcp?project=attacker-project", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set(ProjectHeader, " my-project ")

	if _, err := ResolverFromRequest(r, &rest.Config{Host: "https://api.datum.example"})(context.Background()); err != nil {
		t.Fatalf("resolver: %v", err)
	}
	cfg, err := ClientConfig(baseConfig(), bearerToken(r), strings.TrimSpace(r.Header.Get(ProjectHeader)))
	if err != nil {
		t.Fatalf("ClientConfig: %v", err)
	}
	if strings.Contains(cfg.Host, "attacker-project") {
		t.Errorf("Host = %q, want the header's project", cfg.Host)
	}
	if !strings.Contains(cfg.Host, "/projects/my-project/control-plane") {
		t.Errorf("Host = %q, want the trimmed header project", cfg.Host)
	}
}

func TestBearerToken(t *testing.T) {
	tests := []struct {
		auth string
		want string
	}{
		{auth: "Bearer " + testToken, want: testToken},
		{auth: "bearer " + testToken, want: testToken},
		{auth: "Bearer  " + testToken + " ", want: testToken},
		{auth: "Basic " + testToken, want: ""},
		{auth: testToken, want: ""},
		{auth: "", want: ""},
	}
	for _, tc := range tests {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if tc.auth != "" {
			r.Header.Set("Authorization", tc.auth)
		}
		if got := bearerToken(r); got != tc.want {
			t.Errorf("bearerToken(%q) = %q, want %q", tc.auth, got, tc.want)
		}
	}
}

func TestCheckBaseConfig(t *testing.T) {
	tests := []struct {
		name        string
		serviceHost string
		servicePort string
		host        string
		wantErr     string
	}{
		{name: "explicit endpoint", host: "https://api.datum.example"},
		{name: "explicit endpoint, trailing slash", host: "https://api.datum.example/"},
		{name: "no scheme", host: "api.datum.example:6443"},
		{name: "empty", host: "", wantErr: "empty"},
		{
			name:    "already a project control plane",
			host:    "https://api.datum.example/apis/resourcemanager.miloapis.com/v1alpha1/projects/p/control-plane",
			wantErr: "already has a path",
		},
		{name: "any path", host: "https://api.datum.example/prefix", wantErr: "already has a path"},
		{name: "userinfo", host: "https://user:pass@api.datum.example", wantErr: "credentials"},
		{name: "user only", host: "https://user@api.datum.example", wantErr: "credentials"},
		{name: "query", host: "https://api.datum.example?x=1", wantErr: "query or fragment"},
		{name: "empty query", host: "https://api.datum.example?", wantErr: "query or fragment"},
		{name: "fragment", host: "https://api.datum.example#frag", wantErr: "query or fragment"},
		{
			name:        "in-cluster fallback",
			serviceHost: "10.0.0.1",
			servicePort: "443",
			host:        "https://10.0.0.1:443",
			wantErr:     "KUBECONFIG",
		},
		{
			name:        "IPv6 in-cluster fallback",
			serviceHost: "fd00::1",
			servicePort: "443",
			host:        "https://[fd00::1]:443/",
			wantErr:     "KUBECONFIG",
		},
		{
			name:        "in a cluster, explicit endpoint",
			serviceHost: "10.0.0.1",
			servicePort: "443",
			host:        "https://api.datum.example",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KUBERNETES_SERVICE_HOST", tc.serviceHost)
			t.Setenv("KUBERNETES_SERVICE_PORT", tc.servicePort)

			err := CheckBaseConfig(&rest.Config{Host: tc.host})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckBaseConfig(%q) = %v, want nil", tc.host, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("CheckBaseConfig(%q) = %v, want an error mentioning %q", tc.host, err, tc.wantErr)
			}
		})
	}
}

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: milo
  cluster:
    server: https://api.datum.example
contexts:
- name: milo
  context:
    cluster: milo
    user: milo
current-context: milo
users:
- name: milo
  user:
    token: server-token
`

func TestLoadBaseConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadBaseConfig(path, "")
	if err != nil {
		t.Fatalf("LoadBaseConfig: %v", err)
	}
	if cfg.Host != "https://api.datum.example" {
		t.Errorf("Host = %q", cfg.Host)
	}
}

// TestLoadBaseConfigNeverFallsBackInCluster: with no usable kubeconfig, even
// inside a pod, loading must fail rather than silently use the pod's own
// API server and service account.
func TestLoadBaseConfigNeverFallsBackInCluster(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", empty)
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	if cfg, err := LoadBaseConfig("", ""); err == nil {
		t.Fatalf("LoadBaseConfig with no kubeconfig = %q, want an error", cfg.Host)
	}
	if _, err := LoadBaseConfig(filepath.Join(t.TempDir(), "missing"), ""); err == nil {
		t.Fatal("LoadBaseConfig with a missing kubeconfig succeeded, want an error")
	}
}
