package processor_test

// Corpus drift guard: replays every case in test/policies against the example
// policies through the production entry points and through activitytool, and
// requires the same matched policy and the same Activity. The YAML suite then
// proves parity for every example rule, not just the hand-picked cases in
// policytest_drift_test.go.

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	"go.miloapis.com/activity/pkg/policytest"
)

const (
	corpusPolicies = "../../examples/basic-kubernetes"
	corpusTests    = "../../test/policies"
)

var corpusTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// corpusResourceKinds is the RESTMapper stand-in for the example policies.
var corpusResourceKinds = map[string]map[string]string{
	"": {
		"pods": "Pod", "services": "Service", "configmaps": "ConfigMap", "secrets": "Secret",
		"namespaces": "Namespace", "serviceaccounts": "ServiceAccount", "nodes": "Node",
		"persistentvolumeclaims": "PersistentVolumeClaim", "persistentvolumes": "PersistentVolume",
	},
	"apps": {
		"deployments": "Deployment", "replicasets": "ReplicaSet", "statefulsets": "StatefulSet",
		"daemonsets": "DaemonSet", "controllerrevisions": "ControllerRevision",
	},
	"batch":                     {"jobs": "Job", "cronjobs": "CronJob"},
	"networking.k8s.io":         {"ingresses": "Ingress"},
	"rbac.authorization.k8s.io": {"clusterroles": "ClusterRole", "clusterrolebindings": "ClusterRoleBinding", "roles": "Role", "rolebindings": "RoleBinding"},
	"extensions":                {"deployments": "Deployment"},
}

func corpusResolver(apiGroup, resource string) (string, error) {
	if kind := corpusResourceKinds[apiGroup][resource]; kind != "" {
		return kind, nil
	}
	return "", errors.New("unknown resource")
}

func corpusResource(apiGroup, kind string) string {
	for resource, k := range corpusResourceKinds[apiGroup] {
		if k == kind {
			return resource
		}
	}
	return ""
}

func TestPolicyTestCorpusDrift(t *testing.T) {
	loaded, issues := policytest.LoadPoliciesFromPaths([]string{corpusPolicies})
	if len(issues) > 0 {
		t.Fatalf("loading example policies: %+v", issues)
	}

	cache := activityprocessor.NewPolicyCache()
	for _, p := range loaded {
		resource := corpusResource(p.Policy.Spec.Resource.APIGroup, p.Policy.Spec.Resource.Kind)
		if resource == "" {
			t.Fatalf("add %s/%s to corpusResourceKinds", p.Policy.Spec.Resource.APIGroup, p.Policy.Spec.Resource.Kind)
		}
		if err := cache.Add(p.Policy, resource); err != nil {
			t.Fatal(err)
		}
	}
	ev := policytest.NewEvaluator(loaded, policytest.EvaluatorOptions{ResourceKinds: corpusResourceKinds})

	files, err := policytest.DiscoverTestFiles([]string{corpusTests})
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for _, file := range files {
		for _, group := range readTestGroups(t, file) {
			for _, tc := range group.Tests {
				cases++
				t.Run(filepath.Base(file)+"/"+tc.Name, func(t *testing.T) {
					// Test inputs carry no timestamp, so both sides would stamp
					// time.Now(). Pin one so the Activities are comparable.
					if tc.Audit != nil && tc.Audit.RequestReceivedTimestamp.IsZero() {
						tc.Audit.RequestReceivedTimestamp = metav1.NewMicroTime(corpusTime)
					}
					if tc.Event != nil && tc.Event["eventTime"] == nil {
						tc.Event["eventTime"] = corpusTime.Format(time.RFC3339)
					}
					switch {
					case tc.Audit != nil:
						compareAudit(t, cache, ev, tc)
					case tc.Event != nil:
						compareEvent(t, cache, ev, tc)
					default:
						t.Fatal("test case has neither audit nor event")
					}
				})
			}
		}
	}
	if cases < 100 {
		t.Fatalf("expected the full example suite, found only %d cases", cases)
	}
}

func compareAudit(t *testing.T, cache *activityprocessor.PolicyCache, ev *policytest.Evaluator, tc policytest.TestCase) {
	t.Helper()
	// Production: the processor's audit loop (processor.go) over the cache.
	auditMap := mustAuditMap(t, tc.Audit)
	var prod *v1alpha1.Activity
	prodPolicy := ""
	for _, policy := range cache.Get(tc.Audit.ObjectRef.APIGroup, tc.Audit.ObjectRef.Resource) {
		act, _, err := activityprocessor.EvaluateCompiledAuditRules(policy, auditMap, tc.Audit, corpusResolver)
		if err != nil {
			continue
		}
		if act != nil {
			prod, prodPolicy = act, policy.Name
			break
		}
	}

	res, err := ev.EvaluateAudit(tc.Audit, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.MatchedPolicy != prodPolicy {
		t.Fatalf("matched policy: production=%q tool=%q", prodPolicy, res.MatchedPolicy)
	}
	assertSameActivity(t, prod, res.Activity)
}

func compareEvent(t *testing.T, cache *activityprocessor.PolicyCache, ev *policytest.Evaluator, tc policytest.TestCase) {
	t.Helper()
	// Production: EventProcessor.processMessage without NATS.
	involved := processor.ResolveInvolvedObject(tc.Event)
	kind, apiGroup := processor.ResolveKindAndAPIGroupForTest(involved)
	normalized := processor.NormalizeEventMapForTest(tc.Event, involved)
	matched, prodErr := cache.MatchEvent(apiGroup, kind, normalized)
	var prod *v1alpha1.Activity
	prodPolicy := ""
	if matched != nil {
		prod = processor.BuildEventActivityForTest(tc.Event, matched)
		prodPolicy = matched.PolicyName
	}

	res, err := ev.EvaluateEvent(tc.Event, "")
	if err != nil {
		t.Fatal(err)
	}
	if (prodErr != nil) != (len(res.Errors) > 0) {
		t.Fatalf("errors: production=%v tool=%+v", prodErr, res.Errors)
	}
	if res.MatchedPolicy != prodPolicy {
		t.Fatalf("matched policy: production=%q tool=%q", prodPolicy, res.MatchedPolicy)
	}
	assertSameActivity(t, prod, res.Activity)
}

// readTestGroups parses every "---" document of a test file.
func readTestGroups(t *testing.T, file string) []policytest.TestFile {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	var groups []policytest.TestFile
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(doc)) == "" {
			continue
		}
		var group policytest.TestFile
		if err := sigsyaml.UnmarshalStrict(doc, &group); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		groups = append(groups, group)
	}
	return groups
}
