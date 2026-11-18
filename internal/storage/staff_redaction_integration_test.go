//go:build integration

package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// TestTenantScopeStickyRedaction_ClickHouse runs a tenant-scoped audit log
// query, including the sticky semi-join between the UUID audit_id and the
// String origin_id, against a real ClickHouse.
//
// Run with:
//
//	ACTIVITY_TEST_CLICKHOUSE_ADDR=localhost:9000 go test -tags=integration -run StickyRedaction ./internal/storage/
//
// It creates and drops a scratch database with minimal audit_logs and
// activities tables (only the columns the query uses).
func TestTenantScopeStickyRedaction_ClickHouse(t *testing.T) {
	addr := os.Getenv("ACTIVITY_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("ACTIVITY_TEST_CLICKHOUSE_ADDR not set")
	}
	ctx := context.Background()
	db := fmt.Sprintf("activity_it_%d", time.Now().UnixNano())

	s, err := NewClickHouseStorage(ClickHouseConfig{Address: addr, Database: "default", Username: os.Getenv("ACTIVITY_TEST_CLICKHOUSE_USER"),
		Password: os.Getenv("ACTIVITY_TEST_CLICKHOUSE_PASSWORD"), MaxPageSize: 100, MaxQueryWindow: 24 * time.Hour})
	if err != nil {
		t.Skipf("ClickHouse unavailable: %v", err)
	}
	defer s.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if err := s.conn.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("CREATE DATABASE " + db)
	defer func() { _ = s.conn.Exec(ctx, "DROP DATABASE "+db) }()
	exec("CREATE TABLE " + db + ".audit_logs (event_json String, audit_id UUID, user String, user_uid String, " +
		"scope_type String, scope_name String, timestamp DateTime64(3)) ENGINE = MergeTree " +
		"ORDER BY (toStartOfHour(timestamp), timestamp, scope_type, scope_name, user, audit_id)")
	exec("CREATE TABLE " + db + ".activities (origin_id String, origin_type String, actor_type String, " +
		"tenant_type String, tenant_name String, timestamp DateTime64(3)) ENGINE = MergeTree " +
		"ORDER BY (toStartOfHour(timestamp), timestamp, tenant_type, tenant_name, origin_id)")

	ts := time.Now().Add(-time.Hour).UTC()
	staffAudit := "11111111-1111-1111-1111-111111111111" // sticky: an activity recorded it as staff
	custAudit := "22222222-2222-2222-2222-222222222222"
	for _, row := range []struct{ id, user, uid string }{
		{staffAudit, "former-staff@acme.com", "u-1"},
		{custAudit, "c@acme.com", "u-2"},
	} {
		event := fmt.Sprintf(`{"auditID":%q,"verb":"delete","user":{"username":%q,"uid":%q},"requestURI":"/secret"}`, row.id, row.user, row.uid)
		exec("INSERT INTO "+db+".audit_logs VALUES (?, ?, ?, ?, 'Organization', 'acme', ?)", event, row.id, row.user, row.uid, ts)
	}
	exec("INSERT INTO "+db+".activities VALUES (?, 'audit', 'staff', 'Organization', 'acme', ?)", staffAudit, ts)

	s.config.Database = db
	// Membership says nobody is staff now: only the sticky clause applies.
	s.SetStaffScopes(fakeStaffScopes{scope: StaffScope{Redact: true, Synced: true, Domains: []string{"datum.net"}}})

	result, err := s.QueryAuditLogs(ctx, v1alpha1.AuditLogQuerySpec{StartTime: "now-1d", EndTime: "now", Limit: 10},
		ScopeContext{Type: "Organization", Name: "acme"})
	if err != nil {
		t.Fatalf("QueryAuditLogs: %v", err)
	}
	got := map[string]string{}
	for _, e := range result.Events {
		got[string(e.AuditID)] = e.User.Username + "|" + e.RequestURI
	}
	if got[staffAudit] != SupportActorName+"|" {
		t.Errorf("sticky staff event = %q, want redacted", got[staffAudit])
	}
	if got[custAudit] != "c@acme.com|/secret" {
		t.Errorf("customer event = %q, want unredacted", got[custAudit])
	}
}
