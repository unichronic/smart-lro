package analytics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type Summary struct {
	Total       int            `json:"total"`
	Succeeded   int            `json:"succeeded"`
	Failed      int            `json:"failed"`
	DryRuns     int            `json:"dry_runs"`
	ByCommand   map[string]int `json:"by_command"`
	ByDest      map[string]int `json:"by_destination"`
	ErrorCounts map[string]int `json:"error_counts"`
}

func SummarizeJSONL(path string) (Summary, error) {
	s := Summary{ByCommand: map[string]int{}, ByDest: map[string]int{}, ErrorCounts: map[string]int{}}
	if path == "" {
		return s, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return s, fmt.Errorf("open attempt log: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e AttemptEvent
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		s.Total++
		s.ByCommand[e.Command]++
		if e.Destination != "" {
			s.ByDest[e.Destination]++
		}
		if e.DryRun {
			s.DryRuns++
		}
		if e.Success {
			s.Succeeded++
		} else {
			s.Failed++
			if e.Error != "" {
				s.ErrorCounts[e.Error]++
			}
		}
	}
	if err := sc.Err(); err != nil {
		return s, fmt.Errorf("scan attempt log: %w", err)
	}
	return s, nil
}
