package demo

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-avkit/avkit/vp8enc"
	gfxsvg "github.com/go-gfx/gfx/svg"
	"github.com/go-widgets/svg"
	"image/color"
)

// The WebM encoder settings. CPUUsed 4 is avkit's slow, sharp preset (the default
// 5 costs about 5 dB), right for UI text; the frames are few and the encode is
// bounded by Quality, a constant quantizer, so the file size follows the content.
const (
	// WebMFPS is the nominal frame rate. Timestamps come from the scenario's own
	// holds; this only sizes the last frame's duration.
	WebMFPS = 25
	// WebMCPUUsed is the encoder speed preset.
	WebMCPUUsed = 4
	// DefaultQuality is the VP8 quantizer index held: 1 (best) to 63 (worst).
	DefaultQuality = 10
	// MinQuality and MaxQuality bound the --quality flag: avkit's vp8enc takes
	// 1 to 63 (the bitstream's own quantizer index is 7 bits, RFC 6386 section 9.6).
	MinQuality, MaxQuality = 1, 63
)

// Format names.
const (
	FormatSVG  = "svg"
	FormatWebM = "webm"
)

// ResolveFormat settles the output format from --format and the --out path. An
// empty format is inferred from the extension (.svg or .webm) and is svg when the
// path has neither; a named format must not contradict a known extension.
func ResolveFormat(format, out string) (string, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(out), "."))
	fromExt := ""
	if ext == FormatSVG || ext == FormatWebM {
		fromExt = ext
	}
	switch format = strings.ToLower(format); format {
	case "":
		if fromExt != "" {
			return fromExt, nil
		}
		return FormatSVG, nil
	case FormatSVG, FormatWebM:
		if fromExt != "" && fromExt != format {
			return "", fmt.Errorf("--format %s contradicts the .%s extension of --out %q", format, fromExt, truncate(out, 80))
		}
		return format, nil
	}
	return "", fmt.Errorf("--format %q: want svg or webm", truncate(format, 40))
}

// CheckQuality refuses a quality outside 1 to 63 (0 is the default). Any size is
// taken: avkit's vp8enc pads the planes to whole macroblocks itself (v0.20.2).
func CheckQuality(quality int) error {
	if quality != 0 && (quality < MinQuality || quality > MaxQuality) {
		return fmt.Errorf("--quality %d: want %d to %d", quality, MinQuality, MaxQuality)
	}
	return nil
}

// WebM is one rendered scenario as a video.
type WebM struct {
	*Result
	Bytes   []byte
	Quality int
}

// gfxRasterizer paints a still frame with go-gfx/gfx/svg, at one pixel per user
// unit: the frames are the SVG's own, there is no second drawing path.
type gfxRasterizer struct{}

func (gfxRasterizer) Rasterize(doc []byte, w, h int) (*image.NRGBA, error) {
	res, err := gfxsvg.Rasterize(string(doc), gfxsvg.Options{
		Scale: 1, Ink: color.RGBA{A: 255}, Paper: color.RGBA{R: 255, G: 255, B: 255, A: 255},
		// The frames are this program's own, not a stranger's upload: a 720p
		// frame is many small layered boxes, past the default budget of 64 full
		// surfaces. The budget stays finite.
		MaxWork: 1 << 30,
	})
	if err != nil {
		return nil, err
	}
	if res.Image.W != w || res.Image.H != h {
		return nil, fmt.Errorf("frame rasterised at %dx%d, want %dx%d", res.Image.W, res.Image.H, w, h)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, res.Image.Pix)
	return img, nil
}

// vp8Sink adapts avkit's encoder to svg.VideoEncoder. Each slot's frame is shown
// for the slot's duration; one copy of the last frame closes the file, at the end
// of the loop, so the last slot keeps its whole duration.
type vp8Sink struct {
	enc     *vp8enc.Encoder
	quality int
	pts     time.Duration
	last    *image.NRGBA
}

var newVP8 = vp8enc.New

func (s *vp8Sink) Open(w io.Writer, width, height int) error {
	enc, err := newVP8(w, vp8enc.Options{
		Width: width, Height: height, FPS: WebMFPS, Quality: s.quality,
		CPUUsed: WebMCPUUsed, ScreenContent: true, Buffered: true,
	})
	s.enc = enc
	return err
}

func (s *vp8Sink) WriteFrame(img *image.NRGBA, d time.Duration) error {
	if err := s.enc.AddFrame(img, s.pts); err != nil {
		return err
	}
	s.pts += d
	s.last = img
	return nil
}

func (s *vp8Sink) Close() error {
	if s.enc == nil {
		return nil
	}
	var err error
	if s.last != nil {
		err = s.enc.AddFrame(s.last, s.pts)
	}
	if cerr := s.enc.Close(); err == nil {
		err = cerr
	}
	return err
}

// RenderWebM plays the scenario exactly as Render does and encodes the frames it
// recorded, rasterised, as a VP8 WebM. quality 0 means DefaultQuality.
func RenderWebM(name string, o Options, quality int) (*WebM, error) {
	if o.W == 0 && o.H == 0 {
		o.W, o.H, _ = ParseSize(DefaultSize)
	}
	if err := CheckQuality(quality); err != nil {
		return nil, err
	}
	if quality == 0 {
		quality = DefaultQuality
	}
	r, err := Render(name, o)
	if err != nil {
		return nil, err
	}
	return encodeWebM(r, o, quality, gfxRasterizer{})
}

func encodeWebM(r *Result, o Options, quality int, ras svg.Rasterizer) (*WebM, error) {
	var buf bytes.Buffer
	if err := r.Doc.Video(&buf, o.W, o.H, ras, &vp8Sink{quality: quality}); err != nil {
		return nil, err
	}
	return &WebM{Result: r, Bytes: buf.Bytes(), Quality: quality}, nil
}

// refuseGitTree is the guard both writers share.
func refuseGitTree(out string) error {
	in, err := InsideGitTree(out)
	switch {
	case err != nil:
		return fmt.Errorf("refusing to write a capture: cannot tell whether %s is inside a git work tree: %w", out, err)
	case in:
		return fmt.Errorf("refusing to write a capture inside a git work tree (%s)", out)
	}
	return nil
}

// WriteWebM renders the scenario to out, refusing a path inside a git work tree.
func WriteWebM(name, out string, o Options, quality int) (*WebM, error) {
	if err := refuseGitTree(out); err != nil {
		return nil, err
	}
	w, err := RenderWebM(name, o, quality)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	return w, os.WriteFile(out, w.Bytes, 0o644)
}
