package model

import "testing"

func TestParseColorAcceptsOnlySixHexDigits(t *testing.T) {
	for in, want := range map[string][3]uint8{"#e69f00": {0xe6, 0x9f, 0}, "#E69F00": {0xe6, 0x9f, 0}, "#000000": {}, "#ffFFff": {255, 255, 255}} {
		r, g, b, ok := ParseColor(in)
		if !ok || [3]uint8{r, g, b} != want {
			t.Errorf("%q = %d %d %d %v", in, r, g, b, ok)
		}
	}
	for _, bad := range []string{"", "#", "e69f00", "#e69f0", "#e69f000", "#e69f0g", "#e69g00", "#g69f00", "#e69f0\n", "red", "rgb(1,2,3)", "#fff", "url(x)", "#e69f00\"", "<b>", "#é69f0"} {
		if r, g, b, ok := ParseColor(bad); ok || r|g|b != 0 {
			t.Errorf("%q accepted: %d %d %d", bad, r, g, b)
		}
	}
}

func TestFormatColorRoundTrips(t *testing.T) {
	if got := FormatColor(0xe6, 0x9f, 0x00); got != "#e69f00" {
		t.Fatalf("%q", got)
	}
	for _, c := range [][3]uint8{{0, 0, 0}, {255, 255, 255}, {1, 16, 171}, {0x12, 0x34, 0xab}} {
		r, g, b, ok := ParseColor(FormatColor(c[0], c[1], c[2]))
		if !ok || [3]uint8{r, g, b} != c {
			t.Errorf("%v came back %d %d %d %v", c, r, g, b, ok)
		}
	}
}
