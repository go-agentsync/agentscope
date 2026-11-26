package accounts

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-agentsync/agentscope/model"
)

// Prefs are the viewer's theme and language, kept in dir/settings.toml next to
// account-colors.toml:
//
//	theme = "dark"
//	lang = "fr"
//
// A missing or corrupt file means nothing chosen; a value that is not a theme or
// a language is dropped, the other is kept. The file is read through a size bound
// and written atomically, mode 0600, only by the serve process.
type Prefs struct {
	path string
	mu   sync.Mutex
	p    model.Prefs
}

// NewPrefs keeps the preferences in dir/settings.toml and loads them.
func NewPrefs(dir string) *Prefs {
	p := &Prefs{path: filepath.Join(dir, "settings.toml")}
	p.Reload()
	return p
}

// Reload reads the file again (the owner may have edited it by hand).
func (p *Prefs) Reload() {
	var next model.Prefs
	if f, err := os.Open(p.path); err == nil {
		raw, _ := parseAliases(io.LimitReader(f, maxAliasFile+1))
		f.Close()
		if t, ok := model.ParseTheme(raw["theme"]); ok {
			next.Theme = t
		}
		if l, ok := model.ParseLang(raw["lang"]); ok {
			next.Lang = l
		}
	}
	p.mu.Lock()
	p.p = next
	p.mu.Unlock()
}

// Get is what is chosen now.
func (p *Prefs) Get() model.Prefs {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.p
}

// Set stores a choice and writes the file. An empty theme or language means "not
// chosen" (system, automatic); anything else must pass ParseTheme or ParseLang,
// or the whole call is refused and nothing is written.
func (p *Prefs) Set(theme, lang string) error {
	var next model.Prefs
	if theme != "" {
		t, ok := model.ParseTheme(theme)
		if !ok {
			return errors.New("accounts: a theme is light, dark or system")
		}
		next.Theme = t
	}
	if lang != "" {
		l, ok := model.ParseLang(lang)
		if !ok {
			return errors.New("accounts: not a language tag")
		}
		next.Lang = l
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := writeText(p.path, formatPrefs(next)); err != nil {
		return err
	}
	p.p = next
	return nil
}

// formatPrefs is the file's text: a header, then a line per choice made.
func formatPrefs(p model.Prefs) string {
	var b strings.Builder
	b.WriteString("# agentscope: the viewer's choices. theme: light, dark or system; lang: a language tag such as fr.\n")
	if p.Theme != "" {
		b.WriteString("theme = " + quote(p.Theme) + "\n")
	}
	if p.Lang != "" {
		b.WriteString("lang = " + quote(p.Lang) + "\n")
	}
	return b.String()
}
