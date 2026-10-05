package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestGetEventFieldValue_RelatedField(t *testing.T) {
	eventWithRelated := &corev1.Event{
		InvolvedObject: corev1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Pod",
			Namespace:  "default",
			Name:       "my-pod",
			UID:        types.UID("pod-uid-123"),
		},
		Related: &corev1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Node",
			Namespace:  "",
			Name:       "worker-node-1",
		},
	}

	tests := []struct {
		name   string
		event  *corev1.Event
		column string
		want   string
	}{
		// Event WITH a related object — each column returns the correct value.
		{
			name:   "related_api_version with Related set",
			event:  eventWithRelated,
			column: "related_api_version",
			want:   "v1",
		},
		{
			name:   "related_kind with Related set",
			event:  eventWithRelated,
			column: "related_kind",
			want:   "Node",
		},
		{
			name:   "related_namespace with Related set (empty namespace for cluster-scoped)",
			event:  eventWithRelated,
			column: "related_namespace",
			want:   "",
		},
		{
			name:   "related_name with Related set",
			event:  eventWithRelated,
			column: "related_name",
			want:   "worker-node-1",
		},

		// Event WITHOUT a related object — nil safety: must return "" without panicking.
		{
			name:   "related_api_version when Related is nil returns empty string",
			event:  &corev1.Event{},
			column: "related_api_version",
			want:   "",
		},
		{
			name:   "related_kind when Related is nil returns empty string",
			event:  &corev1.Event{},
			column: "related_kind",
			want:   "",
		},
		{
			name:   "related_namespace when Related is nil returns empty string",
			event:  &corev1.Event{},
			column: "related_namespace",
			want:   "",
		},
		{
			name:   "related_name when Related is nil returns empty string",
			event:  &corev1.Event{},
			column: "related_name",
			want:   "",
		},

		// Existing regarding fields still work correctly.
		{
			name:   "regarding_kind returns InvolvedObject.Kind",
			event:  eventWithRelated,
			column: "regarding_kind",
			want:   "Pod",
		},
		{
			name:   "regarding_namespace returns InvolvedObject.Namespace",
			event:  eventWithRelated,
			column: "regarding_namespace",
			want:   "default",
		},
		{
			name:   "regarding_name returns InvolvedObject.Name",
			event:  eventWithRelated,
			column: "regarding_name",
			want:   "my-pod",
		},
		{
			name:   "regarding_api_version returns InvolvedObject.APIVersion",
			event:  eventWithRelated,
			column: "regarding_api_version",
			want:   "v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetEventFieldValue(tt.event, tt.column)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEventFieldValue_RelatedNilDoesNotPanic(t *testing.T) {
	// Explicit panic-guard test: calling all related_* columns on a nil-Related
	// event must not panic. This is the most critical nil-safety check.
	event := &corev1.Event{} // Related is nil

	relatedColumns := []string{
		"related_api_version",
		"related_kind",
		"related_namespace",
		"related_name",
	}

	for _, col := range relatedColumns {
		t.Run(col, func(t *testing.T) {
			assert.NotPanics(t, func() {
				got := GetEventFieldValue(event, col)
				assert.Equal(t, "", got, "expected empty string for %s when Related is nil", col)
			})
		})
	}
}

func TestGetEventFieldValue_SourceFields(t *testing.T) {
	eventWithSource := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"activity.miloapis.com/source-plane-type": "edge",
				"activity.miloapis.com/source-cluster":    "cluster-dfw-1",
				"activity.miloapis.com/source-region":     "us-south",
				"activity.miloapis.com/source-city":       "dfw",
			},
		},
	}

	tests := []struct {
		name   string
		event  *corev1.Event
		column string
		want   string
	}{
		{name: "source_plane_type with annotation set", event: eventWithSource, column: "source_plane_type", want: "edge"},
		{name: "source_cluster with annotation set", event: eventWithSource, column: "source_cluster", want: "cluster-dfw-1"},
		{name: "source_region with annotation set", event: eventWithSource, column: "source_region", want: "us-south"},
		{name: "source_city with annotation set", event: eventWithSource, column: "source_city", want: "dfw"},
		// No source-* annotations present (the state of every event today, since
		// nothing emits them yet) - must return empty string, not panic.
		{name: "source_plane_type with no annotations", event: &corev1.Event{}, column: "source_plane_type", want: ""},
		{name: "source_cluster with no annotations", event: &corev1.Event{}, column: "source_cluster", want: ""},
		{name: "source_region with no annotations", event: &corev1.Event{}, column: "source_region", want: ""},
		{name: "source_city with no annotations", event: &corev1.Event{}, column: "source_city", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetEventFieldValue(tt.event, tt.column)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestActivityFacetColumnMapping_SourceFields(t *testing.T) {
	tests := []struct {
		field          string
		expectedColumn string
	}{
		{"spec.source.planeType", "source_plane_type"},
		{"spec.source.cluster", "source_cluster"},
		{"spec.source.region", "source_region"},
		{"spec.source.city", "source_city"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got, err := GetActivityFacetColumn(tt.field)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedColumn, got)
		})
	}
}

func TestEventFacetColumnMapping_SourceFields(t *testing.T) {
	tests := []struct {
		field          string
		expectedColumn string
	}{
		{"sourcePlaneType", "source_plane_type"},
		{"sourceCluster", "source_cluster"},
		{"sourceRegion", "source_region"},
		{"sourceCity", "source_city"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got, err := GetEventFacetColumn(tt.field)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedColumn, got)
		})
	}
}

// TestFacetFieldColumnMappingsAreConsistent re-runs the same consistency check
// performed by fields.go's init() (which panics at apiserver startup on a
// mismatch) as an ordinary test assertion, so a future regression here fails
// CI with a clear message instead of only surfacing as a boot-time panic.
func TestFacetFieldColumnMappingsAreConsistent(t *testing.T) {
	for field := range AuditLogFacetFields {
		_, ok := auditLogFacetColumnMapping[field]
		assert.Truef(t, ok, "missing ClickHouse column mapping for audit log facet field %q", field)
	}
	for field := range auditLogFacetColumnMapping {
		_, ok := AuditLogFacetFields[field]
		assert.Truef(t, ok, "audit log facet column mapping %q has no field definition", field)
	}

	for field := range ActivityFacetFields {
		_, ok := activityFacetColumnMapping[field]
		assert.Truef(t, ok, "missing ClickHouse column mapping for activity facet field %q", field)
	}
	for field := range activityFacetColumnMapping {
		_, ok := ActivityFacetFields[field]
		assert.Truef(t, ok, "activity facet column mapping %q has no field definition", field)
	}

	for field := range EventFacetFields {
		_, ok := eventFacetColumnMapping[field]
		assert.Truef(t, ok, "missing ClickHouse column mapping for event facet field %q", field)
	}
	for field := range eventFacetColumnMapping {
		_, ok := EventFacetFields[field]
		assert.Truef(t, ok, "event facet column mapping %q has no field definition", field)
	}
}

func TestEventFacetColumnMapping(t *testing.T) {
	tests := []struct {
		name           string
		field          string
		expectedColumn string
		wantErr        bool
	}{
		{
			name:           "related.kind maps to related_kind",
			field:          "related.kind",
			expectedColumn: "related_kind",
			wantErr:        false,
		},
		{
			name:           "related.namespace maps to related_namespace",
			field:          "related.namespace",
			expectedColumn: "related_namespace",
			wantErr:        false,
		},
		{
			name:    "related.unsupported returns error",
			field:   "related.unsupported",
			wantErr: true,
		},
		// Verify existing regarding mappings are not broken.
		{
			name:           "regarding.kind maps to regarding_kind",
			field:          "regarding.kind",
			expectedColumn: "regarding_kind",
			wantErr:        false,
		},
		{
			name:           "regarding.namespace maps to regarding_namespace",
			field:          "regarding.namespace",
			expectedColumn: "regarding_namespace",
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetEventFacetColumn(tt.field)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedColumn, got)
		})
	}
}
