package analytics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSummarizeJSONL(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "attempts.jsonl")

	_ = AppendJSONL(p, AttemptEvent{Command: "routes", Success: true, Destination: "a"})
	_ = AppendJSONL(p, AttemptEvent{Command: "send-route", Success: false, DryRun: true, Destination: "a", Error: "e1"})
	_ = AppendJSONL(p, AttemptEvent{Command: "send-route", Success: false, Destination: "b", Error: "e1"})
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}

	s, err := SummarizeJSONL(p)
	if err != nil {
		t.Fatalf("summary err: %v", err)
	}
	if s.Total != 3 || s.Succeeded != 1 || s.Failed != 2 || s.DryRuns != 1 {
		t.Fatalf("bad totals: %+v", s)
	}
	if s.ByCommand["send-route"] != 2 || s.ByDest["a"] != 2 || s.ErrorCounts["e1"] != 2 {
		t.Fatalf("bad maps: %+v", s)
	}
}
