package reindex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// staticResolver resolves a single user by metadata.name.
type staticResolver struct{ user processor.UserInfo }

func (s staticResolver) LookupByName(name string) (processor.UserInfo, bool) {
	return s.user, name == s.user.Name
}
func (staticResolver) LookupByUID(string) (processor.UserInfo, bool) {
	return processor.UserInfo{}, false
}
func (staticResolver) LookupByEmail(string) (processor.UserInfo, bool) {
	return processor.UserInfo{}, false
}

// TestEvaluateAuditBatch_EnrichesActor asserts re-indexing uses the same
// display-name enrichment as the live processor, so history can be backfilled.
func TestEvaluateAuditBatch_EnrichesActor(t *testing.T) {
	policy := &v1alpha1.ActivityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "proxies"},
		Spec: v1alpha1.ActivityPolicySpec{
			Resource: v1alpha1.ActivityPolicyResource{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy"},
			AuditRules: []v1alpha1.ActivityPolicyRule{{
				Name:    "create",
				Match:   `audit.verb == "create"`,
				Summary: `{{ actor }} created HTTPProxy {{ audit.objectRef.name }}`,
			}},
		},
	}
	cache := activityprocessor.NewPolicyCache()
	if err := cache.Add(policy, "httpproxies"); err != nil {
		t.Fatalf("add policy: %v", err)
	}

	r := &Reindexer{
		policyCache:     cache,
		staffActivities: noStoredStaff,
		Enrichment: processor.Enrichment{Users: staticResolver{user: processor.UserInfo{
			Name:      "340583683847098197",
			GivenName: "Smith", FamilyName: "Nelson", AvatarURL: "https://a.example/s.png",
		}}},
	}

	audit := &auditv1.Event{
		AuditID:   types.UID("audit-1"),
		Verb:      "create",
		UserAgent: "datumctl/v0.9.0 (darwin/arm64) kubernetes/abc",
		User:      authnv1.UserInfo{Username: "smith@datum.net", UID: "340583683847098197"},
		ObjectRef: &auditv1.ObjectReference{
			APIGroup: "networking.datumapis.com", Resource: "httpproxies", Name: "api-gateway",
		},
	}

	activities, err := r.evaluateAuditBatch(context.Background(), []*auditv1.Event{audit})
	if err != nil {
		t.Fatalf("evaluateAuditBatch: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("len(activities) = %d, want 1", len(activities))
	}
	a := activities[0]
	if a.Spec.Actor.DisplayName != "Smith Nelson" || a.Spec.Actor.AvatarURL != "https://a.example/s.png" {
		t.Errorf("actor = %+v, want display name and avatar", a.Spec.Actor)
	}
	if a.Spec.Interface == nil || a.Spec.Interface.Type != "cli" || a.Spec.Interface.Client != "datumctl" {
		t.Errorf("Interface = %+v, want cli/datumctl (backfilled on re-index)", a.Spec.Interface)
	}
	if want := "smith@datum.net created HTTPProxy api-gateway"; a.Spec.Summary != want {
		t.Errorf("Summary = %q, want %q (summaries are never rewritten)", a.Spec.Summary, want)
	}
	if len(a.Spec.Links) != 1 || a.Spec.Links[0].Resource.Kind != "User" ||
		a.Spec.Links[0].Resource.Name != "340583683847098197" || a.Spec.Links[0].DisplayName != "Smith Nelson" {
		t.Errorf("links = %+v, want one enriched iam User link for the actor", a.Spec.Links)
	}
}

type alwaysStaff struct{}

func (alwaysStaff) ActsAsSupport(processor.StaffCandidate, v1alpha1.ActivityTenant) bool { return true }

// Re-indexing applies the same staff disclosure as the live processor.
func TestEvaluateAuditBatch_StaffShownAsDatumSupport(t *testing.T) {
	policy := &v1alpha1.ActivityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "proxies"},
		Spec: v1alpha1.ActivityPolicySpec{
			Resource: v1alpha1.ActivityPolicyResource{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy"},
			AuditRules: []v1alpha1.ActivityPolicyRule{{
				Name: "create", Match: `audit.verb == "create"`,
				Summary: `{{ actor }} created HTTPProxy {{ audit.objectRef.name }}`,
			}},
		},
	}
	cache := activityprocessor.NewPolicyCache()
	if err := cache.Add(policy, "httpproxies"); err != nil {
		t.Fatalf("add policy: %v", err)
	}
	r := &Reindexer{policyCache: cache, Enrichment: processor.Enrichment{Staff: alwaysStaff{}}, staffActivities: noStoredStaff}
	audit := &auditv1.Event{
		AuditID:   types.UID("audit-2"),
		Verb:      "create",
		User:      authnv1.UserInfo{Username: "sam@datum.net", UID: "sam-sub"},
		ObjectRef: &auditv1.ObjectReference{APIGroup: "networking.datumapis.com", Resource: "httpproxies", Name: "api"},
	}
	activities, err := r.evaluateAuditBatch(context.Background(), []*auditv1.Event{audit})
	if err != nil || len(activities) != 1 {
		t.Fatalf("evaluateAuditBatch = %v, %v", activities, err)
	}
	a := activities[0]
	if a.Spec.Summary != "Datum Support created HTTPProxy api" || a.Spec.Actor.Name != processor.SupportActorName || a.Spec.Actor.Email != "" {
		t.Errorf("activity = %+v", a.Spec)
	}
}

func noStoredStaff(context.Context, []string, time.Time, time.Time) (map[string]bool, error) {
	return nil, nil
}

type neverStaff struct{}

func (neverStaff) ActsAsSupport(processor.StaffCandidate, v1alpha1.ActivityTenant) bool { return false }

func staffBatchFixture(t *testing.T) (*activityprocessor.PolicyCache, *auditv1.Event) {
	t.Helper()
	policy := &v1alpha1.ActivityPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "proxies"},
		Spec: v1alpha1.ActivityPolicySpec{
			Resource: v1alpha1.ActivityPolicyResource{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy"},
			AuditRules: []v1alpha1.ActivityPolicyRule{{
				Name: "create", Match: `audit.verb == "create"`,
				Summary: `{{ actor }} created HTTPProxy {{ audit.objectRef.name }}`,
			}},
		},
	}
	cache := activityprocessor.NewPolicyCache()
	if err := cache.Add(policy, "httpproxies"); err != nil {
		t.Fatalf("add policy: %v", err)
	}
	return cache, &auditv1.Event{
		AuditID:                  types.UID("audit-3"),
		Verb:                     "create",
		RequestReceivedTimestamp: metav1.NewMicroTime(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)),
		User:                     authnv1.UserInfo{Username: "sam@datum.net", UID: "sam-sub"},
		ObjectRef:                &auditv1.ObjectReference{APIGroup: "networking.datumapis.com", Resource: "httpproxies", Name: "api"},
	}
}

// An activity already stored as staff access stays "Datum Support" on
// re-index, even though the staff member is no longer classified as staff
// (for example because they have joined the organization).
func TestEvaluateAuditBatch_StoredStaffActivityStaysRedacted(t *testing.T) {
	cache, audit := staffBatchFixture(t)
	name := processor.AuditActivityName("audit-3", "networking.datumapis.com", "HTTPProxy")
	var gotNames []string
	var gotStart, gotEnd time.Time
	r := &Reindexer{
		policyCache: cache,
		Enrichment:  processor.Enrichment{Staff: neverStaff{}},
		staffActivities: func(_ context.Context, names []string, start, end time.Time) (map[string]bool, error) {
			gotNames, gotStart, gotEnd = names, start, end
			return map[string]bool{name: true}, nil
		},
	}

	activities, err := r.evaluateAuditBatch(context.Background(), []*auditv1.Event{audit})
	if err != nil || len(activities) != 1 {
		t.Fatalf("evaluateAuditBatch = %v, %v", activities, err)
	}
	if len(gotNames) != 1 || gotNames[0] != name {
		t.Errorf("looked up %v, want [%s]", gotNames, name)
	}
	ts := audit.RequestReceivedTimestamp.Time
	if !gotStart.Before(ts) || !gotEnd.After(ts) {
		t.Errorf("lookup window [%s, %s] does not cover %s", gotStart, gotEnd, ts)
	}
	a := activities[0]
	if a.Name != name || a.Spec.Actor.Type != processor.ActorTypeStaff || a.Spec.Summary != "Datum Support created HTTPProxy api" {
		t.Errorf("activity = %s %+v, want it kept as Datum Support", a.Name, a.Spec)
	}
}

// The converse: an activity not stored as staff, but detected as staff now,
// is redacted.
func TestEvaluateAuditBatch_NewlyDetectedStaffIsRedacted(t *testing.T) {
	cache, audit := staffBatchFixture(t)
	r := &Reindexer{policyCache: cache, Enrichment: processor.Enrichment{Staff: alwaysStaff{}}, staffActivities: noStoredStaff}
	activities, err := r.evaluateAuditBatch(context.Background(), []*auditv1.Event{audit})
	if err != nil || len(activities) != 1 || activities[0].Spec.Actor.Type != processor.ActorTypeStaff {
		t.Fatalf("evaluateAuditBatch = %v, %v; want a staff actor", activities, err)
	}
}

// Neither stored nor detected as staff: the actor is shown normally.
func TestEvaluateAuditBatch_NonStaffUnchanged(t *testing.T) {
	cache, audit := staffBatchFixture(t)
	r := &Reindexer{policyCache: cache, Enrichment: processor.Enrichment{Staff: neverStaff{}}, staffActivities: noStoredStaff}
	activities, err := r.evaluateAuditBatch(context.Background(), []*auditv1.Event{audit})
	if err != nil || len(activities) != 1 || activities[0].Spec.Actor.Name != "sam@datum.net" {
		t.Fatalf("evaluateAuditBatch = %v, %v; want the real actor", activities, err)
	}
}

// If the lookup fails the batch fails: it must not silently un-redact.
func TestEvaluateAuditBatch_StaffLookupFailureFailsBatch(t *testing.T) {
	cache, audit := staffBatchFixture(t)
	r := &Reindexer{
		policyCache: cache,
		Enrichment:  processor.Enrichment{Staff: neverStaff{}},
		staffActivities: func(context.Context, []string, time.Time, time.Time) (map[string]bool, error) {
			return nil, errors.New("query failed")
		},
	}
	if _, err := r.evaluateAuditBatch(context.Background(), []*auditv1.Event{audit}); err == nil {
		t.Fatal("expected the batch to fail when stored staff activities cannot be looked up")
	}
}

func TestStaffActivityFilter_CompilesToClickHouse(t *testing.T) {
	sql, args, err := cel.ConvertActivityToClickHouseSQL(context.Background(), staffActivityFilter([]string{"act-aaa", "act-bbb"}))
	if err != nil {
		t.Fatalf("filter does not compile: %v", err)
	}
	if !strings.Contains(sql, "actor_type") || !strings.Contains(sql, "activity_name") {
		t.Errorf("sql = %q", sql)
	}
	if len(args) != 3 || args[0] != "staff" {
		t.Errorf("args = %v, want staff and the two names bound as parameters", args)
	}
}

// The reviewer's probe: a single filter over thousands of names exceeds the
// CEL size limit, so lookups are chunked; a full chunk must still compile.
func TestStaffActivityFilter_FullChunkCompiles(t *testing.T) {
	names := make([]string, staffLookupChunk)
	for i := range names {
		names[i] = processor.AuditActivityName(fmt.Sprintf("id-%d", i), "g", "K")
	}
	if _, _, err := cel.ConvertActivityToClickHouseSQL(context.Background(), staffActivityFilter(names)); err != nil {
		t.Fatalf("a %d-name filter does not compile: %v", staffLookupChunk, err)
	}
}

type lookupCall struct {
	names      int
	start, end time.Time
}

func batchOf(t *testing.T, cache *activityprocessor.PolicyCache, n int, step time.Duration, base time.Time) []*auditv1.Event {
	t.Helper()
	batch := make([]*auditv1.Event, n)
	for i := range batch {
		batch[i] = &auditv1.Event{
			AuditID:                  types.UID(fmt.Sprintf("a-%d", i)),
			Verb:                     "create",
			RequestReceivedTimestamp: metav1.NewMicroTime(base.Add(time.Duration(i) * step)),
			User:                     authnv1.UserInfo{Username: "c@acme.com", UID: "cust"},
			ObjectRef:                &auditv1.ObjectReference{APIGroup: "networking.datumapis.com", Resource: "httpproxies", Name: "p"},
		}
	}
	return batch
}

func TestStoredStaffActivities_ChunksAndWindows(t *testing.T) {
	cache, _ := staffBatchFixture(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var calls []lookupCall
	r := &Reindexer{policyCache: cache, staffActivities: func(_ context.Context, names []string, start, end time.Time) (map[string]bool, error) {
		calls = append(calls, lookupCall{len(names), start, end})
		return nil, nil
	}}

	// 2,500 events in one day: three queries of at most 1,000 names.
	if _, err := r.storedStaffActivities(context.Background(), batchOf(t, cache, 2500, time.Second, base)); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[0].names != 1000 || calls[1].names != 1000 || calls[2].names != 500 {
		t.Errorf("chunked calls = %+v", calls)
	}

	// 70 days of events: no query spans more than 30 days.
	calls = nil
	if _, err := r.storedStaffActivities(context.Background(), batchOf(t, cache, 71, 24*time.Hour, base)); err != nil {
		t.Fatal(err)
	}
	if len(calls) < 3 {
		t.Errorf("70 days looked up in %d queries, want at least 3", len(calls))
	}
	total := 0
	for _, c := range calls {
		total += c.names
		if c.end.Sub(c.start) > staffLookupWindow {
			t.Errorf("query window %s exceeds %s", c.end.Sub(c.start), staffLookupWindow)
		}
	}
	if total != 71 {
		t.Errorf("looked up %d names, want 71", total)
	}

	// Events without a timestamp are skipped rather than widening the window.
	calls = nil
	batch := batchOf(t, cache, 2, time.Hour, base)
	batch[0].RequestReceivedTimestamp = metav1.MicroTime{}
	if _, err := r.storedStaffActivities(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].names != 1 || calls[0].start.Year() != 2026 {
		t.Errorf("calls with a zero timestamp = %+v", calls)
	}
}

func TestStoredStaffActivities_AnyChunkFailureFailsBatch(t *testing.T) {
	cache, _ := staffBatchFixture(t)
	n := 0
	r := &Reindexer{policyCache: cache, staffActivities: func(context.Context, []string, time.Time, time.Time) (map[string]bool, error) {
		n++
		if n == 2 {
			return nil, errors.New("second chunk failed")
		}
		return nil, nil
	}}
	batch := batchOf(t, cache, 1500, time.Second, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := r.storedStaffActivities(context.Background(), batch); err == nil {
		t.Fatal("expected a chunk failure to fail the batch")
	}
}
