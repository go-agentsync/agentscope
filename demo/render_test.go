package demo

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-widgets/painter/svgrec"
	"github.com/go-widgets/svg"
)

func TestRenderIsByteForByteDeterministic(t *testing.T) {
	for _, n := range Names() {
		a, err := Render(n, Options{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := Render(n, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.SVG, b.SVG) {
			t.Errorf("%s: two renders differ (%d and %d bytes)", n, len(a.SVG), len(b.SVG))
		}
		if !bytes.HasPrefix(a.SVG, []byte("<svg")) && !bytes.Contains(a.SVG[:min(200, len(a.SVG))], []byte("<svg")) {
			t.Errorf("%s: not an SVG", n)
		}
	}
}

func TestRenderShapes(t *testing.T) {
	for _, c := range []struct {
		name   string
		frames int
	}{{"overview", 7}, {"colors", 12}, {"theme-lang", 7}} {
		r, err := Render(c.name, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Frames != c.frames {
			t.Errorf("%s: %d frames, want %d", c.name, r.Frames, c.frames)
		}
		if r.Duration <= 0 || r.Stats.Frames == 0 || r.Stats.OutputBytes != len(r.SVG) {
			t.Errorf("%s: duration %v stats %+v", c.name, r.Duration, r.Stats)
		}
		if r.Stats.Width != 1280 || r.Stats.Height != 720 {
			t.Errorf("%s: %dx%d", c.name, r.Stats.Width, r.Stats.Height)
		}
		if !bytes.Contains(r.SVG, []byte("@keyframes")) {
			t.Errorf("%s: nothing animates", c.name)
		}
	}
}

func TestRenderSizeBounds(t *testing.T) {
	if _, err := Render("overview", Options{W: 100, H: 100}); err == nil {
		t.Error("a 100x100 capture was accepted")
	}
	if _, err := Render("overview", Options{W: 9000, H: 720}); err == nil {
		t.Error("a 9000x720 capture was accepted")
	}
	if _, err := Render("nope", Options{}); err == nil || !strings.Contains(err.Error(), `unknown scenario "nope"`) {
		t.Errorf("an unknown scenario: %v", err)
	}
	r, err := Render("overview", Options{W: 640, H: 400})
	if err != nil || r.Stats.Width != 640 || r.Stats.Height != 400 {
		t.Fatalf("640x400: %v %+v", err, r)
	}
}

// frame is the still of step i as text.
func frame(t *testing.T, r *Result, i int) string {
	t.Helper()
	b, err := r.Doc.Frame(i)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestScenariosChangeTheScreen(t *testing.T) {
	// Each step must show something the previous one did not: otherwise a step
	// that stopped reaching the view-model would pass unnoticed.
	for _, n := range Names() {
		r, err := Render(n, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := len(r.Doc.Slots()); got < 2 {
			t.Errorf("%s: %d slots", n, got)
		}
	}
	r, _ := Render("colors", Options{})
	first, last := frame(t, r, 0), frame(t, r, len(r.Doc.Slots())-1)
	for _, hex := range []string{"d55e00", "0072b2"} {
		if strings.Contains(strings.ToLower(first), hex) {
			t.Errorf("colors: %s is drawn before the owner picked it", hex)
		}
		if !strings.Contains(strings.ToLower(last), hex) {
			t.Errorf("colors: %s is not drawn after the owner picked it", hex)
		}
	}
	fr := allFrames(t, r)
	if fr[4] == fr[3] || fr[5] == fr[4] {
		t.Error("colors: the Colour dialog does not open, or the pick changes nothing")
	}
}

func allFrames(t *testing.T, r *Result) []string {
	var out []string
	for i := range r.Doc.Slots() {
		out = append(out, frame(t, r, i))
	}
	return out
}

func TestThemeAndLanguageSwitch(t *testing.T) {
	r, err := Render("theme-lang", Options{})
	if err != nil {
		t.Fatal(err)
	}
	fr := allFrames(t, r)
	if !strings.Contains(fr[2], "Guardrails") || strings.Contains(fr[2], "Garde-fous") {
		t.Error("step 2 should be English")
	}
	if fr[3] == fr[2] {
		t.Error("Light -> Dark changed nothing")
	}
	if !strings.Contains(fr[3], "Guardrails") {
		t.Error("the theme switch changed the language")
	}
	if !strings.Contains(fr[4], "Garde-fous") {
		t.Error("en -> fr changed nothing")
	}
	if bg2, bg3 := firstFill(fr[2]), firstFill(fr[3]); bg2 == bg3 {
		t.Errorf("same background %q in Light and Dark", bg2)
	}
}

// firstFill is the first fill colour of a still: the window background.
func firstFill(s string) string {
	_, rest, ok := strings.Cut(s, `fill="`)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, `"`)
	return v
}

func TestRenderIsVectorOutsideIcons(t *testing.T) {
	// Text must scale: a run drawn as a bitmap or a PNG mask would blur on zoom.
	for _, n := range Names() {
		r, err := Render(n, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Raster > 20 {
			t.Errorf("%s: %d raster elements; text fell back to bitmaps", n, r.Raster)
		}
	}
}

func TestRenderReportsAFailingPartInsteadOfPanicking(t *testing.T) {
	boom := errors.New("boom")
	defer func(a func(int, int) (*svgrec.Recorder, error), b func(*svgrec.Recorder) ([]byte, error), c func([]svg.Frame, svg.Options) (*svg.Document, error)) {
		newRecorder, recorderSVG, assemble = a, b, c
	}(newRecorder, recorderSVG, assemble)
	for name, set := range map[string]func(){
		"recorder": func() { newRecorder = func(int, int) (*svgrec.Recorder, error) { return nil, boom } },
		"svg":      func() { recorderSVG = func(*svgrec.Recorder) ([]byte, error) { return nil, boom } },
		"assemble": func() { assemble = func([]svg.Frame, svg.Options) (*svg.Document, error) { return nil, boom } },
	} {
		newRecorder, recorderSVG, assemble = svgrec.New, func(r *svgrec.Recorder) ([]byte, error) { return r.SVG() }, svg.Assemble
		set()
		r, err := Render("overview", Options{})
		if r != nil || err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("%s: %v %v", name, r, err)
		}
	}
}

func TestInsideGitTreeReportsAnUnresolvablePath(t *testing.T) {
	defer func(a func(string) (string, error)) { absPath = a }(absPath)
	absPath = func(string) (string, error) { return "", errors.New("no cwd") }
	if in, err := InsideGitTree("x"); in || err == nil {
		t.Errorf("%v %v", in, err)
	}
	if r, err := Write("overview", filepath.Join(t.TempDir(), "x.svg"), Options{}); r != nil || err == nil {
		t.Errorf("a capture was written to an unresolvable path: %v %v", r, err)
	}
}

func TestInsideGitTree(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "plain"), 0o755)
	for p, want := range map[string]bool{
		filepath.Join(root, "repo", "a.svg"):                true,
		filepath.Join(root, "repo", "sub", "deep", "a.svg"): true,
		filepath.Join(root, "plain", "a.svg"):               false,
	} {
		if got, err := InsideGitTree(p); err != nil || got != want {
			t.Errorf("InsideGitTree(%q) = %v, %v", p, got, err)
		}
	}
	if _, err := InsideGitTree("relative/not/there.svg"); err != nil {
		t.Error(err)
	}
}

func TestWriteRefusesAGitTreeAndCreatesDirectories(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755)
	if _, err := Write("overview", filepath.Join(root, "repo", "x.svg"), Options{}); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "repo", "x.svg")); err == nil {
		t.Fatal("a capture was written inside a git work tree")
	}
	out := filepath.Join(root, "a", "b", "x.svg")
	r, err := Write("overview", out, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); !bytes.Equal(b, r.SVG) {
		t.Error("the file is not the rendered SVG")
	}
	if _, err := Write("nope", filepath.Join(root, "y.svg"), Options{}); err == nil || !strings.Contains(err.Error(), "unknown scenario") {
		t.Errorf("an unknown scenario was written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "y.svg")); err == nil {
		t.Error("a file was left for an unknown scenario")
	}
	file := filepath.Join(root, "afile")
	os.WriteFile(file, nil, 0o644)
	if r, err := Write("overview", filepath.Join(file, "x.svg"), Options{}); r != nil || err == nil || strings.Contains(err.Error(), "refusing") {
		t.Errorf("a path under a file: %v %v", r, err)
	}
}

func TestDefaultOutDirIsUnderTheConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	d, err := DefaultOutDir()
	if err != nil || !strings.HasPrefix(d, home) || filepath.Base(d) != "captures" {
		t.Fatalf("%q %v", d, err)
	}
	if in, _ := InsideGitTree(filepath.Join(d, "x.svg")); in {
		t.Error("the default directory is inside a git work tree")
	}
}

func TestDefaultOutDirWithoutAHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, err := DefaultOutDir(); err == nil {
		t.Skip("this platform has a config directory without HOME")
	}
}

func BenchmarkRender(b *testing.B) {
	for _, n := range Names() {
		b.Run(n, func(b *testing.B) {
			var size int
			for i := 0; i < b.N; i++ {
				r, err := Render(n, Options{})
				if err != nil {
					b.Fatal(err)
				}
				size = len(r.SVG)
			}
			b.ReportMetric(float64(size), "svg-bytes")
		})
	}
}
