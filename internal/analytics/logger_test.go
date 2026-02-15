package analytics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendJSONL(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "attempts.jsonl")

	if err := AppendJSONL(p, AttemptEvent{Command: "routes", Success: true}); err != nil {
		t.Fatalf("append event 1: %v", err)
	}
	if err := AppendJSONL(p, AttemptEvent{Command: "send-route", Success: false, Error: "boom"}); err != nil {
		t.Fatalf("append event 2: %v", err)
	}

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}
