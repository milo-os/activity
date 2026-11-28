package preview

import (
	"context"
	"encoding/json"
	"testing"

	authnv1 "k8s.io/api/authentication/v1"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"

	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// Mock storage backends for testing
type mockAuditLogBackend struct {
	result *storage.QueryResult
	err    error
	scopes []storage.ScopeContext
}

func (m *mockAuditLogBackend) QueryAuditLogs(ctx context.Context, spec v1alpha1.AuditLogQuerySpec, scope storage.ScopeContext) (*storage.QueryResult, error) {
	m.scopes = append(m.scopes, scope)
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

type mockEventBackend struct {
	result *storage.EventQueryResult
	err    error
	scopes []storage.ScopeContext
}

func (m *mockEventBackend) QueryEvents(ctx context.Context, spec v1alpha1.EventQuerySpec, scope storage.ScopeContext) (*storage.EventQueryResult, error) {
	m.scopes = append(m.scopes, scope)
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// Helper to create eventsv1.Event for tests
func createEventV1(reason, message, controller string) eventsv1.Event {
	return eventsv1.Event{
		Reason:              reason,
		Note:                message,
		ReportingController: controller,
	}
}

func TestPolicyPreviewStorage_Create_SingleAuditMatch(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-e5841d",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
				},
			},
			Inputs: []v1alpha1.PolicyPreviewInput{
				{
					Type: "audit",
					Audit: &auditv1.Event{
						Verb: "create",
						User: authnv1.UserInfo{
							Username: "alice@example.com",
							UID:      "user-alice-123",
						},
						ObjectRef: &auditv1.ObjectReference{
							APIGroup: "networking.datumapis.com",
							Resource: "httpproxies",
							Name:     "my-proxy",
						},
					},
				},
			},
		},
	}

	result, err := storage.Create(context.Background(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultPreview, ok := result.(*v1alpha1.PolicyPreview)
	if !ok {
		t.Fatalf("Expected *v1alpha1.PolicyPreview, got %T", result)
	}

	if resultPreview.Status.Error != "" {
		t.Errorf("Unexpected error: %s", resultPreview.Status.Error)
	}

	// Check results
	if len(resultPreview.Status.Results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(resultPreview.Status.Results))
	}

	if !resultPreview.Status.Results[0].Matched {
		t.Error("Expected rule to match, but it didn't")
	}

	if resultPreview.Status.Results[0].MatchedRuleIndex != 0 {
		t.Errorf("Expected MatchedRuleIndex=0, got %d", resultPreview.Status.Results[0].MatchedRuleIndex)
	}

	if resultPreview.Status.Results[0].MatchedRuleType != "audit" {
		t.Errorf("Expected MatchedRuleType='audit', got %s", resultPreview.Status.Results[0].MatchedRuleType)
	}

	// Check activities
	if len(resultPreview.Status.Activities) != 1 {
		t.Fatalf("Expected 1 activity, got %d", len(resultPreview.Status.Activities))
	}

	activity := resultPreview.Status.Activities[0]

	expectedSummary := "alice@example.com created HTTPProxy"
	if activity.Spec.Summary != expectedSummary {
		t.Errorf("Expected Summary=%q, got %q", expectedSummary, activity.Spec.Summary)
	}

	if activity.Spec.Actor.Name != "alice@example.com" {
		t.Errorf("Expected Actor.Name='alice@example.com', got %q", activity.Spec.Actor.Name)
	}

	if activity.Spec.Actor.UID != "user-alice-123" {
		t.Errorf("Expected Actor.UID='user-alice-123', got %q", activity.Spec.Actor.UID)
	}

	if activity.Spec.ChangeSource != "human" {
		t.Errorf("Expected ChangeSource='human', got %q", activity.Spec.ChangeSource)
	}

	if activity.Spec.Origin.Type != "audit" {
		t.Errorf("Expected Origin.Type='audit', got %q", activity.Spec.Origin.Type)
	}
}

func TestPolicyPreviewStorage_Create_MultipleInputs(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-22b1cd",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
					{
						Name:    "rule-6754e0",
						Match:   `audit.verb == "delete"`,
						Summary: `{{ actor }} deleted HTTPProxy`,
					},
				},
			},
			Inputs: []v1alpha1.PolicyPreviewInput{
				{
					Type: "audit",
					Audit: &auditv1.Event{
						Verb: "create",
						User: authnv1.UserInfo{
							Username: "alice@example.com",
						},
						ObjectRef: &auditv1.ObjectReference{
							APIGroup: "networking.datumapis.com",
							Resource: "httpproxies",
							Name:     "proxy-1",
						},
					},
				},
				{
					Type: "audit",
					Audit: &auditv1.Event{
						Verb: "delete",
						User: authnv1.UserInfo{
							Username: "bob@example.com",
						},
						ObjectRef: &auditv1.ObjectReference{
							APIGroup: "networking.datumapis.com",
							Resource: "httpproxies",
							Name:     "proxy-2",
						},
					},
				},
				{
					Type: "audit",
					Audit: &auditv1.Event{
						Verb: "update", // No matching rule
						User: authnv1.UserInfo{
							Username: "charlie@example.com",
						},
					},
				},
			},
		},
	}

	result, err := storage.Create(context.Background(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultPreview := result.(*v1alpha1.PolicyPreview)

	// Check results
	if len(resultPreview.Status.Results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(resultPreview.Status.Results))
	}

	// First input should match rule 0
	if !resultPreview.Status.Results[0].Matched {
		t.Error("Expected first input to match")
	}
	if resultPreview.Status.Results[0].MatchedRuleIndex != 0 {
		t.Errorf("Expected first input MatchedRuleIndex=0, got %d", resultPreview.Status.Results[0].MatchedRuleIndex)
	}

	// Second input should match rule 1
	if !resultPreview.Status.Results[1].Matched {
		t.Error("Expected second input to match")
	}
	if resultPreview.Status.Results[1].MatchedRuleIndex != 1 {
		t.Errorf("Expected second input MatchedRuleIndex=1, got %d", resultPreview.Status.Results[1].MatchedRuleIndex)
	}

	// Third input should not match
	if resultPreview.Status.Results[2].Matched {
		t.Error("Expected third input NOT to match")
	}
	if resultPreview.Status.Results[2].MatchedRuleIndex != -1 {
		t.Errorf("Expected third input MatchedRuleIndex=-1, got %d", resultPreview.Status.Results[2].MatchedRuleIndex)
	}

	// Check activities - should have 2 (only matched inputs)
	if len(resultPreview.Status.Activities) != 2 {
		t.Fatalf("Expected 2 activities, got %d", len(resultPreview.Status.Activities))
	}

	// First activity
	if resultPreview.Status.Activities[0].Spec.Summary != "alice@example.com created HTTPProxy" {
		t.Errorf("Unexpected first activity summary: %q", resultPreview.Status.Activities[0].Spec.Summary)
	}

	// Second activity
	if resultPreview.Status.Activities[1].Spec.Summary != "bob@example.com deleted HTTPProxy" {
		t.Errorf("Unexpected second activity summary: %q", resultPreview.Status.Activities[1].Spec.Summary)
	}
}

func TestPolicyPreviewStorage_Create_MixedAuditAndEvent(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	eventData := map[string]interface{}{
		"reason":              "Deployed",
		"message":             "Successfully deployed",
		"reportingController": "deploy-controller",
		"regarding": map[string]interface{}{
			"apiVersion": "networking.datumapis.com/v1",
			"kind":       "HTTPProxy",
			"name":       "event-proxy",
			"namespace":  "default",
		},
	}
	eventBytes, _ := json.Marshal(eventData)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-968c9b",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
				},
				EventRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-a8acc2",
						Match:   `event.reason == "Deployed"`,
						Summary: `{{ actor }} deployed HTTPProxy`,
					},
				},
			},
			Inputs: []v1alpha1.PolicyPreviewInput{
				{
					Type: "audit",
					Audit: &auditv1.Event{
						Verb: "create",
						User: authnv1.UserInfo{
							Username: "alice@example.com",
						},
						ObjectRef: &auditv1.ObjectReference{
							APIGroup: "networking.datumapis.com",
							Resource: "httpproxies",
							Name:     "audit-proxy",
						},
					},
				},
				{
					Type: "event",
					Event: &runtime.RawExtension{
						Raw: eventBytes,
					},
				},
			},
		},
	}

	result, err := storage.Create(context.Background(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultPreview := result.(*v1alpha1.PolicyPreview)

	// Check results
	if len(resultPreview.Status.Results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(resultPreview.Status.Results))
	}

	// First input (audit) should match
	if !resultPreview.Status.Results[0].Matched {
		t.Error("Expected audit input to match")
	}
	if resultPreview.Status.Results[0].MatchedRuleType != "audit" {
		t.Errorf("Expected first result type='audit', got %q", resultPreview.Status.Results[0].MatchedRuleType)
	}

	// Second input (event) should match
	if !resultPreview.Status.Results[1].Matched {
		t.Error("Expected event input to match")
	}
	if resultPreview.Status.Results[1].MatchedRuleType != "event" {
		t.Errorf("Expected second result type='event', got %q", resultPreview.Status.Results[1].MatchedRuleType)
	}

	// Check activities
	if len(resultPreview.Status.Activities) != 2 {
		t.Fatalf("Expected 2 activities, got %d", len(resultPreview.Status.Activities))
	}

	// First activity (from audit)
	if resultPreview.Status.Activities[0].Spec.Origin.Type != "audit" {
		t.Errorf("Expected first activity origin='audit', got %q", resultPreview.Status.Activities[0].Spec.Origin.Type)
	}

	// Second activity (from event)
	if resultPreview.Status.Activities[1].Spec.Origin.Type != "event" {
		t.Errorf("Expected second activity origin='event', got %q", resultPreview.Status.Activities[1].Spec.Origin.Type)
	}
	if resultPreview.Status.Activities[1].Spec.Summary != "deploy-controller deployed HTTPProxy" {
		t.Errorf("Unexpected event activity summary: %q", resultPreview.Status.Activities[1].Spec.Summary)
	}
}

func TestPolicyPreviewStorage_Create_AuditNoMatch(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-a52fd5",
						Match:   `audit.verb == "delete"`,
						Summary: `{{ actor }} deleted HTTPProxy`,
					},
				},
			},
			Inputs: []v1alpha1.PolicyPreviewInput{
				{
					Type: "audit",
					Audit: &auditv1.Event{
						Verb: "create", // Different verb - should not match
						User: authnv1.UserInfo{
							Username: "alice@example.com",
						},
					},
				},
			},
		},
	}

	result, err := storage.Create(context.Background(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultPreview := result.(*v1alpha1.PolicyPreview)

	if resultPreview.Status.Results[0].Matched {
		t.Error("Expected rule NOT to match, but it did")
	}

	if resultPreview.Status.Results[0].MatchedRuleIndex != -1 {
		t.Errorf("Expected MatchedRuleIndex=-1, got %d", resultPreview.Status.Results[0].MatchedRuleIndex)
	}

	// No activities should be generated
	if len(resultPreview.Status.Activities) != 0 {
		t.Errorf("Expected 0 activities, got %d", len(resultPreview.Status.Activities))
	}
}

func TestPolicyPreviewStorage_Create_InvalidInput(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	// Valid policy to use for input validation tests
	validPolicy := v1alpha1.ActivityPolicySpec{
		Resource: v1alpha1.ActivityPolicyResource{
			APIGroup: "test.example.com",
			Kind:     "TestResource",
		},
		AuditRules: []v1alpha1.ActivityPolicyRule{
			{
				Name:    "rule-c7a19f",
				Match:   "true",
				Summary: `HTTPProxy changed`,
			},
		},
	}

	tests := []struct {
		name    string
		preview *v1alpha1.PolicyPreview
		wantErr string
	}{
		{
			name: "no inputs",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: validPolicy,
					Inputs: []v1alpha1.PolicyPreviewInput{},
				},
			},
			wantErr: "Provide either 'inputs' or 'autoFetch'",
		},
		{
			name: "missing input type",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: validPolicy,
					Inputs: []v1alpha1.PolicyPreviewInput{
						{Type: ""},
					},
				},
			},
			wantErr: "Specify whether this input is an 'audit' log or 'event'.",
		},
		{
			name: "invalid input type",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: validPolicy,
					Inputs: []v1alpha1.PolicyPreviewInput{
						{Type: "invalid"},
					},
				},
			},
			wantErr: `Supported values: "audit", "event".`,
		},
		{
			name: "audit type without audit data",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: validPolicy,
					Inputs: []v1alpha1.PolicyPreviewInput{
						{
							Type:  "audit",
							Audit: nil,
						},
					},
				},
			},
			wantErr: "Provide the audit log data to test.",
		},
		{
			name: "event type without event data",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: validPolicy,
					Inputs: []v1alpha1.PolicyPreviewInput{
						{
							Type:  "event",
							Event: nil,
						},
					},
				},
			},
			wantErr: "Provide the event data to test.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := storage.Create(context.Background(), tt.preview, nil, &metav1.CreateOptions{})
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !containsSubstring(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestPolicyPreviewStorage_Create_InvalidPolicy(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	tests := []struct {
		name    string
		preview *v1alpha1.PolicyPreview
		wantErr string
	}{
		{
			name: "missing inputs",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: v1alpha1.ActivityPolicySpec{
						Resource: v1alpha1.ActivityPolicyResource{
							APIGroup: "", // empty apiGroup is valid for core API resources (v1)
							Kind:     "Pod",
						},
						AuditRules: []v1alpha1.ActivityPolicyRule{
							{
								Name:    "rule-bf1384",
								Match:   "true",
								Summary: `HTTPProxy changed`,
							},
						},
					},
					// Inputs missing - this is what we're testing
				},
			},
			wantErr: "Provide either 'inputs' or 'autoFetch'",
		},
		{
			name: "missing kind",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: v1alpha1.ActivityPolicySpec{
						Resource: v1alpha1.ActivityPolicyResource{
							APIGroup: "test.example.com",
						},
						AuditRules: []v1alpha1.ActivityPolicyRule{
							{
								Name:    "rule-54c024",
								Match:   "true",
								Summary: `HTTPProxy changed`,
							},
						},
					},
					Inputs: []v1alpha1.PolicyPreviewInput{
						{
							Type: "audit",
							Audit: &auditv1.Event{
								Verb: "create",
							},
						},
					},
				},
			},
			wantErr: "Specify the kind of resource this policy targets",
		},
		{
			name: "invalid match expression",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: v1alpha1.ActivityPolicySpec{
						Resource: v1alpha1.ActivityPolicyResource{
							APIGroup: "test.example.com",
							Kind:     "TestResource",
						},
						AuditRules: []v1alpha1.ActivityPolicyRule{
							{
								Name:    "rule-1eea79",
								Match:   "invalid.expression[",
								Summary: `HTTPProxy changed`,
							},
						},
					},
					Inputs: []v1alpha1.PolicyPreviewInput{
						{
							Type: "audit",
							Audit: &auditv1.Event{
								Verb: "create",
							},
						},
					},
				},
			},
			wantErr: "Invalid match expression",
		},
		{
			name: "invalid summary expression",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: v1alpha1.ActivityPolicySpec{
						Resource: v1alpha1.ActivityPolicyResource{
							APIGroup: "test.example.com",
							Kind:     "TestResource",
						},
						AuditRules: []v1alpha1.ActivityPolicyRule{
							{
								Name:    "rule-53b1ab",
								Match:   "true",
								Summary: `{{ undeclared_variable }}`,
							},
						},
					},
					Inputs: []v1alpha1.PolicyPreviewInput{
						{
							Type: "audit",
							Audit: &auditv1.Event{
								Verb: "create",
							},
						},
					},
				},
			},
			wantErr: "Invalid summary expression",
		},
		{
			name: "missing match expression",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: v1alpha1.ActivityPolicySpec{
						Resource: v1alpha1.ActivityPolicyResource{
							APIGroup: "test.example.com",
							Kind:     "TestResource",
						},
						AuditRules: []v1alpha1.ActivityPolicyRule{
							{
								Name:    "rule-b527e9",
								Summary: `HTTPProxy changed`,
							},
						},
					},
					Inputs: []v1alpha1.PolicyPreviewInput{
						{
							Type: "audit",
							Audit: &auditv1.Event{
								Verb: "create",
							},
						},
					},
				},
			},
			wantErr: "Provide a CEL expression that determines when this rule applies",
		},
		{
			name: "missing summary expression",
			preview: &v1alpha1.PolicyPreview{
				Spec: v1alpha1.PolicyPreviewSpec{
					Policy: v1alpha1.ActivityPolicySpec{
						Resource: v1alpha1.ActivityPolicyResource{
							APIGroup: "test.example.com",
							Kind:     "TestResource",
						},
						AuditRules: []v1alpha1.ActivityPolicyRule{
							{
								Name:  "rule-d78931",
								Match: "true",
							},
						},
					},
					Inputs: []v1alpha1.PolicyPreviewInput{
						{
							Type: "audit",
							Audit: &auditv1.Event{
								Verb: "create",
							},
						},
					},
				},
			},
			wantErr: "Provide a template for the activity summary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := storage.Create(context.Background(), tt.preview, nil, &metav1.CreateOptions{})
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !containsSubstring(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestPolicyPreviewStorage_Create_ActivityFields(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-2425ad",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
				},
			},
			Inputs: []v1alpha1.PolicyPreviewInput{
				{
					Type: "audit",
					Audit: &auditv1.Event{
						AuditID: "test-audit-123",
						Verb:    "create",
						User: authnv1.UserInfo{
							Username: "system:serviceaccount:default:my-sa",
							UID:      "uid-123",
						},
						ObjectRef: &auditv1.ObjectReference{
							APIGroup:   "networking.datumapis.com",
							APIVersion: "v1",
							Resource:   "httpproxies",
							Name:       "my-proxy",
							Namespace:  "test-ns",
						},
					},
				},
			},
		},
	}

	result, err := storage.Create(context.Background(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultPreview := result.(*v1alpha1.PolicyPreview)
	activity := resultPreview.Status.Activities[0]

	// Check actor fields
	if activity.Spec.Actor.Type != "system" {
		t.Errorf("Expected Actor.Type='system', got %q", activity.Spec.Actor.Type)
	}
	if activity.Spec.Actor.Name != "serviceaccount:default:my-sa" {
		t.Errorf("Expected Actor.Name='serviceaccount:default:my-sa', got %q", activity.Spec.Actor.Name)
	}
	if activity.Spec.Actor.UID != "uid-123" {
		t.Errorf("Expected Actor.UID='uid-123', got %q", activity.Spec.Actor.UID)
	}

	// Check resource fields
	if activity.Spec.Resource.APIGroup != "networking.datumapis.com" {
		t.Errorf("Expected Resource.APIGroup='networking.datumapis.com', got %q", activity.Spec.Resource.APIGroup)
	}
	if activity.Spec.Resource.Name != "my-proxy" {
		t.Errorf("Expected Resource.Name='my-proxy', got %q", activity.Spec.Resource.Name)
	}
	if activity.Spec.Resource.Namespace != "test-ns" {
		t.Errorf("Expected Resource.Namespace='test-ns', got %q", activity.Spec.Resource.Namespace)
	}

	// Check origin fields
	if activity.Spec.Origin.Type != "audit" {
		t.Errorf("Expected Origin.Type='audit', got %q", activity.Spec.Origin.Type)
	}
	if activity.Spec.Origin.ID != "test-audit-123" {
		t.Errorf("Expected Origin.ID='test-audit-123', got %q", activity.Spec.Origin.ID)
	}

	// Check change source (service account should be system)
	if activity.Spec.ChangeSource != "system" {
		t.Errorf("Expected ChangeSource='system', got %q", activity.Spec.ChangeSource)
	}

	// Check labels
	if activity.Labels["activity.miloapis.com/origin-type"] != "audit" {
		t.Errorf("Expected label origin-type='audit', got %q", activity.Labels["activity.miloapis.com/origin-type"])
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && contains(s, substr)))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Auto-fetch tests

func TestPolicyPreviewStorage_Create_AutoFetch_AuditOnly(t *testing.T) {
	// Mock audit log backend that returns sample data
	mockAudit := &mockAuditLogBackend{
		result: &storage.QueryResult{
			Events: []auditv1.Event{
				{
					Verb: "create",
					User: authnv1.UserInfo{
						Username: "alice@example.com",
					},
					ObjectRef: &auditv1.ObjectReference{
						APIGroup: "networking.datumapis.com",
						Resource: "httpproxies",
						Name:     "test-proxy",
					},
				},
			},
		},
	}

	storage := NewPolicyPreviewStorage(mockAudit, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-create",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
				},
			},
			AutoFetch: &v1alpha1.AutoFetchSpec{
				Limit:     10,
				TimeRange: "24h",
				Sources:   "audit",
			},
		},
	}

	result, err := storage.Create(platformCtx(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultPreview := result.(*v1alpha1.PolicyPreview)

	// Verify fetched inputs are included in status
	if len(resultPreview.Status.FetchedInputs) != 1 {
		t.Fatalf("Expected 1 fetched input, got %d", len(resultPreview.Status.FetchedInputs))
	}

	if resultPreview.Status.FetchedInputs[0].Type != "audit" {
		t.Errorf("Expected fetched input type='audit', got %q", resultPreview.Status.FetchedInputs[0].Type)
	}

	// Verify evaluation happened
	if len(resultPreview.Status.Results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(resultPreview.Status.Results))
	}

	if !resultPreview.Status.Results[0].Matched {
		t.Error("Expected fetched input to match")
	}

	if len(resultPreview.Status.Activities) != 1 {
		t.Fatalf("Expected 1 activity, got %d", len(resultPreview.Status.Activities))
	}
}

func TestPolicyPreviewStorage_Create_AutoFetch_EventsOnly(t *testing.T) {
	// Mock event backend that returns sample data
	mockEvent := &mockEventBackend{
		result: &storage.EventQueryResult{
			Events: []v1alpha1.EventRecord{
				{
					Event: createEventV1("Deployed", "Successfully deployed", "deploy-controller"),
				},
			},
		},
	}

	storage := NewPolicyPreviewStorage(nil, mockEvent)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				EventRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-deployed",
						Match:   `event.reason == "Deployed"`,
						Summary: `{{ actor }} deployed HTTPProxy`,
					},
				},
			},
			AutoFetch: &v1alpha1.AutoFetchSpec{
				Limit:     10,
				TimeRange: "24h",
				Sources:   "events",
			},
		},
	}

	result, err := storage.Create(platformCtx(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultPreview := result.(*v1alpha1.PolicyPreview)

	// Verify fetched inputs
	if len(resultPreview.Status.FetchedInputs) != 1 {
		t.Fatalf("Expected 1 fetched input, got %d", len(resultPreview.Status.FetchedInputs))
	}

	if resultPreview.Status.FetchedInputs[0].Type != "event" {
		t.Errorf("Expected fetched input type='event', got %q", resultPreview.Status.FetchedInputs[0].Type)
	}

	// Verify evaluation
	if len(resultPreview.Status.Results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(resultPreview.Status.Results))
	}

	if !resultPreview.Status.Results[0].Matched {
		t.Error("Expected fetched input to match")
	}
}

func TestPolicyPreviewStorage_Create_AutoFetch_Both(t *testing.T) {
	mockAudit := &mockAuditLogBackend{
		result: &storage.QueryResult{
			Events: []auditv1.Event{
				{
					Verb: "create",
					User: authnv1.UserInfo{
						Username: "alice@example.com",
					},
					ObjectRef: &auditv1.ObjectReference{
						APIGroup: "networking.datumapis.com",
						Resource: "httpproxies",
						Name:     "test-proxy",
					},
				},
			},
		},
	}

	mockEvent := &mockEventBackend{
		result: &storage.EventQueryResult{
			Events: []v1alpha1.EventRecord{
				{
					Event: createEventV1("Deployed", "Successfully deployed", "deploy-controller"),
				},
			},
		},
	}

	storage := NewPolicyPreviewStorage(mockAudit, mockEvent)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-create",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
				},
				EventRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-deployed",
						Match:   `event.reason == "Deployed"`,
						Summary: `{{ actor }} deployed HTTPProxy`,
					},
				},
			},
			AutoFetch: &v1alpha1.AutoFetchSpec{
				Limit:     10,
				TimeRange: "24h",
				Sources:   "both",
			},
		},
	}

	result, err := storage.Create(platformCtx(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultPreview := result.(*v1alpha1.PolicyPreview)

	// Should have both audit and event inputs
	if len(resultPreview.Status.FetchedInputs) != 2 {
		t.Fatalf("Expected 2 fetched inputs, got %d", len(resultPreview.Status.FetchedInputs))
	}

	// Both should match
	if len(resultPreview.Status.Activities) != 2 {
		t.Fatalf("Expected 2 activities, got %d", len(resultPreview.Status.Activities))
	}
}

func TestPolicyPreviewStorage_Create_AutoFetch_EmptyResults(t *testing.T) {
	// Mock returns empty results
	mockAudit := &mockAuditLogBackend{
		result: &storage.QueryResult{
			Events: []auditv1.Event{},
		},
	}

	storage := NewPolicyPreviewStorage(mockAudit, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-create",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
				},
			},
			AutoFetch: &v1alpha1.AutoFetchSpec{
				Limit:     10,
				TimeRange: "24h",
				Sources:   "audit",
			},
		},
	}

	result, err := storage.Create(platformCtx(), preview, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create should succeed with empty results, got error: %v", err)
	}

	resultPreview := result.(*v1alpha1.PolicyPreview)

	// Should have empty fetched inputs
	if len(resultPreview.Status.FetchedInputs) != 0 {
		t.Errorf("Expected 0 fetched inputs, got %d", len(resultPreview.Status.FetchedInputs))
	}

	// No activities
	if len(resultPreview.Status.Activities) != 0 {
		t.Errorf("Expected 0 activities, got %d", len(resultPreview.Status.Activities))
	}
}

func TestPolicyPreviewStorage_Create_Validation_BothInputsAndAutoFetch(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-create",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
				},
			},
			Inputs: []v1alpha1.PolicyPreviewInput{
				{
					Type: "audit",
					Audit: &auditv1.Event{
						Verb: "create",
					},
				},
			},
			AutoFetch: &v1alpha1.AutoFetchSpec{
				Limit: 10,
			},
		},
	}

	_, err := storage.Create(platformCtx(), preview, nil, &metav1.CreateOptions{})
	if err == nil {
		t.Fatal("Expected validation error for both inputs and autoFetch, got nil")
	}

	if !containsSubstring(err.Error(), "Cannot specify both") && !containsSubstring(err.Error(), "cannot specify both") {
		t.Errorf("Expected error about mutual exclusivity, got: %v", err)
	}
}

func TestPolicyPreviewStorage_Create_Validation_NeitherInputsNorAutoFetch(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-create",
						Match:   `audit.verb == "create"`,
						Summary: `{{ actor }} created HTTPProxy`,
					},
				},
			},
			// No inputs, no autoFetch
		},
	}

	_, err := storage.Create(platformCtx(), preview, nil, &metav1.CreateOptions{})
	if err == nil {
		t.Fatal("Expected validation error for missing inputs/autoFetch, got nil")
	}

	if !containsSubstring(err.Error(), "Provide either") && !containsSubstring(err.Error(), "provide either") {
		t.Errorf("Expected error about providing either inputs or autoFetch, got: %v", err)
	}
}

func TestPolicyPreviewStorage_Create_Validation_AutoFetchInvalidLimit(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-create",
						Match:   "true",
						Summary: `Test`,
					},
				},
			},
			AutoFetch: &v1alpha1.AutoFetchSpec{
				Limit: 100, // Over max of 50
			},
		},
	}

	_, err := storage.Create(platformCtx(), preview, nil, &metav1.CreateOptions{})
	if err == nil {
		t.Fatal("Expected validation error for limit > 50, got nil")
	}

	if !containsSubstring(err.Error(), "Must be <= 50") && !containsSubstring(err.Error(), "must be <= 50") {
		t.Errorf("Expected error about limit, got: %v", err)
	}
}

func TestPolicyPreviewStorage_Create_Validation_AutoFetchInvalidSources(t *testing.T) {
	storage := NewPolicyPreviewStorage(nil, nil)

	preview := &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{
					APIGroup: "networking.datumapis.com",
					Kind:     "HTTPProxy",
				},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{
						Name:    "rule-create",
						Match:   "true",
						Summary: `Test`,
					},
				},
			},
			AutoFetch: &v1alpha1.AutoFetchSpec{
				Limit:   10,
				Sources: "invalid",
			},
		},
	}

	_, err := storage.Create(platformCtx(), preview, nil, &metav1.CreateOptions{})
	if err == nil {
		t.Fatal("Expected validation error for invalid sources, got nil")
	}

	if !containsSubstring(err.Error(), "Supported values") {
		t.Errorf("Expected error about supported values, got: %v", err)
	}
}

func TestBuildRuleFilter(t *testing.T) {
	tests := []struct {
		name     string
		rules    []v1alpha1.ActivityPolicyRule
		expected string
	}{
		{
			name: "single rule - audit prefix stripped",
			rules: []v1alpha1.ActivityPolicyRule{
				{Name: "rule1", Match: `audit.verb == "create"`},
			},
			expected: `(verb == "create")`,
		},
		{
			name: "multiple rules - ORed together with prefix stripped",
			rules: []v1alpha1.ActivityPolicyRule{
				{Name: "rule1", Match: `audit.verb == "create"`},
				{Name: "rule2", Match: `audit.verb == "delete"`},
			},
			expected: `((verb == "create") || (verb == "delete"))`,
		},
		{
			name: "complex expression - audit prefix stripped",
			rules: []v1alpha1.ActivityPolicyRule{
				{Name: "rule1", Match: `audit.verb == "update" && audit.objectRef.namespace == "production"`},
			},
			expected: `(verb == "update" && objectRef.namespace == "production")`,
		},
		{
			name: "skips empty match",
			rules: []v1alpha1.ActivityPolicyRule{
				{Name: "rule1", Match: ""},
				{Name: "rule2", Match: `audit.verb == "create"`},
			},
			expected: `(verb == "create")`,
		},
		{
			name: "skips 'true' match",
			rules: []v1alpha1.ActivityPolicyRule{
				{Name: "rule1", Match: "true"},
				{Name: "rule2", Match: `audit.verb == "create"`},
			},
			expected: `(verb == "create")`,
		},
		{
			name:     "empty rules",
			rules:    []v1alpha1.ActivityPolicyRule{},
			expected: "",
		},
		{
			name: "all rules are true or empty",
			rules: []v1alpha1.ActivityPolicyRule{
				{Name: "rule1", Match: "true"},
				{Name: "rule2", Match: ""},
			},
			expected: "",
		},
		{
			name: "preserves audit. inside string literals",
			rules: []v1alpha1.ActivityPolicyRule{
				{Name: "rule1", Match: `audit.objectRef.name == "audit.log-collector"`},
			},
			expected: `(objectRef.name == "audit.log-collector")`,
		},
		{
			name: "preserves audit. inside single-quoted strings",
			rules: []v1alpha1.ActivityPolicyRule{
				{Name: "rule1", Match: `audit.verb == 'audit.create'`},
			},
			expected: `(verb == 'audit.create')`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRuleFilter(tt.rules)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractEventFiltersFromCEL(t *testing.T) {
	tests := []struct {
		name     string
		celExpr  string
		expected []string
	}{
		{
			name:     "reason equals",
			celExpr:  `event.reason == "Ready"`,
			expected: []string{"reason=Ready"},
		},
		{
			name:     "type equals",
			celExpr:  `event.type == "Warning"`,
			expected: []string{"type=Warning"},
		},
		{
			name:     "reason and type",
			celExpr:  `event.reason == "Failed" && event.type == "Warning"`,
			expected: []string{"reason=Failed", "type=Warning"},
		},
		{
			name:     "no extractable filters",
			celExpr:  `event.note.contains("error")`,
			expected: nil,
		},
		{
			name:     "empty expression",
			celExpr:  ``,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractEventFiltersFromCEL(tt.celExpr)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d filters, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("Filter %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

// platformCtx is a request context for a platform-scoped caller.
func platformCtx() context.Context {
	return request.WithUser(context.Background(), &user.DefaultInfo{Name: "admin"})
}

// orgCtx is a request context for a caller acting in organization org.
func orgCtx(org string) context.Context {
	return request.WithUser(context.Background(), &user.DefaultInfo{
		Name: "alice@acme.com",
		Extra: map[string][]string{
			"iam.miloapis.com/parent-type": {"Organization"},
			"iam.miloapis.com/parent-name": {org},
		},
	})
}

func autoFetchPreview(sources string) *v1alpha1.PolicyPreview {
	return &v1alpha1.PolicyPreview{
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: v1alpha1.ActivityPolicySpec{
				Resource: v1alpha1.ActivityPolicyResource{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy"},
				AuditRules: []v1alpha1.ActivityPolicyRule{
					{Name: "rule-create", Match: `audit.verb == "create"`, Summary: `{{ actor }} created HTTPProxy`},
				},
				EventRules: []v1alpha1.ActivityPolicyRule{
					{Name: "rule-event", Match: `event.reason == "Programmed"`, Summary: `programmed`},
				},
			},
			AutoFetch: &v1alpha1.AutoFetchSpec{Limit: 10, TimeRange: "24h", Sources: sources},
		},
	}
}

// AutoFetch samples come from the caller's scope, through the same query
// path as AuditLogQuery, so staff redaction applies (the storage layer
// returns staff events already reduced to "Datum Support").
func TestPolicyPreviewStorage_Create_AutoFetch_UsesCallerScope(t *testing.T) {
	mockAudit := &mockAuditLogBackend{result: &storage.QueryResult{Events: []auditv1.Event{{
		Verb:      "create",
		User:      authnv1.UserInfo{Username: storage.SupportActorName},
		ObjectRef: &auditv1.ObjectReference{APIGroup: "networking.datumapis.com", Resource: "httpproxies", Name: "p"},
	}}}}
	mockEvents := &mockEventBackend{result: &storage.EventQueryResult{}}
	st := NewPolicyPreviewStorage(mockAudit, mockEvents)

	result, err := st.Create(orgCtx("acme"), autoFetchPreview("both"), nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	want := storage.ScopeContext{Type: "Organization", Name: "acme"}
	if len(mockAudit.scopes) != 1 || mockAudit.scopes[0] != want {
		t.Errorf("audit samples fetched with scopes %+v, want %+v", mockAudit.scopes, want)
	}
	if len(mockEvents.scopes) != 1 || mockEvents.scopes[0] != want {
		t.Errorf("event samples fetched with scopes %+v, want %+v", mockEvents.scopes, want)
	}
	acts := result.(*v1alpha1.PolicyPreview).Status.Activities
	if len(acts) != 1 || acts[0].Spec.Summary != "Datum Support created HTTPProxy" {
		t.Errorf("activities = %+v, want the redacted staff event", acts)
	}

	// A platform caller keeps platform-wide samples.
	mockAudit.scopes = nil
	if _, err := st.Create(platformCtx(), autoFetchPreview("audit"), nil, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if len(mockAudit.scopes) != 1 || mockAudit.scopes[0].Type != "platform" {
		t.Errorf("platform caller scopes = %+v", mockAudit.scopes)
	}
}

func TestPolicyPreviewStorage_Create_AutoFetch_StaffNotReadyIs503(t *testing.T) {
	st := NewPolicyPreviewStorage(&mockAuditLogBackend{err: storage.ErrStaffRedactionNotReady}, nil)
	_, err := st.Create(orgCtx("acme"), autoFetchPreview("audit"), nil, &metav1.CreateOptions{})
	status, ok := err.(*apierrors.StatusError)
	if !ok || status.ErrStatus.Code != 503 || status.ErrStatus.Details == nil || status.ErrStatus.Details.RetryAfterSeconds == 0 {
		t.Fatalf("error = %v, want 503 with Retry-After", err)
	}
}

func TestPolicyPreviewStorage_Create_AutoFetch_RequiresUser(t *testing.T) {
	st := NewPolicyPreviewStorage(&mockAuditLogBackend{result: &storage.QueryResult{}}, nil)
	if _, err := st.Create(context.Background(), autoFetchPreview("audit"), nil, &metav1.CreateOptions{}); err == nil {
		t.Fatal("autoFetch without a request user succeeded")
	}
}
