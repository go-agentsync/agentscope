package usage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/accounts"
	"github.com/go-agentsync/agentscope/model"
)

func TestPrefsShowAtOnceSurviveAPassAndAreRefused(t *testing.T) {
	e, cfg, _ := engineWithAccount(t, false)
	if v := e.View(); v.Prefs != (model.Prefs{}) {
		t.Fatalf("nothing chosen yet: %+v", v.Prefs)
	}
	if err := e.SetPrefs("dark", "FR"); err != nil {
		t.Fatal(err)
	}
	if v := e.View(); v.Prefs != (model.Prefs{Theme: "dark", Lang: "fr"}) {
		t.Fatalf("shows at once, normalised: %+v", v.Prefs)
	}
	if got := accounts.NewPrefs(cfg).Get(); got != (model.Prefs{Theme: "dark", Lang: "fr"}) {
		t.Fatalf("file: %+v", got)
	}
	e.Step(context.Background(), now.Add(time.Minute))
	if got := e.View().Prefs; got != (model.Prefs{Theme: "dark", Lang: "fr"}) {
		t.Fatalf("after a pass: %+v", got)
	}
	// A refused value changes nothing; so does a disk that cannot take it.
	if err := e.SetPrefs("blue", "fr"); err == nil {
		t.Fatal("a theme name outside the three was accepted")
	}
	if got := e.View().Prefs; got != (model.Prefs{Theme: "dark", Lang: "fr"}) {
		t.Fatalf("a refusal changed the view: %+v", got)
	}
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, nil, 0o600)
	e2 := NewEngine(t.TempDir(), t.TempDir(), filepath.Join(file, "x"), nil)
	if err := e2.SetPrefs("dark", ""); err == nil {
		t.Fatal("an unwritable directory")
	}
}
