package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/lro/internal/analytics"
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
	case "routes", "optimize":
		return runRoutes(args[1:])
	case "send-route", "send":
		return runSendRoute(args[1:])
	case "batch-send":
		return runBatchSend(args[1:])
	case "report":
		return runReport(args[1:])
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
  optimize     Alias for routes (invoice-aware)
  send-route   Query+rerrank then attempt send via selected route
  send         Alias for send-route (invoice-aware)
  batch-send   Process multiple invoices concurrently
  report       Summarize JSONL attempt logs

Examples:
  lro optimize --profile ./profile.json --invoice <bolt11> --num-routes 5 --json
  lro send --profile ./profile.json --invoice <bolt11> --pick-rank 1 --dry-run
  lro batch-send --profile ./profile.json --invoices-file ./invoices.txt --workers 3 --dry-run
  lro report --attempt-log .lro-attempts.jsonl --json`)
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
		profilePath       string
		invoice           string
		dest              string
		amtSat            int64
		numRoutes         int
		feeLimitSat       int64
		finalCLTV         int
		failureLog        string
		attemptLog        string
		jsonOutput        bool
		feeWeight         float64
		failWeight        float64
		probWeight        float64
		avoidFailureHours int
	)

	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	config.BindLNDFlags(fs, &cfg)
	fs.StringVar(&profilePath, "profile", "", "Path to JSON profile for reproducible experiments")
	fs.StringVar(&invoice, "invoice", "", "BOLT11 invoice (dest/amount auto-decoded)")
	fs.StringVar(&dest, "dest", "", "Destination node pubkey (hex)")
	fs.Int64Var(&amtSat, "amt-sat", 0, "Payment amount in satoshis")
	fs.IntVar(&numRoutes, "num-routes", 10, "Maximum reranked routes to print")
	fs.Int64Var(&feeLimitSat, "fee-limit-sat", 0, "Optional fee limit in satoshis")
	fs.IntVar(&finalCLTV, "final-cltv", 40, "Final CLTV delta for QueryRoutes")
	fs.IntVar(&avoidFailureHours, "avoid-failures-hours", 24, "Penalize only channels with failures in this recent window")
	fs.StringVar(&failureLog, "failure-log", ".lro-failures.json", "Failure history JSON path")
	fs.StringVar(&attemptLog, "attempt-log", ".lro-attempts.jsonl", "Structured attempt/output log path")
	fs.BoolVar(&jsonOutput, "json", false, "Print scored routes as JSON")
	fs.Float64Var(&feeWeight, "w-fee", 1.0, "Fee penalty weight")
	fs.Float64Var(&failWeight, "w-fail", 4000.0, "Failed-channel penalty weight")
	fs.Float64Var(&probWeight, "w-prob", 2000.0, "Mission-control probability boost weight")
	if err := fs.Parse(args); err != nil {
		return err
	}

	profile, err := config.LoadProfile(profilePath)
	if err != nil {
		return err
	}
	applyProfileToLNDConfig(profile, &cfg)
	applyProfileToRoutes(profile, &numRoutes, &feeLimitSat, &failureLog, &attemptLog, &feeWeight, &failWeight, &probWeight, &finalCLTV, &avoidFailureHours)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	client, err := lnd.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	dest, amtSat, _, err = populateFromInvoice(ctx, client, invoice, dest, amtSat, "")
	if err != nil {
		return err
	}
	if err := config.RequireNonEmpty("dest", dest); err != nil {
		return err
	}
	if amtSat <= 0 {
		return errors.New("amt-sat must be > 0 (or provide an invoice with amount)")
	}
	if numRoutes <= 0 {
		return errors.New("num-routes must be > 0")
	}
	if _, err := hex.DecodeString(strings.TrimSpace(dest)); err != nil {
		return fmt.Errorf("decode dest pubkey: %w", err)
	}

	query := &lnrpc.QueryRoutesRequest{PubKey: dest, Amt: amtSat, FinalCltvDelta: int32(finalCLTV), UseMissionControl: true, DestCustomRecords: map[uint64][]byte{}}
	if feeLimitSat > 0 {
		query.FeeLimit = &lnrpc.FeeLimit{Limit: &lnrpc.FeeLimit_Fixed{Fixed: feeLimitSat}}
	}

	routesResp, err := client.LN.QueryRoutes(ctx, query)
	if err != nil {
		_ = analytics.AppendJSONL(attemptLog, analytics.AttemptEvent{Command: "routes", Destination: dest, AmtSat: amtSat, Success: false, Error: err.Error()})
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
	weights := router.ScoreWeights{FeeWeight: feeWeight, FailureWeight: failWeight, ProbabilityWeight: probWeight, AvoidFailuresHours: avoidFailureHours}
	scored := limitScores(router.ScoreRoutes(ctx, info.IdentityPubkey, client.Router, routesResp.Routes, history, amtSat*1000, weights), numRoutes)
	_ = analytics.AppendJSONL(attemptLog, analytics.AttemptEvent{Command: "routes", Destination: dest, AmtSat: amtSat, Success: true, Meta: map[string]any{"returned": len(routesResp.Routes), "shown": len(scored), "invoice_mode": invoice != ""}})

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
		profilePath       string
		invoice           string
		dest              string
		amtSat            int64
		numRoutes         int
		pickRank          int
		paymentHash       string
		failureLog        string
		attemptLog        string
		dryRun            bool
		avoidFailureHours int
	)
	fs := flag.NewFlagSet("send-route", flag.ContinueOnError)
	config.BindLNDFlags(fs, &cfg)
	fs.StringVar(&profilePath, "profile", "", "Path to JSON profile for reproducible experiments")
	fs.StringVar(&invoice, "invoice", "", "BOLT11 invoice (dest/amount/hash auto-decoded)")
	fs.StringVar(&dest, "dest", "", "Destination node pubkey (hex)")
	fs.Int64Var(&amtSat, "amt-sat", 0, "Amount in satoshis")
	fs.IntVar(&numRoutes, "num-routes", 10, "Maximum reranked routes considered")
	fs.IntVar(&pickRank, "pick-rank", 1, "1-based reranked route index to attempt")
	fs.StringVar(&paymentHash, "payment-hash", "", "32-byte payment hash hex (optional if --invoice is provided)")
	fs.IntVar(&avoidFailureHours, "avoid-failures-hours", 24, "Penalize only channels with failures in this recent window")
	fs.StringVar(&failureLog, "failure-log", ".lro-failures.json", "Failure history JSON path")
	fs.StringVar(&attemptLog, "attempt-log", ".lro-attempts.jsonl", "Structured attempt/output log path")
	fs.BoolVar(&dryRun, "dry-run", false, "Do not send; only print selected route")
	if err := fs.Parse(args); err != nil {
		return err
	}

	profile, err := config.LoadProfile(profilePath)
	if err != nil {
		return err
	}
	applyProfileToLNDConfig(profile, &cfg)
	applyProfileToSend(profile, &numRoutes, &pickRank, &failureLog, &attemptLog, &avoidFailureHours)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := lnd.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	dest, amtSat, paymentHash, err = populateFromInvoice(ctx, client, invoice, dest, amtSat, paymentHash)
	if err != nil {
		return err
	}

	history, err := router.LoadFailureHistory(failureLog)
	if err != nil {
		return err
	}
	historyMu := &sync.Mutex{}

	res, err := executeSend(ctx, client, historyMu, &history, sendInput{
		invoice:           invoice,
		dest:              dest,
		amtSat:            amtSat,
		numRoutes:         numRoutes,
		pickRank:          pickRank,
		paymentHash:       paymentHash,
		failureLog:        failureLog,
		attemptLog:        attemptLog,
		dryRun:            dryRun,
		avoidFailureHours: avoidFailureHours,
	})
	if err != nil {
		return err
	}
	if res != nil {
		fmt.Printf("selected route rank=%d score=%.2f fee_msat=%d hops=%d\n", res.SelectedRank, res.Score, res.FeeMsat, res.Hops)
		if res.DryRun {
			fmt.Println("dry-run enabled: no payment was attempted")
		} else {
			fmt.Printf("status=%s htlc_attempt_id=%d\n", res.Status, res.AttemptID)
		}
	}
	return nil
}

func runBatchSend(args []string) error {
	var cfg config.LNDConfig
	var (
		profilePath       string
		invoicesFile      string
		workers           int
		numRoutes         int
		pickRank          int
		failureLog        string
		attemptLog        string
		dryRun            bool
		avoidFailureHours int
	)

	fs := flag.NewFlagSet("batch-send", flag.ContinueOnError)
	config.BindLNDFlags(fs, &cfg)
	fs.StringVar(&profilePath, "profile", "", "Path to JSON profile for reproducible experiments")
	fs.StringVar(&invoicesFile, "invoices-file", "", "Path to newline-delimited BOLT11 invoices")
	fs.IntVar(&workers, "workers", 2, "Number of concurrent workers")
	fs.IntVar(&numRoutes, "num-routes", 10, "Maximum reranked routes considered")
	fs.IntVar(&pickRank, "pick-rank", 1, "1-based reranked route index to attempt")
	fs.IntVar(&avoidFailureHours, "avoid-failures-hours", 24, "Penalize only channels with failures in this recent window")
	fs.StringVar(&failureLog, "failure-log", ".lro-failures.json", "Failure history JSON path")
	fs.StringVar(&attemptLog, "attempt-log", ".lro-attempts.jsonl", "Structured attempt/output log path")
	fs.BoolVar(&dryRun, "dry-run", true, "Do not send; only score/select route per invoice")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if invoicesFile == "" {
		return errors.New("invoices-file is required")
	}
	if workers <= 0 {
		return errors.New("workers must be > 0")
	}

	profile, err := config.LoadProfile(profilePath)
	if err != nil {
		return err
	}
	applyProfileToLNDConfig(profile, &cfg)
	applyProfileToSend(profile, &numRoutes, &pickRank, &failureLog, &attemptLog, &avoidFailureHours)

	invoices, err := loadInvoicesFile(invoicesFile)
	if err != nil {
		return err
	}
	if len(invoices) == 0 {
		return errors.New("no invoices found in invoices-file")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := lnd.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	history, err := router.LoadFailureHistory(failureLog)
	if err != nil {
		return err
	}
	historyMu := &sync.Mutex{}

	jobs := make(chan string)
	var okCount int64
	var failCount int64
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for inv := range jobs {
				itemCtx, itemCancel := context.WithTimeout(context.Background(), 45*time.Second)
				_, err := executeSend(itemCtx, client, historyMu, &history, sendInput{
					invoice:           inv,
					numRoutes:         numRoutes,
					pickRank:          pickRank,
					failureLog:        failureLog,
					attemptLog:        attemptLog,
					dryRun:            dryRun,
					avoidFailureHours: avoidFailureHours,
				})
				itemCancel()
				if err != nil {
					atomic.AddInt64(&failCount, 1)
					fmt.Printf("worker=%d invoice_failed err=%v\n", workerID, err)
					continue
				}
				atomic.AddInt64(&okCount, 1)
			}
		}(i + 1)
	}

	for _, inv := range invoices {
		jobs <- inv
	}
	close(jobs)
	wg.Wait()

	historyMu.Lock()
	saveErr := router.SaveFailureHistory(failureLog, history)
	historyMu.Unlock()
	if saveErr != nil {
		return saveErr
	}

	fmt.Printf("batch completed total=%d success=%d failed=%d dry_run=%v\n", len(invoices), okCount, failCount, dryRun)
	if failCount > 0 {
		return fmt.Errorf("%d invoice(s) failed", failCount)
	}
	return nil
}

func runReport(args []string) error {
	var attemptLog string
	var asJSON bool
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.StringVar(&attemptLog, "attempt-log", ".lro-attempts.jsonl", "Structured attempt/output log path")
	fs.BoolVar(&asJSON, "json", false, "Print summary as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	summary, err := analytics.SummarizeJSONL(attemptLog)
	if err != nil {
		return err
	}
	if asJSON {
		b, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("attempt log: %s\n", attemptLog)
	fmt.Printf("total=%d success=%d failed=%d dry_runs=%d\n", summary.Total, summary.Succeeded, summary.Failed, summary.DryRuns)
	fmt.Printf("commands=%v\n", summary.ByCommand)
	fmt.Printf("top_destinations=%v\n", summary.ByDest)
	if len(summary.ErrorCounts) > 0 {
		fmt.Printf("errors=%v\n", summary.ErrorCounts)
	}
	return nil
}

type sendInput struct {
	invoice           string
	dest              string
	amtSat            int64
	numRoutes         int
	pickRank          int
	paymentHash       string
	failureLog        string
	attemptLog        string
	dryRun            bool
	avoidFailureHours int
}

type sendResult struct {
	SelectedRank int
	Score        float64
	FeeMsat      int64
	Hops         int
	DryRun       bool
	Status       string
	AttemptID    uint64
}

func executeSend(ctx context.Context, client *lnd.Client, historyMu *sync.Mutex, history *router.FailureHistory, in sendInput) (*sendResult, error) {
	dest, amtSat, paymentHash, err := populateFromInvoice(ctx, client, in.invoice, in.dest, in.amtSat, in.paymentHash)
	if err != nil {
		return nil, err
	}
	if err := config.RequireNonEmpty("dest", dest); err != nil {
		return nil, err
	}
	if err := config.RequireNonEmpty("payment-hash", paymentHash); err != nil {
		return nil, err
	}
	if amtSat <= 0 {
		return nil, errors.New("amt-sat must be > 0 (or provide an invoice with amount)")
	}
	if in.numRoutes <= 0 {
		return nil, errors.New("num-routes must be > 0")
	}
	if in.pickRank <= 0 {
		return nil, errors.New("pick-rank must be > 0")
	}

	routesResp, err := client.LN.QueryRoutes(ctx, &lnrpc.QueryRoutesRequest{PubKey: dest, Amt: amtSat, UseMissionControl: true})
	if err != nil {
		_ = analytics.AppendJSONL(in.attemptLog, analytics.AttemptEvent{Command: "send-route", Destination: dest, AmtSat: amtSat, Selected: in.pickRank, DryRun: in.dryRun, Success: false, Error: err.Error()})
		return nil, err
	}
	if len(routesResp.Routes) == 0 {
		return nil, errors.New("no route candidates returned")
	}

	info, err := client.LN.GetInfo(ctx, &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}

	historyMu.Lock()
	localHistory := *history
	historyMu.Unlock()

	weights := router.DefaultWeights()
	weights.AvoidFailuresHours = in.avoidFailureHours
	scored := limitScores(router.ScoreRoutes(ctx, info.IdentityPubkey, client.Router, routesResp.Routes, localHistory, amtSat*1000, weights), in.numRoutes)
	if in.pickRank > len(scored) {
		return nil, fmt.Errorf("pick-rank=%d out of range; only %d scored routes available", in.pickRank, len(scored))
	}

	selected := scored[in.pickRank-1]
	if in.dryRun {
		_ = analytics.AppendJSONL(in.attemptLog, analytics.AttemptEvent{Command: "send-route", Destination: dest, AmtSat: amtSat, Selected: in.pickRank, DryRun: true, Success: true, Meta: map[string]any{"invoice_mode": in.invoice != ""}})
		return &sendResult{SelectedRank: in.pickRank, Score: selected.Score, FeeMsat: selected.TotalFeesMsat, Hops: selected.HopCount, DryRun: true}, nil
	}

	hashBytes, err := hex.DecodeString(strings.TrimSpace(paymentHash))
	if err != nil {
		return nil, err
	}
	if len(hashBytes) != 32 {
		return nil, fmt.Errorf("payment hash must be 32 bytes, got %d", len(hashBytes))
	}

	resp, err := client.Router.SendToRouteV2(ctx, &routerrpc.SendToRouteRequest{PaymentHash: hashBytes, Route: selected.Route, SkipTempErr: true})
	if err != nil {
		historyMu.Lock()
		*history = router.RegisterFailure(*history, router.ExtractChannelIDs(selected.Route))
		_ = router.SaveFailureHistory(in.failureLog, *history)
		historyMu.Unlock()
		_ = analytics.AppendJSONL(in.attemptLog, analytics.AttemptEvent{Command: "send-route", Destination: dest, AmtSat: amtSat, Selected: in.pickRank, Success: false, Error: err.Error()})
		return nil, fmt.Errorf("send to route failed: %w", err)
	}

	succeeded := resp.Status == lnrpc.HTLCAttempt_SUCCEEDED
	if !succeeded {
		historyMu.Lock()
		*history = router.RegisterFailure(*history, router.ExtractChannelIDs(selected.Route))
		_ = router.SaveFailureHistory(in.failureLog, *history)
		historyMu.Unlock()
	}
	_ = analytics.AppendJSONL(in.attemptLog, analytics.AttemptEvent{Command: "send-route", Destination: dest, AmtSat: amtSat, Selected: in.pickRank, Success: succeeded, Meta: map[string]any{"htlc_status": resp.Status.String(), "attempt_id": resp.AttemptId, "invoice_mode": in.invoice != ""}})
	return &sendResult{SelectedRank: in.pickRank, Score: selected.Score, FeeMsat: selected.TotalFeesMsat, Hops: selected.HopCount, DryRun: false, Status: resp.Status.String(), AttemptID: resp.AttemptId}, nil
}

func loadInvoicesFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func populateFromInvoice(ctx context.Context, client *lnd.Client, invoice, dest string, amtSat int64, paymentHash string) (string, int64, string, error) {
	if strings.TrimSpace(invoice) == "" {
		return dest, amtSat, paymentHash, nil
	}
	res, err := client.LN.DecodePayReq(ctx, &lnrpc.PayReqString{PayReq: invoice})
	if err != nil {
		return "", 0, "", fmt.Errorf("decode invoice: %w", err)
	}
	if dest == "" {
		dest = res.Destination
	}
	if amtSat == 0 {
		amtSat = res.NumSatoshis
	}
	if paymentHash == "" {
		paymentHash = res.PaymentHash
	}
	return dest, amtSat, paymentHash, nil
}

func applyProfileToLNDConfig(p config.Profile, cfg *config.LNDConfig) {
	if p.LNDHost != "" {
		cfg.Host = p.LNDHost
	}
	if p.TLSCert != "" {
		cfg.TLSCertPath = p.TLSCert
	}
	if p.Macaroon != "" {
		cfg.MacaroonPath = p.Macaroon
	}
	if p.InsecureTLS != nil {
		cfg.InsecureTLS = *p.InsecureTLS
	}
}

func applyProfileToRoutes(p config.Profile, numRoutes *int, feeLimitSat *int64, failureLog, attemptLog *string, feeW, failW, probW *float64, finalCLTV *int, avoidFailuresHours *int) {
	if p.NumRoutes > 0 {
		*numRoutes = p.NumRoutes
	}
	if p.FeeLimitSat > 0 {
		*feeLimitSat = p.FeeLimitSat
	}
	if p.FailureLog != "" {
		*failureLog = p.FailureLog
	}
	if p.AttemptLog != "" {
		*attemptLog = p.AttemptLog
	}
	if p.FeeWeight > 0 {
		*feeW = p.FeeWeight
	}
	if p.FailWeight > 0 {
		*failW = p.FailWeight
	}
	if p.ProbWeight > 0 {
		*probW = p.ProbWeight
	}
	if p.FinalCLTV > 0 {
		*finalCLTV = int(p.FinalCLTV)
	}
	if p.AvoidFailuresHours > 0 {
		*avoidFailuresHours = p.AvoidFailuresHours
	}
}

func applyProfileToSend(p config.Profile, numRoutes, pickRank *int, failureLog, attemptLog *string, avoidFailuresHours *int) {
	if p.NumRoutes > 0 {
		*numRoutes = p.NumRoutes
	}
	if p.PickRank > 0 {
		*pickRank = p.PickRank
	}
	if p.FailureLog != "" {
		*failureLog = p.FailureLog
	}
	if p.AttemptLog != "" {
		*attemptLog = p.AttemptLog
	}
	if p.AvoidFailuresHours > 0 {
		*avoidFailuresHours = p.AvoidFailuresHours
	}
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
