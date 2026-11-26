package wire

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePrefser struct {
	theme, lang string
	fail        error
}

func (f *fakePrefser) SetPrefs(theme, lang string) error {
	f.theme, f.lang = theme, lang
	return f.fail
}

// A hub with no Prefser refuses; deleting the refusal would call a nil interface.
// That must fail on an assertion, so the panic is recovered and reported.
func TestHubSetPrefsWithoutAPrefserRefusesWithoutPanicking(t *testing.T) {
	h := NewHub(&src{}, time.Hour, time.Hour)
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("a nil Prefser was called: %v", r)
			}
		}()
		err = h.SetPrefs(context.Background(), "dark", "fr")
	}()
	if err == nil {
		t.Error("a hub with no Prefser kept a preference")
	}
}

// The hub hands the choice to its Prefser, passes its refusal back, and collects
// only after a success.
func TestHubSetPrefsDelegates(t *testing.T) {
	h := NewHub(&src{}, time.Hour, time.Hour)
	f := &fakePrefser{}
	h.SetPrefser(f)
	if err := h.SetPrefs(context.Background(), "dark", "fr"); err != nil {
		t.Fatal(err)
	}
	if f.theme != "dark" || f.lang != "fr" {
		t.Fatalf("%+v", f)
	}
	f.fail = errors.New("a theme is light, dark or system")
	if err := h.SetPrefs(context.Background(), "blue", ""); err == nil || err.Error() != f.fail.Error() {
		t.Fatalf("the refusal must reach the caller: %v", err)
	}
}
