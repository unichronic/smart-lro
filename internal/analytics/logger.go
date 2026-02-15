package analytics

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type AttemptEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Command     string    `json:"command"`
	Destination string    `json:"destination,omitempty"`
	AmtSat      int64     `json:"amt_sat,omitempty"`
	Selected    int       `json:"selected_rank,omitempty"`
	DryRun      bool      `json:"dry_run,omitempty"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
	Meta        any       `json:"meta,omitempty"`
}

func AppendJSONL(path string, event AttemptEvent) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open attempt log: %w", err)
	}
	defer f.Close()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	b, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal attempt event: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write attempt event: %w", err)
	}
	return nil
}
