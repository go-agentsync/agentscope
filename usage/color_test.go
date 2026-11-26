package usage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/accounts"
)

func TestColourShowsAtOnceSurvivesAPassAndResets(t *testing.T) {
	e, cfg, _ := engineWithAccount(t, false)
	if v := e.View(); v.Accounts[0].Color != "" || v.Capacity.Items[0].Color != "" {
		t.Fatalf("no colour yet: %+v", v.Accounts[0])
	}
	if err := e.SetColor(acctA, "#E69F00"); err != nil {
		t.Fatal(err)
	}
	v := e.View()
	if v.Accounts[0].Color != "#e69f00" || v.Capacity.Items[0].Color != "#e69f00" {
		t.Fatalf("the colour shows at once, normalised: %+v", v.Accounts[0])
	}
	if got := accounts.NewColors(cfg).Color(acctA); got != "#e69f00" {
		t.Fatalf("file: %q", got)
	}
	// A pass rebuilds from the file; a hand edit is picked up.
	e.Step(context.Background(), now.Add(time.Minute))
	if e.View().Accounts[0].Color != "#e69f00" || e.View().Capacity.Items[0].Color != "#e69f00" {
		t.Fatalf("after a pass: %+v", e.View().Accounts[0])
	}
	os.WriteFile(filepath.Join(cfg, "account-colors.toml"), []byte("\""+acctA[:4]+"\" = \"#112233\"\n"), 0o600)
	e.Step(context.Background(), now.Add(2*time.Minute))
	if e.View().Accounts[0].Color != "#112233" {
		t.Fatalf("hand edit: %+v", e.View().Accounts[0])
	}
	// Reset restores the automatic colour.
	if err := e.SetColor(acctA, ""); err != nil {
		t.Fatal(err)
	}
	if v := e.View(); v.Accounts[0].Color != "" || v.Capacity.Items[0].Color != "" {
		t.Fatalf("reset: %+v", v.Accounts[0])
	}
	// A refused value changes nothing; so does a disk that cannot take it.
	if err := e.SetColor(acctA, "red"); err == nil {
		t.Fatal("a colour name was accepted")
	}
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, nil, 0o600)
	e2 := NewEngine(t.TempDir(), t.TempDir(), filepath.Join(file, "x"), nil)
	if err := e2.SetColor("abcd1234", "#010203"); err == nil {
		t.Fatal("an unwritable directory")
	}
	if e2.SetColor("", "#010203") == nil {
		t.Fatal("no key")
	}
}
