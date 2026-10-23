package processor

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

func TestBuildFromAuditResourceName(t *testing.T) {
	tests := []struct {
		name     string
		refName  string
		response string
		wantName string
		wantUID  string
	}{
		{
			name:     "generateName create takes the name from the response",
			response: `{"metadata":{"name":"web-0-x7k2p","generateName":"web-0-","uid":"uid-1"}}`,
			wantName: "web-0-x7k2p",
			wantUID:  "uid-1",
		},
		{
			name:     "named request keeps the ObjectRef name",
			refName:  "web-0",
			response: `{"metadata":{"name":"web-0","uid":"uid-2"}}`,
			wantName: "web-0",
			wantUID:  "uid-2",
		},
		{
			name:     "no response object leaves the name empty",
			wantName: "",
		},
		{
			name:     "malformed response object leaves the name empty",
			response: `not json`,
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit := &auditv1.Event{
				AuditID: "audit-1",
				Verb:    "create",
				ObjectRef: &auditv1.ObjectReference{
					APIGroup:   "compute.datumapis.com",
					APIVersion: "v1alpha",
					Resource:   "instanceconsolesessions",
					Namespace:  "default",
					Name:       tt.refName,
				},
			}
			if tt.response != "" {
				audit.ResponseObject = &runtime.Unknown{Raw: []byte(tt.response)}
			}

			b := &ActivityBuilder{APIGroup: "compute.datumapis.com", Kind: "InstanceConsoleSession"}
			activity, err := b.BuildFromAudit(audit, "summary", nil, nil)
			if err != nil {
				t.Fatalf("BuildFromAudit() error = %v", err)
			}
			if got := activity.Spec.Resource.Name; got != tt.wantName {
				t.Errorf("Resource.Name = %q, want %q", got, tt.wantName)
			}
			if got := activity.Spec.Resource.UID; got != tt.wantUID {
				t.Errorf("Resource.UID = %q, want %q", got, tt.wantUID)
			}
		})
	}
}
