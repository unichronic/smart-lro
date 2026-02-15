package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProfile(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "profile.json")
	if err := os.WriteFile(p, []byte(`{"lnd_host":"localhost:10009","num_routes":7,"pick_rank":2}`), 0o600); err != nil {
		t.Fatal(err)
	}

	prof, err := LoadProfile(p)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if prof.LNDHost != "localhost:10009" || prof.NumRoutes != 7 || prof.PickRank != 2 {
		t.Fatalf("unexpected profile: %+v", prof)
	}
}
