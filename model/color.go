package model

// ParseColor is the one place a colour is accepted, on the server and in the
// view: exactly "#" and six hex digits (either case). Nothing else is a colour --
// no name, no short form, no rgb() or url() -- so a value read from a file or a
// snapshot can never carry markup or CSS.
func ParseColor(s string) (r, g, b uint8, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	var v [3]uint8
	for i := range v {
		hi, ok1 := hexDigit(s[1+2*i])
		lo, ok2 := hexDigit(s[2+2*i])
		if !ok1 || !ok2 {
			return 0, 0, 0, false
		}
		v[i] = hi<<4 | lo
	}
	return v[0], v[1], v[2], true
}

func hexDigit(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// FormatColor is the colour as "#rrggbb", in lower case: the form that is stored
// and sent.
func FormatColor(r, g, b uint8) string {
	const digits = "0123456789abcdef"
	return string([]byte{'#', digits[r>>4], digits[r&15], digits[g>>4], digits[g&15], digits[b>>4], digits[b&15]})
}
