package demo

import (
	"sort"
	"strconv"
	"unsafe"

	"github.com/go-opentype/opentype"
	"github.com/go-opentype/shape"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/painter/svgrec"
)

// outlines is the glyph-outline source of a capture: the toolkit's OpenType
// face, shaped (kerning, ligatures) and turned into filled paths, so text scales
// without loss. svgrec writes a run drawn through painter.Text as a 5x7 bitmap,
// and a run drawn through a mask as a PNG; neither survives a zoom. A
// painter.FacePainter is handed the face itself, which is what lets a recorder
// emit true outlines. One outlines value serves every frame of a capture.
type outlines struct {
	faces map[faceKey]*opentype.Face
	texts map[string]bool // every string drawn, so a scan can read what the outlines hide
	// faceIDs numbers each font so a glyph key stays short and the face data
	// pointer never reaches the document.
	faceIDs map[*byte]int
}

// note records a drawn string.
func (o *outlines) note(s string) { o.texts[s] = true }

// Texts are the distinct strings drawn so far, sorted.
func (o *outlines) Texts() []string {
	out := make([]string, 0, len(o.texts))
	for s := range o.texts {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Text records the string and draws it as the recorder does.
func (r *vecRecorder) Text(x, y int, s string, ink painter.RGBA) {
	r.src.note(s)
	r.Recorder.Text(x, y, s, ink)
}

type faceKey struct {
	data *byte
	px   int
}

func newOutlines() *outlines {
	return &outlines{faces: map[faceKey]*opentype.Face{}, texts: map[string]bool{}, faceIDs: map[*byte]int{}}
}

func (o *outlines) face(f painter.Face) (*opentype.Face, bool) {
	data := f.FontData() // no data: Parse refuses it
	k := faceKey{unsafe.SliceData(data), f.SizePx()}
	if fc, ok := o.faces[k]; ok {
		return fc, true
	}
	font, err := opentype.Parse(data)
	if err != nil {
		return nil, false
	}
	fc := font.NewFace(f.SizePx())
	o.faces[k] = fc
	return fc, true
}

// vecRecorder is an svgrec.Recorder that also implements painter.FacePainter.
type vecRecorder struct {
	*svgrec.Recorder
	src *outlines
}

// TextFace draws s in face as filled glyph outlines, (x, y) the top-left corner
// of the run. The string is also recorded, invisibly, through Text, so it stays in
// the document as a <title> (the accessible name, and what a leak scan reads).
func (r *vecRecorder) TextFace(x, y int, s string, face painter.Face, ink painter.RGBA) {
	r.src.note(s)
	fc, ok := r.src.face(face)
	if !ok {
		r.Recorder.Text(x, y, s, ink)
		return
	}
	scale := float64(face.SizePx()) / float64(fc.Font().UnitsPerEm())
	base := float64(y + face.Ascent())
	pen := float64(x)
	var run []svgrec.Glyph
	for _, g := range shape.Shape(fc, s, shape.Options{}) {
		if !g.Invisible {
			if segs, ok := fc.GlyphOutline(g.GID); ok && len(segs) > 0 {
				gs := scale * gscale(g)
				run = append(run, svgrec.Glyph{
					Key: r.src.glyphKey(face, g.GID, gs),
					X:   pen + float64(g.XOffset), Y: base - float64(g.YOffset),
					Outline: func() *painter.Path { return glyphPath(segs, 0, 0, gs) },
				})
			}
		}
		pen += float64(g.XAdvance)
	}
	// One <symbol> per distinct glyph, one <use> per occurrence; the string stays
	// in the document as the group's <title> (the accessible name, and what a
	// leak scan reads).
	r.Recorder.TextGlyphs(s, run, ink)
}

// glyphKey names a glyph outline by face, size, glyph and scale, so a recorder
// builds each outline once however often it is drawn.
func (o *outlines) glyphKey(f painter.Face, gid opentype.GlyphIndex, scale float64) string {
	data := f.FontData()
	id, ok := o.faceIDs[unsafe.SliceData(data)]
	if !ok {
		id = len(o.faceIDs)
		o.faceIDs[unsafe.SliceData(data)] = id
	}
	return strconv.Itoa(id) + ":" + strconv.Itoa(f.SizePx()) + ":" + strconv.Itoa(int(gid)) + ":" + strconv.FormatFloat(scale, 'g', -1, 64)
}

func gscale(g shape.Glyph) float64 {
	if g.Scale > 0 {
		return g.Scale
	}
	return 1
}

// glyphPath places an outline given in font units, Y up, at (ox, oy) in pixels,
// Y down.
func glyphPath(segs []opentype.Segment, ox, oy, scale float64) *painter.Path {
	p := painter.NewPath()
	px := func(pt opentype.Point) (float64, float64) { return ox + pt.X*scale, oy - pt.Y*scale }
	for _, s := range segs {
		switch s.Op {
		case opentype.SegMoveTo:
			p.MoveTo(px(s.P[0]))
		case opentype.SegLineTo:
			p.LineTo(px(s.P[0]))
		case opentype.SegQuadTo:
			cx, cy := px(s.P[0])
			x, y := px(s.P[1])
			p.QuadTo(cx, cy, x, y)
		case opentype.SegClose:
			p.Close()
		}
	}
	return p
}
