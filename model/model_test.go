package model

import (
	"testing"
	"time"
	"unicode/utf8"
)

func fixture() Snapshot {
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	return Snapshot{
		At: t0,
		Sessions: []Session{
			{ID: "aaaaaaaa-0000", Short: "aaaaaaaa", Active: true, Title: "titled", Slug: "slugged", Agents: []Agent{{ID: "x", Active: true}, {ID: "y"}}},
			{ID: "bbbbbbbb-0000", Short: "bbbbbbbb", Slug: "only-slug"},
			{ID: "cccccccc-0000", Short: "cccccccc"},
		},
		Leases: []Lease{
			{Repo: "o/old", Owner: "aaaaaaaa-0000", Expired: true, Note: "old note", Acquired: t0.Add(-time.Hour)},
			{Repo: "o/new", Owner: "aaaaaaaa-0000", Note: "live note", Acquired: t0.Add(-time.Minute)},
			{Repo: "o/live2", Owner: "aaaaaaaa-0000", Note: "", Acquired: t0.Add(-2 * time.Minute)},
			{Repo: "o/new", Owner: "cccccccc-0000", Expired: true, Note: "gone", Acquired: t0.Add(-3 * time.Hour)},
			{Repo: "o/x", Owner: "other"},
		},
		Tells:     []Tell{{}},
		Anomalies: []Anomaly{{}, {}},
	}
}

func TestCounts(t *testing.T) {
	c := fixture().Counts()
	if c.Sessions != 3 || c.Active != 1 || c.Agents != 2 || c.ActiveAgents != 1 || c.Repos != 4 || c.Held != 3 || c.Leases != 5 || c.Tells != 1 || c.Anomalies != 2 {
		t.Errorf("%+v", c)
	}
}

func TestSessionByShort(t *testing.T) {
	s := fixture()
	if se, ok := s.SessionByShort("bbbbbbbb"); !ok || se.Slug != "only-slug" {
		t.Error("found")
	}
	if _, ok := s.SessionByShort("nope"); ok {
		t.Error("absent")
	}
}

func TestLeasesOfLiveFirstThenNewest(t *testing.T) {
	ls := fixture().LeasesOf("aaaaaaaa-0000")
	if len(ls) != 3 || ls[0].Repo != "o/new" || ls[1].Repo != "o/live2" || !ls[2].Expired {
		t.Errorf("%+v", ls)
	}
}

func TestTaskPriority(t *testing.T) {
	s := fixture()
	if got := s.Task(s.Sessions[0]); got != "live note" {
		t.Errorf("a live lease note comes first: %q", got)
	}
	s.Leases = s.Leases[:1] // only the expired one remains for A
	if got := s.Task(s.Sessions[0]); got != "titled" {
		t.Errorf("then the title: %q", got)
	}
	s.Sessions[0].Title = ""
	if got := s.Task(s.Sessions[0]); got != "slugged" {
		t.Errorf("then the slug: %q", got)
	}
	s.Sessions[0].Slug = ""
	if got := s.Task(s.Sessions[0]); got != "old note" {
		t.Errorf("then an expired note: %q", got)
	}
	if got := s.Task(s.Sessions[2]); got != "" {
		t.Errorf("nothing: %q", got)
	}
}

func TestModelNames(t *testing.T) {
	for id, want := range map[string]string{
		"claude-opus-5-5":               "Opus 5.5",
		"claude-sonnet-5-5":             "Sonnet 5.5",
		"claude-haiku-5-5":              "Haiku 5.5",
		"claude-fable-5-2":              "Fable 5.2",
		"claude-opus-5":                 "Opus 5",
		"claude-opus-4-1-20250805":      "Opus 4.1",
		"claude-sonnet-4-20250514":      "Sonnet 4",
		"claude-opus-5-5[1m]":           "Opus 5.5",
		"<synthetic>":                   "?",
		"":                              "?",
		"gpt-4":                         "?",
		"claude-mystery-9":              "?",
		"claude-opus-5-5-extra-garbage": "?",
		"CLAUDE-OPUS-5-5":               "?", // case is not guessed
	} {
		name, ok := ModelName(id)
		if name != want || ok != (want != "?") {
			t.Errorf("ModelName(%q) = %q, %v; want %q", id, name, ok, want)
		}
	}
	if ModelFamily("claude-sonnet-5-5") != "Sonnet" || ModelFamily("nope") != "?" {
		t.Error("ModelFamily")
	}
	if ModelLabel("claude-opus-5-5") != "Opus 5.5 (claude-opus-5-5)" || ModelLabel("x") != "? (x)" || ModelLabel("") != "? (none seen)" {
		t.Error("ModelLabel")
	}
}

func TestModelCounts(t *testing.T) {
	s := Snapshot{Sessions: []Session{
		{Model: "claude-opus-5-5", Agents: []Agent{{Model: "claude-sonnet-5-5"}, {Model: "claude-sonnet-5-5"}, {}}},
		{Model: "claude-haiku-5-5", Agents: []Agent{{Model: "claude-sonnet-5-5"}}},
		{Model: "claude-opus-5-5"},
	}}
	got := s.ModelCounts()
	want := []ModelCount{{"Sonnet", 3}, {"Opus", 2}, {"?", 1}, {"Haiku", 1}}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %v, want %v", i, got[i], want[i])
		}
	}
}

func TestShortIDAndTruncate(t *testing.T) {
	if ShortID("0123456789") != "01234567" || ShortID("abc") != "abc" {
		t.Error("ShortID")
	}
	if Truncate("a  b\n c", 10) != "a b c" {
		t.Error("whitespace is folded")
	}
	if got := Truncate("héllo wörld", 6); got != "héllo…" || !utf8.ValidString(got) {
		t.Errorf("%q", got)
	}
	if Truncate("abc", 1) != "a" || Truncate("abc", 0) != "" {
		t.Error("tiny bounds")
	}
}
