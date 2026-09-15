package processor

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	authnv1 "k8s.io/api/authentication/v1"
)

func TestConvertLinks(t *testing.T) {
	// Mock KindResolver for testing resource-to-kind conversion
	mockResolver := func(apiGroup, resource string) (string, error) {
		kinds := map[string]string{
			"deployments": "Deployment",
			"services":    "Service",
			"pods":        "Pod",
		}
		if kind, ok := kinds[resource]; ok {
			return kind, nil
		}
		return "", nil
	}
	tests := []struct {
		name     string
		celLinks []cel.Link
		want     int
		wantKind string
	}{
		{
			name:     "nil links",
			celLinks: nil,
			want:     0,
		},
		{
			name:     "empty links",
			celLinks: []cel.Link{},
			want:     0,
		},
		{
			name: "link with kind field (Kubernetes events)",
			celLinks: []cel.Link{
				{
					Marker: "Pod my-pod",
					Resource: map[string]any{
						"kind":      "Pod",
						"name":      "my-pod",
						"namespace": "default",
						"uid":       "pod-123",
						"apiGroup":  "",
					},
				},
			},
			want:     1,
			wantKind: "Pod",
		},
		{
			name: "link with resource field (Kubernetes audit objectRef)",
			celLinks: []cel.Link{
				{
					Marker: "Deployment my-deployment",
					Resource: map[string]any{
						"resource":  "deployments",
						"name":      "my-deployment",
						"namespace": "default",
						"uid":       "deploy-456",
						"apiGroup":  "apps",
					},
				},
			},
			want:     1,
			wantKind: "Deployment",
		},
		{
			name: "link with both kind and resource (kind wins)",
			celLinks: []cel.Link{
				{
					Marker: "Service my-service",
					Resource: map[string]any{
						"kind":      "Service",
						"resource":  "services",
						"name":      "my-service",
						"namespace": "default",
					},
				},
			},
			want:     1,
			wantKind: "Service",
		},
		{
			name: "link with type field (actorRef)",
			celLinks: []cel.Link{
				{
					Marker: "kubernetes-admin",
					Resource: map[string]any{
						"type": "user",
						"name": "kubernetes-admin",
					},
				},
			},
			want:     1,
			wantKind: "user",
		},
		{
			name: "multiple links with mixed formats",
			celLinks: []cel.Link{
				{
					Marker: "Pod my-pod",
					Resource: map[string]any{
						"kind": "Pod",
						"name": "my-pod",
					},
				},
				{
					Marker: "Deployment my-deployment",
					Resource: map[string]any{
						"resource": "deployments",
						"name":     "my-deployment",
					},
				},
				{
					Marker: "kubernetes-admin",
					Resource: map[string]any{
						"type": "user",
						"name": "kubernetes-admin",
					},
				},
			},
			want:     3,
			wantKind: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertLinks(tt.celLinks, mockResolver)
			if err != nil {
				t.Fatalf("ConvertLinks() returned error: %v", err)
			}

			if len(got) != tt.want {
				t.Errorf("ConvertLinks() returned %d links, want %d", len(got), tt.want)
				return
			}

			if tt.want > 0 && tt.wantKind != "" {
				if got[0].Resource.Kind != tt.wantKind {
					t.Errorf("ConvertLinks() first link Kind = %q, want %q", got[0].Resource.Kind, tt.wantKind)
				}
			}
		})
	}
}

func TestConvertLinksErrorPaths(t *testing.T) {
	tests := []struct {
		name      string
		celLinks  []cel.Link
		resolver  KindResolver
		wantErr   bool
		wantErrIs error
	}{
		{
			name: "resolver returns error",
			celLinks: []cel.Link{
				{
					Marker: "unknown resource",
					Resource: map[string]any{
						"resource": "unknowns",
						"apiGroup": "test.example.com",
						"name":     "test-resource",
					},
				},
			},
			resolver: func(apiGroup, resource string) (string, error) {
				return "", fmt.Errorf("unknown resource: %s", resource)
			},
			wantErr:   true,
			wantErrIs: ErrKindResolution,
		},
		{
			name: "resolver returns error for second link",
			celLinks: []cel.Link{
				{
					Marker: "known resource",
					Resource: map[string]any{
						"kind": "Pod", // This has kind, so no resolution needed
						"name": "my-pod",
					},
				},
				{
					Marker: "unknown resource",
					Resource: map[string]any{
						"resource": "unknowns",
						"apiGroup": "test.example.com",
						"name":     "test-resource",
					},
				},
			},
			resolver: func(apiGroup, resource string) (string, error) {
				return "", fmt.Errorf("discovery failed: %s", resource)
			},
			wantErr:   true,
			wantErrIs: ErrKindResolution,
		},
		{
			name: "nil resolver with resource field returns no error",
			celLinks: []cel.Link{
				{
					Marker: "no resolver",
					Resource: map[string]any{
						"resource": "deployments",
						"apiGroup": "apps",
						"name":     "my-deployment",
					},
				},
			},
			resolver: nil, // No resolver provided
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertLinks(tt.celLinks, tt.resolver)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ConvertLinks() expected error, got nil")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("ConvertLinks() error = %v, want error wrapping %v", err, tt.wantErrIs)
				}
				return
			}

			if err != nil {
				t.Errorf("ConvertLinks() unexpected error: %v", err)
				return
			}

			if len(got) != len(tt.celLinks) {
				t.Errorf("ConvertLinks() returned %d links, want %d", len(got), len(tt.celLinks))
			}
		})
	}
}

func TestExtractTenant(t *testing.T) {
	tests := []struct {
		name     string
		user     authnv1.UserInfo
		wantType string
		wantName string
	}{
		{
			name:     "platform (no extra fields)",
			user:     authnv1.UserInfo{},
			wantType: TenantTypePlatform,
			wantName: "",
		},
		{
			name: "organization from parent fields",
			user: authnv1.UserInfo{
				Extra: map[string]authnv1.ExtraValue{
					"iam.miloapis.com/parent-type": {"Organization"},
					"iam.miloapis.com/parent-name": {"acme-corp"},
				},
			},
			wantType: TenantTypeOrganization,
			wantName: "acme-corp",
		},
		{
			name: "project from parent fields",
			user: authnv1.UserInfo{
				Extra: map[string]authnv1.ExtraValue{
					"iam.miloapis.com/parent-type": {"Project"},
					"iam.miloapis.com/parent-name": {"my-project"},
				},
			},
			wantType: TenantTypeProject,
			wantName: "my-project",
		},
		{
			name: "organization from legacy field",
			user: authnv1.UserInfo{
				Extra: map[string]authnv1.ExtraValue{
					"organization": {"legacy-org"},
				},
			},
			wantType: TenantTypeOrganization,
			wantName: "legacy-org",
		},
		{
			name: "project overrides organization",
			user: authnv1.UserInfo{
				Extra: map[string]authnv1.ExtraValue{
					"organization": {"my-org"},
					"project":      {"my-project"},
				},
			},
			wantType: TenantTypeProject,
			wantName: "my-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTenant(tt.user)
			if got.Type != tt.wantType {
				t.Errorf("ExtractTenant() Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.Name != tt.wantName {
				t.Errorf("ExtractTenant() Name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestExtractTenantFromAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		eventMap map[string]any
		wantType string
		wantName string
	}{
		{
			name:     "nil event map falls back to platform",
			eventMap: nil,
			wantType: TenantTypePlatform,
			wantName: "",
		},
		{
			name:     "no metadata falls back to platform",
			eventMap: map[string]any{},
			wantType: TenantTypePlatform,
			wantName: "",
		},
		{
			name: "metadata without annotations falls back to platform",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"uid": "event-123",
				},
			},
			wantType: TenantTypePlatform,
			wantName: "",
		},
		{
			name: "annotations without scope keys fall back to platform",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"some-other-annotation": "value",
					},
				},
			},
			wantType: TenantTypePlatform,
			wantName: "",
		},
		{
			name: "project scope from annotations",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"platform.miloapis.com/scope.type": TenantTypeProject,
						"platform.miloapis.com/scope.name": "my-project",
					},
				},
			},
			wantType: TenantTypeProject,
			wantName: "my-project",
		},
		{
			name: "organization scope from annotations",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"platform.miloapis.com/scope.type": TenantTypeOrganization,
						"platform.miloapis.com/scope.name": "acme-corp",
					},
				},
			},
			wantType: TenantTypeOrganization,
			wantName: "acme-corp",
		},
		{
			name: "scope.type present but empty falls back to platform",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"platform.miloapis.com/scope.type": "",
						"platform.miloapis.com/scope.name": "my-project",
					},
				},
			},
			wantType: TenantTypePlatform,
			wantName: "",
		},
		{
			name: "scope.type present but scope.name absent uses empty name",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"platform.miloapis.com/scope.type": TenantTypeProject,
					},
				},
			},
			wantType: TenantTypeProject,
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTenantFromAnnotations(tt.eventMap)
			if got.Type != tt.wantType {
				t.Errorf("ExtractTenantFromAnnotations() Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.Name != tt.wantName {
				t.Errorf("ExtractTenantFromAnnotations() Name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestExtractSourceFromAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		eventMap map[string]any
		want     v1alpha1.ActivitySource
	}{
		{
			name:     "nil event map returns empty source",
			eventMap: nil,
			want:     v1alpha1.ActivitySource{},
		},
		{
			name:     "no metadata returns empty source",
			eventMap: map[string]any{},
			want:     v1alpha1.ActivitySource{},
		},
		{
			name: "metadata without annotations returns empty source",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"uid": "event-123",
				},
			},
			want: v1alpha1.ActivitySource{},
		},
		{
			name: "annotations without source keys returns empty source",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"some-other-annotation": "value",
					},
				},
			},
			want: v1alpha1.ActivitySource{},
		},
		{
			name: "all four source annotations populate ActivitySource",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"activity.miloapis.com/source-plane-type": "edge",
						"activity.miloapis.com/source-cluster":    "cluster-dfw-1",
						"activity.miloapis.com/source-region":     "us-central1",
						"activity.miloapis.com/source-city":       "dfw",
					},
				},
			},
			want: v1alpha1.ActivitySource{
				PlaneType: "edge",
				Cluster:   "cluster-dfw-1",
				Region:    "us-central1",
				City:      "dfw",
			},
		},
		{
			name: "partial source annotations leave the rest empty",
			eventMap: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"activity.miloapis.com/source-cluster": "cluster-dfw-1",
					},
				},
			},
			want: v1alpha1.ActivitySource{
				Cluster: "cluster-dfw-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSourceFromAnnotations(tt.eventMap)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQualifiedOriginID(t *testing.T) {
	tests := []struct {
		name     string
		source   v1alpha1.ActivitySource
		originID string
		want     string
	}{
		{
			name:     "no source data returns bare ID",
			source:   v1alpha1.ActivitySource{},
			originID: "a1b2c3",
			want:     "a1b2c3",
		},
		{
			name:     "source without cluster returns bare ID",
			source:   v1alpha1.ActivitySource{PlaneType: "management"},
			originID: "a1b2c3",
			want:     "a1b2c3",
		},
		{
			name:     "source with cluster qualifies ID",
			source:   v1alpha1.ActivitySource{PlaneType: "edge", Cluster: "cluster-dfw-1"},
			originID: "a1b2c3",
			want:     "edge/cluster-dfw-1/a1b2c3",
		},
		{
			name:     "cluster without plane type returns bare ID",
			source:   v1alpha1.ActivitySource{Cluster: "cluster-dfw-1"},
			originID: "a1b2c3",
			want:     "a1b2c3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qualifiedOriginID(tt.source, tt.originID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetNestedString(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		keys []string
		want string
	}{
		{
			name: "single level",
			m:    map[string]any{"key": "value"},
			keys: []string{"key"},
			want: "value",
		},
		{
			name: "nested levels",
			m: map[string]any{
				"user": map[string]any{
					"username": "alice",
				},
			},
			keys: []string{"user", "username"},
			want: "alice",
		},
		{
			name: "deeply nested",
			m: map[string]any{
				"audit": map[string]any{
					"objectRef": map[string]any{
						"name": "my-resource",
					},
				},
			},
			keys: []string{"audit", "objectRef", "name"},
			want: "my-resource",
		},
		{
			name: "missing key",
			m:    map[string]any{"key": "value"},
			keys: []string{"missing"},
			want: "",
		},
		{
			name: "nil map",
			m:    nil,
			keys: []string{"key"},
			want: "",
		},
		{
			name: "empty keys",
			m:    map[string]any{"key": "value"},
			keys: []string{},
			want: "",
		},
		{
			name: "non-string value",
			m:    map[string]any{"key": 123},
			keys: []string{"key"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetNestedString(tt.m, tt.keys...)
			if got != tt.want {
				t.Errorf("GetNestedString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConvertLinksMetadataFallback(t *testing.T) {
	tests := []struct {
		name         string
		link         cel.Link
		wantResource v1alpha1.ActivityResource
	}{
		{
			name: "kubernetes resource object with nested metadata",
			link: cel.Link{
				Marker: "javalords.shop",
				Resource: map[string]any{
					"apiVersion": "dns.networking.miloapis.com/v1alpha1",
					"kind":       "DNSZone",
					"metadata": map[string]any{
						"name":      "javalords-shop-06vr2d",
						"namespace": "default",
						"uid":       "0d3b8c4e-7609-4635-8457-a59309eb390f",
					},
				},
			},
			wantResource: v1alpha1.ActivityResource{
				APIGroup:   "dns.networking.miloapis.com",
				APIVersion: "v1alpha1",
				Kind:       "DNSZone",
				Name:       "javalords-shop-06vr2d",
				Namespace:  "default",
				UID:        "0d3b8c4e-7609-4635-8457-a59309eb390f",
			},
		},
		{
			name: "core group resource with apiVersion v1",
			link: cel.Link{
				Marker: "my-config",
				Resource: map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]any{
						"name":      "my-config",
						"namespace": "kube-system",
					},
				},
			},
			wantResource: v1alpha1.ActivityResource{
				APIGroup:   "",
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "my-config",
				Namespace:  "kube-system",
			},
		},
		{
			name: "top-level fields take precedence over metadata",
			link: cel.Link{
				Marker: "top-level-resource",
				Resource: map[string]any{
					"kind":      "Deployment",
					"name":      "top-level-name",
					"namespace": "production",
					"uid":       "flat-uid",
					"apiGroup":  "apps",
					"metadata": map[string]any{
						"name":      "metadata-name",
						"namespace": "metadata-ns",
						"uid":       "metadata-uid",
					},
				},
			},
			wantResource: v1alpha1.ActivityResource{
				APIGroup:  "apps",
				Kind:      "Deployment",
				Name:      "top-level-name",
				Namespace: "production",
				UID:       "flat-uid",
			},
		},
		{
			name: "actorRef format unchanged",
			link: cel.Link{
				Marker: "alice",
				Resource: map[string]any{
					"type": "user",
					"name": "alice",
				},
			},
			wantResource: v1alpha1.ActivityResource{
				Kind: "user",
				Name: "alice",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertLinks([]cel.Link{tt.link}, nil)
			require.NoError(t, err)
			require.Len(t, got, 1)

			assert.Equal(t, tt.link.Marker, got[0].Marker, "Marker")
			assert.Equal(t, tt.wantResource.APIGroup, got[0].Resource.APIGroup, "APIGroup")
			assert.Equal(t, tt.wantResource.APIVersion, got[0].Resource.APIVersion, "APIVersion")
			assert.Equal(t, tt.wantResource.Kind, got[0].Resource.Kind, "Kind")
			assert.Equal(t, tt.wantResource.Name, got[0].Resource.Name, "Name")
			assert.Equal(t, tt.wantResource.Namespace, got[0].Resource.Namespace, "Namespace")
			assert.Equal(t, tt.wantResource.UID, got[0].Resource.UID, "UID")
		})
	}
}
