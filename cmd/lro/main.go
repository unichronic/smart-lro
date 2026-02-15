package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/example/lro/internal/config"
	"github.com/example/lro/internal/lnd"
	"github.com/example/lro/internal/router"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage()
		return nil
	case "health":
		return runHealth(args[1:])
	case "routes":
		return runRoutes(args[1:])
	case "send-route":
		return runSendRoute(args[1:])
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func printUsage() {
	fmt.Println(config.UsageHeader())
	fmt.Println(`Commands:
  health       Verify LND connectivity (GetInfo)
  routes       Query routes and rerank using custom scorer
  send-route   Query+rerrank then attempt send via selected route

Example:
  lro health --lnd-host localhost:10009 --tls-cert ~/.lnd/tls.cert --macaroon ~/.lnd/data/chain/bitcoin/regtest/admin.macaroon
  lro routes --dest <pubkey> --amt-sat 2000 --num-routes 5 --failure-log .lro-failures.json --json
  lro send-route --dest <pubkey> --amt-sat 2000 --payment-hash <hashhex> --pick-rank 1 --dry-run`)
}

func runHealth(args []string) error {
	var cfg config.LNDConfig
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	config.BindLNDFlags(fs, &cfg)
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := lnd.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	info, err := client.LN.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return err
	}

	fmt.Printf("connected to LND %s\n", cfg.Host)
	fmt.Printf("pubkey=%s synced_to_chain=%v synced_to_graph=%v num_active_channels=%d\n", info.IdentityPubkey, info.SyncedToChain, info.SyncedToGraph, info.NumActiveChannels)
	return nil
}

func runRoutes(args []string) error {
	var cfg config.LNDConfig
	var (
		dest        string
		amtSat      int64
		numRoutes   int
		feeLimitSat int64
		failureLog  string
		jsonOutput  bool
		feeWeight   float64
		failWeight  float64
		probWeight  float64
	)

	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	config.BindLNDFlags(fs, &cfg)
	fs.StringVar(&dest, "dest", "", "Destination node pubkey (hex)")
	fs.Int64Var(&amtSat, "amt-sat", 0, "Payment amount in satoshis")
	fs.IntVar(&numRoutes, "num-routes", 10, "Maximum reranked routes to print")
	fs.Int64Var(&feeLimitSat, "fee-limit-sat", 0, "Optional fee limit in satoshis")
	fs.StringVar(&failureLog, "failure-log", ".lro-failures.json", "Failure history JSON path")
	fs.BoolVar(&jsonOutput, "json", false, "Print scored routes as JSON")
	fs.Float64Var(&feeWeight, "w-fee", 1.0, "Fee penalty weight")
	fs.Float64Var(&failWeight, "w-fail", 4000.0, "Failed-channel penalty weight")
	fs.Float64Var(&probWeight, "w-prob", 2000.0, "Mission-control probability boost weight")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := config.RequireNonEmpty("dest", dest); err != nil {
		return err
	}
	if amtSat <= 0 {
		return errors.New("amt-sat must be > 0")
	}
	if numRoutes <= 0 {
		return errors.New("num-routes must be > 0")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	client, err := lnd.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	if _, err := hex.DecodeString(strings.TrimSpace(dest)); err != nil {
		return fmt.Errorf("decode dest pubkey: %w", err)
	}

	query := &lnrpc.QueryRoutesRequest{
		PubKey:            dest,
		Amt:               amtSat,
		FinalCltvDelta:    40,
		UseMissionControl: true,
		DestCustomRecords: map[uint64][]byte{},
	}
	if feeLimitSat > 0 {
		query.FeeLimit = &lnrpc.FeeLimit{Limit: &lnrpc.FeeLimit_Fixed{Fixed: feeLimitSat}}
	}

	routesResp, err := client.LN.QueryRoutes(ctx, query)
	if err != nil {
		return fmt.Errorf("query routes: %w", err)
	}
	if len(routesResp.Routes) == 0 {
		return errors.New("no routes returned by LND")
	}

	history, err := router.LoadFailureHistory(failureLog)
	if err != nil {
		return err
	}
	info, err := client.LN.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return err
	}
	weights := router.ScoreWeights{FeeWeight: feeWeight, FailureWeight: failWeight, ProbabilityWeight: probWeight}
	scored := router.ScoreRoutes(ctx, info.IdentityPubkey, client.Router, routesResp.Routes, history, amtSat*1000, weights)
	scored = limitScores(scored, numRoutes)

	if jsonOutput {
		b, _ := json.MarshalIndent(scored, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("fetched %d routes; showing %d reranked results:\n", len(routesResp.Routes), len(scored))
	for i, r := range scored {
		fmt.Printf("rank=%d source_idx=%d score=%.2f fee_msat=%d hops=%d fail_hits=%d avg_prob=%.3f\n", i+1, r.Index, r.Score, r.TotalFeesMsat, r.HopCount, r.FailedChannelHits, r.AvgProbability)
	}
	return nil
}

func runSendRoute(args []string) error {
	var cfg config.LNDConfig
	var (
		dest        string
		amtSat      int64
		numRoutes   int
		pickRank    int
		paymentHash string
		failureLog  string
		dryRun      bool
	)
	fs := flag.NewFlagSet("send-route", flag.ContinueOnError)
	config.BindLNDFlags(fs, &cfg)
	fs.StringVar(&dest, "dest", "", "Destination node pubkey (hex)")
	fs.Int64Var(&amtSat, "amt-sat", 0, "Amount in satoshis")
	fs.IntVar(&numRoutes, "num-routes", 10, "Maximum reranked routes considered")
	fs.IntVar(&pickRank, "pick-rank", 1, "1-based reranked route index to attempt")
	fs.StringVar(&paymentHash, "payment-hash", "", "32-byte payment hash hex")
	fs.StringVar(&failureLog, "failure-log", ".lro-failures.json", "Failure history JSON path")
	fs.BoolVar(&dryRun, "dry-run", false, "Do not send; only print selected route")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := config.RequireNonEmpty("dest", dest); err != nil {
		return err
	}
	if err := config.RequireNonEmpty("payment-hash", paymentHash); err != nil {
		return err
	}
	if amtSat <= 0 {
		return errors.New("amt-sat must be > 0")
	}
	if numRoutes <= 0 {
		return errors.New("num-routes must be > 0")
	}
	if pickRank <= 0 {
		return errors.New("pick-rank must be > 0")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := lnd.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	query := &lnrpc.QueryRoutesRequest{
		PubKey:            dest,
		Amt:               amtSat,
		UseMissionControl: true,
	}
	routesResp, err := client.LN.QueryRoutes(ctx, query)
	if err != nil {
		return err
	}
	if len(routesResp.Routes) == 0 {
		return errors.New("no route candidates returned")
	}

	history, err := router.LoadFailureHistory(failureLog)
	if err != nil {
		return err
	}
	info, err := client.LN.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return err
	}
	scored := router.ScoreRoutes(ctx, info.IdentityPubkey, client.Router, routesResp.Routes, history, amtSat*1000, router.DefaultWeights())
	scored = limitScores(scored, numRoutes)
	if pickRank > len(scored) {
		return fmt.Errorf("pick-rank=%d out of range; only %d scored routes available", pickRank, len(scored))
	}

	selected := scored[pickRank-1]
	fmt.Printf("selected route rank=%d score=%.2f fee_msat=%d hops=%d\n", pickRank, selected.Score, selected.TotalFeesMsat, selected.HopCount)
	if dryRun {
		fmt.Println("dry-run enabled: no payment was attempted")
		return nil
	}

	hashBytes, err := hex.DecodeString(strings.TrimSpace(paymentHash))
	if err != nil {
		return err
	}
	if len(hashBytes) != 32 {
		return fmt.Errorf("payment hash must be 32 bytes, got %d", len(hashBytes))
	}

	resp, err := client.Router.SendToRouteV2(ctx, &routerrpc.SendToRouteRequest{
		PaymentHash: hashBytes,
		Route:       selected.Route,
		SkipTempErr: true,
	})
	if err != nil {
		history = router.RegisterFailure(history, router.ExtractChannelIDs(selected.Route))
		_ = router.SaveFailureHistory(failureLog, history)
		return fmt.Errorf("send to route failed: %w", err)
	}

	fmt.Printf("status=%v preimage=%x htlc_attempt_id=%d\n", resp.Status, resp.Preimage, resp.AttemptId)
	if resp.Status != lnrpc.HTLCAttempt_SUCCEEDED {
		history = router.RegisterFailure(history, router.ExtractChannelIDs(selected.Route))
	}
	_ = router.SaveFailureHistory(failureLog, history)
	return nil
}

func limitScores(scored []router.RouteScore, max int) []router.RouteScore {
	if max <= 0 || len(scored) <= max {
		return scored
	}

	top := append([]router.RouteScore(nil), scored...)
	sort.SliceStable(top, func(i, j int) bool {
		if top[i].Score == top[j].Score {
			return top[i].TotalFeesMsat < top[j].TotalFeesMsat
		}
		return top[i].Score > top[j].Score
	})
	return top[:max]
}
