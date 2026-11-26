package model

import "testing"

func TestWindowOfAgentPrefersTheTypeThenTheSessionThenTheDefault(t *testing.T) {
	s := Snapshot{AgentWindows: map[string]int64{"big": 1000}, ContextWindow: 500}
	if w := s.WindowOfAgent(Agent{Type: "big"}); w != 1000 {
		t.Fatalf("type window: %d", w)
	}
	if w := s.WindowOfAgent(Agent{Type: "other"}); w != 500 {
		t.Fatalf("session window: %d", w)
	}
	if w := (Snapshot{}).WindowOfAgent(Agent{}); w != DefaultCompactWindow {
		t.Fatalf("default window: %d", w)
	}
}
