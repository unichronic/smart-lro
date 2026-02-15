package router

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

type RouteScore struct {
	Route             *lnrpc.Route `json:"-"`
	Index             int          `json:"index"`
	Score             float64      `json:"score"`
	TotalFeesMsat     int64        `json:"total_fees_msat"`
	TotalAmtMsat      int64        `json:"total_amt_msat"`
	HopCount          int          `json:"hop_count"`
	FailedChannelHits int          `json:"failed_channel_hits"`
	AvgProbability    float64      `json:"avg_probability"`
}

type ScoreWeights struct {
	FeeWeight          float64
	FailureWeight      float64
	ProbabilityWeight  float64
	AvoidFailuresHours int
}

type FailureRecord struct {
	ChannelID   uint64    `json:"channel_id"`
	Count       int       `json:"count"`
	LastFailure time.Time `json:"last_failure"`
}

type FailureHistory struct {
	Records map[uint64]FailureRecord `json:"records"`
}

func DefaultWeights() ScoreWeights {
	return ScoreWeights{FeeWeight: 1.0, FailureWeight: 4000.0, ProbabilityWeight: 2000.0, AvoidFailuresHours: 24}
}

func ScoreRoutes(ctx context.Context, sourcePubkey string, routerClient routerrpc.RouterClient, routes []*lnrpc.Route, failures FailureHistory, amtMsat int64, weights ScoreWeights) []RouteScore {
	scored := make([]RouteScore, 0, len(routes))
	for i, r := range routes {
		failHits := failedHits(r, failures, weights.AvoidFailuresHours)
		avgProb := avgRouteProbability(ctx, sourcePubkey, routerClient, r, amtMsat)

		feePenalty := float64(r.TotalFeesMsat) * weights.FeeWeight
		failurePenalty := float64(failHits) * weights.FailureWeight
		probabilityBoost := avgProb * weights.ProbabilityWeight

		score := (100000.0 - feePenalty - failurePenalty) + probabilityBoost
		scored = append(scored, RouteScore{
			Route:             r,
			Index:             i,
			Score:             round(score),
			TotalFeesMsat:     r.TotalFeesMsat,
			TotalAmtMsat:      r.TotalAmtMsat,
			HopCount:          len(r.Hops),
			FailedChannelHits: failHits,
			AvgProbability:    round(avgProb),
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].TotalFeesMsat < scored[j].TotalFeesMsat
		}
		return scored[i].Score > scored[j].Score
	})

	return scored
}

func LoadFailureHistory(path string) (FailureHistory, error) {
	if path == "" {
		return FailureHistory{Records: map[uint64]FailureRecord{}}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FailureHistory{Records: map[uint64]FailureRecord{}}, nil
		}
		return FailureHistory{}, fmt.Errorf("read failure history: %w", err)
	}

	var history FailureHistory
	if err := json.Unmarshal(b, &history); err != nil {
		return FailureHistory{}, fmt.Errorf("decode failure history: %w", err)
	}
	if history.Records == nil {
		history.Records = map[uint64]FailureRecord{}
	}
	return history, nil
}

func SaveFailureHistory(path string, history FailureHistory) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func RegisterFailure(history FailureHistory, chanIDs []uint64) FailureHistory {
	if history.Records == nil {
		history.Records = map[uint64]FailureRecord{}
	}
	now := time.Now().UTC()
	for _, id := range chanIDs {
		rec := history.Records[id]
		rec.ChannelID = id
		rec.Count++
		rec.LastFailure = now
		history.Records[id] = rec
	}
	return history
}

func failedHits(route *lnrpc.Route, history FailureHistory, avoidHours int) int {
	hits := 0
	var cutoff time.Time
	if avoidHours > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(avoidHours) * time.Hour)
	}

	for _, h := range route.Hops {
		rec, ok := history.Records[h.ChanId]
		if !ok || rec.Count <= 0 {
			continue
		}
		if !cutoff.IsZero() && rec.LastFailure.Before(cutoff) {
			continue
		}
		hits += rec.Count
	}
	return hits
}

func avgRouteProbability(ctx context.Context, sourcePubkey string, client routerrpc.RouterClient, route *lnrpc.Route, amtMsat int64) float64 {
	if client == nil || len(route.Hops) == 0 || sourcePubkey == "" {
		return 0
	}

	fromBytes, err := hex.DecodeString(sourcePubkey)
	if err != nil {
		return 0
	}

	var probs []float64
	from := fromBytes
	for _, hop := range route.Hops {
		to, err := hex.DecodeString(hop.PubKey)
		if err != nil {
			continue
		}
		res, err := client.QueryProbability(ctx, &routerrpc.QueryProbabilityRequest{
			FromNode: from,
			ToNode:   to,
			AmtMsat:  amtMsat,
		})
		if err != nil {
			continue
		}
		probs = append(probs, res.Probability)
		from = to
	}
	if len(probs) == 0 {
		return 0
	}
	var sum float64
	for _, p := range probs {
		sum += p
	}
	return sum / float64(len(probs))
}

func ExtractChannelIDs(route *lnrpc.Route) []uint64 {
	ids := make([]uint64, 0, len(route.Hops))
	for _, h := range route.Hops {
		ids = append(ids, h.ChanId)
	}
	return ids
}

func round(v float64) float64 {
	return math.Round(v*1000) / 1000
}
