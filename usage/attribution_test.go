package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/accounts"
	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/quota"
)

func stampAt(at time.Time, hash string) accounts.Stamp {
	return accounts.Stamp{At: at, SessionID: "s", AccountHash: hash, Label: "Claude Code", Plan: "team"}
}

// Attribution order: the transcript's session-owner entry, else the latest stamp
// at or before the call, else nobody.
func TestAttributionPrecedence(t *testing.T) {
	cfg := t.TempDir()
	st := accounts.NewStamps(cfg)
	t0 := now.Add(-3 * time.Hour)
	st.Append(stampAt(t0, acctA))
	st.Append(stampAt(t0.Add(time.Hour), acctB)) // from t0+1h calls are B's

	root := t.TempDir()
	write(t, root, "p/owned.jsonl", owner("uuid-a"), entry(t0.Add(90*time.Minute), "m1", "claude-opus-5-5", 5, 50)) // owner A beats the stamp (B)
	write(t, root, "p/stamped.jsonl",
		entry(t0.Add(-time.Minute), "s0", "claude-opus-5-5", 1, 1),   // before the first stamp: unattributed
		entry(t0, "s1", "claude-opus-5-5", 2, 2),                     // at the first stamp: A
		entry(t0.Add(59*time.Minute), "s2", "claude-opus-5-5", 4, 4), // just before the switch: A
		entry(t0.Add(time.Hour), "s3", "claude-opus-5-5", 8, 8),      // at the switch: B
		entry(t0.Add(2*time.Hour), "s4", "claude-opus-5-5", 16, 16),  // later: B
	)
	s := NewScanner(filepath.Join(root, "projects"), "")
	s.SetStamps(st.Load())
	s.Refresh(now)
	if got := total(s, 0, acctA); got.Out != 5+2+4 {
		t.Fatalf("A: %+v, want the owned call, and the stamped calls at and before the switch", got)
	}
	if got := total(s, 0, acctB); got.Out != 8+16 {
		t.Fatalf("B: %+v", got)
	}
	if got := total(s, 0, ""); got.Out != 1 {
		t.Fatalf("unattributed: %+v, want only the call before any stamp", got)
	}

	// Without a timeline, a transcript with no owner stays unattributed.
	root2 := t.TempDir()
	write(t, root2, "p/x.jsonl", entry(t0, "m", "claude-opus-5-5", 3, 3))
	s2 := NewScanner(filepath.Join(root2, "projects"), "")
	s2.Refresh(now)
	if got := total(s2, 0, ""); got.Out != 3 {
		t.Fatalf("%+v", got)
	}
	if s2.Attribute("zz", t0) != "zz" || s2.Attribute("", t0) != "" {
		t.Fatal("Attribute")
	}
}

var emailShaped = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

func engineWithAccount(t *testing.T, org bool) (*Engine, string, string) {
	t.Helper()
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	cfg := t.TempDir()
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"accountUuid":"uuid-a","emailAddress":"someone@example.org","organizationName":"Secret Lab Org","organizationType":"claude_team"}}`), 0o600)
	write(t, claude, "p/a.jsonl", owner("uuid-a"), entry(now.Add(-time.Hour), "m", "claude-opus-5-5", 4, 0))
	body := fmt.Sprintf(`{"at":%q,"windows":[{"account":"Claude Code","plan":"team","label":"Weekly (7 day)","used_percent":54,"resets_at_unix":%d,"verdict":"available"}]}`, now.Format(time.RFC3339), weekReset)
	e := NewEngine(claude, t.TempDir(), cfg, func(context.Context) ([]byte, error) { return []byte(body), nil })
	if org {
		e.ShowOrg(claude)
	}
	e.Step(context.Background(), now)
	return e, cfg, claude
}

func TestAliasReplacesTheLabelAndTheFileFollows(t *testing.T) {
	e, cfg, _ := engineWithAccount(t, false)
	a := e.View().Accounts[0]
	if a.Alias != "" || a.Plan != "team" || e.View().Capacity.Items[0].Plan != "team" {
		t.Fatalf("no alias yet; the plan comes from the reading: %+v", a)
	}
	if err := e.Rename(acctA, "Labo (team)"); err != nil {
		t.Fatal(err)
	}
	v := e.View()
	if v.Accounts[0].Alias != "Labo (team)" || v.Capacity.Items[0].Alias != "Labo (team)" {
		t.Fatalf("the rename shows at once: %+v", v.Accounts[0])
	}
	if got := accounts.NewAliases(cfg).Name(acctA); got != "Labo (team)" {
		t.Fatalf("file: %q", got)
	}
	// A pass rebuilds from the file, and a hand edit is picked up.
	os.WriteFile(filepath.Join(cfg, "accounts.toml"), []byte("\""+acctA[:4]+"\" = \"By hand\"\n"), 0o600)
	e.Step(context.Background(), now.Add(time.Minute))
	if e.View().Accounts[0].Alias != "By hand" {
		t.Fatalf("%+v", e.View().Accounts[0])
	}
	// A refused name changes nothing; so does one the disk cannot take.
	if err := e.Rename(acctA, "bad\nname"); err == nil {
		t.Fatal("a name with a newline")
	}
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, nil, 0o600)
	e2 := NewEngine(t.TempDir(), t.TempDir(), filepath.Join(file, "x"), nil)
	if err := e2.Rename("abcd1234", "x"); err == nil {
		t.Fatal("an unwritable directory")
	}
	if e2.Rename("", "x") == nil {
		t.Fatal("no key")
	}
}

// An account the readings do not name takes its label and plan from the stamps.
func TestStampsNameAnAccountThatOnlyHasTokens(t *testing.T) {
	cfg := t.TempDir()
	accounts.NewStamps(cfg).Append(accounts.Stamp{At: now.Add(-time.Hour), AccountHash: acctB, Label: "Claude Code (max)", Plan: "max"})
	root := t.TempDir()
	write(t, root, "p/b.jsonl", owner("uuid-b"), entry(now.Add(-time.Minute), "m", "claude-opus-5-5", 4, 0))
	e := NewEngine(root, t.TempDir(), cfg, func(context.Context) ([]byte, error) { return nil, fmt.Errorf("no aiquota") })
	e.Step(context.Background(), now)
	v := e.View()
	if len(v.Accounts) != 1 || v.Accounts[0].Label != "Claude Code (max)" || v.Accounts[0].Plan != "max" {
		t.Fatalf("%+v", v.Accounts)
	}
}

// The plan is recorded with each sample, so a past reading keeps the plan it had.
func TestThePlanIsRecordedWithEachSample(t *testing.T) {
	store := quota.NewStore(t.TempDir())
	for i, plan := range []string{"pro", "max", "team"} {
		rs, err := quota.Parse([]byte(fmt.Sprintf(`{"at":%q,"windows":[{"account":"Claude Code","plan":%q,"label":"Weekly (7 day)","used_percent":%d,"resets_at_unix":%d,"verdict":"available"}]}`,
			now.Add(time.Duration(i)*time.Hour).Format(time.RFC3339), plan, 10*i, weekReset)))
		if err != nil {
			t.Fatal(err)
		}
		quota.Tag(rs, acctA)
		if err := store.Append(rs[0]); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := store.Load()
	if len(got) != 3 || got[0].Plan != "pro" || got[1].Plan != "max" || got[2].Plan != "team" {
		t.Fatalf("every sample keeps its own plan: %+v", got)
	}
}

// With --show-org off no organisation name is read, kept or sent; with it on it
// is a suggestion for the active account alone. An e-mail is never sent.
func TestOrganisationNameOnlyWithShowOrg(t *testing.T) {
	for _, on := range []bool{false, true} {
		e, cfg, _ := engineWithAccount(t, on)
		snap := Source{Inner: fakeSource{model.Snapshot{Version: model.Version}}, Eng: e}.Snapshot()
		b, err := json.Marshal(snap)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if emailShaped.MatchString(s) || strings.Contains(s, "someone") || strings.Contains(s, "uuid-a") || strings.Contains(s, "example.org") {
			t.Fatalf("show-org %v: the stream carries an e-mail or an id: %s", on, s)
		}
		if got := strings.Contains(s, "Secret Lab Org"); got != on {
			t.Fatalf("show-org %v: organisation name in the stream = %v", on, got)
		}
		if on && snap.Usage.Accounts[0].Suggest != "Secret Lab Org" {
			t.Fatalf("%+v", snap.Usage.Accounts[0])
		}
		// Nothing is saved until the owner confirms: the alias file does not exist.
		if _, err := os.Stat(filepath.Join(cfg, "accounts.toml")); err == nil {
			t.Fatal("a suggestion was saved without confirmation")
		}
		for _, f := range []string{"accounts.jsonl", "quota.jsonl"} {
			if b, err := os.ReadFile(filepath.Join(cfg, f)); err == nil && (strings.Contains(string(b), "Secret Lab") || emailShaped.Match(b)) {
				t.Fatalf("%s holds an organisation or an e-mail", f)
			}
		}
	}
}
