package demo

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-agentsync/agentscope/ui"
	"github.com/go-widgets/painter/svgrec"
	"github.com/go-widgets/svg"
)

// DefaultSize is the captured window, in logical pixels.
const DefaultSize = "1280x720"

// Options tune Render. The zero value renders DefaultSize.
type Options struct {
	W, H int
}

// Result is one rendered scenario.
type Result struct {
	SVG      []byte
	Frames   int           // frames recorded, one per step
	Duration time.Duration // the loop length
	Stats    svg.Stats     // the assembly: slots, pictures, shared subtrees, sizes
	Raster   int           // elements that embed a PNG; 0 means fully vector
	Doc      *svg.Document // the assembly, whose Frame(i) is the still of slot i
	Texts    []string      // every distinct string drawn: the outlines hide them from a text scan
}

// Render plays the scenario against a fresh Scene and view-model and assembles
// the recorded frames into one animated SVG. It is deterministic: no clock, no
// file and no network is read.
func Render(name string, o Options) (*Result, error) {
	sc, err := Lookup(name)
	if err != nil {
		return nil, err
	}
	if o.W == 0 && o.H == 0 {
		o.W, o.H, _ = ParseSize(DefaultSize)
	}
	if o.W < MinSize || o.W > MaxSize || o.H < MinSize || o.H > MaxSize {
		return nil, fmt.Errorf("size %dx%d: each side %d to %d", o.W, o.H, MinSize, MaxSize)
	}
	scene := ui.NewScene(o.W, o.H)
	env := &Env{Scene: scene, VM: scene.VM()}
	src := newOutlines()

	var frames []svg.Frame
	var at time.Duration
	raster := 0
	for i, st := range sc.Steps() {
		st.Do(env)
		rec, err := svgrec.New(o.W, o.H)
		if err != nil {
			return nil, err
		}
		scene.DrawTo(&vecRecorder{Recorder: rec, src: src})
		b, err := rec.SVG()
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		raster += rec.Stats().Raster
		frames = append(frames, svg.Frame{At: at, SVG: b})
		at += st.Hold
	}
	doc, err := svg.Assemble(frames, svg.Options{End: at, Title: sc.Title})
	if err != nil {
		return nil, err
	}
	return &Result{SVG: doc.SVG(), Frames: len(frames), Duration: doc.Duration(), Stats: doc.Stats(), Raster: raster, Doc: doc, Texts: src.Texts()}, nil
}

// DefaultOutDir is the durable directory captures go to: under the user's config
// directory, never inside a repository.
func DefaultOutDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "agentscope", "captures"), nil
}

// InsideGitTree reports whether p, or a directory above it, holds a .git.
func InsideGitTree(p string) (bool, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false, err
	}
	for d := filepath.Dir(abs); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return true, nil
		}
		if d == filepath.Dir(d) {
			return false, nil
		}
	}
}

// Write renders the scenario and writes it to out, refusing a path inside a git
// work tree. The directory is created when missing.
func Write(name, out string, o Options) (*Result, error) {
	if in, err := InsideGitTree(out); err != nil || in {
		return nil, fmt.Errorf("refusing to write a capture inside a git work tree (%s): %v", out, err)
	}
	r, err := Render(name, o)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	return r, os.WriteFile(out, r.SVG, 0o644)
}
