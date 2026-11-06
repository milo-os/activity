package cel

import (
	"testing"
)

func TestEvaluateAuditSummary_BareActorRegistersActorLink(t *testing.T) {
	audit := func(username string) map[string]interface{} {
		return map[string]interface{}{"user": map[string]interface{}{"username": username}}
	}

	cases := []struct {
		name        string
		template    string
		username    string
		wantSummary string
		wantLinks   []Link
	}{
		{
			name:        "human actor",
			template:    "{{ actor }} created {{actor}}",
			username:    "alice@x.com",
			wantSummary: "alice@x.com created alice@x.com",
			// Both mentions produce the same link, recorded once.
			wantLinks: []Link{
				{Marker: "alice@x.com", Resource: map[string]interface{}{"type": "user", "name": "alice@x.com"}},
			},
		},
		{
			name:        "system actor stays plain text",
			template:    "{{ actor }} reconciled",
			username:    "system:serviceaccount:ns:sa",
			wantSummary: "system:serviceaccount:ns:sa reconciled",
		},
		{
			name:        "expressions other than a bare actor are not linked",
			template:    "{{ actor != '' ? actor : 'system' }} did it",
			username:    "alice@x.com",
			wantSummary: "alice@x.com did it",
		},
		{
			name:        "empty username registers nothing",
			template:    "{{ actor }}x",
			username:    "",
			wantSummary: "x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary, links, err := EvaluateAuditSummaryMap(tc.template, audit(tc.username))
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tc.wantSummary)
			}
			if len(links) != len(tc.wantLinks) {
				t.Fatalf("links = %+v, want %+v", links, tc.wantLinks)
			}
			for i := range links {
				if links[i].Marker != tc.wantLinks[i].Marker ||
					links[i].Resource["type"] != tc.wantLinks[i].Resource["type"] ||
					links[i].Resource["name"] != tc.wantLinks[i].Resource["name"] {
					t.Errorf("link %d = %+v, want %+v", i, links[i], tc.wantLinks[i])
				}
			}
		})
	}
}

func TestEvaluateEventSummary_BareActorIsNotLinked(t *testing.T) {
	event := map[string]interface{}{"reportingController": "dns-controller"}
	summary, links, err := EvaluateEventSummary("{{ actor }} updated a record", event)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if summary != "dns-controller updated a record" || len(links) != 0 {
		t.Fatalf("summary = %q, links = %+v", summary, links)
	}
}

func TestEvaluateAuditSummary_IdenticalLinksAreDeduplicated(t *testing.T) {
	audit := map[string]interface{}{"user": map[string]interface{}{"username": "alice@x.com"}}

	summary, links, err := EvaluateAuditSummaryMap(
		"{{ actor }} gave {{ link(actor, actorRef) }} access to {{ link('zone z', {'kind': 'Zone', 'name': 'z'}) }} "+
			"and {{ link('zone z', {'kind': 'Zone', 'name': 'z'}) }}, not {{ link('zone z', {'kind': 'Zone', 'name': 'y'}) }}",
		audit)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if want := "alice@x.com gave alice@x.com access to zone z and zone z, not zone z"; summary != want {
		t.Errorf("summary = %q, want %q", summary, want)
	}
	// One actor link, one link to zone z, and one to zone y (same marker,
	// different resource, so not a duplicate).
	if len(links) != 3 {
		t.Fatalf("links = %+v, want 3", links)
	}
	if links[0].Marker != "alice@x.com" || links[0].Resource["type"] != "user" {
		t.Errorf("links[0] = %+v, want the actor link", links[0])
	}
	if links[1].Resource["name"] != "z" || links[2].Resource["name"] != "y" {
		t.Errorf("zone links = %+v, %+v", links[1], links[2])
	}
}
