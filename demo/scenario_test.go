package demo

import (
	"strings"
	"testing"
)

func TestNamesAreSortedAndLookupable(t *testing.T) {
	names := Names()
	if len(names) < 3 {
		t.Fatalf("names %v", names)
	}
	for i, n := range names {
		if i > 0 && names[i-1] >= n {
			t.Errorf("names not sorted: %v", names)
		}
		sc, err := Lookup(n)
		if err != nil || sc.Name != n || sc.Title == "" || len(sc.Steps()) == 0 {
			t.Errorf("Lookup(%q) = %+v, %v", n, sc, err)
		}
	}
	for _, want := range []string{"overview", "colors", "theme-lang"} {
		if _, err := Lookup(want); err != nil {
			t.Error(err)
		}
	}
}

func TestLookupNamesTheValidScenarios(t *testing.T) {
	_, err := Lookup(strings.Repeat("é", 100))
	if err == nil || !strings.Contains(err.Error(), "colors") || !strings.Contains(err.Error(), "...") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Lookup(""); err == nil {
		t.Fatal("the empty name was accepted")
	}
}

func TestParseSize(t *testing.T) {
	for _, c := range []struct {
		in   string
		w, h int
		ok   bool
	}{
		{"1280x720", 1280, 720, true},
		{"320x320", 320, 320, true},
		{"4096x4096", 4096, 4096, true},
		{"319x720", 0, 0, false},
		{"1280x319", 0, 0, false},
		{"4097x720", 0, 0, false},
		{"1280x4097", 0, 0, false},
		{"1280", 0, 0, false},
		{"", 0, 0, false},
		{"axb", 0, 0, false},
		{"1280xb", 0, 0, false},
		{"-1280x720", 0, 0, false},
		{"1280x720x3", 0, 0, false},
		{"99999999999999999999x720", 0, 0, false},
		{strings.Repeat("9", 200) + "x" + strings.Repeat("9", 200), 0, 0, false},
	} {
		w, h, err := ParseSize(c.in)
		if (err == nil) != c.ok || w != c.w || h != c.h {
			t.Errorf("ParseSize(%.30q) = %d, %d, %v", c.in, w, h, err)
		}
	}
}

func FuzzParseSize(f *testing.F) {
	for _, s := range []string{"1280x720", "", "x", "320x4096", "-1x-1", "1e3x1e3", "０x０"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		w, h, err := ParseSize(s)
		if err != nil {
			if w != 0 || h != 0 {
				t.Fatalf("error with a size: %d %d", w, h)
			}
			return
		}
		if w < MinSize || w > MaxSize || h < MinSize || h > MaxSize {
			t.Fatalf("%q gave %dx%d, outside the bounds", s, w, h)
		}
	})
}

func FuzzLookup(f *testing.F) {
	for _, s := range []string{"overview", "", "../../etc/passwd", "colors\x00", "θ"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		sc, err := Lookup(s)
		if err == nil && sc.Name != s {
			t.Fatalf("Lookup(%q) returned %q", s, sc.Name)
		}
		if err != nil && len(err.Error()) > 400 {
			t.Fatalf("an unbounded error of %d bytes", len(err.Error()))
		}
	})
}

func TestSnapshotIsSyntheticAndFixed(t *testing.T) {
	a, b := Snapshot("#112233", ""), Snapshot("#112233", "")
	if a.At != Epoch || !a.At.Equal(b.At) {
		t.Fatalf("clock %v", a.At)
	}
	if len(a.Sessions) != 5 || len(a.Usage.Accounts) != 2 || len(a.Leases) != 3 {
		t.Errorf("shape: %d sessions, %d accounts, %d leases", len(a.Sessions), len(a.Usage.Accounts), len(a.Leases))
	}
	if a.Usage.Accounts[0].Color != "#112233" || a.Usage.Accounts[1].Color != "" {
		t.Errorf("colours %q %q", a.Usage.Accounts[0].Color, a.Usage.Accounts[1].Color)
	}
	if series(2, 3, 1)[1][2] != series(2, 3, 1)[1][2] {
		t.Error("series is not deterministic")
	}
	for _, s := range a.Sessions {
		if !strings.HasPrefix(s.ID, "d3m0") {
			t.Errorf("session %q is not invented", s.ID)
		}
	}
}
