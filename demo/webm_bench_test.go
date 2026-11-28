package demo

import (
	"os/exec"
	"strings"
	"testing"
)

// BenchmarkWebM times the rasterising and the VP8 encoding of each scenario at the
// default 1280x720 window (the recording itself is made once, outside the timer)
// and reports the file's size. Times are not exact under load, so each run logs
// the machine's load average, the stamp to read the numbers by:
//
//	go test ./demo -run '^$' -bench WebM -benchtime 3x -count 5
func BenchmarkWebM(b *testing.B) {
	if out, err := exec.Command("uptime").Output(); err == nil {
		b.Logf("load stamp: %s", strings.TrimSpace(string(out)))
	}
	o := Options{W: 1280, H: 720}
	for _, n := range Names() {
		r, err := Render(n, o)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(n, func(b *testing.B) {
			var size int
			for i := 0; i < b.N; i++ {
				w, err := encodeWebM(r, o, DefaultQuality, gfxRasterizer{})
				if err != nil {
					b.Fatal(err)
				}
				size = len(w.Bytes)
			}
			b.ReportMetric(float64(size), "webm-bytes")
			b.ReportMetric(float64(len(r.Doc.Slots())), "slots")
			b.ReportMetric(float64(len(r.SVG)), "svg-bytes")
		})
	}
}
