package model

import (
	"testing"
	"time"
)

// A live lease is listed before an expired one however old its acquisition is:
// the order is "live first, then most recent", and the first half is held here.
func TestLeasesOfListsLiveBeforeExpiredWhateverTheirAge(t *testing.T) {
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	s := Snapshot{Leases: []Lease{
		{Resource: "expired-new", Owner: "o", Expired: true, Acquired: at},
		{Resource: "live-old", Owner: "o", Acquired: at.Add(-5 * time.Hour)},
		{Resource: "live-new", Owner: "o", Acquired: at.Add(-time.Hour)},
		{Resource: "other", Owner: "x", Acquired: at},
	}}
	var got []string
	for _, l := range s.LeasesOf("o") {
		got = append(got, l.Resource)
	}
	want := []string{"live-new", "live-old", "expired-new"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v, want %v", got, want)
		}
	}
}
