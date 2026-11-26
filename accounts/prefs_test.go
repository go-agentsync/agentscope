package accounts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/model"
)

func TestPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := NewPrefs(dir)
	if got := p.Get(); got != (model.Prefs{}) {
		t.Fatalf("nothing chosen: %+v", got)
	}
	if err := p.Set("light", "pt_BR"); err != nil {
		t.Fatal(err)
	}
	want := model.Prefs{Theme: "light", Lang: "pt-br"}
	if got := p.Get(); got != want {
		t.Fatalf("%+v", got)
	}
	// A new process reads the same file.
	if got := NewPrefs(dir).Get(); got != want {
		t.Fatalf("reloaded: %+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "theme = \"light\"\n") || !strings.Contains(string(raw), "lang = \"pt-br\"\n") {
		t.Fatalf("file text:\n%s", raw)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "settings.toml")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	// Empty values mean "not chosen" and leave no line.
	if err := p.Set("", ""); err != nil {
		t.Fatal(err)
	}
	if got := NewPrefs(dir).Get(); got != (model.Prefs{}) {
		t.Fatalf("cleared: %+v", got)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "settings.toml"))
	if strings.Contains(string(raw), "theme =") || strings.Contains(string(raw), "lang =") {
		t.Fatalf("cleared file text:\n%s", raw)
	}
}

func TestPrefsRefuseWhatIsNotAThemeOrALanguage(t *testing.T) {
	dir := t.TempDir()
	p := NewPrefs(dir)
	if err := p.Set("dark", "fr"); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{
		{"blue", "fr"}, {"Dark ", "fr"}, {"dark", "f"}, {"dark", "fr\nx = 1"}, {"dark", "français"}, {"dark", "fr-"},
		{"dark", strings.Repeat("a", model.MaxLangLen+1)}, {"dark", "1a"},
	} {
		if err := p.Set(c[0], c[1]); err == nil {
			t.Errorf("accepted theme %q lang %q", c[0], c[1])
		}
	}
	// Nothing changed, in memory or on disk.
	want := model.Prefs{Theme: "dark", Lang: "fr"}
	if got := p.Get(); got != want {
		t.Fatalf("memory: %+v", got)
	}
	if got := NewPrefs(dir).Get(); got != want {
		t.Fatalf("disk: %+v", got)
	}
}

func TestPrefsKeepTheGoodValueOfAHandEditedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("theme = \"purple\"\nlang = \"fr\"\n")
	if got := NewPrefs(dir).Get(); got != (model.Prefs{Lang: "fr"}) {
		t.Fatalf("a bad theme is dropped, the language kept: %+v", got)
	}
	write("theme = \"dark\"\nlang = \"<script>\"\n")
	if got := NewPrefs(dir).Get(); got != (model.Prefs{Theme: "dark"}) {
		t.Fatalf("a bad language is dropped, the theme kept: %+v", got)
	}
	write("this is not toml\n")
	if got := NewPrefs(dir).Get(); got != (model.Prefs{}) {
		t.Fatalf("a corrupt file chooses nothing: %+v", got)
	}
	// Reload picks up a hand edit.
	p := NewPrefs(dir)
	write("theme = \"light\"\n")
	p.Reload()
	if got := p.Get(); got != (model.Prefs{Theme: "light"}) {
		t.Fatalf("reload: %+v", got)
	}
}

func TestPrefsWriteFailureChangesNothing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, nil, 0o600)
	p := NewPrefs(filepath.Join(file, "x"))
	if err := p.Set("dark", "fr"); err == nil {
		t.Fatal("an unwritable directory")
	}
	if got := p.Get(); got != (model.Prefs{}) {
		t.Fatalf("a failed write changed the memory: %+v", got)
	}
}
