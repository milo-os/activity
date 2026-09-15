package eventexporter

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

func TestUpstreamClusterNameFromLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  string
	}{
		{
			name:  "modern slash-less name",
			label: "cluster-my-project-abc123",
			want:  "my-project-abc123",
		},
		{
			name:  "legacy leading-slash name",
			label: "cluster-_my-project-abc123",
			want:  "my-project-abc123",
		},
		{
			name:  "multi-segment name is preserved",
			label: "cluster-org_my-project",
			want:  "org/my-project",
		},
		{
			name:  "empty label",
			label: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upstreamClusterNameFromLabel(tt.label); got != tt.want {
				t.Errorf("upstreamClusterNameFromLabel(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

func newNamespaceLister(namespaces ...*corev1.Namespace) corelisters.NamespaceLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, ns := range namespaces {
		_ = indexer.Add(ns)
	}
	return corelisters.NewNamespaceLister(indexer)
}

func TestNamespaceScopeResolver(t *testing.T) {
	tests := []struct {
		name          string
		lookupName    string
		namespaces    []*corev1.Namespace
		wantScopeType string
		wantScopeName string
		wantOK        bool
	}{
		{
			name:       "namespace not found: unscoped",
			lookupName: "missing",
			wantOK:     false,
		},
		{
			name:       "namespace with no labels: unscoped",
			lookupName: "default",
			namespaces: []*corev1.Namespace{
				{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			},
			wantOK: false,
		},
		{
			name:       "namespace with empty upstream label: unscoped",
			lookupName: "default",
			namespaces: []*corev1.Namespace{
				{ObjectMeta: metav1.ObjectMeta{
					Name:   "default",
					Labels: map[string]string{upstreamClusterNameLabel: ""},
				}},
			},
			wantOK: false,
		},
		{
			name:       "namespace with upstream label: resolves project scope",
			lookupName: "default",
			namespaces: []*corev1.Namespace{
				{ObjectMeta: metav1.ObjectMeta{
					Name:   "default",
					Labels: map[string]string{upstreamClusterNameLabel: "cluster-my-project"},
				}},
			},
			wantScopeType: scopeTypeProject,
			wantScopeName: "my-project",
			wantOK:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := newNamespaceScopeResolver(newNamespaceLister(tt.namespaces...))

			gotType, gotName, gotOK := resolver.Resolve(tt.lookupName)
			if gotOK != tt.wantOK {
				t.Fatalf("Resolve() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if !gotOK {
				return
			}
			if gotType != tt.wantScopeType || gotName != tt.wantScopeName {
				t.Errorf("Resolve() = (%q, %q), want (%q, %q)", gotType, gotName, tt.wantScopeType, tt.wantScopeName)
			}
		})
	}
}
