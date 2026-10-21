package cel

import (
	"context"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// TestCompileActivityFilterProgram tests the CEL filter compilation for watch evaluation.
func TestCompileActivityFilterProgram(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		wantErr bool
	}{
		{
			name:    "valid simple equality filter",
			filter:  `spec.changeSource == "human"`,
			wantErr: false,
		},
		{
			name:    "valid resource kind filter",
			filter:  `spec.resource.kind == "Deployment"`,
			wantErr: false,
		},
		{
			name:    "valid actor name filter",
			filter:  `spec.actor.name == "admin@example.com"`,
			wantErr: false,
		},
		{
			name:    "valid string contains filter",
			filter:  `spec.actor.name.contains("admin")`,
			wantErr: false,
		},
		{
			name:    "valid combined AND filter",
			filter:  `spec.changeSource == "human" && spec.resource.kind == "HTTPProxy"`,
			wantErr: false,
		},
		{
			name:    "valid combined OR filter",
			filter:  `spec.resource.kind == "Deployment" || spec.resource.kind == "StatefulSet"`,
			wantErr: false,
		},
		{
			name:    "valid NOT filter",
			filter:  `!(spec.changeSource == "system")`,
			wantErr: false,
		},
		{
			name:    "valid metadata namespace filter",
			filter:  `metadata.namespace == "production"`,
			wantErr: false,
		},
		{
			name:    "valid source region filter",
			filter:  `spec.source.region == "us-east-1"`,
			wantErr: false,
		},
		{
			name:    "valid relatedUIDs filter",
			filter:  `"workload-uid" in spec.relatedUIDs`,
			wantErr: false,
		},
		{
			name:    "unknown source field",
			filter:  `spec.source.zone == "a"`,
			wantErr: true,
		},
		{
			name:    "empty filter",
			filter:  "",
			wantErr: true,
		},
		{
			name:    "invalid syntax",
			filter:  `spec.changeSource = "human"`,
			wantErr: true,
		},
		{
			name:    "undeclared field",
			filter:  `spec.unknownField == "value"`,
			wantErr: true,
		},
		{
			name:    "non-boolean return type",
			filter:  `spec.changeSource`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := CompileActivityFilterProgram(tt.filter)

			if (err != nil) != tt.wantErr {
				t.Errorf("CompileActivityFilterProgram() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && compiled == nil {
				t.Error("CompileActivityFilterProgram() returned nil for valid filter")
			}
		})
	}
}

// TestActivityToMap tests the Activity to map conversion.
func TestActivityToMap(t *testing.T) {
	activity := &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-activity",
			Namespace: "default",
		},
		Spec: v1alpha1.ActivitySpec{
			Summary:      "alice created deployment nginx",
			ChangeSource: "human",
			Actor: v1alpha1.ActivityActor{
				Name: "alice@example.com",
				Type: "user",
				UID:  "user-uid-123",
			},
			Resource: v1alpha1.ActivityResource{
				APIGroup:  "apps",
				Kind:      "Deployment",
				Name:      "nginx",
				Namespace: "production",
				UID:       "deployment-uid-456",
			},
			Related: []v1alpha1.ActivityResource{{UID: "replicaset-uid"}, {}},
			Links: []v1alpha1.ActivityLink{
				{Resource: v1alpha1.ActivityResource{UID: "replicaset-uid"}},
				{Resource: v1alpha1.ActivityResource{UID: "configmap-uid"}},
			},
			Origin: v1alpha1.ActivityOrigin{
				Type: "audit",
			},
			Source: &v1alpha1.ActivitySource{Region: "us-east-1"},
		},
	}

	m := ActivityToMap(activity)

	// Verify spec fields
	spec, ok := m["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("spec is not a map")
	}

	if spec["changeSource"] != "human" {
		t.Errorf("spec.changeSource = %v, want %v", spec["changeSource"], "human")
	}

	if spec["summary"] != "alice created deployment nginx" {
		t.Errorf("spec.summary = %v, want %v", spec["summary"], "alice created deployment nginx")
	}

	// Verify actor fields
	actor, ok := spec["actor"].(map[string]interface{})
	if !ok {
		t.Fatal("spec.actor is not a map")
	}

	if actor["name"] != "alice@example.com" {
		t.Errorf("spec.actor.name = %v, want %v", actor["name"], "alice@example.com")
	}

	if actor["type"] != "user" {
		t.Errorf("spec.actor.type = %v, want %v", actor["type"], "user")
	}

	// Verify resource fields
	resource, ok := spec["resource"].(map[string]interface{})
	if !ok {
		t.Fatal("spec.resource is not a map")
	}

	if resource["kind"] != "Deployment" {
		t.Errorf("spec.resource.kind = %v, want %v", resource["kind"], "Deployment")
	}

	if resource["apiGroup"] != "apps" {
		t.Errorf("spec.resource.apiGroup = %v, want %v", resource["apiGroup"], "apps")
	}

	// Verify relatedUIDs are distinct and skip empty UIDs
	wantRelated := []string{"deployment-uid-456", "replicaset-uid", "configmap-uid"}
	if !reflect.DeepEqual(spec["relatedUIDs"], wantRelated) {
		t.Errorf("spec.relatedUIDs = %v, want %v", spec["relatedUIDs"], wantRelated)
	}

	// Verify source fields
	source, ok := spec["source"].(map[string]interface{})
	if !ok {
		t.Fatal("spec.source is not a map")
	}

	if source["region"] != "us-east-1" {
		t.Errorf("spec.source.region = %v, want %v", source["region"], "us-east-1")
	}

	// Verify metadata fields
	metadata, ok := m["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("metadata is not a map")
	}

	if metadata["name"] != "test-activity" {
		t.Errorf("metadata.name = %v, want %v", metadata["name"], "test-activity")
	}

	if metadata["namespace"] != "default" {
		t.Errorf("metadata.namespace = %v, want %v", metadata["namespace"], "default")
	}
}

// TestEvaluateActivity tests the CEL filter evaluation against Activity objects.
func TestEvaluateActivity(t *testing.T) {
	humanDeploymentActivity := &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "human-deployment",
			Namespace: "production",
		},
		Spec: v1alpha1.ActivitySpec{
			Summary:      "alice created deployment nginx",
			ChangeSource: "human",
			Actor: v1alpha1.ActivityActor{
				Name: "alice@example.com",
				Type: "user",
				UID:  "user-uid-123",
			},
			Resource: v1alpha1.ActivityResource{
				APIGroup:  "apps",
				Kind:      "Deployment",
				Name:      "nginx",
				Namespace: "production",
				UID:       "deployment-uid-456",
			},
			Origin: v1alpha1.ActivityOrigin{
				Type: "audit",
			},
		},
	}

	systemPodActivity := &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "system-pod",
			Namespace: "kube-system",
		},
		Spec: v1alpha1.ActivitySpec{
			Summary:      "kube-controller-manager updated pod coredns",
			ChangeSource: "system",
			Actor: v1alpha1.ActivityActor{
				Name: "system:serviceaccount:kube-system:controller-manager",
				Type: "serviceaccount",
				UID:  "sa-uid-789",
			},
			Resource: v1alpha1.ActivityResource{
				APIGroup:  "",
				Kind:      "Pod",
				Name:      "coredns",
				Namespace: "kube-system",
				UID:       "pod-uid-999",
			},
			Origin: v1alpha1.ActivityOrigin{
				Type: "audit",
			},
		},
	}

	httpProxyActivity := &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "httpproxy-create",
			Namespace: "ingress",
		},
		Spec: v1alpha1.ActivitySpec{
			Summary:      "admin created HTTPProxy api-gateway",
			ChangeSource: "human",
			Actor: v1alpha1.ActivityActor{
				Name: "admin@example.com",
				Type: "user",
				UID:  "admin-uid-111",
			},
			Resource: v1alpha1.ActivityResource{
				APIGroup:  "projectcontour.io",
				Kind:      "HTTPProxy",
				Name:      "api-gateway",
				Namespace: "ingress",
				UID:       "httpproxy-uid-222",
			},
			Origin: v1alpha1.ActivityOrigin{
				Type: "audit",
			},
		},
	}

	instanceActivity := &v1alpha1.Activity{
		Spec: v1alpha1.ActivitySpec{
			Resource: v1alpha1.ActivityResource{Kind: "Instance", UID: "instance-uid"},
			Related:  []v1alpha1.ActivityResource{{Kind: "Workload", UID: "workload-uid"}},
			Links: []v1alpha1.ActivityLink{
				{Resource: v1alpha1.ActivityResource{Kind: "Network", UID: "network-uid"}},
			},
			Source: &v1alpha1.ActivitySource{PlaneType: "edge", Region: "us-east-1"},
		},
	}

	tests := []struct {
		name     string
		filter   string
		activity *v1alpha1.Activity
		want     bool
	}{
		{
			name:     "human changeSource - matches",
			filter:   `spec.changeSource == "human"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "human changeSource - does not match",
			filter:   `spec.changeSource == "human"`,
			activity: systemPodActivity,
			want:     false,
		},
		{
			name:     "system changeSource - matches",
			filter:   `spec.changeSource == "system"`,
			activity: systemPodActivity,
			want:     true,
		},
		{
			name:     "resource kind Deployment - matches",
			filter:   `spec.resource.kind == "Deployment"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "resource kind Deployment - does not match Pod",
			filter:   `spec.resource.kind == "Deployment"`,
			activity: systemPodActivity,
			want:     false,
		},
		{
			name:     "actor name contains admin - matches",
			filter:   `spec.actor.name.contains("admin")`,
			activity: httpProxyActivity,
			want:     true,
		},
		{
			name:     "actor name contains admin - does not match",
			filter:   `spec.actor.name.contains("admin")`,
			activity: humanDeploymentActivity,
			want:     false,
		},
		{
			name:     "combined filter - human AND Deployment",
			filter:   `spec.changeSource == "human" && spec.resource.kind == "Deployment"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "combined filter - human AND HTTPProxy",
			filter:   `spec.changeSource == "human" && spec.resource.kind == "HTTPProxy"`,
			activity: httpProxyActivity,
			want:     true,
		},
		{
			name:     "combined filter - human AND HTTPProxy on Deployment",
			filter:   `spec.changeSource == "human" && spec.resource.kind == "HTTPProxy"`,
			activity: humanDeploymentActivity,
			want:     false,
		},
		{
			name:     "OR filter - Deployment OR Pod",
			filter:   `spec.resource.kind == "Deployment" || spec.resource.kind == "Pod"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "OR filter - Deployment OR Pod - matches Pod",
			filter:   `spec.resource.kind == "Deployment" || spec.resource.kind == "Pod"`,
			activity: systemPodActivity,
			want:     true,
		},
		{
			name:     "OR filter - does not match HTTPProxy",
			filter:   `spec.resource.kind == "Deployment" || spec.resource.kind == "Pod"`,
			activity: httpProxyActivity,
			want:     false,
		},
		{
			name:     "NOT filter - not system",
			filter:   `!(spec.changeSource == "system")`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "NOT filter - not system - does not match system activity",
			filter:   `!(spec.changeSource == "system")`,
			activity: systemPodActivity,
			want:     false,
		},
		{
			name:     "metadata namespace filter",
			filter:   `metadata.namespace == "production"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "metadata namespace filter - does not match",
			filter:   `metadata.namespace == "production"`,
			activity: systemPodActivity,
			want:     false,
		},
		{
			name:     "apiGroup filter - apps",
			filter:   `spec.resource.apiGroup == "apps"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "apiGroup filter - empty string for core",
			filter:   `spec.resource.apiGroup == ""`,
			activity: systemPodActivity,
			want:     true,
		},
		{
			name:     "apiGroup filter - projectcontour.io",
			filter:   `spec.resource.apiGroup == "projectcontour.io"`,
			activity: httpProxyActivity,
			want:     true,
		},
		{
			name:     "actor type filter - user",
			filter:   `spec.actor.type == "user"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "actor type filter - serviceaccount",
			filter:   `spec.actor.type == "serviceaccount"`,
			activity: systemPodActivity,
			want:     true,
		},
		{
			name:     "origin type filter",
			filter:   `spec.origin.type == "audit"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "summary contains filter",
			filter:   `spec.summary.contains("created")`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "summary startsWith filter",
			filter:   `spec.summary.startsWith("alice")`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "resource name filter",
			filter:   `spec.resource.name == "nginx"`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "complex combined filter",
			filter:   `spec.changeSource == "human" && spec.resource.apiGroup == "apps" && spec.actor.name.contains("alice")`,
			activity: humanDeploymentActivity,
			want:     true,
		},
		{
			name:     "relatedUIDs - matches own resource",
			filter:   `"instance-uid" in spec.relatedUIDs`,
			activity: instanceActivity,
			want:     true,
		},
		{
			name:     "relatedUIDs - matches related resource",
			filter:   `"workload-uid" in spec.relatedUIDs`,
			activity: instanceActivity,
			want:     true,
		},
		{
			name:     "relatedUIDs - matches linked resource",
			filter:   `"network-uid" in spec.relatedUIDs`,
			activity: instanceActivity,
			want:     true,
		},
		{
			name:     "relatedUIDs - does not match",
			filter:   `"workload-uid" in spec.relatedUIDs`,
			activity: humanDeploymentActivity,
			want:     false,
		},
		{
			name:     "source region filter",
			filter:   `spec.source.region == "us-east-1"`,
			activity: instanceActivity,
			want:     true,
		},
		{
			name:     "source region filter - unset source does not match",
			filter:   `spec.source.region == "us-east-1"`,
			activity: humanDeploymentActivity,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := CompileActivityFilterProgram(tt.filter)
			if err != nil {
				t.Fatalf("CompileActivityFilterProgram() error = %v", err)
			}

			got, err := compiled.EvaluateActivity(tt.activity)
			if err != nil {
				t.Fatalf("EvaluateActivity() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("EvaluateActivity() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEvaluateActivity_NilFilter tests that nil filter returns true for all activities.
func TestEvaluateActivity_NilFilter(t *testing.T) {
	activity := &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-activity",
			Namespace: "default",
		},
		Spec: v1alpha1.ActivitySpec{
			ChangeSource: "human",
		},
	}

	var compiled *CompiledActivityFilter = nil
	got, err := compiled.EvaluateActivity(activity)
	if err != nil {
		t.Fatalf("EvaluateActivity() error = %v", err)
	}

	if !got {
		t.Errorf("EvaluateActivity() with nil filter = %v, want true", got)
	}
}

// TestConvertActivityToClickHouseSQL tests the SQL conversion for activity filters.
func TestConvertActivityToClickHouseSQL(t *testing.T) {
	tests := []struct {
		name           string
		filter         string
		wantSQLContain string
		wantArg        string
		wantErr        bool
	}{
		{
			name:           "changeSource equals human",
			filter:         `spec.changeSource == "human"`,
			wantSQLContain: "change_source = {arg",
			wantArg:        "human",
		},
		{
			name:           "changeSource equals system",
			filter:         `spec.changeSource == "system"`,
			wantSQLContain: "change_source = {arg",
			wantArg:        "system",
		},
		{
			name:           "source region",
			filter:         `spec.source.region == "us-east-1"`,
			wantSQLContain: "source_region = {arg",
			wantArg:        "us-east-1",
		},
		{
			name:           "uid in relatedUIDs uses has()",
			filter:         `"workload-uid" in spec.relatedUIDs`,
			wantSQLContain: "has(related_uids, {arg",
			wantArg:        "workload-uid",
		},
		{
			name:           "in with a list literal still uses IN",
			filter:         `spec.resource.kind in ["Workload"]`,
			wantSQLContain: "resource_kind IN [{arg",
			wantArg:        "Workload",
		},
		{
			name:    "relatedUIDs rejects equality",
			filter:  `spec.relatedUIDs == "workload-uid"`,
			wantErr: true,
		},
		{
			name:    "relatedUIDs rejects string methods",
			filter:  `spec.relatedUIDs.contains("workload-uid")`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := ConvertActivityToClickHouseSQL(context.Background(), tt.filter)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ConvertActivityToClickHouseSQL() = %q, want error", sql)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConvertActivityToClickHouseSQL() error = %v", err)
			}

			t.Logf("Generated SQL: %s", sql)
			t.Logf("Args: %v", args)

			if !contains(sql, tt.wantSQLContain) {
				t.Errorf("SQL = %q, want to contain %q", sql, tt.wantSQLContain)
			}

			// Check that the argument value is correct
			found := false
			for _, arg := range args {
				if arg == tt.wantArg {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("args = %v, want to contain %q", args, tt.wantArg)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
