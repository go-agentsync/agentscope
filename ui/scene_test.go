package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func newScene(t *testing.T) *Scene {
	t.Helper()
	s := NewScene(SurfaceW, SurfaceH)
	s.VM().Apply(snap())
	return s
}

func pixels(s *Scene) []byte {
	buf := make([]byte, 4*SurfaceW*SurfaceH)
	s.Draw(buf)
	return buf
}

func center(r toolkit.Rect) (int, int) { return r.X + r.W/2, r.Y + r.H/2 }

func TestSceneDrawsAndRepaintsOnChange(t *testing.T) {
	s := newScene(t)
	if !s.Dirty() {
		t.Error("a fresh scene must be painted")
	}
	if s.Dirty() {
		t.Error("Dirty reports and clears")
	}
	before := pixels(s)
	nonBG := 0
	for i := 0; i+3 < len(before); i += 4 {
		if before[i] != before[0] || before[i+1] != before[1] {
			nonBG++
		}
	}
	if nonBG < 10_000 {
		t.Errorf("the scene drew almost nothing: %d differing pixels", nonBG)
	}
	s.VM().Conn.Set("connected")
	if !s.Dirty() {
		t.Error("a ViewModel change must raise the repaint flag")
	}
	s.VM().Query.Set("o/b")
	if !s.Dirty() {
		t.Error("the query must raise the repaint flag")
	}
	after := pixels(s)
	same := true
	for i := range before {
		if before[i] != after[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("filtering changed nothing on screen")
	}
}

func TestSceneClickInGraphSelectsAndSyncsTableAndDetail(t *testing.T) {
	s := newScene(t)
	box, ok := s.graph.NodeBox("s:bbbbbbbb")
	if !ok {
		t.Fatal("node not laid out")
	}
	pixels(s) // lay out
	x, y := center(box)
	if !s.HandleClick(x, y) {
		t.Error("click")
	}
	if got := s.VM().Graph.Selected.Get(); got != "node:s:bbbbbbbb" {
		t.Fatalf("selected %q", got)
	}
	if s.VM().TableSel.Get() != 1 {
		t.Errorf("table row %d", s.VM().TableSel.Get())
	}
	if s.VM().Detail.At(0) != "session "+idB {
		t.Errorf("detail %q", s.VM().Detail.At(0))
	}
	// Keys move the graph selection; Escape clears it.
	if !s.HandleKeyDown("ArrowRight") {
		t.Error("graph is the key target")
	}
	if s.VM().Graph.Selected.Get() == "node:s:bbbbbbbb" {
		t.Error("ArrowRight must move the selection")
	}
	s.HandleKeyDown("Escape")
	if s.VM().Graph.Selected.Get() != "" {
		t.Error("Escape must clear")
	}
	if got := s.VM().Detail.At(0); got != "ANOMALIES (2): shown, not hidden" {
		t.Errorf("detail %q", got)
	}
}

func TestSceneClickInTableSelectsTheNode(t *testing.T) {
	s := newScene(t)
	pixels(s)
	tb := s.table.Bounds()
	// Find, by the table's own hit-test, a y inside the second row.
	y := -1
	for ly := 0; ly < tb.H; ly++ {
		if s.table.RowAt(20, ly) == 1 {
			y = tb.Y + ly
			break
		}
	}
	if y < 0 {
		t.Fatal("no y hits row 1")
	}
	if !s.HandleClick(tb.X+20, y) {
		t.Error("click")
	}
	if got := s.VM().Graph.Selected.Get(); got != "node:s:bbbbbbbb" {
		t.Errorf("selected %q (table selection %d)", got, s.VM().TableSel.Get())
	}
}

func TestSceneTypingFiltersAndFocus(t *testing.T) {
	s := newScene(t)
	if s.HandleChar("x") || s.HandleKeyDown("Backspace") {
		t.Error("nothing is focused yet")
	}
	sb := s.search.Bounds()
	s.HandleClick(sb.X+10, sb.Y+5)
	for _, ch := range "bbbb" {
		if !s.HandleChar(string(ch)) {
			t.Fatal("char")
		}
	}
	if s.VM().Query.Get() != "bbbb" || s.VM().Rows.Len() != 1 {
		t.Errorf("query %q rows %d", s.VM().Query.Get(), s.VM().Rows.Len())
	}
	s.HandleKeyDown("Backspace")
	if s.VM().Query.Get() != "bbb" {
		t.Errorf("query %q", s.VM().Query.Get())
	}
	// A click on nothing drops the focus and the capture.
	s.HandleClick(1, SurfaceH-1)
	if s.HandleChar("x") || s.HandleMove(5, 5) || s.HandleRelease(5, 5) {
		t.Error("a click outside every widget must drop focus")
	}
}

func TestSceneDragAndRelease(t *testing.T) {
	s := newScene(t)
	pixels(s)
	db := s.detail.Bounds()
	s.HandleClick(db.X+db.W-3, db.Y+10)
	if !s.HandleMove(db.X+db.W-3, db.Y+40) {
		t.Error("a captured widget gets the drag")
	}
	if !s.HandleRelease(db.X+db.W-3, db.Y+40) {
		t.Error("and the release")
	}
	if s.HandleRelease(0, 0) {
		t.Error("the capture is dropped after the release")
	}
}

func TestSceneScrollReachesTheWidgetUnderThePointer(t *testing.T) {
	s := newScene(t)
	pixels(s)
	db := s.detail.Bounds()
	if !s.HandleScroll(db.X+10, db.Y+10, 3) {
		t.Error("scroll")
	}
	if !s.HandleScroll(db.X+10, db.Y+10, -3) {
		t.Error("scroll")
	}
}

func TestSceneCheckButtonAndEscapeAndActivate(t *testing.T) {
	s := NewScene(SurfaceW, SurfaceH)
	s.VM().Apply(orgSnap())
	pixels(s)
	// The "expired leases" check button is bound to ShowExpired.
	s.expired.Checked().Set(true)
	if !s.VM().ShowExpired.Get() {
		t.Error("the check button did not drive the ViewModel")
	}
	s.VM().ShowExpired.Set(false)
	if s.expired.Checked().Get() {
		t.Error("the ViewModel did not drive the check button")
	}
	// Select the org node, activate it with Enter: it expands.
	box, ok := s.graph.NodeBox("o:ghost")
	if !ok {
		t.Fatal("no org node")
	}
	x, y := center(box)
	s.HandleClick(x, y)
	s.HandleKeyDown("Enter")
	if s.VM().Expanded.Len() != 1 {
		t.Fatalf("Enter did not expand: %v", s.VM().Expanded.Slice())
	}
	if !s.Dirty() {
		t.Error("an expansion must repaint")
	}
	// Escape collapses, with or without a focused widget.
	s.HandleKeyDown("Escape")
	if s.VM().Expanded.Len() != 0 {
		t.Error("Escape did not collapse")
	}
	s.VM().ToggleOrg("node:o:ghost")
	s.HandleClick(1, SurfaceH-1) // drops the focus
	if !s.HandleKeyDown("Escape") || s.VM().Expanded.Len() != 0 {
		t.Error("Escape with nothing focused must still collapse")
	}
}

func TestSceneConnectionLabelAndStatusAreBound(t *testing.T) {
	s := newScene(t)
	if s.conn.Text().Get() != "connecting" {
		t.Errorf("conn %q", s.conn.Text().Get())
	}
	s.VM().Conn.Set("lost: eof")
	if s.conn.Text().Get() != "lost: eof" {
		t.Error("conn not followed")
	}
	if s.status.Text().Get() == "" {
		t.Error("status not bound")
	}
	for _, u := range s.unbind {
		u()
	}
}

func TestHelpers(t *testing.T) {
	buf := make([]byte, 8)
	fillBG(buf, toolkit.RGBA{R: 1, G: 2, B: 3, A: 4})
	if buf[4] != 1 || buf[7] != 4 {
		t.Error("fillBG")
	}
	r := toolkit.Rect{X: 1, Y: 1, W: 2, H: 2}
	if !inside(1, 1, r) || inside(3, 1, r) || inside(0, 1, r) || inside(1, 3, r) {
		t.Error("inside")
	}
	if e := local(toolkit.Event{X: 5, Y: 6}, r); e.X != 4 || e.Y != 5 {
		t.Error("local")
	}
}
