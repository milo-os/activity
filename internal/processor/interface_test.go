package processor

import (
	"testing"

	authnv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/types"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func auditFrom(username, uid, userAgent string) *auditv1.Event {
	return &auditv1.Event{
		AuditID:   types.UID("audit-1"),
		Verb:      "create",
		User:      authnv1.UserInfo{Username: username, UID: uid},
		UserAgent: userAgent,
		ObjectRef: &auditv1.ObjectReference{APIGroup: "networking.datumapis.com", Resource: "httpproxies", Name: "p"},
	}
}

func TestBuildFromAudit_SetsInterface(t *testing.T) {
	cases := []struct {
		username, userAgent string
		want                v1alpha1.ActivityInterface
	}{
		{"alice@x.com", "datumctl/v0.9.0 (darwin/arm64) kubernetes/abc", v1alpha1.ActivityInterface{Type: "cli", Client: "datumctl"}},
		{"alice@x.com", "Mozilla/5.0 (Macintosh)", v1alpha1.ActivityInterface{Type: "console"}},
		{"alice@x.com", "", v1alpha1.ActivityInterface{Type: "unknown"}},
		{"ci@proj.identity.miloapis.com", "curl/8.4.0", v1alpha1.ActivityInterface{Type: "api", Client: "service-account"}},
		{"system:control@compute.datumapis.com", "", v1alpha1.ActivityInterface{Type: "controller", Client: "compute"}},
	}
	for _, tc := range cases {
		t.Run(tc.username+" "+tc.userAgent, func(t *testing.T) {
			b := &ActivityBuilder{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy"}
			a, err := b.BuildFromAudit(auditFrom(tc.username, "", tc.userAgent), "s", nil, nil)
			if err != nil {
				t.Fatalf("BuildFromAudit: %v", err)
			}
			if a.Spec.Interface == nil || *a.Spec.Interface != tc.want {
				t.Errorf("Interface = %+v, want %+v", a.Spec.Interface, tc.want)
			}
		})
	}
}

func TestBuildFromAudit_MiloServiceAccountIsNotAPerson(t *testing.T) {
	resolver := &fakeResolver{names: map[string]UserInfo{"sa-uid": alice}, uids: map[string]UserInfo{"sa-uid": alice}}
	b := &ActivityBuilder{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy", UserResolver: resolver}

	a, err := b.BuildFromAudit(auditFrom("ci@proj.identity.miloapis.com", "sa-uid", "Go-http-client/1.1"), "ci@proj.identity.miloapis.com created p", nil, nil)
	if err != nil {
		t.Fatalf("BuildFromAudit: %v", err)
	}
	actor := a.Spec.Actor
	if actor.Type != ActorTypeServiceAccount || actor.Name != "ci@proj.identity.miloapis.com" {
		t.Errorf("actor = %+v, want a serviceaccount actor", actor)
	}
	if actor.Email != "" || actor.DisplayName != "" || actor.AvatarURL != "" {
		t.Errorf("service account actor has person fields: %+v", actor)
	}
	if resolver.totalCalls() != 0 {
		t.Errorf("User index consulted %d times for a service account", resolver.totalCalls())
	}
	if a.Spec.ChangeSource != ChangeSourceSystem || a.Labels["activity.miloapis.com/change-source"] != ChangeSourceSystem {
		t.Errorf("changeSource = %q (label %q), want system", a.Spec.ChangeSource, a.Labels["activity.miloapis.com/change-source"])
	}
}

func TestClassifyChangeSource(t *testing.T) {
	for username, want := range map[string]string{
		"alice@x.com":                          ChangeSourceHuman,
		"ci@proj.identity.miloapis.com":        ChangeSourceSystem,
		"system:control@compute.datumapis.com": ChangeSourceSystem,
		"system:serviceaccount:ns:sa":          ChangeSourceSystem,
	} {
		if got := ClassifyChangeSource(authnv1.UserInfo{Username: username}); got != want {
			t.Errorf("ClassifyChangeSource(%q) = %q, want %q", username, got, want)
		}
	}
}

func TestEventActivities_InterfaceIsController(t *testing.T) {
	event := map[string]interface{}{
		"metadata":            map[string]interface{}{"uid": "e1"},
		"reportingController": "dns-controller",
		"regarding":           map[string]interface{}{"apiVersion": "v1", "kind": "Pod", "name": "p"},
	}
	want := v1alpha1.ActivityInterface{Type: "controller", Client: "dns-controller"}

	b := &ActivityBuilder{APIGroup: "", Kind: "Pod"}
	fromReindex, err := b.BuildFromEvent(event, "s", nil, nil)
	if err != nil {
		t.Fatalf("BuildFromEvent: %v", err)
	}
	fromLive := (&EventProcessor{}).buildActivity(event, &MatchedPolicy{Kind: "Pod"}, "s", nil)
	for name, a := range map[string]*v1alpha1.Activity{"reindex": fromReindex, "live": fromLive} {
		if a.Spec.Interface == nil || *a.Spec.Interface != want {
			t.Errorf("%s: Interface = %+v, want %+v", name, a.Spec.Interface, want)
		}
	}
}
