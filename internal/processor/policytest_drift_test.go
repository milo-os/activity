package processor_test

// Drift guards for activitytool (pkg/policytest). The tool mirrors the live
// processor's evaluation loops; these tests run the same policies and inputs
// through the real production entry points and through the tool, and fail if
// the matched policy, summary, links or Activity differ. When production
// evaluation changes, update the tool and these tests together.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	"go.miloapis.com/activity/pkg/policytest"
)

// driftResourceKinds is the RESTMapper stand-in shared by both sides.
var driftResourceKinds = map[string]map[string]string{
	"networking.datumapis.com": {"httpproxies": "HTTPProxy", "backends": "Backend"},
	"":                         {"services": "Service", "pods": "Pod"},
}

func driftResolver(apiGroup, resource string) (string, error) {
	return driftResourceKinds[apiGroup][resource], nil
}

func driftResource(apiGroup, kind string) string {
	for resource, k := range driftResourceKinds[apiGroup] {
		if k == kind {
			return resource
		}
	}
	return ""
}

func driftPolicy(name, group, kind string, audit, event []v1alpha1.ActivityPolicyRule) *v1alpha1.ActivityPolicy {
	return &v1alpha1.ActivityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Generation: 1},
		Spec: v1alpha1.ActivityPolicySpec{
			Resource:   v1alpha1.ActivityPolicyResource{APIGroup: group, Kind: kind},
			AuditRules: audit,
			EventRules: event,
		},
	}
}

func rule(name, match, summary string) v1alpha1.ActivityPolicyRule {
	return v1alpha1.ActivityPolicyRule{Name: name, Match: match, Summary: summary}
}

// driftPolicies are listed in name order, the order the processor's informer
// adds them after a restart and the order the tool evaluates them.
func driftPolicies() []*v1alpha1.ActivityPolicy {
	return []*v1alpha1.ActivityPolicy{
		driftPolicy("a-proxies", "networking.datumapis.com", "HTTPProxy",
			[]v1alpha1.ActivityPolicyRule{
				rule("broken", "audit.verb ==", "never"), // fails to compile: skipped
				rule("by-response", "audit.verb == 'delete' && audit.responseObject.metadata.name == 'x'", "never"),
				rule("create", "audit.verb == 'create'",
					"{{ audit.user.username }} created {{ link(audit.objectRef.name, audit.objectRef) }} backed by {{ link('backend-a', audit.responseObject.spec.backend) }}"),
			},
			[]v1alpha1.ActivityPolicyRule{
				rule("by-label", "event.regarding.labels.tier == 'edge'", "never"), // runtime error: skipped
				rule("programmed", "event.reason == 'Programmed'",
					"{{ link(event.regarding.name, event.regarding) }} programmed"),
				rule("bad-summary", "event.reason == 'Broken'", "{{ event.regarding.missing.field }}"),
			}),
		driftPolicy("b-proxies", "networking.datumapis.com", "HTTPProxy",
			[]v1alpha1.ActivityPolicyRule{
				rule("delete", "audit.verb == 'delete'", "{{ audit.user.username }} deleted {{ audit.objectRef.name }}"),
			},
			[]v1alpha1.ActivityPolicyRule{
				rule("accepted", "event.reason == 'Accepted'", "{{ event.regarding.name }} accepted"),
			}),
		driftPolicy("pods", "", "Pod", nil,
			[]v1alpha1.ActivityPolicyRule{
				rule("scheduled", "event.reason == 'Scheduled'",
					"{{ link(event.regarding.name, event.regarding) }} scheduled by {{ link(event.reportingController, {'type': 'controller', 'name': event.reportingController}) }}"),
			}),
	}
}

func productionCache(t *testing.T) *activityprocessor.PolicyCache {
	t.Helper()
	cache := activityprocessor.NewPolicyCache()
	for _, p := range driftPolicies() {
		if err := cache.Add(p, driftResource(p.Spec.Resource.APIGroup, p.Spec.Resource.Kind)); err != nil {
			t.Fatal(err)
		}
	}
	return cache
}

func toolEvaluator() *policytest.Evaluator {
	var loaded []policytest.LoadedPolicy
	for _, p := range driftPolicies() {
		loaded = append(loaded, policytest.LoadedPolicy{Policy: p, Source: "drift"})
	}
	return policytest.NewEvaluator(loaded, policytest.EvaluatorOptions{ResourceKinds: driftResourceKinds})
}

func assertSameActivity(t *testing.T, prod, tool *v1alpha1.Activity) {
	t.Helper()
	if (prod == nil) != (tool == nil) {
		t.Fatalf("match differs: production=%v tool=%v", prod != nil, tool != nil)
	}
	if prod == nil {
		return
	}
	if prod.Name != tool.Name || !reflect.DeepEqual(prod.Labels, tool.Labels) ||
		!prod.CreationTimestamp.Equal(&tool.CreationTimestamp) {
		t.Errorf("metadata differs:\n production: %s %v %v\n tool:       %s %v %v",
			prod.Name, prod.Labels, prod.CreationTimestamp, tool.Name, tool.Labels, tool.CreationTimestamp)
	}
	if !reflect.DeepEqual(prod.Spec, tool.Spec) {
		pj, _ := json.Marshal(prod.Spec)
		tj, _ := json.Marshal(tool.Spec)
		t.Errorf("spec differs:\n production: %s\n tool:       %s", pj, tj)
	}
}

func TestPolicyTestAuditDrift(t *testing.T) {
	response := func(obj map[string]any) *runtime.Unknown {
		raw, _ := json.Marshal(obj)
		return &runtime.Unknown{Raw: raw, ContentType: runtime.ContentTypeJSON}
	}
	audit := func(verb string, resp *runtime.Unknown) *auditv1.Event {
		return &auditv1.Event{
			AuditID:                  "audit-1",
			Verb:                     verb,
			User:                     authnv1.UserInfo{Username: "alice@example.com"},
			RequestReceivedTimestamp: metav1.NewMicroTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
			ObjectRef: &auditv1.ObjectReference{
				APIGroup: "networking.datumapis.com", APIVersion: "v1alpha", Resource: "httpproxies",
				Namespace: "default", Name: "edge",
			},
			ResponseObject: resp,
		}
	}

	tests := []struct {
		name       string
		audit      *auditv1.Event
		wantPolicy string
		wantRule   string
		wantErrors int
		wantLinks  []v1alpha1.ActivityLink
	}{
		{
			name: "match with objectRef and nested apiVersion links; compile-broken rule skipped",
			audit: audit("create", response(map[string]any{
				"metadata": map[string]any{"name": "edge", "uid": "proxy-1"},
				"spec": map[string]any{"backend": map[string]any{
					"apiVersion": "networking.datumapis.com/v1alpha",
					"kind":       "Backend",
					"metadata":   map[string]any{"name": "backend-a", "namespace": "default"},
				}},
			})),
			wantPolicy: "a-proxies",
			wantRule:   "create",
			wantLinks: []v1alpha1.ActivityLink{
				{Marker: "edge", Resource: v1alpha1.ActivityResource{APIGroup: "networking.datumapis.com", APIVersion: "v1alpha", Kind: "HTTPProxy", Name: "edge", Namespace: "default"}},
				{Marker: "backend-a", Resource: v1alpha1.ActivityResource{APIGroup: "networking.datumapis.com", APIVersion: "v1alpha", Kind: "Backend", Name: "backend-a", Namespace: "default"}},
			},
		},
		{
			name:       "runtime error abandons the policy and the next policy matches",
			audit:      audit("delete", nil),
			wantPolicy: "b-proxies",
			wantRule:   "delete",
			wantErrors: 1,
		},
		{
			name:  "no match",
			audit: audit("get", nil),
		},
	}

	cache := productionCache(t)
	ev := toolEvaluator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Production: the processor's audit loop (processor.go) over the
			// cache, using the exported per-policy evaluator.
			auditMap := mustAuditMap(t, tt.audit)
			var prod *v1alpha1.Activity
			prodPolicy, prodErrors := "", 0
			for _, policy := range cache.Get(tt.audit.ObjectRef.APIGroup, tt.audit.ObjectRef.Resource) {
				act, _, err := activityprocessor.EvaluateCompiledAuditRules(policy, auditMap, tt.audit, driftResolver)
				if err != nil {
					prodErrors++
					continue
				}
				if act != nil {
					prod, prodPolicy = act, policy.Name
					break
				}
			}

			res, err := ev.EvaluateAudit(tt.audit, "")
			if err != nil {
				t.Fatal(err)
			}

			if res.MatchedPolicy != prodPolicy || prodPolicy != tt.wantPolicy {
				t.Fatalf("matched policy: production=%q tool=%q want=%q", prodPolicy, res.MatchedPolicy, tt.wantPolicy)
			}
			if res.MatchedRule != tt.wantRule {
				t.Errorf("tool matched rule %q, want %q", res.MatchedRule, tt.wantRule)
			}
			if len(res.Errors) != prodErrors || prodErrors != tt.wantErrors {
				t.Errorf("errors: production=%d tool=%+v want=%d", prodErrors, res.Errors, tt.wantErrors)
			}
			assertSameActivity(t, prod, res.Activity)
			if tt.wantLinks != nil && !reflect.DeepEqual(prod.Spec.Links, tt.wantLinks) {
				t.Errorf("production links = %+v, want %+v", prod.Spec.Links, tt.wantLinks)
			}

			skipped := false
			for _, w := range res.Warnings {
				skipped = skipped || (w.Type == policytest.WarningRuleSkipped && w.Rule == "broken")
			}
			if !skipped {
				t.Errorf("tool should warn that compile-broken rule %q was skipped: %+v", "broken", res.Warnings)
			}
		})
	}
}

func mustAuditMap(t *testing.T, a *auditv1.Event) map[string]any {
	t.Helper()
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPolicyTestEventDrift(t *testing.T) {
	event := func(reason string, subjectKey string, subject map[string]any) map[string]any {
		return map[string]any{
			"metadata":            map[string]any{"uid": "event-1"},
			"eventTime":           "2024-01-01T00:00:00Z",
			"reason":              reason,
			"reportingController": "edge-controller",
			subjectKey:            subject,
		}
	}
	proxy := map[string]any{
		"apiVersion": "networking.datumapis.com/v1alpha",
		"kind":       "HTTPProxy",
		"name":       "edge",
		"namespace":  "default",
		"uid":        "proxy-1",
	}
	pod := map[string]any{"apiVersion": "v1", "kind": "Pod", "name": "web-0", "namespace": "default"}

	tests := []struct {
		name        string
		event       map[string]any
		wantPolicy  string
		wantRule    string
		wantSkipped string
		wantErr     bool
		wantLinks   []v1alpha1.ActivityLink
	}{
		{
			name:        "link with apiVersion only: live path leaves apiGroup and apiVersion empty",
			event:       event("Programmed", "regarding", proxy),
			wantPolicy:  "a-proxies",
			wantRule:    "programmed",
			wantSkipped: "by-label",
			wantLinks: []v1alpha1.ActivityLink{
				{Marker: "edge", Resource: v1alpha1.ActivityResource{Kind: "HTTPProxy", Name: "edge", Namespace: "default", UID: "proxy-1"}},
			},
		},
		{
			name:        "match error in first policy skips the rule; second policy matches",
			event:       event("Accepted", "regarding", proxy),
			wantPolicy:  "b-proxies",
			wantRule:    "accepted",
			wantSkipped: "by-label",
		},
		{
			name:        "summary error stops evaluation",
			event:       event("Broken", "regarding", proxy),
			wantSkipped: "by-label",
			wantErr:     true,
		},
		{
			name:       "legacy involvedObject with actor-style link",
			event:      event("Scheduled", "involvedObject", pod),
			wantPolicy: "pods",
			wantRule:   "scheduled",
			wantLinks: []v1alpha1.ActivityLink{
				{Marker: "web-0", Resource: v1alpha1.ActivityResource{Kind: "Pod", Name: "web-0", Namespace: "default"}},
				{Marker: "edge-controller", Resource: v1alpha1.ActivityResource{Name: "edge-controller"}},
			},
		},
		{
			name:  "no match",
			event: event("Unrelated", "regarding", pod),
		},
	}

	cache := productionCache(t)
	ev := toolEvaluator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Production: EventProcessor.processMessage without NATS.
			involved := processor.ResolveInvolvedObject(tt.event)
			kind, apiGroup := processor.ResolveKindAndAPIGroupForTest(involved)
			normalized := processor.NormalizeEventMapForTest(tt.event, involved)
			matched, prodErr := cache.MatchEvent(apiGroup, kind, normalized)
			var prod *v1alpha1.Activity
			prodPolicy := ""
			if matched != nil {
				prod = processor.BuildEventActivityForTest(tt.event, matched)
				prodPolicy = matched.PolicyName
			}

			_, toolGroup, toolKind := policytest.EventTarget(tt.event)
			if toolGroup != apiGroup || toolKind != kind {
				t.Fatalf("routing differs: production=%s/%s tool=%s/%s", apiGroup, kind, toolGroup, toolKind)
			}

			res, err := ev.EvaluateEvent(tt.event, "")
			if err != nil {
				t.Fatal(err)
			}

			if (prodErr != nil) != tt.wantErr || (len(res.Errors) > 0) != tt.wantErr {
				t.Fatalf("errors: production=%v tool=%+v wantErr=%v", prodErr, res.Errors, tt.wantErr)
			}
			if res.MatchedPolicy != prodPolicy || prodPolicy != tt.wantPolicy {
				t.Fatalf("matched policy: production=%q tool=%q want=%q", prodPolicy, res.MatchedPolicy, tt.wantPolicy)
			}
			if res.MatchedRule != tt.wantRule {
				t.Errorf("tool matched rule %q, want %q", res.MatchedRule, tt.wantRule)
			}
			if matched != nil && matched.Summary != res.Activity.Spec.Summary {
				t.Errorf("summary: production=%q tool=%q", matched.Summary, res.Activity.Spec.Summary)
			}
			assertSameActivity(t, prod, res.Activity)
			if tt.wantLinks != nil && !reflect.DeepEqual(prod.Spec.Links, tt.wantLinks) {
				t.Errorf("production links = %+v, want %+v", prod.Spec.Links, tt.wantLinks)
			}

			var skipped []string
			for _, w := range res.Warnings {
				if w.Type == policytest.WarningRuleSkipped {
					skipped = append(skipped, w.Rule)
				}
			}
			if tt.wantSkipped != "" && !strings.Contains(strings.Join(skipped, ","), tt.wantSkipped) {
				t.Errorf("tool should warn that rule %q was skipped, got %v", tt.wantSkipped, skipped)
			}
		})
	}
}
