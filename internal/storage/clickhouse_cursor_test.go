package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func TestCursorEncodeDecodeRoundtrip(t *testing.T) {
	timestamp := time.Now()
	auditID := "abc-123-def-456"
	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	// Encode
	cursor := encodeCursor(timestamp, auditID, spec)

	// Decode with same spec should succeed
	decodedTime, decodedID, err := decodeCursor(cursor, spec)
	if err != nil {
		t.Fatalf("decodeCursor failed: %v", err)
	}

	// Verify timestamp (with some tolerance for serialization)
	if !decodedTime.Equal(timestamp) {
		t.Errorf("timestamp mismatch: got %v, want %v", decodedTime, timestamp)
	}

	// Verify audit ID
	if decodedID != auditID {
		t.Errorf("auditID mismatch: got %s, want %s", decodedID, auditID)
	}
}

func TestCursorValidation_FilterChanged(t *testing.T) {
	timestamp := time.Now()
	auditID := "abc-123"

	// Original spec
	originalSpec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	// Encode with original spec
	cursor := encodeCursor(timestamp, auditID, originalSpec)

	// Try to decode with modified filter
	modifiedSpec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'create'", // Changed!
		Limit:     100,
	}

	_, _, err := decodeCursor(cursor, modifiedSpec)
	if err == nil {
		t.Fatal("expected error when filter changed, got nil")
	}

	if !strings.Contains(err.Error(), "query parameters changed") {
		t.Errorf("expected 'query parameters changed' error, got: %v", err)
	}
}

func TestCursorValidation_StartTimeChanged(t *testing.T) {
	timestamp := time.Now()
	auditID := "abc-123"

	originalSpec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	cursor := encodeCursor(timestamp, auditID, originalSpec)

	// Change startTime
	modifiedSpec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-05T00:00:00Z", // Changed!
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	_, _, err := decodeCursor(cursor, modifiedSpec)
	if err == nil {
		t.Fatal("expected error when startTime changed, got nil")
	}

	if !strings.Contains(err.Error(), "query parameters changed") {
		t.Errorf("expected 'query parameters changed' error, got: %v", err)
	}
}

func TestCursorValidation_EndTimeChanged(t *testing.T) {
	timestamp := time.Now()
	auditID := "abc-123"

	originalSpec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	cursor := encodeCursor(timestamp, auditID, originalSpec)

	// Change endTime
	modifiedSpec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-10T00:00:00Z", // Changed!
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	_, _, err := decodeCursor(cursor, modifiedSpec)
	if err == nil {
		t.Fatal("expected error when endTime changed, got nil")
	}
}

func TestCursorValidation_LimitChanged(t *testing.T) {
	timestamp := time.Now()
	auditID := "abc-123"

	originalSpec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	cursor := encodeCursor(timestamp, auditID, originalSpec)

	// Change limit
	modifiedSpec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     500, // Changed!
	}

	_, _, err := decodeCursor(cursor, modifiedSpec)
	if err == nil {
		t.Fatal("expected error when limit changed, got nil")
	}
}

func TestCursorValidation_AllParamsSame(t *testing.T) {
	timestamp := time.Now()
	auditID := "abc-123"

	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete' && objectRef.namespace == 'prod'",
		Limit:     250,
	}

	cursor := encodeCursor(timestamp, auditID, spec)

	// Decode with identical spec - should succeed
	_, _, err := decodeCursor(cursor, spec)
	if err != nil {
		t.Fatalf("unexpected error with identical spec: %v", err)
	}
}

func TestCursorValidation_EmptyFilter(t *testing.T) {
	timestamp := time.Now()
	auditID := "abc-123"

	// Spec with no filter
	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "",
		Limit:     100,
	}

	cursor := encodeCursor(timestamp, auditID, spec)

	// Should work with same empty filter
	_, _, err := decodeCursor(cursor, spec)
	if err != nil {
		t.Fatalf("unexpected error with empty filter: %v", err)
	}

	// Should fail if filter is added
	specWithFilter := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'", // Added!
		Limit:     100,
	}

	_, _, err = decodeCursor(cursor, specWithFilter)
	if err == nil {
		t.Fatal("expected error when filter added, got nil")
	}
}

func TestHashQueryParams_Deterministic(t *testing.T) {
	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	// Hash should be deterministic
	hash1 := hashQueryParams(spec)
	hash2 := hashQueryParams(spec)

	if hash1 != hash2 {
		t.Errorf("hash not deterministic: %s != %s", hash1, hash2)
	}
}

func TestHashQueryParams_DifferentForDifferentParams(t *testing.T) {
	spec1 := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	spec2 := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'create'", // Different!
		Limit:     100,
	}

	hash1 := hashQueryParams(spec1)
	hash2 := hashQueryParams(spec2)

	if hash1 == hash2 {
		t.Error("expected different hashes for different specs")
	}
}

func TestHashQueryParams_IgnoresContinueAfter(t *testing.T) {
	spec1 := v1alpha1.AuditLogQuerySpec{
		StartTime:     "2024-01-01T00:00:00Z",
		EndTime:       "2024-01-02T00:00:00Z",
		Filter:        "verb == 'delete'",
		Limit:         100,
		Continue: "", // Empty
	}

	spec2 := v1alpha1.AuditLogQuerySpec{
		StartTime:     "2024-01-01T00:00:00Z",
		EndTime:       "2024-01-02T00:00:00Z",
		Filter:        "verb == 'delete'",
		Limit:         100,
		Continue: "some-cursor-value", // Different!
	}

	hash1 := hashQueryParams(spec1)
	hash2 := hashQueryParams(spec2)

	if hash1 != hash2 {
		t.Error("hash should ignore continueAfter field")
	}
}

func TestDecodeCursor_InvalidBase64(t *testing.T) {
	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
	}

	_, _, err := decodeCursor("not-valid-base64!@#$", spec)
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}

	if !strings.Contains(err.Error(), "cannot decode pagination cursor") {
		t.Errorf("expected 'cannot decode pagination cursor' error, got: %v", err)
	}
}

func TestDecodeCursor_InvalidJSON(t *testing.T) {
	// Valid base64 but invalid JSON and invalid legacy format
	invalidCursor := "aW52YWxpZGpzb24=" // base64("invalidjson")

	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
	}

	_, _, err := decodeCursor(invalidCursor, spec)
	if err == nil {
		t.Fatal("expected error for invalid cursor format, got nil")
	}
}

func TestCursorExpiration_ValidCursor(t *testing.T) {
	timestamp := time.Now()
	auditID := "abc-123"
	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	// Encode a fresh cursor
	cursor := encodeCursor(timestamp, auditID, spec)

	// Should decode successfully (cursor is fresh)
	_, _, err := decodeCursor(cursor, spec)
	if err != nil {
		t.Fatalf("expected fresh cursor to be valid, got error: %v", err)
	}
}

func TestCursorExpiration_ExpiredCursor(t *testing.T) {
	// Create an expired cursor by manually crafting one with old IssuedAt
	expiredTime := time.Now().Add(-2 * time.Hour) // 2 hours ago (older than 1 hour TTL)

	data := cursorData{
		Timestamp: time.Now(),
		AuditID:   "abc-123",
		QueryHash: hashQueryParams(v1alpha1.AuditLogQuerySpec{
			StartTime: "2024-01-01T00:00:00Z",
			EndTime:   "2024-01-02T00:00:00Z",
			Filter:    "verb == 'delete'",
			Limit:     100,
		}),
		IssuedAt: expiredTime,
	}

	// Manually encode the cursor
	jsonData, _ := json.Marshal(data)
	expiredCursor := base64.URLEncoding.EncodeToString(jsonData)

	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	// Should fail with expiration error
	_, _, err := decodeCursor(expiredCursor, spec)
	if err == nil {
		t.Fatal("expected error for expired cursor, got nil")
	}

	if !strings.Contains(err.Error(), "cursor expired") {
		t.Errorf("expected 'cursor expired' error, got: %v", err)
	}
}

func TestCursorExpiration_EdgeCase_ExactlyAtTTL(t *testing.T) {
	// Create a cursor that's exactly at the TTL boundary
	exactlyAtTTL := time.Now().Add(-cursorTTL)

	data := cursorData{
		Timestamp: time.Now(),
		AuditID:   "abc-123",
		QueryHash: hashQueryParams(v1alpha1.AuditLogQuerySpec{
			StartTime: "2024-01-01T00:00:00Z",
			EndTime:   "2024-01-02T00:00:00Z",
			Filter:    "verb == 'delete'",
			Limit:     100,
		}),
		IssuedAt: exactlyAtTTL,
	}

	jsonData, _ := json.Marshal(data)
	cursor := base64.URLEncoding.EncodeToString(jsonData)

	spec := v1alpha1.AuditLogQuerySpec{
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
		Filter:    "verb == 'delete'",
		Limit:     100,
	}

	// Should fail (age > cursorTTL, even if just barely)
	_, _, err := decodeCursor(cursor, spec)
	if err == nil {
		t.Fatal("expected error for cursor at TTL boundary, got nil")
	}
}

const lastActivityJSON = `{"metadata":{"name":"act-df69938cba05","creationTimestamp":"2026-10-08T14:12:06Z"}}`

func TestActivityCursorEncodeDecodeRoundtrip(t *testing.T) {
	spec := ActivityQuerySpec{StartTime: "now-30d", Filter: "spec.resource.kind == 'Instance'", Limit: 100}

	gotTime, gotName, err := decodeActivityCursor(encodeActivityCursor(lastActivityJSON, spec), spec)
	if err != nil {
		t.Fatalf("decodeActivityCursor failed: %v", err)
	}
	if want := time.Date(2026, 10, 8, 14, 12, 6, 0, time.UTC); !gotTime.Equal(want) {
		t.Errorf("timestamp = %v, want %v", gotTime, want)
	}
	if want := "act-df69938cba05"; gotName != want {
		t.Errorf("name = %q, want %q", gotName, want)
	}
}

// Tokens issued before the activity_name tie-breaker carry a resource UID.
func TestDecodeActivityCursor_LegacyToken(t *testing.T) {
	spec := ActivityQuerySpec{StartTime: "now-30d", Limit: 100}
	legacy, _ := json.Marshal(map[string]interface{}{
		"t": time.Now(),
		"r": "resource-uid",
		"h": hashActivityQueryParams(spec),
		"i": time.Now(),
	})

	_, _, err := decodeActivityCursor(base64.URLEncoding.EncodeToString(legacy), spec)
	if err == nil {
		t.Fatal("expected error for legacy cursor, got nil")
	}
	if !strings.Contains(err.Error(), "continue token is invalid") {
		t.Errorf("expected 'continue token is invalid' error, got: %v", err)
	}
}

func TestBuildActivityQuery_TieBreakMatchesCursor(t *testing.T) {
	s := &ClickHouseStorage{config: ClickHouseConfig{Database: "audit", MaxPageSize: 1000}}
	spec := ActivityQuerySpec{StartTime: "now-30d", Limit: 100}
	spec.Continue = encodeActivityCursor(lastActivityJSON, spec)

	for _, scope := range []ScopeContext{
		{Type: "platform"},
		{Type: "Project", Name: "p"},
		{Type: "Organization", Name: "o"},
		{Type: "User", Name: "u"},
	} {
		t.Run(scope.Type, func(t *testing.T) {
			query, args, err := s.buildActivityQuery(context.Background(), spec, scope)
			if err != nil {
				t.Fatalf("buildActivityQuery failed: %v", err)
			}
			if !strings.Contains(query, "(timestamp = ? AND activity_name < ?)") {
				t.Errorf("cursor predicate does not tie-break on activity_name: %s", query)
			}
			if !strings.Contains(query, "ORDER BY toStartOfHour(timestamp) DESC, timestamp DESC, activity_name DESC LIMIT") {
				t.Errorf("ORDER BY does not tie-break on activity_name: %s", query)
			}
			if got, want := args[len(args)-1], "act-df69938cba05"; got != want {
				t.Errorf("last arg = %v, want %q", got, want)
			}
		})
	}
}
