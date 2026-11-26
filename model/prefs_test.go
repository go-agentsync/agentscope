package model

import "testing"

func TestParseTheme(t *testing.T) {
	for _, s := range []string{"light", "dark", "system"} {
		if got, ok := ParseTheme(s); !ok || got != s {
			t.Errorf("%q: %q %v", s, got, ok)
		}
	}
	for _, s := range []string{"", "Light", "dark ", "blue", "system\n"} {
		if got, ok := ParseTheme(s); ok || got != "" {
			t.Errorf("%q accepted: %q", s, got)
		}
	}
}

func TestParseLang(t *testing.T) {
	good := map[string]string{"fr": "fr", "FR": "fr", "pt_BR": "pt-br", "zh-Hant-TW": "zh-hant-tw", "fil": "fil", "en-1": "en-1"}
	for in, want := range good {
		if got, ok := ParseLang(in); !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	for _, s := range []string{"", "f", "frfr", "1a", "fr-", "-fr", "fr--ca", "fr-abcdefghi", "fr ca", "fr\n", "français", "f1", "<b>"} {
		if got, ok := ParseLang(s); ok || got != "" {
			t.Errorf("%q accepted: %q", s, got)
		}
	}
	long := "fr-" + "abcdefgh-abcdefgh-abcdefgh-abcdefgh"
	if _, ok := ParseLang(long); ok {
		t.Error("a tag over MaxLangLen was accepted")
	}
	if _, ok := ParseLang(long[:MaxLangLen]); !ok {
		t.Errorf("a tag of exactly MaxLangLen (%d) was refused: %q", MaxLangLen, long[:MaxLangLen])
	}
}
