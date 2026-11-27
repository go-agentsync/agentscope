package demo

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/accounts"
	"github.com/go-agentsync/agentscope/quota"
)

// The leak test: a capture is published, so it must hold nothing of the machine
// that made it. The deny-list is built at test time from what this machine really
// holds, so nothing real is written into the repository.

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	ghRe    = regexp.MustCompile(`github\.com/[A-Za-z0-9_.-]+`)
	// quoted is a "key": "value" (JSON) or key = "value" (TOML) pair, or a bare
	// quoted TOML key, whose value is what we collect.
	pairRe = regexp.MustCompile(`"([A-Za-z_]+)"\s*[:=]\s*"([^"\\]{1,200})"`)
	tomlRe = regexp.MustCompile(`(?m)^\s*"([^"\\]{1,200})"\s*=\s*"([^"\\]{1,200})"`)
)

// identifying are the JSON keys whose values name an account, a person, a host or
// a repository.
var identifying = map[string]bool{
	"email": true, "name": true, "alias": true, "org": true, "organization": true,
	"organizationname": true, "account": true, "account_name": true, "repo": true,
	"repository": true, "host": true, "hostname": true, "user": true, "owner": true,
	"displayname": true, "emailaddress": true,
}

// synthetic are the values the demo is allowed to show: example.* domains are
// reserved by RFC 2606, and the two organisations are made up.
var syntheticOrgs = map[string]bool{"acme-demo": true, "example-labs": true}

// generic words are never a leak, whoever's they are.
var generic = map[string]bool{"team": true, "max": true, "pro": true, "free": true, "claude code": true, "default": true}

func keep(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) < 4 || generic[strings.ToLower(v)] || strings.HasPrefix(strings.ToLower(v), "claude code") {
		return false
	}
	// A short hex string (the 4 digits the screen shows after "#") is a substring
	// of half the numbers in a picture: only the whole hash is a name.
	return len(v) >= 8 || !hexRe.MatchString(v)
}

var hexRe = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// payloadRe is an embedded PNG: base64 is noise to a text scan, and a deny-list
// word of six letters shows up in it by chance.
var payloadRe = regexp.MustCompile(`base64,[A-Za-z0-9+/=]*`)

// pathRe is a glyph outline: numbers only, and nine tenths of a capture. The
// words a capture holds are in its titles, its attributes and r.Texts.
var pathRe = regexp.MustCompile(` d="[^"]*"`)

// denyFrom collects the identifying values of the config files in dirs.
func denyFrom(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(d, e.Name()))
			if err != nil || len(b) > 8<<20 {
				continue
			}
			for _, m := range pairRe.FindAllStringSubmatch(string(b), -1) {
				if identifying[strings.ToLower(m[1])] && keep(m[2]) {
					out = append(out, m[2])
				}
			}
			if strings.HasSuffix(e.Name(), ".toml") {
				for _, m := range tomlRe.FindAllStringSubmatch(string(b), -1) {
					if keep(m[2]) {
						out = append(out, m[2])
					}
				}
			}
		}
	}
	return out
}

// buildDeny is the deny-list: account name, organisation and plan of the signed-in
// account (claudeJSON), the aliases and stamps and quota history in cfgDirs, the
// git e-mail, the host and user names, and the names of the clones in cloneDir.
func buildDeny(cfgDirs []string, claudeJSON, gitEmail, host, cloneDir string) []string {
	var deny []string
	if id, ok := accounts.ReadIdentity(claudeJSON, true); ok {
		deny = append(deny, id.Org, id.Key)
		for _, d := range cfgDirs {
			deny = append(deny, accounts.NewAliases(d).Name(id.Key))
		}
	}
	deny = append(deny, denyFrom(cfgDirs)...)
	deny = append(deny, gitEmail, host, os.Getenv("USER"))
	if h, err := os.UserHomeDir(); err == nil {
		deny = append(deny, h, filepath.Base(h))
	}
	if ents, err := os.ReadDir(cloneDir); err == nil {
		for _, e := range ents {
			// <org>-<repo>: a bare word is a clone of a library, not a name.
			if strings.Contains(e.Name(), "-") {
				deny = append(deny, e.Name())
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, v := range deny {
		v = strings.TrimSpace(v)
		if k := strings.ToLower(v); keep(v) && !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}

// leaks returns what in text matches the deny-list or a fixed pattern.
func leaks(text string, deny []string) []string {
	var hits []string
	low := strings.ToLower(text)
	for _, d := range deny {
		if strings.Contains(low, strings.ToLower(d)) {
			hits = append(hits, "deny-list: "+d)
		}
	}
	for _, m := range emailRe.FindAllString(text, -1) {
		dom := strings.ToLower(m[strings.LastIndex(m, "@")+1:])
		if dom != "example.com" && dom != "example.org" && dom != "example.net" && !strings.HasSuffix(dom, ".example") {
			hits = append(hits, "e-mail: "+m)
		}
	}
	for _, m := range ghRe.FindAllString(text, -1) {
		if !syntheticOrgs[strings.ToLower(strings.TrimPrefix(m, "github.com/"))] {
			hits = append(hits, "github: "+m)
		}
	}
	return hits
}

// surfaces is every string a viewer or a tool can read in a capture: the drawn
// strings (the outlines hide them from the markup), the markup, and the decoded
// character data and attribute values (the titles included).
func surfaces(t *testing.T, r *Result) []string {
	t.Helper()
	svg := pathRe.ReplaceAllString(payloadRe.ReplaceAllString(string(r.SVG), "base64,"), "")
	return []string{strings.Join(r.Texts, "\n"), svg, strings.Join(decoded(t, []byte(svg)), "\n")}
}

func decoded(t *testing.T, b []byte) []string {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	dec.Strict = false
	var out []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("the capture is not well-formed XML: %v", err)
		}
		switch v := tok.(type) {
		case xml.CharData:
			out = append(out, string(v))
		case xml.Comment:
			out = append(out, string(v))
		case xml.StartElement:
			for _, a := range v.Attr {
				out = append(out, a.Value)
			}
		}
	}
}

func gitEmail() string {
	b, err := exec.Command("git", "config", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func realDeny() []string {
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	var dirs []string
	if c, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(c, "agentscope"), filepath.Join(c, "aiquota"))
	}
	dirs = append(dirs, filepath.Join(home, ".config", "agentscope"), filepath.Join(home, ".config", "aiquota"))
	return buildDeny(dirs, filepath.Join(home, ".claude.json"), gitEmail(), host, filepath.Join(home, "Documents", "VCS", "GIT", "localhost"))
}

func TestNoCaptureHoldsAnythingOfThisMachine(t *testing.T) {
	deny := realDeny()
	for _, n := range Names() {
		r, err := Render(n, Options{})
		if err != nil {
			t.Fatal(err)
		}
		// The scan must have read the drawn text: a scan of nothing proves nothing.
		if !strings.Contains(strings.Join(r.Texts, "\n"), "Acme Lab") {
			t.Fatalf("%s: the synthetic account name is not among the %d drawn strings", n, len(r.Texts))
		}
		if !strings.Contains(string(r.SVG), "<title>") {
			t.Fatalf("%s: no <title> in the output", n)
		}
		for _, s := range surfaces(t, r) {
			if h := leaks(s, deny); len(h) > 0 {
				t.Errorf("%s: leaks %v", n, h)
			}
		}
	}
}

// fixture is a made-up "real" machine, to prove the deny-list catches what it
// should without depending on this machine's own configuration.
func fixture(t *testing.T) (dirs []string, claudeJSON string) {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "agentscope")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeJSON = filepath.Join(root, "claude.json")
	b, _ := json.Marshal(map[string]any{"oauthAccount": map[string]string{
		"accountUuid": "11111111-2222-3333-4444-555555555555", "organizationName": "Realorg Holdings", "organizationType": "claude_team",
	}})
	os.WriteFile(claudeJSON, b, 0o644)
	key := quota.AccountKey("11111111-2222-3333-4444-555555555555")
	os.WriteFile(filepath.Join(cfg, "accounts.toml"), []byte("\""+key[:4]+"\" = \"Jane Realperson\"\n"), 0o644)
	os.WriteFile(filepath.Join(cfg, "quota.jsonl"), []byte(`{"email":"jane.realperson@realcorp.io","repo":"realcorp-secret","plan":"team"}`+"\n"), 0o644)
	return []string{cfg}, claudeJSON
}

func TestTheLeakScanCatchesAnInjectedRealLookingValue(t *testing.T) {
	dirs, cj := fixture(t)
	deny := buildDeny(dirs, cj, "owner@realcorp.io", "mybox.local", t.TempDir())
	for _, want := range []string{"Realorg Holdings", "Jane Realperson", "jane.realperson@realcorp.io", "realcorp-secret", "owner@realcorp.io", "mybox.local"} {
		found := false
		for _, d := range deny {
			found = found || strings.EqualFold(d, want)
		}
		if !found {
			t.Errorf("%q is not in the deny-list %q", want, deny)
		}
	}
	r, err := Render("colors", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if clean := firstLeak(t, r, deny); clean != "" {
		t.Fatalf("a clean capture reads as leaking: %s", clean)
	}
	// Each injection goes into a different surface of a small, clean capture
	// (decoding a 3 MB one 18 times would only slow the proof down).
	r = &Result{SVG: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><title>Acme Lab</title><g/></svg>`), Texts: []string{"Acme Lab"}}
	if clean := firstLeak(t, r, deny); clean != "" {
		t.Fatalf("a clean stub reads as leaking: %s", clean)
	}
	for _, inj := range []struct{ name, value string }{
		{"name", "Jane Realperson"}, {"org", "REALORG HOLDINGS"}, {"email", "someone@elsewhere.org"},
		{"repo path", "see github.com/realorg/secret"}, {"host", "mybox.local"}, {"deny email", "jane.realperson@realcorp.io"},
	} {
		bad := *r
		bad.Texts = append(append([]string{}, r.Texts...), inj.value)
		if firstLeak(t, &bad, deny) == "" {
			t.Errorf("%s: %q in a drawn string was not caught", inj.name, inj.value)
		}
		bad = *r
		bad.SVG = []byte(strings.Replace(string(r.SVG), "</svg>", "<title>"+inj.value+"</title></svg>", 1))
		if firstLeak(t, &bad, deny) == "" {
			t.Errorf("%s: %q in a <title> was not caught", inj.name, inj.value)
		}
		bad.SVG = []byte(strings.Replace(string(r.SVG), "<svg", `<svg data-x="`+inj.value+`"`, 1))
		if firstLeak(t, &bad, deny) == "" {
			t.Errorf("%s: %q in an attribute was not caught", inj.name, inj.value)
		}
	}
}

func firstLeak(t *testing.T, r *Result, deny []string) string {
	t.Helper()
	for _, s := range surfaces(t, r) {
		if h := leaks(s, deny); len(h) > 0 {
			return strings.Join(h, ", ")
		}
	}
	return ""
}

func TestLeaksFixedPatterns(t *testing.T) {
	for s, want := range map[string]bool{
		"mail me at a@b.io":           true,
		"demo@example.com":            false,
		"x@sub.example.org":           true,
		"see github.com/someorg/repo": true,
		"github.com/acme-demo/api":    false,
		"github.com/Example-Labs/x":   false,
		"plain text, no handle":       false,
		"@ alone, or a @ b":           false,
	} {
		if got := len(leaks(s, nil)) > 0; got != want {
			t.Errorf("%q: leak = %v, want %v", s, got, want)
		}
	}
}

func TestKeepDropsGenericWords(t *testing.T) {
	for v, want := range map[string]bool{"team": false, "Max": false, "abc": false, "Claude Code (max)": false, "Jane Realperson": true, "  padded value ": true} {
		if keep(v) != want {
			t.Errorf("keep(%q) = %v", v, !want)
		}
	}
}

func TestBuildDenyToleratesAMissingMachine(t *testing.T) {
	d := buildDeny([]string{"/nonexistent/dir"}, "/nonexistent/claude.json", "", "", "/nonexistent/clones")
	for _, v := range d {
		if len(v) < 4 {
			t.Errorf("short entry %q", v)
		}
	}
}
