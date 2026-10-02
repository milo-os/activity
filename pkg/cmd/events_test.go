package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	eventsv1 "k8s.io/api/events/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	activityv1alpha1 "go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	"go.miloapis.com/activity/pkg/cmd/common"
)

func TestEventsOptions_buildFieldSelector(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		reason        string
		regardingKind string
		regardingName string
		fieldSelector string
		want          string
	}{
		{
			name: "no selectors",
			want: "",
		},
		{
			name:      "type only",
			eventType: "Warning",
			want:      "type=Warning",
		},
		{
			name:   "reason only",
			reason: "FailedMount",
			want:   "reason=FailedMount",
		},
		{
			name:          "regarding kind only",
			regardingKind: "Pod",
			want:          "regarding.kind=Pod",
		},
		{
			name:          "regarding name only",
			regardingName: "my-pod",
			want:          "regarding.name=my-pod",
		},
		{
			name:      "multiple shorthand selectors",
			eventType: "Warning",
			reason:    "FailedMount",
			want:      "type=Warning,reason=FailedMount",
		},
		{
			name:          "all shorthand selectors",
			eventType:     "Warning",
			reason:        "BackOff",
			regardingKind: "Pod",
			regardingName: "crashing-pod",
			want:          "type=Warning,reason=BackOff,regarding.kind=Pod,regarding.name=crashing-pod",
		},
		{
			name:          "explicit selector only",
			fieldSelector: "metadata.namespace=production",
			want:          "metadata.namespace=production",
		},
		{
			name:          "shorthand and explicit selector",
			eventType:     "Warning",
			fieldSelector: "metadata.namespace=production",
			want:          "type=Warning,metadata.namespace=production",
		},
		{
			name:          "all selectors",
			eventType:     "Warning",
			reason:        "FailedMount",
			regardingKind: "Pod",
			regardingName: "my-pod",
			fieldSelector: "metadata.namespace=default",
			want:          "type=Warning,reason=FailedMount,regarding.kind=Pod,regarding.name=my-pod,metadata.namespace=default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &EventsOptions{
				Type:          tt.eventType,
				Reason:        tt.reason,
				RegardingKind: tt.regardingKind,
				RegardingName: tt.regardingName,
				FieldSelector: tt.fieldSelector,
			}

			got := o.buildFieldSelector()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEventsOptions_Validate(t *testing.T) {
	tests := []struct {
		name          string
		timeRange     common.TimeRangeFlags
		pagination    common.PaginationFlags
		eventType     string
		reason        string
		regardingKind string
		regardingName string
		wantErr       bool
		errMsg        string
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
			name: "invalid time range",
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
			name: "invalid pagination",
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
			name: "valid event type - Normal",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			eventType: "Normal",
			wantErr:   false,
		},
		{
			name: "valid event type - Warning",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			eventType: "Warning",
			wantErr:   false,
		},
		{
			name: "invalid event type - lowercase",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			eventType: "warning",
			wantErr:   true,
			errMsg:    "event type must be 'Normal' or 'Warning'",
		},
		{
			name: "invalid event type - injection attempt",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			eventType: "Warning' || 'x' == 'x",
			wantErr:   true,
			errMsg:    "event type must be 'Normal' or 'Warning'",
		},
		{
			name: "invalid reason - contains equals",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			reason:  "type=Warning",
			wantErr: true,
			errMsg:  "invalid --reason value",
		},
		{
			name: "invalid regarding-kind - contains comma",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			regardingKind: "Pod,Deployment",
			wantErr:       true,
			errMsg:        "invalid --regarding-kind value",
		},
		{
			name: "invalid regarding-name - field selector injection",
			timeRange: common.TimeRangeFlags{
				StartTime: "now-24h",
				EndTime:   "now",
			},
			pagination: common.PaginationFlags{
				Limit: 25,
			},
			regardingName: "pod-name,type=Warning",
			wantErr:       true,
			errMsg:        "invalid --regarding-name value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &EventsOptions{
				TimeRange:     tt.timeRange,
				Pagination:    tt.pagination,
				Type:          tt.eventType,
				Reason:        tt.reason,
				RegardingKind: tt.regardingKind,
				RegardingName: tt.regardingName,
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

func makeEventRecord(eventTime metav1.MicroTime, eventType, reason string, regarding corev1.ObjectReference, note string) activityv1alpha1.EventRecord {
	return activityv1alpha1.EventRecord{
		Event: eventsv1.Event{
			EventTime: eventTime,
			Type:      eventType,
			Reason:    reason,
			Regarding: regarding,
			Note:      note,
		},
	}
}

func TestKubeEventsToTable(t *testing.T) {
	now := metav1.NewMicroTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.UTC))

	tests := []struct {
		name        string
		events      []activityv1alpha1.EventRecord
		wantRows    int
		wantColumns int
	}{
		{
			name: "single event",
			events: []activityv1alpha1.EventRecord{
				makeEventRecord(now, "Warning", "FailedMount", corev1.ObjectReference{
					Kind:      "Pod",
					Name:      "my-pod",
					Namespace: "default",
				}, "Unable to mount volume"),
			},
			wantRows:       1,
			wantColumns:    5,
		},
		{
			name: "multiple events",
			events: []activityv1alpha1.EventRecord{
				makeEventRecord(now, "Normal", "Pulled", corev1.ObjectReference{
					Kind: "Pod",
					Name: "app-pod",
				}, "Successfully pulled image"),
				makeEventRecord(now, "Warning", "BackOff", corev1.ObjectReference{
					Kind:      "Pod",
					Name:      "crash-pod",
					Namespace: "production",
				}, "Back-off restarting failed container"),
			},
			wantRows:       2,
			wantColumns:    5,
		},
		{
			name:           "empty events",
			events:         []activityv1alpha1.EventRecord{},
			wantRows:       0,
			wantColumns:    5,
		},
		{
			name: "event with long message",
			events: []activityv1alpha1.EventRecord{
				makeEventRecord(now, "Warning", "FailedScheduling", corev1.ObjectReference{
					Kind: "Pod",
					Name: "pending-pod",
				}, "This is a very long message that exceeds the 80 character limit and should be truncated when displayed in the table format to ensure readability"),
			},
			wantRows:       1,
			wantColumns:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := kubeEventsToTable(tt.events)

			assert.NotNil(t, table)
			assert.Equal(t, "Table", table.Kind)
			assert.Len(t, table.ColumnDefinitions, tt.wantColumns)
			assert.Len(t, table.Rows, tt.wantRows)

			// Verify column names
			expectedColumns := []string{"Last Seen", "Type", "Reason", "Object", "Message"}
			for i, col := range table.ColumnDefinitions {
				assert.Equal(t, expectedColumns[i], col.Name)
			}
		})
	}
}

func TestKubeEventsToRows(t *testing.T) {
	now := metav1.NewMicroTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.UTC))

	tests := []struct {
		name      string
		events    []activityv1alpha1.EventRecord
		wantCells [][]interface{}
	}{
		{
			name: "event with namespace",
			events: []activityv1alpha1.EventRecord{
				makeEventRecord(now, "Warning", "FailedMount", corev1.ObjectReference{
					Kind:      "Pod",
					Name:      "my-pod",
					Namespace: "production",
				}, "Unable to mount volume"),
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "Warning", "FailedMount", "production/Pod/my-pod", "Unable to mount volume"},
			},
		},
		{
			name: "event without namespace",
			events: []activityv1alpha1.EventRecord{
				makeEventRecord(now, "Normal", "NodeReady", corev1.ObjectReference{
					Kind: "Node",
					Name: "node-1",
				}, "Node is ready"),
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "Normal", "NodeReady", "Node/node-1", "Node is ready"},
			},
		},
		{
			name: "non-UTC timestamp converted to UTC for output",
			events: []activityv1alpha1.EventRecord{
				makeEventRecord(metav1.NewMicroTime(time.Date(2026, 2, 21, 15, 30, 0, 0, time.FixedZone("EST", -5*60*60))), "Warning", "FailedMount", corev1.ObjectReference{
					Kind:      "Pod",
					Name:      "my-pod",
					Namespace: "production",
				}, "Unable to mount volume"),
			},
			wantCells: [][]interface{}{
				{"2026-02-21T20:30:00Z", "Warning", "FailedMount", "production/Pod/my-pod", "Unable to mount volume"},
			},
		},
		{
			name: "event with long message truncated",
			events: []activityv1alpha1.EventRecord{
				makeEventRecord(now, "Warning", "LongMessage", corev1.ObjectReference{
					Kind: "Pod",
					Name: "test-pod",
				}, "This is a very long message that exceeds the 80 character limit and should be truncated"),
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "Warning", "LongMessage", "Pod/test-pod", "This is a very long message that exceeds the 80 character limit and should be..."},
			},
		},
		{
			name: "event with EventTime",
			events: []activityv1alpha1.EventRecord{
				makeEventRecord(now, "Normal", "Created", corev1.ObjectReference{
					Kind: "Pod",
					Name: "new-pod",
				}, "Pod created"),
			},
			wantCells: [][]interface{}{
				{"2026-02-21T15:30:00Z", "Normal", "Created", "Pod/new-pod", "Pod created"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := kubeEventsToRows(tt.events)

			require.Len(t, rows, len(tt.wantCells))
			for i, row := range rows {
				assert.Equal(t, tt.wantCells[i], row.Cells)
			}
		})
	}
}

func TestNewEventsOptions(t *testing.T) {
	ioStreams := genericclioptions.IOStreams{}

	o := NewEventsOptions(nil, ioStreams)

	assert.NotNil(t, o)
	assert.Equal(t, "now-24h", o.TimeRange.StartTime)
	assert.Equal(t, "now", o.TimeRange.EndTime)
	assert.Equal(t, int32(25), o.Pagination.Limit)
	assert.False(t, o.Pagination.AllPages)
	assert.NotNil(t, o.PrintFlags)
}

func TestEventsOptions_Complete(t *testing.T) {
	o := &EventsOptions{}

	err := o.Complete(nil)

	require.NoError(t, err)
}
