package accounts

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/go-agentsync/agentscope/model"
)

// ParseColor is the one place a colour is accepted: exactly "#" and six hex digits
// (either case), returned in lower case. Nothing else is a colour -- no name, no
// short form, no rgb() or url() -- so a stored value can never carry markup or CSS.
func ParseColor(s string) (string, error) {
	r, g, b, ok := model.ParseColor(s)
	if !ok {
		return "", errors.New("accounts: a colour is #rrggbb")
	}
	return model.FormatColor(r, g, b), nil
}

// Colors are the colours the owner gave accounts, by account hash, kept in
// dir/account-colors.toml next to the aliases:
//
//	"7567" = "#e69f00"
//
// An account without an entry keeps its automatic colour. A missing or corrupt
// file means no colour at all; one entry that is not a colour is dropped, the
// others are kept. The file is read through a size bound and written atomically,
// mode 0600, only by the serve process.
type Colors struct {
	path string
	mu   sync.Mutex
	m    map[string]string
}

// NewColors keeps colours in dir/account-colors.toml and loads them. A key is the
// first 4 hex digits of an account hash or the whole hash, as for Aliases.
func NewColors(dir string) *Colors {
	c := &Colors{path: filepath.Join(dir, "account-colors.toml"), m: map[string]string{}}
	c.Reload()
	return c
}

// Reload reads the file again (the owner may have edited it by hand).
func (c *Colors) Reload() {
	m := map[string]string{}
	if f, err := os.Open(c.path); err == nil {
		raw, _ := parseAliases(io.LimitReader(f, maxAliasFile+1))
		f.Close()
		for k, v := range raw {
			if col, err := ParseColor(v); err == nil {
				m[k] = col
			}
		}
	}
	c.mu.Lock()
	c.m = m
	c.mu.Unlock()
}

// Color is the colour of the account with the hash key, "" when it has none (its
// automatic colour applies).
func (c *Colors) Color(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.m[key]; ok {
		return v
	}
	return c.m[short(key)]
}

// Set gives the account a colour, or restores its automatic one when color is
// empty, and writes the file. A colour that is not #rrggbb is refused and nothing
// is written.
func (c *Colors) Set(key, color string) error {
	if key == "" || strings.ContainsAny(key, "\"\\\r\n\x00") {
		return errors.New("accounts: not an account key")
	}
	if color != "" {
		var err error
		if color, err = ParseColor(color); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	next := make(map[string]string, len(c.m)+1)
	for k, v := range c.m {
		next[k] = v
	}
	delete(next, short(key)) // a short key and a full one name the same account
	delete(next, key)
	if color != "" {
		if len(next) >= maxAliases {
			return errors.New("accounts: too many colours")
		}
		next[key] = color
	}
	if err := writeText(c.path, formatColors(next)); err != nil {
		return err
	}
	c.m = next
	return nil
}

// formatColors is the file's text: a header, then one line per colour in key
// order.
func formatColors(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# agentscope: colours of accounts (#rrggbb), by the hash shown after # on screen.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", quote(k), quote(m[k]))
	}
	return b.String()
}
