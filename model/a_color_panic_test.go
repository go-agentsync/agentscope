package model

import "testing"

// A refusal that is deleted must fail on an assertion, not by a crash somewhere
// after it: the guard on the length is what keeps s[0] and s[1:] in range. The
// panic is recovered and reported as the failure it is.
func TestParseColorRefusesShortInputsWithoutPanicking(t *testing.T) {
	for _, in := range []string{"", "#", "#1", "#12345", "#1234567", "1234567"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseColor(%q) panicked: %v", in, r)
				}
			}()
			if _, _, _, ok := ParseColor(in); ok {
				t.Errorf("ParseColor(%q) accepted it", in)
			}
		}()
	}
}
