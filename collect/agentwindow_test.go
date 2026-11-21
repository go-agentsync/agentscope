package collect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-agentsync/agentscope/model"
)

func TestAgentWindowsComeFromTheTypeDefinitions(t *testing.T) {
	dir := t.TempDir()
	ad := filepath.Join(dir, "agents")
	if err := os.MkdirAll(ad, 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(name, body string) {
		if err := os.WriteFile(filepath.Join(ad, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("implementer.md", "---\nname: implementer\nautoCompactWindow: 200000\n---\nbody autoCompactWindow: 5\n")
	put("checker.md", "---\nautoCompactWindow: 100000\n---\n")
	put("none.md", "---\nname: none\n---\n")
	put("bad.md", "---\nautoCompactWindow: lots\n---\n")
	put("nofront.md", "# no frontmatter\nautoCompactWindow: 7\n")
	put("empty.md", "")
	got := AgentWindows(dir)
	if len(got) != 2 || got["implementer"] != 200000 || got["checker"] != 100000 {
		t.Fatalf("AgentWindows = %v", got)
	}
	if AgentWindows(filepath.Join(dir, "missing")) != nil {
		t.Fatal("no agents directory must give no windows")
	}
}

func TestAnAgentIsMeasuredAgainstItsTypesWindow(t *testing.T) {
	s := model.Snapshot{ContextWindow: 300000, AgentWindows: map[string]int64{"implementer": 200000}}
	if got := s.WindowOfAgent(model.Agent{Type: "implementer"}); got != 200000 {
		t.Fatalf("implementer = %d, want 200000", got)
	}
	if got := s.WindowOfAgent(model.Agent{Type: "general-purpose"}); got != 300000 {
		t.Fatalf("other type = %d, want the session's 300000", got)
	}
	if got := (model.Snapshot{}).WindowOfAgent(model.Agent{}); got != model.DefaultCompactWindow {
		t.Fatalf("no window anywhere = %d, want the default", got)
	}
}
