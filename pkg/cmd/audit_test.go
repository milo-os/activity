package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authnv1 "k8s.io/api/authentication/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"go.miloapis.com/activity/pkg/cmd/common"
)

func TestAuditOptions_buildFilter(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		resource  string
		verb      string
		user      string
		filter    string
		want      string
	}{
		{
			name: "no filters",
			want: "",
		},
		{
			name:      "namespace only",
			namespace: "production",
			want:      "objectRef.namespace == 'production'",
		},
		{
			name:     "resource only",
			resource: "secrets",
			want:     "objectRef.resource == 'secrets'",
		},
		{
			name: "verb only",
			verb: "delete",
			want: "verb == 'delete'",
		},
		{
			name: "user only",
			user: "alice@example.com",
			want: "user.username == 'alice@example.com'",
		},
		{
			name:      "multiple shorthand filters",
			namespace: "production",
			verb:      "delete",
			resource:  "secrets",
			want:      "objectRef.namespace == 'production' && objectRef.resource == 'secrets' && verb == 'delete'",
		},
		{
			name:   "explicit filter only",
			filter: "responseStatus.code >= 400",
			want:   "responseStatus.code >= 400",
		},
		{
			name:      "shorthand and explicit filter",
			namespace: "production",
			filter:    "responseStatus.code >= 400",
			want:      "(objectRef.namespace == 'production') && (responseStatus.code >= 400)",
		},
		{
			name:      "all filters",
			namespace: "production",
			resource:  "secrets",
			verb:      "delete",
			user:      "alice@example.com",
			filter:    "responseStatus.code >= 400",
			want:      "(objectRef.namespace == 'production' && objectRef.resource == 'secrets' && verb == 'delete' && user.username == 'alice@example.com') && (responseStatus.code >= 400)",
		},
		{
			name:      "namespace with single quote - escaped",
			namespace: "prod'uction",
			want:      "objectRef.namespace == 'prod\\'uction'",
		},
		{
			name:      "injection attempt - OR operator",
			namespace: "prod' || true || '",
			want:      "objectRef.namespace == 'prod\\' || true || \\''",
		},
		{
			name:      "injection attempt - AND operator",
			namespace: "prod' && 'x' == 'x",
			want:      "objectRef.namespace == 'prod\\' && \\'x\\' == \\'x'",
		},
		{
			name: "user with single quote",
			user: "alice'@example.com",
			want: "user.username == 'alice\\'@example.com'",
		},
		{
			name:     "resource with quote",
			resource: "secret's",
			want:     "objectRef.resource == 'secret\\'s'",
		},
		{
			name: "verb with quote",
			verb: "dele'te",
			want: "verb == 'dele\\'te'",
		},
		{
			name:      "multiple fields with quotes",
			namespace: "prod'uction",
			user:      "alice'@example.com",
			want:      "objectRef.namespace == 'prod\\'uction' && user.username == 'alice\\'@example.com'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &AuditOptions{
				Namespace: tt.namespace,
				Resource:  tt.resource,
				Verb:      tt.verb,
				User:      tt.user,
				Filter:    tt.filter,
			}

			got := o.buildFilter()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAuditOptions_Validate(t *testing.T) {
	tests := []struct {
		name       string
		timeRange  common.TimeRangeFlags
		pagination common.PaginationFlags
		wantErr    bool
		errMsg     string
	}{
		{
			name: "valid options",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			wantErr: false,
		},
		{
			name: "invalid time range - empty start",
			timeRange: common.TimeRangeFlags{
				StartTime: "",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			wantErr: true,
			errMsg:  "--start-time is required",
		},
		{
			name: "invalid pagination - limit too low",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 0,
			},
			wantErr: true,
			errMsg:  "--limit must be between 1 and 1000",
		},
		{
			name: "invalid pagination - limit too high",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 1001,
			},
			wantErr: true,
			errMsg:  "--limit must be between 1 and 1000",
		},
		{
			name: "invalid pagination - all-pages with continue-after",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit:         25,
				AllPages:      true,
				ContinueAfter: "cursor123",
			},
			wantErr: true,
			errMsg:  "--all-pages and --continue-after are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &AuditOptions{
				TimeRange:  tt.timeRange,
				Pagination: tt.pagination,
			}

			err := o.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEventsToTable(t *testing.T) {
	now := metav1.NewMicroTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.UTC))

	tests := []struct {
		name        string
		events      []auditv1.Event
		wantRows    int
		wantColumns int
	}{
		{
			name: "single event",
			events: []auditv1.Event{
				{
					Verb:           "delete",
					StageTimestamp: now,
					User: authnv1.UserInfo{
						Username: "alice@example.com",
					},
					ObjectRef: &auditv1.ObjectReference{
						Namespace: "production",
						Resource:  "secrets",
						Name:      "db-password",
					},
					ResponseStatus: &metav1.Status{
						Code: 200,
					},
				},
			},
			wantRows:       1,
			wantColumns:    5,
		},
		{
			name: "multiple events",
			events: []auditv1.Event{
				{
					Verb:           "create",
					StageTimestamp: now,
					User: authnv1.UserInfo{
						Username: "bob@example.com",
					},
					ObjectRef: &auditv1.ObjectReference{
						Resource: "configmaps",
						Name:     "app-config",
					},
					ResponseStatus: &metav1.Status{
						Code: 201,
					},
				},
				{
					Verb:           "update",
					StageTimestamp: now,
					User: authnv1.UserInfo{
						Username: "alice@example.com",
					},
					ObjectRef: &auditv1.ObjectReference{
						Namespace: "default",
						Resource:  "deployments",
						Name:      "api",
					},
					ResponseStatus: &metav1.Status{
						Code: 200,
					},
				},
			},
			wantRows:       2,
			wantColumns:    5,
		},
		{
			name:           "empty events",
			events:         []auditv1.Event{},
			wantRows:       0,
			wantColumns:    5,
		},
		{
			name: "event without namespace",
			events: []auditv1.Event{
				{
					Verb:           "list",
					StageTimestamp: now,
					User: authnv1.UserInfo{
						Username: "system:admin",
					},
					ObjectRef: &auditv1.ObjectReference{
						Resource: "namespaces",
						Name:     "default",
					},
					ResponseStatus: &metav1.Status{
						Code: 200,
					},
				},
			},
			wantRows:       1,
			wantColumns:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := eventsToTable(tt.events)

			assert.NotNil(t, table)
			assert.Equal(t, "Table", table.Kind)
			assert.Len(t, table.ColumnDefinitions, tt.wantColumns)
			assert.Len(t, table.Rows, tt.wantRows)

			// Verify column names
			expectedColumns := []string{"Timestamp", "Verb", "User", "Resource", "Status"}
			for i, col := range table.ColumnDefinitions {
				assert.Equal(t, expectedColumns[i], col.Name)
			}
		})
	}
}

func TestEventsToRows(t *testing.T) {
	now := metav1.NewMicroTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.UTC))

	tests := []struct {
		name       string
		events     []auditv1.Event
		wantCells  [][]interface{}
	}{
		{
			name: "event with namespace",
			events: []auditv1.Event{
				{
					Verb:           "delete",
					StageTimestamp: now,
					User: authnv1.UserInfo{
						Username: "alice@example.com",
					},
					ObjectRef: &auditv1.ObjectReference{
						Namespace: "production",
						Resource:  "secrets",
						Name:      "db-password",
					},
					ResponseStatus: &metav1.Status{
						Code: 200,
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "delete", "alice@example.com", "production/secrets/db-password", "200"},
			},
		},
		{
			name: "event without namespace",
			events: []auditv1.Event{
				{
					Verb:           "list",
					StageTimestamp: now,
					User: authnv1.UserInfo{
						Username: "admin",
					},
					ObjectRef: &auditv1.ObjectReference{
						Resource: "nodes",
						Name:     "node-1",
					},
					ResponseStatus: &metav1.Status{
						Code: 200,
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "list", "admin", "nodes/node-1", "200"},
			},
		},
		{
			name: "non-UTC timestamp converted to UTC for output",
			events: []auditv1.Event{
				{
					Verb:           "delete",
					StageTimestamp: metav1.NewMicroTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.FixedZone("AEST", 10*60*60))),
					User: authnv1.UserInfo{
						Username: "alice@example.com",
					},
					ObjectRef: &auditv1.ObjectReference{
						Namespace: "production",
						Resource:  "secrets",
						Name:      "db-password",
					},
					ResponseStatus: &metav1.Status{
						Code: 200,
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T05:30:00Z", "delete", "alice@example.com", "production/secrets/db-password", "200"},
			},
		},
		{
			name: "event without response status",
			events: []auditv1.Event{
				{
					Verb:           "get",
					StageTimestamp: now,
					User: authnv1.UserInfo{
						Username: "reader",
					},
					ObjectRef: &auditv1.ObjectReference{
						Namespace: "default",
						Resource:  "pods",
						Name:      "my-pod",
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "get", "reader", "default/pods/my-pod", ""},
			},
		},
		{
			name: "event without object ref",
			events: []auditv1.Event{
				{
					Verb:           "create",
					StageTimestamp: now,
					User: authnv1.UserInfo{
						Username: "creator",
					},
					ResponseStatus: &metav1.Status{
						Code: 201,
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "create", "creator", "", "201"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := eventsToRows(tt.events)

			require.Len(t, rows, len(tt.wantCells))
			for i, row := range rows {
				assert.Equal(t, tt.wantCells[i], row.Cells)
			}
		})
	}
}

func TestNewAuditOptions(t *testing.T) {
	ioStreams := genericclioptions.IOStreams{}

	o := NewAuditOptions(nil, ioStreams)

	assert.NotNil(t, o)
	assert.Equal(t, "now-24h", o.TimeRange.StartTime)
	assert.Equal(t, "now", o.TimeRange.EndTime)
	assert.Equal(t, int32(25), o.Pagination.Limit)
	assert.False(t, o.Pagination.AllPages)
	assert.NotNil(t, o.PrintFlags)
}

func TestAuditOptions_Complete(t *testing.T) {
	o := &AuditOptions{}

	err := o.Complete(nil)

	require.NoError(t, err)
	// Complete should set default IO streams if they are nil
	// In this implementation, Complete checks but doesn't modify
	// This test verifies the method succeeds
}
