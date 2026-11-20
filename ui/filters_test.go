package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
)

// filterSnap has five sessions: two active (A Opus 5.5 on o/a and o/b, D Sonnet
// on o/d), three idle (B mystery model, E Opus 5.5, C with no transcript).
func filterSnap() model.Snapshot {
	mk := func(short, mdl, repo string, active bool, ago time.Duration) model.Session {
		return model.Session{ID: short + "-0000-4000-8000-000000000000", Short: short, HasTranscript: true, Active: active, CwdRepo: repo, Model: mdl, LastActivity: t0.Add(-ago)}
	}
	sn := model.Snapshot{Version: 1, At: t0, ActiveFor: "10m0s", Sessions: []model.Session{
		mk("aaaaaaaa", "claude-opus-5-5", "o/a", true, time.Minute),
		mk("bbbbbbbb", "claude-mystery-9", "o/b", false, 2*time.Hour),
		mk("dddddddd", "claude-sonnet-5-5", "o/d", true, 5*time.Minute),
		mk("eeeeeeee", "claude-opus-5-5", "o/e", false, 30*time.Hour),
		{ID: "cccccccc-0000-4000-8000-000000000000", Short: "cccccccc", LastActivity: t0.Add(-72 * time.Hour)},
	}}
	// A holds a second repository: o/b as well as the one it sits in.
	sn.Leases = []model.Lease{{Repo: "o/b", Kind: model.KindRepo, Owner: sn.Sessions[0].ID, Expires: t0.Add(time.Hour)}}
	return sn
}

func shown(vm *ViewModel) string {
	var ids []string
	for _, r := range vm.Rows.Slice() {
		ids = append(ids, r.ID[:1])
	}
	return strings.Join(ids, "")
}

func TestStateFilter(t *testing.T) {
	vm := NewViewModel()
	vm.Apply(filterSnap())
	if shown(vm) != "abdec" {
		t.Fatal(shown(vm))
	}
	vm.Filters.State.Set(StateActive)
	if shown(vm) != "ad" {
		t.Fatalf("active: %s", shown(vm))
	}
	vm.Filters.State.Set(StateIdle)
	if shown(vm) != "bec" { // idle is everything that is not active, a session with no transcript included
		t.Fatalf("idle: %s", shown(vm))
	}
	vm.Filters.State.Set(StateAll)
	if shown(vm) != "abdec" {
		t.Fatal(shown(vm))
	}
}

func TestLastActivityFilterKeepsWhatIsYoungerThanItsLimit(t *testing.T) {
	vm := NewViewModel()
	vm.Apply(filterSnap())
	for _, c := range []struct {
		i    int
		want string
	}{
		{0, "abdec"}, // any
		{1, "ad"},    // < 10 min: A 1 min, D 5 min
		{2, "ad"},    // < 1 h
		{3, "abd"},   // < 6 h: B is 2 h
		{4, "abd"},   // < 24 h: E is 30 h
	} {
		vm.Filters.Last.Set(c.i)
		if got := shown(vm); got != c.want {
			t.Errorf("%s: %s, want %s", LastNames[c.i], got, c.want)
		}
	}
	// A boundary: exactly the limit is not younger than it.
	sn := filterSnap()
	sn.Sessions[0].LastActivity = t0.Add(-10 * time.Minute)
	vm.Filters.Last.Set(1)
	vm.Apply(sn)
	if shown(vm) != "d" {
		t.Fatalf("a session exactly 10 min old is not < 10 min: %s", shown(vm))
	}
	// An index past the table is no filter.
	vm.Filters.Last.Set(99)
	if shown(vm) != "abdec" {
		t.Fatal(shown(vm))
	}
}

func TestRepositoryFilterMatchesAnyRepoOfASession(t *testing.T) {
	vm := NewViewModel()
	vm.Apply(filterSnap())
	opts := vm.Filters.RepoOptions.Slice()
	if len(opts) != 5 || opts[0] != AllRepos || opts[1] != "o/a" || opts[2] != "o/b" {
		t.Fatalf("%v", opts)
	}
	vm.Filters.Repo.Set(2) // o/b: held by A, and the cwd repo of B
	if shown(vm) != "ab" {
		t.Fatalf("a session matches any of its repositories: %s", shown(vm))
	}
	vm.Filters.Repo.Set(0)
	if shown(vm) != "abdec" {
		t.Fatal(shown(vm))
	}
	vm.Filters.Repo.Set(99) // out of range: all
	if shown(vm) != "abdec" {
		t.Fatal(shown(vm))
	}
	// The choice follows its repository by name when the options move, and stays
	// when the repository leaves the list (it then matches nothing).
	vm.Filters.Repo.Set(3) // o/d
	sn := filterSnap()
	sn.Sessions = sn.Sessions[:3] // E and C leave; o/d moves from 3 to 3, o/e is gone
	vm.Apply(sn)
	if shown(vm) != "d" || vm.Filters.RepoOptions.At(vm.Filters.Repo.Get()) != "o/d" {
		t.Fatalf("%s %d", shown(vm), vm.Filters.Repo.Get())
	}
	sn.Sessions = sn.Sessions[:2]
	vm.Apply(sn)
	if shown(vm) != "" || vm.Filters.RepoOptions.At(vm.Filters.Repo.Get()) != "o/d" {
		t.Fatalf("a filter does not switch itself off: %q", shown(vm))
	}
}

func TestModelFilterIsAMultiSelectOfThePresentModels(t *testing.T) {
	vm := NewViewModel()
	vm.Apply(filterSnap())
	f := vm.Filters
	var labels []string
	for _, l := range f.ModelLabels {
		labels = append(labels, l.Get())
	}
	// Most used first (Opus 5.5 has two), then by name; "?" is the unknown one.
	if fmt.Sprint(labels) != "[? Opus 5.5 Sonnet 5.5   ]" {
		t.Fatalf("%q", labels)
	}
	f.ModelOn[1].Set(true) // Opus 5.5
	if shown(vm) != "ae" {
		t.Fatalf("one model: %s", shown(vm))
	}
	f.ModelOn[0].Set(true) // and "?"
	if shown(vm) != "abec" {
		t.Fatalf("two models are OR-ed: %s", shown(vm))
	}
	f.ModelOn[0].Set(false)
	f.ModelOn[1].Set(false)
	if shown(vm) != "abdec" {
		t.Fatalf("none checked is no filter: %s", shown(vm))
	}
	// An empty slot does nothing when toggled.
	f.ModelOn[5].Set(true)
	if len(f.models) != 0 {
		t.Fatal("an empty slot chose a model")
	}
	f.ModelOn[5].Set(false)
	// A chosen model that no session runs any more stays chosen.
	f.ModelOn[2].Set(true) // Sonnet 5.5
	sn := filterSnap()
	sn.Sessions = sn.Sessions[:2]
	vm.Apply(sn)
	if shown(vm) != "" {
		t.Fatalf("%q", shown(vm))
	}
	var chosen int
	for i := range f.ModelOn {
		if f.ModelOn[i].Get() {
			chosen++
			if f.ModelLabels[i].Get() != "Sonnet 5.5" {
				t.Fatal("a different model is shown as chosen")
			}
		}
	}
	if chosen != 1 {
		t.Fatal("the vanished model is not offered as chosen")
	}
}

func TestFiltersCombineWithANDAndWithTheQuery(t *testing.T) {
	vm := NewViewModel()
	vm.Apply(filterSnap())
	f := vm.Filters
	f.State.Set(StateActive)
	f.ModelOn[1].Set(true) // Opus 5.5
	if shown(vm) != "a" {
		t.Fatalf("active AND Opus 5.5: %s", shown(vm))
	}
	f.Repo.Set(3) // o/d: A is not on it
	if shown(vm) != "" {
		t.Fatalf("active AND Opus 5.5 AND o/d: %q", shown(vm))
	}
	f.Repo.Set(0)
	f.Last.Set(1)
	vm.Query.Set("aaaa")
	if shown(vm) != "a" {
		t.Fatalf("with a query: %s", shown(vm))
	}
	vm.Query.Set("dddd")
	if shown(vm) != "" {
		t.Fatalf("the query narrows too: %q", shown(vm))
	}
	if !strings.Contains(vm.Status.Get(), "0 of 5 sessions shown") {
		t.Fatalf("%q", vm.Status.Get())
	}
	vm.Query.Set("")
	if !strings.Contains(vm.Status.Get(), "1 of 5 sessions shown") {
		t.Fatalf("%q", vm.Status.Get())
	}
	// Clear puts everything back.
	if !f.Active() {
		t.Fatal("filters are on")
	}
	f.Clear.Execute()
	if f.Active() || shown(vm) != "abdec" || f.State.Get() != StateAll || f.Last.Get() != 0 || f.Repo.Get() != 0 || f.ModelOn[1].Get() {
		t.Fatalf("after Clear: %s", shown(vm))
	}
	if !strings.Contains(vm.Status.Get(), "5 of 5 sessions shown") {
		t.Fatalf("%q", vm.Status.Get())
	}
}

func TestStatusBeforeAnySnapshotIsLeftAlone(t *testing.T) {
	vm := NewViewModel()
	vm.Filters.State.Set(StateActive) // a filter before there is data
	if vm.Status.Get() != "waiting for the collector..." {
		t.Fatalf("%q", vm.Status.Get())
	}
	vm.Filters.Clear.Execute()
}
