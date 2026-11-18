package apiserver

import (
	"testing"
)

func TestNewStaffIndex_Modes(t *testing.T) {
	// required without a Milo client is a startup error.
	if _, _, err := newStaffIndex(nil, nil, "required"); err == nil {
		t.Error("required mode without a client succeeded")
	}
	if _, _, err := newStaffIndex(nil, nil, ""); err == nil {
		t.Error("default mode without a client succeeded")
	}
	// domain-only needs no client and never syncs or refuses.
	staff, users, err := newStaffIndex(nil, nil, "domain-only")
	if err != nil || staff == nil || users != nil {
		t.Fatalf("domain-only = (%v, %v, %v)", staff, users, err)
	}
	if _, _, err := newStaffIndex(nil, nil, "off"); err == nil {
		t.Error("invalid mode accepted")
	}
}
