package model

import "strings"

// Prefs are the viewer's own choices, kept by the serve process in settings.toml
// (next to account-colors.toml) and carried by every snapshot, so each browser
// opens in the theme and the language its owner chose. An empty field is "not
// chosen": the theme then follows the system and the language is negotiated from
// the browser's languages.
type Prefs struct {
	Theme string `json:"theme,omitempty"` // "light", "dark" or "system"; "" is system
	Lang  string `json:"lang,omitempty"`  // a language tag such as "fr" or "pt-br"; "" is automatic
}

// MaxLangLen bounds a stored language tag (RFC 5646 allows 35 characters).
const MaxLangLen = 35

// ParseTheme is the one place a theme is accepted: exactly "light", "dark" or
// "system", in lower case. Nothing else is a theme.
func ParseTheme(s string) (string, bool) {
	switch s {
	case "light", "dark", "system":
		return s, true
	}
	return "", false
}

// ParseLang is the one place a language is accepted: letters and digits in
// subtags of 1 to 8 characters, joined by "-" (a "_" is read as "-"), the first
// of 2 or 3 letters, at most MaxLangLen long; the result is in lower case. It
// does not check that the language is one the application has a catalog for:
// the view falls back to English for that.
func ParseLang(s string) (string, bool) {
	s = strings.ToLower(strings.ReplaceAll(s, "_", "-"))
	if s == "" || len(s) > MaxLangLen {
		return "", false
	}
	for i, sub := range strings.Split(s, "-") {
		if len(sub) < 1 || len(sub) > 8 || (i == 0 && (len(sub) < 2 || len(sub) > 3)) {
			return "", false
		}
		for _, r := range sub {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') || (i == 0 && r < 'a') {
				return "", false
			}
		}
	}
	return s, true
}
