package quota

import (
	"os"
	"path/filepath"
	"testing"
)

const twoAccounts = `{"at":"2026-11-20T07:12:08Z","windows":[
 {"account":"alpha","plan":"max","label":"Weekly (7 day)","used_percent":54,"resets_at_unix":100,"verdict":"available"},
 {"account":"beta","plan":"team","label":"Weekly (7 day)","used_percent":1,"resets_at_unix":200,"verdict":"available"},
 {"account":"beta","plan":"team","label":"Weekly Opus","verdict":"n/a"}]}`

func TestParseSplitsAccountsAndTagNamesThemOnlyWhenItCan(t *testing.T) {
	rs, err := Parse([]byte(twoAccounts))
	if err != nil || len(rs) != 2 || rs[0].Label != "alpha" || rs[1].Label != "beta" || len(rs[1].Windows) != 2 {
		t.Fatalf("%+v %v", rs, err)
	}
	// Several accounts in one reading: known by label, the active one not guessed.
	Tag(rs, AccountKey("some-id"))
	if rs[0].Acct != LabelKey("alpha") || rs[1].Acct != LabelKey("beta") || rs[0].Acct == rs[1].Acct {
		t.Fatalf("%q %q", rs[0].Acct, rs[1].Acct)
	}
	// One account and a known active one: that one. Active unknown: unknown.
	one, _ := Parse([]byte(sample))
	Tag(one, AccountKey("some-id"))
	if one[0].Acct != AccountKey("some-id") || len(one[0].Acct) != 8 {
		t.Fatal(one[0].Acct)
	}
	Tag(one, "")
	if one[0].Acct != "" {
		t.Fatal("an account that cannot be told is not guessed")
	}
	if AccountKey("") != "" || AccountKey("a") == AccountKey("b") || LabelKey("a") == AccountKey("a") {
		t.Fatal("keys")
	}
}

func TestLegacyRowsLoadAsAnUnknownAccount(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "quota.jsonl"), []byte("{\"at\":5,\"w\":[{\"l\":\"Weekly (7 day)\",\"p\":9,\"a\":true}]}\n{\"at\":6,\"acct\":\"abcd1234\",\"lbl\":\"x\",\"w\":[]}\n"), 0o600)
	rs, bad := NewStore(dir).Load()
	if bad != 0 || len(rs) != 2 || rs[0].Acct != "" || rs[1].Acct != "abcd1234" {
		t.Fatalf("%+v", rs)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(sample))
	f.Add([]byte(twoAccounts))
	f.Add([]byte(`{"at":"2026-11-20T07:12:08Z","windows":[{"label":1,"used_percent":"x"}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		rs, err := Parse(b)
		if err != nil {
			return
		}
		for _, r := range rs {
			for _, w := range r.Windows {
				if w.Percent < 0 || w.Percent > 100 || len(w.Label) > 64 {
					t.Fatalf("%+v", w)
				}
			}
		}
	})
}
