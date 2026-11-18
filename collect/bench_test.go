package collect

import (
	"os"
	"testing"
)

// BenchmarkSnapshotRealTree measures an incremental snapshot of the real
// ~/.claude tree of the machine running it (skipped where there is none). The
// first snapshot, which reads the tail of each listed transcript, is outside
// the timer: this is the cost of every tick after it. Allocation counts are
// exact under load; times are not, so read them on a quiet machine.
func BenchmarkSnapshotRealTree(b *testing.B) {
	o, err := DefaultOptions()
	if err != nil {
		b.Skip(err)
	}
	if _, err := os.Stat(o.ClaudeDir); err != nil {
		b.Skip("no ~/.claude here")
	}
	c := New(o)
	c.Snapshot()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Snapshot()
	}
}

// BenchmarkSnapshotFixture is the same over the synthetic tree used by the
// tests, so the figure is comparable across machines.
func BenchmarkSnapshotFixture(b *testing.B) {
	f := newFixture(b)
	c := New(f.options())
	c.Snapshot()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Snapshot()
	}
}
