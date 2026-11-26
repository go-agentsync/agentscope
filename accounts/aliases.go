package accounts

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Bounds on the alias file: it is a hand-edited list of a few accounts.
const (
	maxAliasFile = 64 << 10
	maxAliases   = 256
	// MaxName is the longest display name, in bytes.
	MaxName = 64
)

// Aliases are the display names the owner gave accounts, by account hash, kept
// in dir/accounts.toml:
//
//	"7567" = "Labo (team)"
//
// A missing or corrupt file means no alias at all, never an error on screen: the
// account is then shown as "label #hash". The file is written atomically, mode
// 0600, only by the serve process.
type Aliases struct {
	path string
	mu   sync.Mutex
	m    map[string]string
}

// NewAliases keeps aliases in dir/accounts.toml and loads them. A key is the
// first 4 hex digits of an account hash (what the screen shows after "#") or
// the whole hash.
func NewAliases(dir string) *Aliases {
	a := &Aliases{path: filepath.Join(dir, "accounts.toml"), m: map[string]string{}}
	a.Reload()
	return a
}

// Reload reads the file again (the owner may have edited it by hand). A file
// that cannot be read or parsed leaves no aliases.
func (a *Aliases) Reload() {
	m := map[string]string{}
	if f, err := os.Open(a.path); err == nil {
		m, _ = parseAliases(io.LimitReader(f, maxAliasFile+1))
		f.Close()
	}
	a.mu.Lock()
	a.m = m
	a.mu.Unlock()
}

// Name is the display name of the account with the hash key, "" when it has
// none.
func (a *Aliases) Name(key string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n, ok := a.m[key]; ok {
		return n
	}
	return a.m[short(key)]
}

// short is the first 4 characters of a hash: how the screen names an account.
func short(key string) string { return key[:min(4, len(key))] }

// Set gives the account a display name, or removes its alias when name is empty
// (after trimming), and writes the file. The name is bounded and must not carry
// control characters.
func (a *Aliases) Set(key, name string) error {
	name = strings.TrimSpace(name)
	if key == "" || strings.ContainsAny(key, "\"\\\r\n\x00") {
		return errors.New("accounts: not an account key")
	}
	if len(name) > MaxName || !utf8.ValidString(name) || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return errors.New("accounts: not a usable display name")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := make(map[string]string, len(a.m)+1)
	for k, v := range a.m {
		next[k] = v
	}
	delete(next, short(key)) // a short key and a full one name the same account
	if name == "" {
		delete(next, key)
	} else {
		if len(next) >= maxAliases {
			return errors.New("accounts: too many aliases")
		}
		next[key] = name
	}
	if err := writeAliases(a.path, next); err != nil {
		return err
	}
	a.m = next
	return nil
}

// tempFile is the part of *os.File that writeAliases uses; a test substitutes
// one whose steps fail, which a real file does not do on demand.
type tempFile interface {
	Name() string
	Chmod(os.FileMode) error
	WriteString(string) (int, error)
	Close() error
}

var createTemp = func(dir, pattern string) (tempFile, error) { return os.CreateTemp(dir, pattern) }

// writeAliases replaces the file atomically: a temporary file in the same
// directory, mode 0600, renamed over it.
func writeAliases(path string, m map[string]string) error {
	return writeText(path, formatAliases(m))
}

// writeText replaces the file at path with text, atomically, mode 0600: the
// writer both the alias file and the colour file share.
func writeText(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := createTemp(filepath.Dir(path), ".accounts-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// formatAliases is the file's text: a header, then one line per alias in key
// order.
func formatAliases(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# agentscope: display names of accounts, by the hash shown after # on screen.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", quote(k), quote(m[k]))
	}
	return b.String()
}

// quote writes a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// parseAliases reads the subset of TOML the file uses: comments, blank lines
// and `"key" = "value"` (or bare-key) lines with basic strings. Anything else
// makes the whole file corrupt, reported as an error with no aliases, because a
// half-read alias file would name accounts wrongly.
func parseAliases(r io.Reader) (map[string]string, error) {
	m := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 4096)
	total := 0
	for n := 1; sc.Scan(); n++ {
		total += len(sc.Text())
		if total > maxAliasFile {
			return map[string]string{}, errors.New("alias file too large")
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		key, rest, err := readKey(line)
		if err != nil {
			return map[string]string{}, fmt.Errorf("line %d: %w", n, err)
		}
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "=") {
			return map[string]string{}, fmt.Errorf("line %d: expected =", n)
		}
		val, tail, err := readString(strings.TrimSpace(rest[1:]))
		if err != nil {
			return map[string]string{}, fmt.Errorf("line %d: %w", n, err)
		}
		if t := strings.TrimSpace(tail); t != "" && t[0] != '#' {
			return map[string]string{}, fmt.Errorf("line %d: text after the value", n)
		}
		if len(val) > MaxName || key == "" || len(m) >= maxAliases {
			return map[string]string{}, fmt.Errorf("line %d: out of bounds", n)
		}
		m[key] = val
	}
	if err := sc.Err(); err != nil {
		return map[string]string{}, err
	}
	return m, nil
}

// readKey reads a quoted or bare key and returns what follows it.
func readKey(s string) (key, rest string, err error) {
	if s[0] == '"' {
		return readString(s)
	}
	i := strings.IndexAny(s, " \t=")
	if i <= 0 {
		return "", "", errors.New("bad key")
	}
	for _, r := range s[:i] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r == '-') {
			return "", "", errors.New("bad bare key")
		}
	}
	return s[:i], s[i:], nil
}

// readString reads a TOML basic string at the start of s and returns what
// follows it.
func readString(s string) (val, rest string, err error) {
	if s == "" || s[0] != '"' {
		return "", "", errors.New("expected a string")
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			return b.String(), s[i+1:], nil
		case c == '\\':
			i++
			if i >= len(s) {
				return "", "", errors.New("unterminated escape")
			}
			switch s[i] {
			case '"', '\\':
				b.WriteByte(s[i])
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if i+4 >= len(s) {
					return "", "", errors.New("short \\u escape")
				}
				v, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
				if err != nil || !utf8.ValidRune(rune(v)) {
					return "", "", errors.New("bad \\u escape")
				}
				b.WriteRune(rune(v))
				i += 4
			default:
				return "", "", errors.New("unknown escape")
			}
		case c < 0x20 || c == 0x7f:
			return "", "", errors.New("control character in a string")
		default:
			b.WriteByte(c)
		}
	}
	return "", "", errors.New("unterminated string")
}
