package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-widgets/toolkit"
)

// crowded is a network like the owner's at --horizon 48h: many sessions, each
// with dozens of sub-agents, long titles, long repository names, several models.
func crowded() model.Snapshot {
	models := []string{"claude-opus-5-5", "claude-opus-5", "claude-sonnet-5-5", "claude-haiku-5", "claude-mystery-9", "claude-fable-1"}
	sn := model.Snapshot{Version: 1, At: t0, ActiveFor: "10m0s", Horizon: "48h0m0s", ContextWindow: 300000}
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("%08x-0000-4000-8000-%012d", 0xaaaa0000+i, i)
		se := model.Session{
			ID: id, Short: id[:8], Active: i%3 == 0, HasTranscript: true,
			Title: strings.Repeat("a very long session title ", 6), CwdRepo: fmt.Sprintf("an-organisation-with-a-long-name/a-repository-with-a-long-name-%d", i),
			Model: models[i%len(models)], LastActivity: t0.Add(-time.Duration(i) * 40 * time.Minute), Context: int64(i) * 25000,
		}
		for j := 0; j < 30; j++ {
			se.Agents = append(se.Agents, model.Agent{
				ID: fmt.Sprintf("agent%03d%02d", i, j), Type: "general-purpose", Description: strings.Repeat("a long sub-agent description ", 8),
				LastActivity: t0.Add(-time.Duration(j) * time.Minute), Active: j%4 == 0, Model: models[j%len(models)], Context: int64(j) * 10000,
			})
		}
		sn.Sessions = append(sn.Sessions, se)
		sn.Leases = append(sn.Leases, model.Lease{
			Resource: "r", Repo: fmt.Sprintf("an-organisation-with-a-long-name/a-repository-with-a-long-name-%d", i), Kind: model.KindRepo,
			Owner: id, Note: strings.Repeat("a long lease note ", 10), Acquired: t0.Add(-time.Hour), Expires: t0.Add(time.Hour),
		})
	}
	return sn
}

// outside lists what is placed outside its parent in the widget trees of a scene:
// the page, the side panel and the Rename dialog.
func outside(s *Scene) []toolkit.LayoutProblem {
	var out []toolkit.LayoutProblem
	for _, root := range []toolkit.Widget{s.root, s.panel, s.renameDlg} {
		for _, p := range toolkit.LayoutProblems(root) {
			if p.Kind == toolkit.LayoutOutside {
				out = append(out, p)
			}
		}
	}
	return out
}

// Nothing leaves its frame, in either theme's layout, at any of the sizes the
// owner uses and at a narrow one, with the real-data shape: many agents, long
// labels, the panel open on a session with every detail line.
func TestNothingLeavesItsFrame(t *testing.T) {
	defer toolkit.SetMetricScale(toolkit.MetricScale())
	for _, size := range []struct{ w, h int }{{1920, 1080}, {1280, 800}, {1024, 768}, {800, 600}, {640, 480}} {
		for _, scale := range []float64{1, 2} {
			s := NewScene(size.w, size.h)
			s.Resize(int(float64(size.w)*scale), int(float64(size.h)*scale), scale)
			vm := s.VM()
			vm.Apply(snapWith2(withUsage(crowded()), guardFixture()))
			vm.PanelOpen.Set(true)
			vm.Graph.Selected.Set("node:s:" + crowded().Sessions[0].Short)
			vm.Rename.Open.Set(true)
			name := fmt.Sprintf("%dx%d at %gx", size.w, size.h, scale)
			if got := outside(s); len(got) != 0 {
				t.Fatalf("%s, graph tab: %v", name, got[0])
			}
			if ps := toolkit.LayoutProblems(s.graph); len(ps) != 0 {
				t.Fatalf("%s: the graph's clusters and nodes: %v", name, ps[0])
			}
			vm.Rename.Open.Set(false)
			vm.Tab.Set(TabQuota)
			if got := outside(s); len(got) != 0 {
				t.Fatalf("%s, quota tab: %v", name, got[0])
			}
			pixels(s)
			vm.Tab.Set(TabGuardrails)
			vm.Guard.Sel.Set(0)
			if got := outside(s); len(got) != 0 {
				t.Fatalf("%s, guardrails tab: %v", name, got[0])
			}
			pixels(s)
			vm.Tab.Set(TabGraph)
			pixels(s)
		}
	}
}

// The check itself fails on a child that overflows its parent, and passes on one
// that does not: both sides, through the scene's own widgets.
func TestTheContainmentCheckSeesAnOverflow(t *testing.T) {
	s := newScene(t)
	s.VM().PanelOpen.Set(true)
	if got := outside(s); len(got) != 0 {
		t.Fatalf("a laid-out scene reports %v", got)
	}
	// Push the close button out of the panel's top bar.
	b := s.closeBtn.Bounds()
	s.closeBtn.SetBounds(toolkit.Rect{X: b.X + 5000, Y: b.Y, W: b.W, H: b.H})
	if got := outside(s); len(got) == 0 {
		t.Fatal("a button placed 5000 px to the right of its bar went unreported")
	}
}

// The same check bites on the Guardrails tab: a chart pushed out of its card is
// reported there too.
func TestTheContainmentCheckSeesAnOverflowOnTheGuardrailsTab(t *testing.T) {
	s := guardScene(t)
	if got := outside(s); len(got) != 0 {
		t.Fatalf("a laid-out Guardrails tab reports %v", got)
	}
	b := s.gBars.Bounds()
	s.gBars.SetBounds(toolkit.Rect{X: b.X + 5000, Y: b.Y, W: b.W, H: b.H})
	if got := outside(s); len(got) == 0 {
		t.Fatal("a chart placed 5000 px to the right of its card went unreported")
	}
}

func snapWith2(sn model.Snapshot, g *model.Guardrails) model.Snapshot {
	sn.Guardrails = g
	return sn
}
