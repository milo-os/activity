package processor

import (
	"context"
	"testing"

	authnv1 "k8s.io/api/authentication/v1"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// userInfoFixture builds a minimal authentication.UserInfo for tests.
func userInfoFixture(username, uid string) authnv1.UserInfo {
	return authnv1.UserInfo{Username: username, UID: uid}
}

func TestEnrichSummaryWithDisplayNames_ActorReplacement(t *testing.T) {
	cases := []struct {
		name        string
		summary     string
		actor       v1alpha1.ActivityActor
		links       []v1alpha1.ActivityLink
		wantSummary string
		wantLinks   int
		wantMarker  string
	}{
		{
			name:    "no display name leaves summary untouched",
			summary: "smith@datum.net created machine account ma-1",
			actor: v1alpha1.ActivityActor{
				Type:  ActorTypeUser,
				Name:  "smith@datum.net",
				Email: "smith@datum.net",
				UID:   "uid-1",
			},
			wantSummary: "smith@datum.net created machine account ma-1",
			wantLinks:   0,
		},
		{
			name:    "display name replaces email and synthetic link is appended",
			summary: "smith@datum.net created machine account ma-1",
			actor: v1alpha1.ActivityActor{
				Type:        ActorTypeUser,
				Name:        "smith@datum.net",
				Email:       "smith@datum.net",
				UID:         "uid-1",
				DisplayName: "Smith Nelson",
			},
			wantSummary: "Smith Nelson created machine account ma-1",
			wantLinks:   1,
			wantMarker:  "Smith Nelson",
		},
		{
			name:    "existing actor link is upgraded in place",
			summary: "smith@datum.net created machine account ma-1",
			actor: v1alpha1.ActivityActor{
				Type:        ActorTypeUser,
				Name:        "smith@datum.net",
				Email:       "smith@datum.net",
				UID:         "uid-1",
				DisplayName: "Smith Nelson",
			},
			links: []v1alpha1.ActivityLink{
				{
					Marker: "smith@datum.net",
					Resource: v1alpha1.ActivityResource{
						APIGroup: iamGroup,
						Kind:     "User",
						Name:     "smith",
					},
				},
			},
			wantSummary: "Smith Nelson created machine account ma-1",
			wantLinks:   1,
			wantMarker:  "Smith Nelson",
		},
		{
			name:    "actor not in summary leaves summary alone and skips link",
			summary: "system updated machine account ma-1",
			actor: v1alpha1.ActivityActor{
				Type:        ActorTypeUser,
				Name:        "smith@datum.net",
				Email:       "smith@datum.net",
				UID:         "uid-1",
				DisplayName: "Smith Nelson",
			},
			wantSummary: "system updated machine account ma-1",
			wantLinks:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSummary, gotLinks := enrichSummaryWithDisplayNames(
				context.Background(), tc.summary, tc.actor, tc.links, nil,
			)
			if gotSummary != tc.wantSummary {
				t.Fatalf("summary = %q, want %q", gotSummary, tc.wantSummary)
			}
			if len(gotLinks) != tc.wantLinks {
				t.Fatalf("links = %d, want %d (links=%+v)", len(gotLinks), tc.wantLinks, gotLinks)
			}
			if tc.wantMarker != "" {
				found := false
				for _, l := range gotLinks {
					if l.Marker == tc.wantMarker {
						found = true
						if l.DisplayName != tc.actor.DisplayName {
							t.Errorf("link.DisplayName = %q, want %q", l.DisplayName, tc.actor.DisplayName)
						}
						if l.Email != tc.actor.Email {
							t.Errorf("link.Email = %q, want %q", l.Email, tc.actor.Email)
						}
					}
				}
				if !found {
					t.Errorf("no link with marker %q", tc.wantMarker)
				}
			}
		})
	}
}

func TestEnrichSummaryWithDisplayNames_UserLinkHydration(t *testing.T) {
	resolver := &fakeResolver{
		names: map[string]UserInfo{
			"340583683847098197": {
				Name:       "340583683847098197",
				GivenName:  "Dean",
				FamilyName: "Gaghan",
				Email:      "dgaghan@datum.net",
			},
		},
	}

	summary := "Smith Nelson updated user 340583683847098197"
	links := []v1alpha1.ActivityLink{
		{
			Marker: "340583683847098197",
			Resource: v1alpha1.ActivityResource{
				APIGroup: iamGroup,
				Kind:     "User",
				Name:     "340583683847098197",
			},
		},
	}
	actor := v1alpha1.ActivityActor{Type: ActorTypeUser, Name: "Smith Nelson", DisplayName: "Smith Nelson"}

	gotSummary, gotLinks := enrichSummaryWithDisplayNames(context.Background(), summary, actor, links, resolver)

	wantSummary := "Smith Nelson updated user Dean Gaghan"
	if gotSummary != wantSummary {
		t.Fatalf("summary = %q, want %q", gotSummary, wantSummary)
	}
	if len(gotLinks) != 1 {
		t.Fatalf("links = %d, want 1", len(gotLinks))
	}
	got := gotLinks[0]
	if got.Marker != "Dean Gaghan" {
		t.Errorf("Marker = %q, want %q", got.Marker, "Dean Gaghan")
	}
	if got.DisplayName != "Dean Gaghan" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Dean Gaghan")
	}
	if got.Email != "dgaghan@datum.net" {
		t.Errorf("Email = %q", got.Email)
	}
}

func TestEnrichSummaryWithDisplayNames_NonUserLinkUntouched(t *testing.T) {
	resolver := &fakeResolver{names: map[string]UserInfo{
		"some-resource": {GivenName: "Should not", FamilyName: "Be Used"},
	}}

	summary := "Smith Nelson created machine account ma-1"
	links := []v1alpha1.ActivityLink{
		{
			Marker: "ma-1",
			Resource: v1alpha1.ActivityResource{
				APIGroup: iamGroup,
				Kind:     "MachineAccount",
				Name:     "ma-1",
			},
		},
	}
	actor := v1alpha1.ActivityActor{Type: ActorTypeUser, Name: "Smith Nelson"}

	gotSummary, gotLinks := enrichSummaryWithDisplayNames(context.Background(), summary, actor, links, resolver)
	if gotSummary != summary {
		t.Fatalf("summary changed: %q", gotSummary)
	}
	if gotLinks[0].DisplayName != "" || gotLinks[0].Email != "" {
		t.Fatalf("MachineAccount link should not be hydrated: %+v", gotLinks[0])
	}
	if resolver.nameCalls.Load() != 0 {
		t.Fatalf("resolver should not be called for non-User links, got %d calls", resolver.nameCalls.Load())
	}
}

func TestResolveActorWithResolver_PopulatesDisplayName(t *testing.T) {
	resolver := &fakeResolver{
		emails: map[string]UserInfo{
			"smith@datum.net": {GivenName: "Smith", FamilyName: "Nelson", Email: "smith@datum.net"},
		},
	}

	actor := ResolveActorWithResolver(context.Background(), userInfoFixture("smith@datum.net", "uid-1"), resolver)

	if actor.DisplayName != "Smith Nelson" {
		t.Fatalf("DisplayName = %q, want %q", actor.DisplayName, "Smith Nelson")
	}
	if actor.Email != "smith@datum.net" {
		t.Errorf("Email = %q", actor.Email)
	}
}

func TestResolveActorWithResolver_NilResolverNoDisplayName(t *testing.T) {
	actor := ResolveActorWithResolver(context.Background(), userInfoFixture("smith@datum.net", "uid-1"), nil)
	if actor.DisplayName != "" {
		t.Fatalf("DisplayName should be empty without resolver, got %q", actor.DisplayName)
	}
}

func TestResolveActorWithResolver_SystemActorSkipsLookup(t *testing.T) {
	resolver := &fakeResolver{}
	actor := ResolveActorWithResolver(context.Background(), userInfoFixture("system:admin", "uid-system"), resolver)
	if actor.Type != ActorTypeSystem {
		t.Fatalf("Type = %q, want %q", actor.Type, ActorTypeSystem)
	}
	if resolver.emailCalls.Load() != 0 {
		t.Fatalf("resolver should not be called for system actors, got %d calls", resolver.emailCalls.Load())
	}
}
