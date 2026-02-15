package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type LNDConfig struct {
	Host         string
	TLSCertPath  string
	MacaroonPath string
	InsecureTLS  bool
}

func (c LNDConfig) Validate() error {
	if c.Host == "" {
		return errors.New("lnd host is required")
	}
	if c.TLSCertPath == "" {
		return errors.New("tls cert path is required")
	}
	if c.MacaroonPath == "" {
		return errors.New("macaroon path is required")
	}
	return nil
}

func BindLNDFlags(fs *flag.FlagSet, cfg *LNDConfig) {
	defaultHost := envOrDefault("LRO_LND_HOST", "localhost:10009")
	defaultTLS := envOrDefault("LRO_TLS_CERT", filepath.Clean(os.ExpandEnv("$HOME/.lnd/tls.cert")))
	defaultMac := envOrDefault("LRO_MACAROON", filepath.Clean(os.ExpandEnv("$HOME/.lnd/data/chain/bitcoin/regtest/admin.macaroon")))

	fs.StringVar(&cfg.Host, "lnd-host", defaultHost, "LND gRPC host:port")
	fs.StringVar(&cfg.TLSCertPath, "tls-cert", defaultTLS, "Path to LND tls.cert")
	fs.StringVar(&cfg.MacaroonPath, "macaroon", defaultMac, "Path to LND admin/readonly macaroon")
	fs.BoolVar(&cfg.InsecureTLS, "insecure-tls", false, "Skip TLS host verification (dev only)")
}

func UsageHeader() string {
	return `lro: Lightning Routing Optimizer (experimental)

Phases in this MVP:
  1) lnd health check
  2) fetch candidate routes from LND
  3) rerank routes with custom heuristics
  4) optionally send via selected route
`
}

func envOrDefault(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func RequireNonEmpty(name string, value any) error {
	switch v := value.(type) {
	case string:
		if v == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}
