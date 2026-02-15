package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Profile struct {
	LNDHost            string  `json:"lnd_host"`
	TLSCert            string  `json:"tls_cert"`
	Macaroon           string  `json:"macaroon"`
	InsecureTLS        *bool   `json:"insecure_tls,omitempty"`
	FailureLog         string  `json:"failure_log,omitempty"`
	AttemptLog         string  `json:"attempt_log,omitempty"`
	FeeWeight          float64 `json:"w_fee,omitempty"`
	FailWeight         float64 `json:"w_fail,omitempty"`
	ProbWeight         float64 `json:"w_prob,omitempty"`
	NumRoutes          int     `json:"num_routes,omitempty"`
	PickRank           int     `json:"pick_rank,omitempty"`
	FeeLimitSat        int64   `json:"fee_limit_sat,omitempty"`
	FinalCLTV          int32   `json:"final_cltv_delta,omitempty"`
	AvoidFailuresHours int     `json:"avoid_failures_hours,omitempty"`
}

func LoadProfile(path string) (Profile, error) {
	if path == "" {
		return Profile{}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("read profile: %w", err)
	}
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return Profile{}, fmt.Errorf("decode profile: %w", err)
	}
	return p, nil
}
