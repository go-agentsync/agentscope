package ui

import (
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/i18ntest"
)

// resetLang puts the process-wide localizer back after a test that changed it.
func resetLang(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { toolkit.SetLocalizer(nil) })
}

func TestCatalogIsComplete(t *testing.T) { i18ntest.Check(t, Catalog) }

type prefSent struct{ theme, lang string }

func recordPrefs(vm *ViewModel) *[]prefSent {
	var got []prefSent
	vm.Prefs.Send = func(th, l string) { got = append(got, prefSent{th, l}) }
	return &got
}

func TestThemeChoiceDrivesDarkAndIsSent(t *testing.T) {
	resetLang(t)
	vm := NewViewModel()
	got := recordPrefs(vm)
	vm.Prefs.ThemeSel.Set(2)
	if !vm.Dark.Get() || len(*got) != 1 || (*got)[0] != (prefSent{"dark", ""}) {
		t.Fatalf("dark: Dark=%v sent=%v", vm.Dark.Get(), *got)
	}
	vm.Prefs.ThemeSel.Set(1)
	if vm.Dark.Get() || (*got)[1] != (prefSent{"light", ""}) {
		t.Fatalf("light: Dark=%v sent=%v", vm.Dark.Get(), *got)
	}
	vm.Prefs.SetSystemDark(true) // the choice is light: the system does not matter
	if vm.Dark.Get() {
		t.Fatal("a fixed light theme followed the system")
	}
	vm.Prefs.ThemeSel.Set(0) // system, which is dark
	if !vm.Dark.Get() || (*got)[2] != (prefSent{"system", ""}) {
		t.Fatalf("system dark: Dark=%v sent=%v", vm.Dark.Get(), *got)
	}
	vm.Prefs.SetSystemDark(false)
	if vm.Dark.Get() {
		t.Fatal("the system theme did not follow the system")
	}
}

func TestLanguageChoiceTranslatesAndIsSent(t *testing.T) {
	resetLang(t)
	vm := NewViewModel()
	got := recordPrefs(vm)
	if T("btn.cancel") != "Cancel" || vm.Prefs.Lang.Get() != "en" {
		t.Fatal("the default language is English")
	}
	vm.Prefs.LangSel.Set(2)
	if vm.Prefs.Lang.Get() != "fr" || T("btn.cancel") != "Annuler" || (*got)[0] != (prefSent{"system", "fr"}) {
		t.Fatalf("fr: lang %q, %q, sent %v", vm.Prefs.Lang.Get(), T("btn.cancel"), *got)
	}
	if LangNames()[0] != "Langue : Auto" || vm.Status.Get() != "en attente du collecteur..." {
		t.Fatalf("the chrome stayed English: %q / %q", LangNames()[0], vm.Status.Get())
	}
	vm.Prefs.LangSel.Set(0) // automatic, no browser language: English
	if vm.Prefs.Lang.Get() != "en" || T("btn.cancel") != "Cancel" {
		t.Fatal("automatic without a browser language must be English")
	}
	vm.Prefs.SetNavLangs([]string{"de", "fr-CH"})
	if vm.Prefs.Lang.Get() != "fr" {
		t.Fatalf("automatic follows the browser: %q", vm.Prefs.Lang.Get())
	}
	vm.Prefs.LangSel.Set(1) // English, whatever the browser says
	if vm.Prefs.Lang.Get() != "en" {
		t.Fatal("a chosen language beats the browser's")
	}
}

func TestSameLanguageIsNotApplyTwice(t *testing.T) {
	resetLang(t)
	vm := NewViewModel()
	vm.Status.Set("kept")                   // a re-localisation would put the waiting text back
	vm.Prefs.SetNavLangs([]string{"en-GB"}) // already English
	if vm.Status.Get() != "kept" {
		t.Fatalf("the language was re-applied: %q", vm.Status.Get())
	}
}

func TestChoiceWithoutAHostDoesNotPanic(t *testing.T) {
	resetLang(t)
	vm := NewViewModel() // no Send: the host has not set it
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("a choice with no Send panicked: %v", r)
		}
	}()
	vm.Prefs.ThemeSel.Set(2)
	vm.Prefs.LangSel.Set(2)
}

func TestLanguageSwitchKeepsTheSnapshotsStatus(t *testing.T) {
	resetLang(t)
	vm := NewViewModel()
	vm.Apply(withUsage(snap()))
	live := vm.Status.Get()
	if live == T("status.waiting") {
		t.Fatal("a snapshot must replace the waiting text")
	}
	vm.Prefs.LangSel.Set(2)
	if vm.Status.Get() == "en attente du collecteur..." {
		t.Fatal("the switch discarded the snapshot and showed the waiting text")
	}
}

func TestManagerThemesAreTheBrandsSlate(t *testing.T) {
	vm := NewViewModel()
	vm.Prefs.ThemeSel.Set(1)
	light := vm.Prefs.Manager.Theme()
	vm.Prefs.ThemeSel.Set(2)
	dark := vm.Prefs.Manager.Theme()
	if light == nil || dark == nil || light == dark {
		t.Fatal("the light and dark themes must be two themes")
	}
}

func TestServerPrefsAreAppliedWithoutEcho(t *testing.T) {
	resetLang(t)
	vm := NewViewModel()
	got := recordPrefs(vm)
	vm.Prefs.Apply(model.Prefs{Theme: "dark", Lang: "fr"})
	if vm.Prefs.ThemeSel.Get() != 2 || vm.Prefs.LangSel.Get() != 2 || !vm.Dark.Get() || vm.Prefs.Lang.Get() != "fr" {
		t.Fatalf("not applied: %d %d dark=%v %q", vm.Prefs.ThemeSel.Get(), vm.Prefs.LangSel.Get(), vm.Dark.Get(), vm.Prefs.Lang.Get())
	}
	if len(*got) != 0 {
		t.Fatalf("a value that came from the server was sent back: %v", *got)
	}
	vm.Prefs.ThemeSel.Set(1) // the owner changes it again: that one is sent
	vm.Prefs.Apply(model.Prefs{Theme: "dark", Lang: "fr"})
	if vm.Prefs.ThemeSel.Get() != 1 {
		t.Fatal("an unchanged snapshot must not undo the owner's choice")
	}
	vm.Prefs.Apply(model.Prefs{Theme: "neon", Lang: "zz"}) // refused values mean "not chosen"
	if vm.Prefs.ThemeSel.Get() != 0 || vm.Prefs.LangSel.Get() != 0 {
		t.Fatalf("invalid prefs must read as system and automatic: %d %d", vm.Prefs.ThemeSel.Get(), vm.Prefs.LangSel.Get())
	}
}

func TestSceneFollowsLanguage(t *testing.T) {
	resetLang(t)
	s := newScene(t)
	vm := s.VM()
	vm.Prefs.LangSel.Set(2)
	if s.langDD.Options[0] != "Langue : Auto" || s.themeDD.Options[2] != "Thème : Sombre" || s.expired.Label != "baux expirés" {
		t.Fatalf("the header stayed English: %q %q %q", s.langDD.Options[0], s.themeDD.Options[2], s.expired.Label)
	}
	pixels(s)
	vm.Prefs.LangSel.Set(1)
	if s.expired.Label != "expired leases" {
		t.Fatalf("back to English: %q", s.expired.Label)
	}
}

func TestDataTextsFollowLanguage(t *testing.T) {
	resetLang(t)
	s := newScene(t)
	vm := s.VM()
	vm.Apply(withUsage(snapWith(guardFixture())))
	vm.Tab.Set(TabGuardrails)
	vm.Guard.Sel.Set(1)
	vm.Prefs.LangSel.Set(2)
	if !strings.Contains(vm.Status.Get(), "sessions affichées") {
		t.Fatalf("status: %q", vm.Status.Get())
	}
	if !strings.HasPrefix(s.gChips[0].Text, "non triées") || !strings.HasPrefix(s.gChips[3].Text, "refus ") {
		t.Fatalf("chips: %q %q", s.gChips[0].Text, s.gChips[3].Text)
	}
	if strings.Contains(s.gNotes.Text().Get(), "undated recurrences") {
		t.Fatalf("guard notes: %q", s.gNotes.Text().Get())
	}
	if l := vm.Guard.Detail.At(2); !strings.HasPrefix(l, "décision : ") {
		t.Fatalf("detail: %q", l)
	}
	vm.Tab.Set(TabQuota)
	if !strings.Contains(vm.QuotaNotes.Get(), "capacité restante") {
		t.Fatalf("quota notes: %q", vm.QuotaNotes.Get())
	}
	vm.Prefs.LangSel.Set(1)
	vm.Tab.Set(TabGuardrails)
	if !strings.HasPrefix(s.gChips[0].Text, "untriaged") {
		t.Fatalf("back to English: %q", s.gChips[0].Text)
	}
}

func TestConnectionTextFollowsLanguage(t *testing.T) {
	resetLang(t)
	vm := NewViewModel()
	vm.SetConn("lost", "eof")
	if vm.Conn.Get() == "" {
		t.Fatal("no connection text")
	}
	en := vm.Conn.Get()
	vm.Prefs.LangSel.Set(2)
	if vm.Conn.Get() == en {
		t.Fatalf("the connection text %q did not change language", en)
	}
}
