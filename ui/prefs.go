package ui

import (
	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/toolkit"
)

// Themes are the theme selector's choices, in the order of its options; Langs the
// language selector's, after its "Auto" option. A theme is kept as the word the
// server stores (model.ParseTheme), a language as its catalog tag.
var (
	Themes = []string{"system", "light", "dark"}
	Langs  = []string{"en", "fr"}
)

// ThemeNames and LangNames are the selectors' options in the language in use:
// "Auto" (follow the system, or the browser's languages) first.
func ThemeNames() []string {
	return []string{T("theme.system"), T("theme.light"), T("theme.dark")}
}

func LangNames() []string {
	out := []string{T("lang.auto")}
	for _, l := range Langs {
		out = append(out, T("lang.name."+l))
	}
	return out
}

// PrefsState is the viewer's theme and language: the two selectors' choices, the
// toolkit's ThemeManager that turns a theme choice (and the system's preference)
// into light or dark, and the language in force. A choice the owner makes is sent
// to the server through Send; one that comes from the server (a snapshot's Prefs)
// is applied without being sent back.
type PrefsState struct {
	vm *ViewModel

	// Manager answers whether the screen is dark: the chosen theme, or the system's
	// preference (SetSystemDark) while the choice is "system".
	Manager *toolkit.ThemeManager
	// ThemeSel is the theme drop-down's index into Themes; LangSel the language
	// drop-down's: 0 is automatic, i the i-th of Langs.
	ThemeSel, LangSel *mvvm.Observable[int]
	// ThemeOpts and LangOpts are those drop-downs' options, in the language in use.
	ThemeOpts, LangOpts *mvvm.ObservableList[string]
	// Lang is the language in force ("en", "fr"): the chosen one, else the best of
	// NavLangs, else English.
	Lang *mvvm.Observable[string]
	// NavLangs are the browser's languages, most wanted first (navigator.languages).
	NavLangs []string
	// Send asks the server to keep the choice (theme "system", "light" or "dark";
	// lang "" for automatic); the host sets it, and it must not block.
	Send func(theme, lang string)

	applying bool        // a server value is being applied: nothing is sent back
	last     model.Prefs // the prefs last applied from a snapshot
}

func newPrefsState(vm *ViewModel) *PrefsState {
	fam := toolkit.ThemeFamily{
		Name:  "agentscope",
		Light: func() *toolkit.Theme { return toolkit.Slate(Brand, false) },
		Dark:  func() *toolkit.Theme { return toolkit.Slate(Brand, true) },
	}
	p := &PrefsState{
		vm: vm, Manager: toolkit.NewThemeManager(fam),
		ThemeSel: mvvm.NewObservable(0), LangSel: mvvm.NewObservable(0), Lang: mvvm.NewObservable("en"),
		ThemeOpts: mvvm.NewObservableList(ThemeNames()...), LangOpts: mvvm.NewObservableList(LangNames()...),
	}
	p.Manager.OnChange(func() { vm.Dark.Set(p.Manager.Dark()) })
	p.ThemeSel.SubscribeChanged(p.onTheme)
	p.LangSel.SubscribeChanged(p.onLang)
	return p
}

// SetSystemDark tells the manager what the system prefers (the browser's
// prefers-color-scheme), now and whenever it changes.
func (p *PrefsState) SetSystemDark(dark bool) {
	p.Manager.SetSystemDark(dark)
	p.vm.Dark.Set(p.Manager.Dark())
}

// SetNavLangs gives the browser's languages and applies the negotiation again.
func (p *PrefsState) SetNavLangs(langs []string) {
	p.NavLangs = langs
	p.resolveLang()
}

func (p *PrefsState) theme() string { return Themes[p.ThemeSel.Get()] }

func (p *PrefsState) lang() string {
	if i := p.LangSel.Get(); i > 0 {
		return Langs[i-1]
	}
	return ""
}

func (p *PrefsState) send() {
	if p.applying || p.Send == nil {
		return
	}
	p.Send(p.theme(), p.lang())
}

func (p *PrefsState) onTheme() {
	mode, _ := toolkit.ParseThemeMode(p.theme())
	p.Manager.SetMode(mode)
	p.vm.Dark.Set(p.Manager.Dark())
	p.send()
}

func (p *PrefsState) onLang() {
	p.resolveLang()
	p.send()
}

// resolveLang puts the language in force: the toolkit's localizer first, so every
// text read after it is in the new language, then the observable the Scene follows.
func (p *PrefsState) resolveLang() {
	want := Catalog.Fallback()
	if l := p.lang(); l != "" {
		want = Catalog.Negotiate([]string{l})
	} else if len(p.NavLangs) > 0 {
		want = Catalog.Negotiate(p.NavLangs)
	}
	if want == p.Lang.Get() {
		return
	}
	toolkit.SetLocalizer(Catalog.Localizer(want))
	p.Lang.Set(want)
	p.vm.relocalize()
}

// Apply shows the prefs the server holds. An empty or unknown value is "not
// chosen" (system, automatic); nothing is sent back.
func (p *PrefsState) Apply(in model.Prefs) {
	if in == p.last {
		return
	}
	p.last = in
	p.applying = true
	defer func() { p.applying = false }()
	th, _ := model.ParseTheme(in.Theme)
	p.ThemeSel.Set(indexOf(Themes, th, 0))
	lang := ""
	if l, ok := model.ParseLang(in.Lang); ok {
		lang = l
	}
	p.LangSel.Set(indexOf(Langs, lang, -1) + 1)
}

// indexOf is the position of s in list, or def.
func indexOf(list []string, s string, def int) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return def
}
