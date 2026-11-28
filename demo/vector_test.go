package demo

import (
	"strings"
	"testing"

	"github.com/go-agentsync/agentscope/ui"
	"github.com/go-opentype/opentype"
	"github.com/go-opentype/shape"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/painter/svgrec"
)

// noFace is a painter.Face with no font data: the recorder must fall back to
// its own text path and still record the string.
type noFace struct{}

func (noFace) FontData() []byte { return nil }
func (noFace) SizePx() int      { return 12 }
func (noFace) Ascent() int      { return 10 }

func TestTextFaceFallsBackWithoutFontData(t *testing.T) {
	rec, err := svgrec.New(100, 40)
	if err != nil {
		t.Fatal(err)
	}
	src := newOutlines()
	v := &vecRecorder{Recorder: rec, src: src}
	v.TextFace(1, 1, "fallback", noFace{}, painter.RGBA{A: 255})
	v.Text(1, 20, "direct", painter.RGBA{A: 255})
	v.TextFace(1, 30, "junk", junkFace{}, painter.RGBA{A: 255})
	if got := strings.Join(src.Texts(), ","); got != "direct,fallback,junk" {
		t.Errorf("texts %q", got)
	}
}

// A face is parsed once per (data, size): a second frame draws with the same
// parsed face, not a copy of it.
func TestParsedFacesAreReusedAcrossFrames(t *testing.T) {
	scene := ui.NewScene(640, 400)
	src := newOutlines()
	draw := func() {
		rec, err := svgrec.New(640, 400)
		if err != nil {
			t.Fatal(err)
		}
		scene.DrawTo(&vecRecorder{Recorder: rec, src: src})
	}
	draw()
	first := map[faceKey]*opentype.Face{}
	for k, v := range src.faces {
		first[k] = v
	}
	if len(first) == 0 {
		t.Fatal("no face was parsed: the text did not go through outlines")
	}
	draw()
	for k, v := range first {
		if src.faces[k] != v {
			t.Errorf("face %v was parsed again", k)
		}
	}
}

type junkFace struct{ noFace }

func (junkFace) FontData() []byte { return []byte("not a font") }

func TestFaceRefusesUnparsableData(t *testing.T) {
	o := newOutlines()
	if _, ok := o.face(junkFace{}); ok {
		t.Error("junk parsed as a font")
	}
	if _, ok := o.face(noFace{}); ok {
		t.Error("no data parsed as a font")
	}
}

func TestGlyphPathOps(t *testing.T) {
	segs := []opentype.Segment{
		{Op: opentype.SegMoveTo, P: [2]opentype.Point{{X: 0, Y: 0}}},
		{Op: opentype.SegLineTo, P: [2]opentype.Point{{X: 10, Y: 0}}},
		{Op: opentype.SegQuadTo, P: [2]opentype.Point{{X: 10, Y: 10}, {X: 0, Y: 10}}},
		{Op: opentype.SegClose},
	}
	if glyphPath(segs, 5, 20, 0.5) == nil {
		t.Fatal("nil path")
	}
}

func TestGscale(t *testing.T) {
	if gscale(shape.Glyph{}) != 1 || gscale(shape.Glyph{Scale: 0.5}) != 0.5 {
		t.Error("gscale")
	}
}
