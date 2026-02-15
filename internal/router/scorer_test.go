package router

import (
	"context"
	"testing"

	"github.com/lightningnetwork/lnd/lnrpc"
)

func TestScoreRoutes_PenalizesFailedChannels(t *testing.T) {
	routes := []*lnrpc.Route{
		{TotalFeesMsat: 1000, TotalAmtMsat: 101000, Hops: []*lnrpc.Hop{{ChanId: 1, PubKey: pubkeyA()}}},
		{TotalFeesMsat: 1200, TotalAmtMsat: 101200, Hops: []*lnrpc.Hop{{ChanId: 2, PubKey: pubkeyB()}}},
	}

	history := FailureHistory{Records: map[uint64]FailureRecord{1: {ChannelID: 1, Count: 3}}}
	scored := ScoreRoutes(context.Background(), "", nil, routes, history, 1000, ScoreWeights{FeeWeight: 1, FailureWeight: 4000, ProbabilityWeight: 0})
	if len(scored) != 2 {
		t.Fatalf("expected 2 scored routes, got %d", len(scored))
	}
	if scored[0].Route.Hops[0].ChanId != 2 {
		t.Fatalf("expected route with chan 2 to rank higher, got chan %d", scored[0].Route.Hops[0].ChanId)
	}
}

func TestRegisterFailure(t *testing.T) {
	h := FailureHistory{Records: map[uint64]FailureRecord{}}
	h = RegisterFailure(h, []uint64{10, 11, 10})
	if got := h.Records[10].Count; got != 2 {
		t.Fatalf("expected channel 10 count=2, got %d", got)
	}
	if got := h.Records[11].Count; got != 1 {
		t.Fatalf("expected channel 11 count=1, got %d", got)
	}
}

func pubkeyA() string { return "02aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }
func pubkeyB() string { return "03bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }
