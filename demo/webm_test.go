package demo

import (
	"bytes"
	"errors"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-avkit/avkit/container"
	"github.com/go-avkit/avkit/vp8enc"
	"github.com/thesyncim/govpx"
)

// minWebMPSNR is the bound the decoded video holds against the rasterised SVG
// frames: the PSNR over luma and both chroma planes of each slot's decoded picture
// against the 4:2:0 conversion of the same rasterised frame. The encoder at
// DefaultQuality measures well above it on every scenario (see the log); a broken
// prediction, a wrong colour matrix or a frame shown at the wrong time costs
// 15 dB or more, so the bound leaves room for noise and catches those.
const minWebMPSNR = 40.0

const muxerApp = "github.com/go-avkit/avkit container"

var alignedOpts = Options{W: 640, H: 384}

func psnrYCbCr(a, b *image.YCbCr) float64 {
	var sse, n float64
	w, h := a.Rect.Dx(), a.Rect.Dy()
	cw, ch := (w+1)/2, (h+1)/2
	plane := func(pa, pb []byte, sa, sb, w, h int) {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				d := float64(pa[y*sa+x]) - float64(pb[y*sb+x])
				sse += d * d
				n++
			}
		}
	}
	plane(a.Y, b.Y, a.YStride, b.YStride, w, h)
	plane(a.Cb, b.Cb, a.CStride, b.CStride, cw, ch)
	plane(a.Cr, b.Cr, a.CStride, b.CStride, cw, ch)
	if sse == 0 {
		return 99
	}
	return 10 * math.Log10(255*255*n/sse)
}

func fromGovpx(img govpx.Image) *image.YCbCr {
	out := image.NewYCbCr(image.Rect(0, 0, img.Width, img.Height), image.YCbCrSubsampleRatio420)
	for y := 0; y < img.Height; y++ {
		copy(out.Y[y*out.YStride:y*out.YStride+img.Width], img.Y[y*img.YStride:])
	}
	cw := (img.Width + 1) / 2
	for y := 0; y < (img.Height+1)/2; y++ {
		copy(out.Cb[y*out.CStride:y*out.CStride+cw], img.U[y*img.UStride:])
		copy(out.Cr[y*out.CStride:y*out.CStride+cw], img.V[y*img.VStride:])
	}
	return out
}

// decodeSlots decodes the WebM and returns, for each slot of the scenario, the
// picture on screen when the slot starts, found from the samples' durations.
func decodeSlots(t *testing.T, w *WebM) []*image.YCbCr {
	t.Helper()
	r, err := container.NewReader(w.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	vt := r.File().VideoTracks()
	if len(vt) != 1 {
		t.Fatalf("%d video tracks", len(vt))
	}
	ss, err := r.Samples(vt[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var starts []time.Duration
	var pics []*image.YCbCr
	var at time.Duration
	for i, s := range ss {
		if err := dec.Decode(s.Data); err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
		img, ok := dec.NextFrame()
		if !ok {
			t.Fatalf("sample %d: no picture", i)
		}
		starts, pics = append(starts, at), append(pics, fromGovpx(img))
		at += time.Duration(s.Duration) * time.Millisecond
	}
	var out []*image.YCbCr
	var slot time.Duration
	for _, s := range w.Doc.Slots() {
		k := 0
		for j, st := range starts {
			if st <= slot+time.Millisecond/2 {
				k = j
			}
		}
		out = append(out, pics[k])
		slot += s.Duration
	}
	return out
}

func TestWebMDecodesBackToTheSVGFrames(t *testing.T) { decodeBack(t, alignedOpts) }

// The bound holds at the size the program records by default, not only at the
// small test size.
func TestWebMDecodesBackAtTheDefaultSize(t *testing.T) {
	w, h, _ := ParseSize(DefaultSize)
	decodeBack(t, Options{W: w, H: h})
}

func decodeBack(t *testing.T, o Options) {
	deny := realDeny()
	for _, n := range Names() {
		w, err := RenderWebM(n, o, 0)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if len(w.Texts) == 0 {
			t.Fatalf("%s: the scan read no drawn text", n)
		}
		// The muxer names itself in the Info element (MuxingApp and WritingApp,
		// no clock and no host); that public library name is the one string in
		// the file that is not the scenario's.
		if !bytes.Contains(w.Bytes, []byte(muxerApp)) {
			t.Errorf("%s: the muxer's own name is not where the scan expects it", n)
		}
		if h := leaks(string(bytes.ReplaceAll(w.Bytes, []byte(muxerApp), nil)), deny); len(h) > 0 {
			t.Errorf("%s: the WebM bytes leak %v", n, h)
		}
		pics := decodeSlots(t, w)
		if len(pics) != len(w.Doc.Slots()) {
			t.Fatalf("%s: %d pictures for %d slots", n, len(pics), len(w.Doc.Slots()))
		}
		var sum float64
		for i, p := range pics {
			svgFrame, err := w.Doc.Frame(i)
			if err != nil {
				t.Fatal(err)
			}
			img, err := gfxRasterizer{}.Rasterize(svgFrame, o.W, o.H)
			if err != nil {
				t.Fatalf("%s frame %d: %v", n, i, err)
			}
			ps := psnrYCbCr(p, vp8enc.ToYCbCr(img))
			sum += ps
			if ps < minWebMPSNR {
				t.Errorf("%s frame %d: PSNR %.2f dB below %.0f", n, i, ps, minWebMPSNR)
			}
		}
		t.Logf("%dx%d %s: %d bytes, %d frames, mean PSNR %.2f dB", o.W, o.H, n, len(w.Bytes), len(pics), sum/float64(len(pics)))
	}
}

// TestWebMFollowsTheContainerGuidelines checks the file against what the WebM
// container guidelines (https://www.webmproject.org/docs/container/) and RFC 6386
// say, as read: DocType "webm", Codec ID "V_VP8", a Cues
// element before the first Cluster, a key frame first (RFC 6386 section 9.1: frame
// type bit 0, start code 9d 01 2a, then the 14-bit width and height).
func TestWebMFollowsTheContainerGuidelines(t *testing.T) {
	w, err := RenderWebM("overview", alignedOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	b := w.Bytes
	// EBML IDs with their size byte: DocType 0x4282 (4 bytes), CodecID 0x86 (5 bytes).
	for what, want := range map[string][]byte{
		"DocType webm":  {0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'},
		"CodecID V_VP8": {0x86, 0x85, 'V', '_', 'V', 'P', '8'},
		"Cues":          {0x1C, 0x53, 0xBB, 0x6B},
		"Cluster":       {0x1F, 0x43, 0xB6, 0x75},
	} {
		if !bytes.Contains(b, want) {
			t.Errorf("%s: not found in the file", what)
		}
	}
	if cues, cluster := bytes.Index(b, []byte{0x1C, 0x53, 0xBB, 0x6B}), bytes.Index(b, []byte{0x1F, 0x43, 0xB6, 0x75}); cues < 0 || cluster < 0 || cues > cluster {
		t.Errorf("Cues at %d, first Cluster at %d: Cues should come before the clusters", cues, cluster)
	}
	r, err := container.NewReader(b)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := r.Samples(r.File().VideoTracks()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) == 0 {
		t.Fatal("no samples")
	}
	d := ss[0].Data
	if len(d) < 10 || d[0]&1 != 0 || d[3] != 0x9d || d[4] != 0x01 || d[5] != 0x2a {
		t.Fatalf("the first sample is not a key frame: % x", d[:min(10, len(d))])
	}
	if kw, kh := int(d[6])|int(d[7]&0x3f)<<8, int(d[8])|int(d[9]&0x3f)<<8; kw != alignedOpts.W || kh != alignedOpts.H {
		t.Errorf("the key frame says %dx%d, want %dx%d", kw, kh, alignedOpts.W, alignedOpts.H)
	}
}

func TestWebMIsDeterministic(t *testing.T) {
	a, err := RenderWebM("overview", alignedOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		b, err := RenderWebM("overview", alignedOpts, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.Bytes, b.Bytes) {
			t.Fatalf("run %d: %d bytes against %d, the contents differ", i, len(b.Bytes), len(a.Bytes))
		}
	}
}

func TestWebMFramesAreTheSVGFrames(t *testing.T) {
	// The rasterised stills are those of the animated SVG's own document.
	w, err := RenderWebM("theme-lang", alignedOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Render("theme-lang", alignedOpts)
	if err != nil {
		t.Fatal(err)
	}
	if w.Frames != s.Frames || w.Duration != s.Duration || !bytes.Equal(w.SVG, s.SVG) {
		t.Errorf("the WebM was made from another recording: %d/%d frames", w.Frames, s.Frames)
	}
}

func TestResolveFormat(t *testing.T) {
	for _, c := range []struct{ format, out, want, err string }{
		{"", "", "svg", ""},
		{"", "a.svg", "svg", ""},
		{"", "a.WEBM", "webm", ""},
		{"", "a.bin", "svg", ""},
		{"webm", "", "webm", ""},
		{"webm", "a.bin", "webm", ""},
		{"WebM", "a.webm", "webm", ""},
		{"svg", "a.webm", "", "contradicts"},
		{"webm", "dir.svg/a.svg", "", "contradicts"},
		{"gif", "", "", "want svg or webm"},
	} {
		got, err := ResolveFormat(c.format, c.out)
		if c.err == "" && (err != nil || got != c.want) || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("ResolveFormat(%q, %q) = %q, %v; want %q %q", c.format, c.out, got, err, c.want, c.err)
		}
	}
}

func FuzzResolveFormat(f *testing.F) {
	f.Add("", "a.svg")
	f.Add("webm", "x.webm")
	f.Add("svg", "x.webm")
	f.Add("\x00", "..")
	f.Fuzz(func(t *testing.T, format, out string) {
		got, err := ResolveFormat(format, out)
		if err != nil {
			return
		}
		if got != FormatSVG && got != FormatWebM {
			t.Fatalf("format %q", got)
		}
		if ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(out), ".")); (ext == "svg" || ext == "webm") && ext != got {
			t.Fatalf("%q resolved to %s against the .%s extension", out, got, ext)
		}
	})
}

func TestCheckWebM(t *testing.T) {
	for _, c := range []struct {
		w, h, q int
		err     string
	}{
		{1280, 720, 0, ""}, // 720 = 45 x 16: aligned
		{1280, 720, 64, "--quality 64"},
		{1280, 720, -1, "--quality -1"},
		{1366, 768, 0, "not a multiple of 16"},
		{1280, 721, 0, "not a multiple of 16"},
		{1280, 720, 1, ""},
		{640, 400, 63, ""},
	} {
		err := CheckWebM(c.w, c.h, c.q)
		if c.err == "" && err != nil || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("CheckWebM(%d, %d, %d) = %v, want %q", c.w, c.h, c.q, err, c.err)
		}
	}
}

func TestWebMRefusesAnUnalignedSize(t *testing.T) {
	// govpx reads whole macroblocks from unpadded planes (thesyncim/govpx#143).
	if _, err := RenderWebM("overview", Options{W: 1366, H: 768}, 0); err == nil || !strings.Contains(err.Error(), "multiple of 16") {
		t.Errorf("err = %v", err)
	}
	if _, err := RenderWebM("overview", Options{W: 640, H: 360}, 0); err == nil {
		t.Error("640x360 accepted")
	}
	if _, err := RenderWebM("overview", Options{W: 640, H: 384}, 99); err == nil {
		t.Error("quality 99 accepted")
	}
	var unknown error
	noPanic(t, "RenderWebM of an unknown scenario", func() { _, unknown = RenderWebM("nope", alignedOpts, 0) })
	if unknown == nil || !strings.Contains(unknown.Error(), "unknown scenario") {
		t.Errorf("an unknown scenario: %v", unknown)
	}
}

// failing makes each seam fail in turn, to show the failure reaches the caller.
type failRas struct{ img *image.NRGBA }

func (f failRas) Rasterize([]byte, int, int) (*image.NRGBA, error) { return f.img, errors.New("boom") }

func TestWebMFailuresReachTheCaller(t *testing.T) {
	r, err := Render("theme-lang", alignedOpts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encodeWebM(r, alignedOpts, 10, failRas{}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("rasteriser failure: %v", err)
	}
	old := newVP8
	defer func() { newVP8 = old }()
	newVP8 = func(io.Writer, vp8enc.Options) (*vp8enc.Encoder, error) { return nil, errors.New("no encoder") }
	if _, err := encodeWebM(r, alignedOpts, 10, gfxRasterizer{}); err == nil || !strings.Contains(err.Error(), "no encoder") {
		t.Errorf("encoder failure: %v", err)
	}
	newVP8 = old
	// A rasteriser answering the wrong size is refused by Video; a doc that gfx
	// cannot parse by the rasteriser.
	var broken error
	noPanic(t, "Rasterize of a broken document", func() { _, broken = (gfxRasterizer{}).Rasterize([]byte("<svg"), 640, 384) })
	if broken == nil {
		t.Error("a broken document rasterised")
	}
	if _, err := (gfxRasterizer{}).Rasterize([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"/>`), 640, 384); err == nil || !strings.Contains(err.Error(), "want 640x384") {
		t.Errorf("a wrong-sized frame: %v", err)
	}
}

func TestVP8SinkRefusesAWrongSizedFrame(t *testing.T) {
	s := &vp8Sink{quality: 10}
	var buf bytes.Buffer
	if err := s.Open(&buf, 64, 64); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFrame(image.NewNRGBA(image.Rect(0, 0, 32, 32)), time.Second); err == nil {
		t.Error("a 32x32 frame in a 64x64 file accepted")
	}
	if err := s.Close(); err != nil {
		t.Error(err)
	}
}

func TestRenderWebMDefaultsToThe720pWindow(t *testing.T) {
	w, err := RenderWebM("theme-lang", Options{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r, err := container.NewReader(w.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err := r.TrackConfig(r.File().VideoTracks()[0].ID); err != nil || cfg.Width != 1280 || cfg.Height != 720 {
		t.Errorf("track %+v, %v", cfg, err)
	}
}

func TestWriteWebMRefusesAGitTreeAndWrites(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteWebM("theme-lang", filepath.Join(repo, "a.webm"), alignedOpts, 0); err == nil {
		t.Error("written inside a work tree")
	}
	var unknown error
	noPanic(t, "WriteWebM of an unknown scenario", func() {
		_, unknown = WriteWebM("nope", filepath.Join(t.TempDir(), "a.webm"), alignedOpts, 0)
	})
	if unknown == nil || !strings.Contains(unknown.Error(), "unknown scenario") {
		t.Errorf("an unknown scenario written: %v", unknown)
	}
	out := filepath.Join(t.TempDir(), "d", "a.webm")
	w, err := WriteWebM("theme-lang", out, alignedOpts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(out); err != nil || !bytes.Equal(b, w.Bytes) {
		t.Errorf("file differs from the render: %v", err)
	}
	// A failed write returns no result: the caller must not report a file it
	// does not have (WriteFile's own failure would return the render with it).
	if got, err := WriteWebM("theme-lang", filepath.Join(out, "b.webm"), alignedOpts, 0); err == nil || got != nil {
		t.Errorf("a directory under a file created: %v, %v", got, err)
	}
}

func TestVP8SinkClosesWithoutOpening(t *testing.T) {
	var err error
	noPanic(t, "Close of a sink never opened", func() { err = (&vp8Sink{}).Close() })
	if err != nil {
		t.Error(err)
	}
}
