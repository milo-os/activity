package types

import (
	"strings"
	"testing"
)

func TestEventSubjectHelpers(t *testing.T) {
	if got := EventSubject("activity.federated", "cluster-dfw-1", "default"); got != "activity.federated.cluster-dfw-1.default" {
		t.Errorf("EventSubject(federated) = %q", got)
	}
	if got := EventSubject("activity.federated", "", "default"); got != "activity.federated.default" {
		t.Errorf("EventSubject(management plane) = %q", got)
	}
	if got := EventSubjectGrant("activity.federated", "cluster-dfw-1"); got != "activity.federated.cluster-dfw-1.*" {
		t.Errorf("EventSubjectGrant() = %q", got)
	}
	if got := EventInboxPrefix("cluster-dfw-1"); got != "_INBOX_cluster-dfw-1" {
		t.Errorf("EventInboxPrefix() = %q", got)
	}
	// nats.CustomInboxPrefix rejects a trailing dot and wildcards.
	for _, bad := range []string{".", ">", "*"} {
		if p := EventInboxPrefix("cluster-dfw-1"); strings.Contains(p, bad) {
			t.Errorf("EventInboxPrefix() = %q contains %q, which CustomInboxPrefix rejects", p, bad)
		}
	}
	if got := EventInboxGrant("cluster-dfw-1"); got != "_INBOX_cluster-dfw-1.>" {
		t.Errorf("EventInboxGrant() = %q", got)
	}
}

func TestValidClusterName(t *testing.T) {
	valid := []string{"us-east-1", "edge-pop-42", "a", "a1"}
	for _, name := range valid {
		if !ValidClusterName(name) {
			t.Errorf("ValidClusterName(%q) = false, want true", name)
		}
	}

	// A dot shifts the subject token count, so a dotted Karmada Cluster name
	// must be rejected; see clusterNameRE.
	invalid := []string{"a.b", "a>", "a*", "a b", "", strings.Repeat("a", 254)}
	for _, name := range invalid {
		if ValidClusterName(name) {
			t.Errorf("ValidClusterName(%q) = true, want false", name)
		}
	}
}

// TestEventInboxIsolation pins the security property behind the per-cluster
// inbox prefix: one cluster's grant must match neither another cluster's reply
// subjects nor the default "_INBOX.>" the processor keeps.
func TestEventInboxIsolation(t *testing.T) {
	// Both reply subjects nats.go generates under a custom prefix: the request
	// inbox, and the mux response subject that carries JetStream PubAcks.
	replies := func(cluster string) []string {
		p := EventInboxPrefix(cluster)
		return []string{p + ".pNXQ4o0qLcJ4nT8T", p + ".pNXQ4o0qLcJ4nT8T.xY7"}
	}

	for _, reply := range replies("us-east-1") {
		if !subjectMatches(EventInboxGrant("us-east-1"), reply) {
			t.Errorf("own grant %q does not cover %q -- PubAcks would never arrive", EventInboxGrant("us-east-1"), reply)
		}
		if subjectMatches(EventInboxGrant("us-west-1"), reply) {
			t.Errorf("grant %q matches another cluster's reply %q", EventInboxGrant("us-west-1"), reply)
		}
		if subjectMatches("_INBOX.>", reply) {
			t.Errorf("default _INBOX.> grant matches per-cluster reply %q", reply)
		}
	}

	// The converse: a per-cluster grant must not reach the default inbox space.
	if subjectMatches(EventInboxGrant("us-east-1"), "_INBOX.pNXQ4o0qLcJ4nT8T.1") {
		t.Errorf("per-cluster grant reaches the default _INBOX space")
	}
}

// subjectMatches reports whether a NATS subject filter matches subject,
// honouring the "*" single-token and ">" tail wildcards.
func subjectMatches(filter, subject string) bool {
	f := strings.Split(filter, ".")
	s := strings.Split(subject, ".")
	for i, tok := range f {
		if tok == ">" {
			return i < len(s)
		}
		if i >= len(s) {
			return false
		}
		if tok != "*" && tok != s[i] {
			return false
		}
	}
	return len(f) == len(s)
}

func TestSubjectMatches(t *testing.T) {
	cases := []struct {
		filter, subject string
		want            bool
	}{
		{"a.b", "a.b", true},
		{"a.b", "a.c", false},
		{"a.*", "a.b", true},
		{"a.*", "a.b.c", false},
		{"a.>", "a.b.c", true},
		{"a.>", "a", false},
		{"a.b", "a", false},
		{"a.b", "a.b.c", false},
	}
	for _, c := range cases {
		if got := subjectMatches(c.filter, c.subject); got != c.want {
			t.Errorf("subjectMatches(%q, %q) = %v, want %v", c.filter, c.subject, got, c.want)
		}
	}
}
