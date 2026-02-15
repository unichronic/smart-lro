package lnd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/example/lro/internal/config"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type Client struct {
	Conn   *grpc.ClientConn
	LN     lnrpc.LightningClient
	Router routerrpc.RouterClient
}

func New(ctx context.Context, cfg config.LNDConfig) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	certBytes, err := os.ReadFile(cfg.TLSCertPath)
	if err != nil {
		return nil, fmt.Errorf("read tls cert: %w", err)
	}

	macBytes, err := os.ReadFile(cfg.MacaroonPath)
	if err != nil {
		return nil, fmt.Errorf("read macaroon: %w", err)
	}
	macHex := hex.EncodeToString(macBytes)

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certBytes) {
		return nil, fmt.Errorf("unable to append cert from %s", cfg.TLSCertPath)
	}

	tlsCfg := &tls.Config{RootCAs: pool}
	if cfg.InsecureTLS {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec
	}

	creds := credentials.NewTLS(tlsCfg)
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(macaroonUnaryInterceptor(macHex)),
		grpc.WithStreamInterceptor(macaroonStreamInterceptor(macHex)),
	}

	conn, err := grpc.DialContext(ctx, cfg.Host, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial lnd: %w", err)
	}

	return &Client{
		Conn:   conn,
		LN:     lnrpc.NewLightningClient(conn),
		Router: routerrpc.NewRouterClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.Conn == nil {
		return nil
	}
	return c.Conn.Close()
}

func macaroonUnaryInterceptor(macaroon string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "macaroon", macaroon)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func macaroonStreamInterceptor(macaroon string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "macaroon", macaroon)
		return streamer(ctx, desc, cc, method, opts...)
	}
}
