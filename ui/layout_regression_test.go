package ui

import (
	"fmt"
	"testing"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

// manySessions is a snapshot with more sessions than the table can show, so its
// last row sits at the table's bottom edge when it is scrolled to the end.
func manySessions(n int) model.Snapshot {
	sn := snap()
	base := sn.Sessions[0]
	sn.Sessions = nil
	for i := 0; i < n; i++ {
		x := base
		x.ID = fmt.Sprintf("%08d-aaaa-bbbb-cccc-dddddddddddd", i)
		sn.Sessions = append(sn.Sessions, x)
	}
	return sn
}

// Two defects shipped in one week: the last table row was cut by the pane's
// bottom edge, and the Details lines overlapped. Both come from layout numbers
// kept outside the toolkit, so they are checked at every splitter position.
func TestAtEverySplitTheLastRowIsWholeAndTheDetailsLinesDoNotOverlap(t *testing.T) {
	for _, sp := range []int{SplitLo, 30, SplitDefault, 70, SplitHi} {
		for _, h := range []int{900, 700, 560} {
			t.Run(fmt.Sprintf("split%d_h%d", sp, h), func(t *testing.T) {
				s := newScene(t)
				vm := s.VM()
				sn := manySessions(60)
				sn.Sessions[0].Title = "a title long enough to wrap at a narrow panel abcdefghij klmnopqrst uvwxyz 0123456789 abcdefghij"
				vm.Apply(sn)
				vm.Graph.Selected.Set("node:s:" + sn.Sessions[0].ID[:8])
				s.Resize(testW, h, 1)
				vm.PanelOpen.Set(true)
				vm.Split.Set(sp)
				pixels(s)

				tb := s.table.Bounds()
				last := len(s.table.Rows) - 1
				if last < 1 {
					t.Fatalf("%d table rows", len(s.table.Rows))
				}
				s.table.ScrollTo(0)
				pixels(s)
				whole := 0
				for y := 0; y < tb.H; y++ {
					if s.table.RowAt(tb.W/4, y) == 0 {
						whole++
					}
				}
				s.table.ScrollTo(last)
				pixels(s)
				// The pixels a row owns: the last row must own as many as the one
				// above it, or the pane's edge cuts it.
				span := func(row int) (n int) {
					for y := 0; y < tb.H; y++ { // RowAt takes widget-local y
						if s.table.RowAt(tb.W/4, y) == row {
							n++
						}
					}
					return n
				}
				if got := span(last); whole == 0 || got != whole {
					t.Fatalf("the last row shows %d px, a whole row is %d: it is cut", got, whole)
				}
				if bottom := s.vsplit.Bounds(); tb.Y+tb.H > bottom.Y+bottom.H {
					t.Fatalf("the table ends at %d, past its pane at %d", tb.Y+tb.H, bottom.Y+bottom.H)
				}

				if got, glyph := s.detail.LineHeight(), toolkit.GlyphHeight(); got <= glyph {
					t.Fatalf("Details line pitch %d for a glyph of %d: the lines overlap", got, glyph)
				}
			})
		}
	}
}
