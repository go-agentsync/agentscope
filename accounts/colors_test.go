package accounts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseColorIsStrict(t *testing.T) {
	for in, want := range map[string]string{"#E69F00": "#e69f00", "#e69f00": "#e69f00", "#000000": "#000000", "#fFfFfF": "#ffffff"} {
		if got, err := ParseColor(in); err != nil || got != want {
			t.Errorf("ParseColor(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "#", "e69f00", "#e69f0", "#e69f000", "#e69f0g", "#e69f0\n", "red", "rgb(1,2,3)", "#fff", "url(x)", "#e69f00\"", "<b>"} {
		if got, err := ParseColor(bad); err == nil || got != "" {
			t.Errorf("ParseColor(%q) = %q, %v; want a refusal", bad, got, err)
		}
	}
}

func TestColorsMissingFileIsDefaults(t *testing.T) {
	c := NewColors(t.TempDir())
	if got := c.Color("7567abcd"); got != "" {
		t.Fatalf("no file, got %q", got)
	}
}

func TestColorsRoundTripAndReset(t *testing.T) {
	dir := t.TempDir()
	c := NewColors(dir)
	if err := c.Set("7567abcd", "#E69F00"); err != nil {
		t.Fatal(err)
	}
	if got := c.Color("7567abcd"); got != "#e69f00" {
		t.Fatalf("got %q", got)
	}
	// An entry written by hand under the short key names the account too, and
	// setting the full key replaces it.
	if err := os.WriteFile(filepath.Join(dir, "account-colors.toml"), []byte("\"7567\" = \"#010203\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Reload()
	if got := c.Color("7567abcd"); got != "#010203" {
		t.Fatalf("short-key entry: got %q", got)
	}
	if err := c.Set("7567abcd", "#E69F00"); err != nil {
		t.Fatal(err)
	}
	if got := c.Color("7567abcd"); got != "#e69f00" {
		t.Fatalf("got %q", got)
	}
	if text, _ := os.ReadFile(filepath.Join(dir, "account-colors.toml")); strings.Contains(string(text), "\"7567\" =") {
		t.Fatalf("the short entry survived: %s", text)
	}
	// A new process reads it back: it survives a restart.
	again := NewColors(dir)
	if got := again.Color("7567abcd"); got != "#e69f00" {
		t.Fatalf("after a restart got %q", got)
	}
	info, err := os.Stat(filepath.Join(dir, "account-colors.toml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v, %v", info, err)
	}
	if err := again.Set("7567abcd", ""); err != nil {
		t.Fatal(err)
	}
	if got := NewColors(dir).Color("7567abcd"); got != "" {
		t.Fatalf("reset left %q", got)
	}
	// Resetting an account that has none is harmless.
	if err := again.Set("beef0000", ""); err != nil {
		t.Fatal(err)
	}
}

func TestColorsRefuseWhatIsNotAColour(t *testing.T) {
	dir := t.TempDir()
	c := NewColors(dir)
	for _, bad := range []string{"red", "#12", "#12345g", "</style><script>", "#e69f00; x:y"} {
		if err := c.Set("7567abcd", bad); err == nil {
			t.Errorf("Set(%q) accepted", bad)
		}
	}
	for _, key := range []string{"", "a\"b", "a\nb", "a\\b", "a\x00b"} {
		if err := c.Set(key, "#e69f00"); err == nil {
			t.Errorf("key %q accepted", key)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "account-colors.toml")); err == nil {
		t.Fatal("a refused value wrote the file")
	}
}

func TestColorsReloadDropsOnlyTheBadEntries(t *testing.T) {
	dir := t.TempDir()
	text := "\"aaaa\" = \"#00ff00\"\n\"bbbb\" = \"red\"\n\"cccc\" = \"</style>\"\n\"dddd\" = \"#ABCDEF\"\n"
	if err := os.WriteFile(filepath.Join(dir, "account-colors.toml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewColors(dir)
	for k, want := range map[string]string{"aaaa": "#00ff00", "bbbb": "", "cccc": "", "dddd": "#abcdef"} {
		if got := c.Color(k); got != want {
			t.Errorf("%s: got %q want %q", k, got, want)
		}
	}
}

func TestColorsCorruptOrHugeFileMeansNoColour(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "account-colors.toml")
	if err := os.WriteFile(path, []byte("not toml at all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := NewColors(dir).Color("aaaa"); got != "" {
		t.Fatalf("corrupt file gave %q", got)
	}
	big := strings.Repeat("# padding padding padding padding padding padding padding\n", 2000) + "\"aaaa\" = \"#00ff00\"\n"
	if len(big) <= maxAliasFile {
		t.Fatal("the test file is not past the bound")
	}
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := NewColors(dir).Color("aaaa"); got != "" {
		t.Fatalf("a file past the bound gave %q", got)
	}
}

func TestColorsBoundTheCount(t *testing.T) {
	c := NewColors(t.TempDir())
	for i := 0; i < maxAliases; i++ {
		if err := c.Set(strings.Repeat("a", 5)+string(rune('0'+i/100))+string(rune('0'+i/10%10))+string(rune('0'+i%10)), "#010203"); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}
	if err := c.Set("zzzzzzzz", "#010203"); err == nil {
		t.Fatal("the 257th colour was accepted")
	}
	// Changing one that exists is still allowed at the bound.
	if err := c.Set("aaaaa000", "#040506"); err != nil {
		t.Fatal(err)
	}
}

func TestColorsWriteFailureKeepsTheOldState(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "blocker")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewColors(filepath.Join(file, "sub")) // a directory cannot be made under a file
	if err := c.Set("aaaa", "#010203"); err == nil {
		t.Fatal("expected a write error")
	}
	if got := c.Color("aaaa"); got != "" {
		t.Fatalf("a failed write still changed the state: %q", got)
	}
}
