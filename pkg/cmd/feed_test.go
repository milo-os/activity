package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	activityv1alpha1 "go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	"go.miloapis.com/activity/pkg/cmd/common"
)

func TestFeedOptions_buildFilter(t *testing.T) {
	tests := []struct {
		name         string
		namespace    string
		actor        string
		kind         string
		apiGroup     string
		changeSource string
		resourceUID  string
		filter       string
		want         string
	}{
		{
			name: "no filters",
			want: "",
		},
		{
			name:      "namespace only",
			namespace: "production",
			want:      "spec.resource.namespace == 'production'",
		},
		{
			name:  "actor only",
			actor: "alice@example.com",
			want:  "spec.actor.name == 'alice@example.com'",
		},
		{
			name: "kind only",
			kind: "Deployment",
			want: "spec.resource.kind == 'Deployment'",
		},
		{
			name:     "api group only",
			apiGroup: "apps",
			want:     "spec.resource.apiGroup == 'apps'",
		},
		{
			name:         "change source only",
			changeSource: "human",
			want:         "spec.changeSource == 'human'",
		},
		{
			name:        "resource uid only",
			resourceUID: "uid-123",
			want:        "spec.resource.uid == 'uid-123'",
		},
		{
			name:      "multiple shorthand filters",
			namespace: "production",
			actor:     "alice@example.com",
			kind:      "Deployment",
			want:      "spec.resource.namespace == 'production' && spec.actor.name == 'alice@example.com' && spec.resource.kind == 'Deployment'",
		},
		{
			name:   "explicit filter only",
			filter: "spec.summary.contains('created')",
			want:   "spec.summary.contains('created')",
		},
		{
			name:   "shorthand and explicit filter",
			kind:   "Pod",
			filter: "spec.changeSource == 'human'",
			want:   "(spec.resource.kind == 'Pod') && (spec.changeSource == 'human')",
		},
		{
			name:         "all filters",
			namespace:    "production",
			actor:        "alice@example.com",
			kind:         "Deployment",
			apiGroup:     "apps",
			changeSource: "human",
			resourceUID:  "uid-123",
			filter:       "spec.summary.contains('updated')",
			want:         "(spec.resource.namespace == 'production' && spec.actor.name == 'alice@example.com' && spec.resource.kind == 'Deployment' && spec.resource.apiGroup == 'apps' && spec.changeSource == 'human' && spec.resource.uid == 'uid-123') && (spec.summary.contains('updated'))",
		},
		{
			name:      "namespace with single quote - escaped",
			namespace: "prod'uction",
			want:      "spec.resource.namespace == 'prod\\'uction'",
		},
		{
			name:  "actor with single quote - escaped",
			actor: "alice'@example.com",
			want:  "spec.actor.name == 'alice\\'@example.com'",
		},
		{
			name: "kind with injection attempt",
			kind: "Pod' || true || '",
			want: "spec.resource.kind == 'Pod\\' || true || \\''",
		},
		{
			name:     "api group with quote",
			apiGroup: "apps'v1",
			want:     "spec.resource.apiGroup == 'apps\\'v1'",
		},
		{
			name:         "change source with injection",
			changeSource: "human' && 'x' == 'x",
			want:         "spec.changeSource == 'human\\' && \\'x\\' == \\'x'",
		},
		{
			name:        "resource uid with quote",
			resourceUID: "uid'123",
			want:        "spec.resource.uid == 'uid\\'123'",
		},
		{
			name:      "multiple fields with quotes",
			namespace: "prod'uction",
			actor:     "alice'@example.com",
			kind:      "Deploy'ment",
			want:      "spec.resource.namespace == 'prod\\'uction' && spec.actor.name == 'alice\\'@example.com' && spec.resource.kind == 'Deploy\\'ment'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &FeedOptions{
				Namespace:    tt.namespace,
				Actor:        tt.actor,
				Kind:         tt.kind,
				APIGroup:     tt.apiGroup,
				ChangeSource: tt.changeSource,
				ResourceUID:  tt.resourceUID,
				Filter:       tt.filter,
			}

			got := o.buildFilter()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFeedOptions_Validate(t *testing.T) {
	tests := []struct {
		name       string
		watch      bool
		timeRange  common.TimeRangeFlags
		pagination common.PaginationFlags
		wantErr    bool
		errMsg     string
	}{
		{
			name:  "valid options",
			watch: false,
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
			name:  "watch mode skips time range validation",
			watch: true,
			timeRange: common.TimeRangeFlags{
				StartTime: "",
				EndTime:   "",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			wantErr: false,
		},
		{
			name:  "invalid time range in non-watch mode",
			watch: false,
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
			name:  "invalid pagination",
			watch: false,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &FeedOptions{
				Watch:      tt.watch,
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

func TestFeedOptions_matchesClientSideFilters(t *testing.T) {
	tests := []struct {
		name     string
		options  FeedOptions
		activity *activityv1alpha1.Activity
		want     bool
	}{
		{
			name:    "no filters - always matches",
			options: FeedOptions{},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Summary: "test activity",
				},
			},
			want: true,
		},
		{
			name: "api group filter matches",
			options: FeedOptions{
				APIGroup: "apps",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Resource: activityv1alpha1.ActivityResource{
						APIGroup: "apps",
					},
				},
			},
			want: true,
		},
		{
			name: "api group filter does not match",
			options: FeedOptions{
				APIGroup: "networking",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Resource: activityv1alpha1.ActivityResource{
						APIGroup: "apps",
					},
				},
			},
			want: false,
		},
		{
			name: "resource uid filter matches",
			options: FeedOptions{
				ResourceUID: "uid-123",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Resource: activityv1alpha1.ActivityResource{
						UID: "uid-123",
					},
				},
			},
			want: true,
		},
		{
			name: "resource uid filter does not match",
			options: FeedOptions{
				ResourceUID: "uid-456",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Resource: activityv1alpha1.ActivityResource{
						UID: "uid-123",
					},
				},
			},
			want: false,
		},
		{
			name: "search filter matches (case insensitive)",
			options: FeedOptions{
				Search: "created deployment",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Summary: "Alice created Deployment my-app",
				},
			},
			want: true,
		},
		{
			name: "search filter matches with different case",
			options: FeedOptions{
				Search: "CREATED",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Summary: "Alice created Deployment my-app",
				},
			},
			want: true,
		},
		{
			name: "search filter does not match",
			options: FeedOptions{
				Search: "deleted",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Summary: "Alice created Deployment my-app",
				},
			},
			want: false,
		},
		{
			name: "multiple filters all match",
			options: FeedOptions{
				APIGroup:    "apps",
				ResourceUID: "uid-123",
				Search:      "deployment",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Summary: "Updated Deployment",
					Resource: activityv1alpha1.ActivityResource{
						APIGroup: "apps",
						UID:      "uid-123",
					},
				},
			},
			want: true,
		},
		{
			name: "multiple filters one does not match",
			options: FeedOptions{
				APIGroup:    "apps",
				ResourceUID: "uid-456",
				Search:      "deployment",
			},
			activity: &activityv1alpha1.Activity{
				Spec: activityv1alpha1.ActivitySpec{
					Summary: "Updated Deployment",
					Resource: activityv1alpha1.ActivityResource{
						APIGroup: "apps",
						UID:      "uid-123",
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.options.matchesClientSideFilters(tt.activity)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestActivitiesToTable(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.UTC))

	tests := []struct {
		name        string
		activities  []activityv1alpha1.Activity
		wantRows    int
		wantColumns int
	}{
		{
			name: "single activity",
			activities: []activityv1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: now,
					},
					Spec: activityv1alpha1.ActivitySpec{
						Actor: activityv1alpha1.ActivityActor{
							Name: "alice@example.com",
						},
						ChangeSource: "human",
						Summary:      "created HTTPProxy api-gateway",
					},
				},
			},
			wantRows:       1,
			wantColumns:    4,
		},
		{
			name: "multiple activities",
			activities: []activityv1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: now,
					},
					Spec: activityv1alpha1.ActivitySpec{
						Actor: activityv1alpha1.ActivityActor{
							Name: "alice@example.com",
						},
						ChangeSource: "human",
						Summary:      "created Deployment my-app",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: now,
					},
					Spec: activityv1alpha1.ActivitySpec{
						Actor: activityv1alpha1.ActivityActor{
							Name: "controller:deployment",
						},
						ChangeSource: "system",
						Summary:      "updated ReplicaSet my-app-xyz",
					},
				},
			},
			wantRows:       2,
			wantColumns:    4,
		},
		{
			name:           "empty activities",
			activities:     []activityv1alpha1.Activity{},
			wantRows:       0,
			wantColumns:    4,
		},
		{
			name: "activity with long summary",
			activities: []activityv1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: now,
					},
					Spec: activityv1alpha1.ActivitySpec{
						Actor: activityv1alpha1.ActivityActor{
							Name: "bob@example.com",
						},
						ChangeSource: "human",
						Summary:      "This is a very long activity summary that exceeds the 80 character limit and should be truncated when displayed in the table",
					},
				},
			},
			wantRows:       1,
			wantColumns:    4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := activitiesToTable(tt.activities)

			assert.NotNil(t, table)
			assert.Equal(t, "Table", table.Kind)
			assert.Len(t, table.ColumnDefinitions, tt.wantColumns)
			assert.Len(t, table.Rows, tt.wantRows)

			// Verify column names
			expectedColumns := []string{"Timestamp", "Actor", "Source", "Summary"}
			for i, col := range table.ColumnDefinitions {
				assert.Equal(t, expectedColumns[i], col.Name)
			}
		})
	}
}

func TestActivitiesToRows(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.UTC))

	tests := []struct {
		name       string
		activities []activityv1alpha1.Activity
		wantCells  [][]interface{}
	}{
		{
			name: "normal activity",
			activities: []activityv1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: now,
					},
					Spec: activityv1alpha1.ActivitySpec{
						Actor: activityv1alpha1.ActivityActor{
							Name: "alice@example.com",
						},
						ChangeSource: "human",
						Summary:      "created HTTPProxy api-gateway",
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "alice@example.com", "human", "created HTTPProxy api-gateway"},
			},
		},
		{
			name: "system activity",
			activities: []activityv1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: now,
					},
					Spec: activityv1alpha1.ActivitySpec{
						Actor: activityv1alpha1.ActivityActor{
							Name: "controller:deployment",
						},
						ChangeSource: "system",
						Summary:      "scaled ReplicaSet",
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "controller:deployment", "system", "scaled ReplicaSet"},
			},
		},
		{
			name: "non-UTC timestamp converted to UTC for output",
			activities: []activityv1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.NewTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.FixedZone("AEST", 10*60*60))),
					},
					Spec: activityv1alpha1.ActivitySpec{
						Actor: activityv1alpha1.ActivityActor{
							Name: "admin",
						},
						ChangeSource: "human",
						Summary:      "created namespace prod",
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T05:30:00Z", "admin", "human", "created namespace prod"},
			},
		},
		{
			name: "long summary truncated",
			activities: []activityv1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: now,
					},
					Spec: activityv1alpha1.ActivitySpec{
						Actor: activityv1alpha1.ActivityActor{
							Name: "admin",
						},
						ChangeSource: "human",
						Summary:      "This is a very long activity summary that exceeds the 80 character limit and should be truncated",
					},
				},
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "admin", "human", "This is a very long activity summary that exceeds the 80 character limit and ..."},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := activitiesToRows(tt.activities)

			require.Len(t, rows, len(tt.wantCells))
			for i, row := range rows {
				assert.Equal(t, tt.wantCells[i], row.Cells)
			}
		})
	}
}

func TestNewFeedOptions(t *testing.T) {
	ioStreams := genericclioptions.IOStreams{}

	o := NewFeedOptions(nil, ioStreams)

	assert.NotNil(t, o)
	assert.Equal(t, "now-24h", o.TimeRange.StartTime)
	assert.Equal(t, "now", o.TimeRange.EndTime)
	assert.Equal(t, int32(25), o.Pagination.Limit)
	assert.False(t, o.Pagination.AllPages)
	assert.False(t, o.Watch)
	assert.NotNil(t, o.PrintFlags)
}

func TestFeedOptions_Complete(t *testing.T) {
	o := &FeedOptions{}

	err := o.Complete(nil)

	require.NoError(t, err)
}
