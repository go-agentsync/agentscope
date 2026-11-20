package ui

import (
	"sort"
	"strings"
	"time"

	"github.com/go-widgets/mvvm"
)

// The state filter's choices.
const (
	StateAll = iota
	StateActive
	StateIdle
)

// StateNames are the state drop-down's options, indexed by StateAll...
var StateNames = []string{"All states", "Active", "Idle"}

// LastNames are the last-activity drop-down's options; LastLimits the age each
// keeps (0: any), in the same order.
var (
	LastNames  = []string{"Any activity", "< 10 min", "< 1 h", "< 6 h", "< 24 h"}
	LastLimits = []time.Duration{0, 10 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour}
)

// AllRepos is the first option of the repository drop-down.
const AllRepos = "All repositories"

// ModelFilterSlots is how many model names the filter row offers a check box
// for: the most used ones present.
const ModelFilterSlots = 6

// Filters is what narrows the sessions table: every filter is MVVM state, and
// they combine with AND (and with the search text).
type Filters struct {
	// State is StateAll, StateActive or StateIdle; Last indexes LastLimits.
	State, Last *mvvm.Observable[int]
	// Repo is the repository drop-down's choice: 0 is AllRepos, i the i-th of
	// RepoOptions. It follows its repository by name when the options change.
	Repo        *mvvm.Observable[int]
	RepoOptions *mvvm.ObservableList[string]
	// ModelLabels and ModelOn are the model check boxes: slot i is the i-th most
	// used model present, "" when there are fewer. None checked is no model
	// filter; checked ones are OR-ed among themselves, AND-ed with the rest.
	ModelLabels [ModelFilterSlots]*mvvm.Observable[string]
	ModelOn     [ModelFilterSlots]*mvvm.Observable[bool]
	// Clear puts every filter back to "all".
	Clear *mvvm.Command

	repoSel string          // the chosen repository, "" for all
	models  map[string]bool // the chosen model names
	syncing bool
	changed func() // what to run when a filter changed
}

func newFilters(changed func()) *Filters {
	f := &Filters{
		State: mvvm.NewObservable(StateAll), Last: mvvm.NewObservable(0), Repo: mvvm.NewObservable(0),
		RepoOptions: mvvm.NewObservableList(AllRepos),
		models:      map[string]bool{}, changed: changed,
	}
	for i := range f.ModelLabels {
		f.ModelLabels[i], f.ModelOn[i] = mvvm.NewObservable(""), mvvm.NewObservable(false)
	}
	f.Clear = mvvm.NewCommand(f.clear, nil)
	f.State.SubscribeChanged(f.touch)
	f.Last.SubscribeChanged(f.touch)
	f.Repo.SubscribeChanged(f.onRepo)
	for i := range f.ModelOn {
		f.ModelOn[i].SubscribeChanged(func() { f.onModel(i) })
	}
	return f
}

func (f *Filters) touch() {
	if !f.syncing {
		f.changed()
	}
}

// Active reports whether any filter is not "all".
func (f *Filters) Active() bool {
	return f.State.Get() != StateAll || f.Last.Get() != 0 || f.repoSel != "" || len(f.models) > 0
}

func (f *Filters) clear() {
	f.syncing = true
	f.State.Set(StateAll)
	f.Last.Set(0)
	f.Repo.Set(0)
	f.repoSel = ""
	f.models = map[string]bool{}
	for _, on := range f.ModelOn {
		on.Set(false)
	}
	f.syncing = false
	f.changed()
}

// onRepo follows the drop-down.
func (f *Filters) onRepo() {
	if f.syncing {
		return
	}
	i := f.Repo.Get()
	if i <= 0 || i >= f.RepoOptions.Len() {
		f.repoSel = ""
	} else {
		f.repoSel = f.RepoOptions.At(i)
	}
	f.changed()
}

// onModel follows check box i.
func (f *Filters) onModel(i int) {
	if f.syncing {
		return
	}
	name := f.ModelLabels[i].Get()
	if name == "" {
		return
	}
	if f.ModelOn[i].Get() {
		f.models[name] = true
	} else {
		delete(f.models, name)
	}
	f.changed()
}

// sync rebuilds the offered repositories and models from the sessions that are
// listed, keeping each choice by name. A choice whose repository or model is no
// longer present stays chosen (it matches nothing) until the owner clears it, so
// a filter never turns itself off.
func (f *Filters) sync(rows []Row) {
	f.syncing = true
	defer func() { f.syncing = false }()

	seen := map[string]bool{}
	for _, r := range rows {
		for _, repo := range strings.Split(r.Repos, "\n") {
			if repo != "" {
				seen[repo] = true
			}
		}
	}
	if f.repoSel != "" {
		seen[f.repoSel] = true
	}
	repos := make([]string, 0, len(seen))
	for r := range seen {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	setList(f.RepoOptions, append([]string{AllRepos}, repos...))
	idx := 0
	for i, r := range repos {
		if r == f.repoSel {
			idx = i + 1
		}
	}
	f.Repo.Set(idx)

	count := map[string]int{}
	for _, r := range rows {
		count[r.Model]++
	}
	for m := range f.models {
		if _, ok := count[m]; !ok {
			count[m] = 0 // chosen, though no listed session runs it now
		}
	}
	names := make([]string, 0, len(count))
	for m := range count {
		names = append(names, m)
	}
	sort.Slice(names, func(i, j int) bool {
		if count[names[i]] != count[names[j]] {
			return count[names[i]] > count[names[j]]
		}
		return names[i] < names[j]
	})
	for i := range f.ModelLabels {
		name := ""
		if i < len(names) {
			name = names[i]
		}
		f.ModelLabels[i].Set(name)
		f.ModelOn[i].Set(name != "" && f.models[name])
	}
}

// keep reports whether a row passes every filter.
func (f *Filters) keep(r Row) bool {
	switch f.State.Get() {
	case StateActive:
		if !r.Active {
			return false
		}
	case StateIdle:
		if r.Active {
			return false
		}
	}
	if i := f.Last.Get(); i > 0 && i < len(LastLimits) && r.Age >= LastLimits[i] {
		return false
	}
	if f.repoSel != "" {
		found := false
		for _, repo := range strings.Split(r.Repos, "\n") {
			if repo == f.repoSel {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if len(f.models) > 0 && !f.models[r.Model] {
		return false
	}
	return true
}
