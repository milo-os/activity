package cel

import (
	"context"
	"strings"
	"testing"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func TestEvaluateAuditSummary_InterfaceVariable(t *testing.T) {
	audit := map[string]interface{}{
		"user":      map[string]interface{}{"username": "alice@x.com"},
		"userAgent": "datumctl/v0.9.0 (darwin/arm64) kubernetes/abc",
	}
	summary, _, err := EvaluateAuditSummaryMap(
		"{{ actor }} used {{ interface.type }}/{{ interface.client }}{{ interface.type == 'cli' ? ' (CLI)' : '' }}", audit)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if want := "alice@x.com used cli/datumctl (CLI)"; summary != want {
		t.Errorf("summary = %q, want %q", summary, want)
	}
	if err := ValidatePolicyExpression("interface.type == 'mcp'", MatchExpression, AuditRule); err != nil {
		t.Errorf("interface not usable in audit match expressions: %v", err)
	}
}

func TestEvaluateEventSummary_InterfaceVariable(t *testing.T) {
	event := map[string]interface{}{"reportingController": "dns-controller"}
	summary, _, err := EvaluateEventSummary("{{ interface.type }}:{{ interface.client }}", event)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if summary != "controller:dns-controller" {
		t.Errorf("summary = %q", summary)
	}
}

func TestBuildActorRef_MiloServiceAccount(t *testing.T) {
	ref := BuildActorRef(map[string]interface{}{"user": map[string]interface{}{"username": "ci@proj.identity.miloapis.com"}})
	if ref["type"] != "serviceaccount" {
		t.Errorf("actorRef = %+v, want type serviceaccount", ref)
	}
	// A bare {{ actor }} for a service account is not linked like a person.
	_, links, err := EvaluateAuditSummaryMap("{{ actor }} applied",
		map[string]interface{}{"user": map[string]interface{}{"username": "ci@proj.identity.miloapis.com"}})
	if err != nil || len(links) != 0 {
		t.Errorf("links = %+v, err = %v; want none", links, err)
	}
}

func TestActivityFilter_InterfaceType(t *testing.T) {
	sql, args, err := ConvertActivityToClickHouseSQL(context.Background(), "spec.interface.type == 'mcp'")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if !strings.Contains(sql, "interface_type") || len(args) != 1 || args[0] != "mcp" {
		t.Errorf("sql = %q, args = %v", sql, args)
	}
}

func TestActivityToMap_Interface(t *testing.T) {
	f, err := CompileActivityFilterProgram("spec.interface.type == 'cli'")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ok, err := f.EvaluateActivity(&v1alpha1.Activity{
		Spec: v1alpha1.ActivitySpec{Interface: &v1alpha1.ActivityInterface{Type: "cli", Client: "datumctl"}},
	})
	if err != nil || !ok {
		t.Errorf("filter on spec.interface.type = %v, %v; want true", ok, err)
	}
	ok, err = f.EvaluateActivity(&v1alpha1.Activity{})
	if err != nil || ok {
		t.Errorf("filter on an activity without interface = %v, %v; want false", ok, err)
	}
}
