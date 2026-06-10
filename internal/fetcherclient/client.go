package fetcherclient

import (
	"context"
	"crypto/tls"
	"time"

	v1 "github.com/wasss4bi/values-from-body-fetcher/api/fetcher/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type Client struct {
	connection *grpc.ClientConn
	rpc        v1.FetcherServiceClient
	health     grpc_health_v1.HealthClient
}

func New(address string, tlsConfig *tls.Config) (*Client, error) {
	// Configuration comes from this application; waiting for optional DNS TXT
	// service configs can consume the entire RPC budget in Docker networks.
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithDisableServiceConfig(), grpc.WithDisableRetry(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(2<<20), grpc.MaxCallSendMsgSize(2<<20)))
	if err != nil {
		return nil, err
	}
	return &Client{connection: conn, rpc: v1.NewFetcherServiceClient(conn), health: grpc_health_v1.NewHealthClient(conn)}, nil
}
func (c *Client) Close() error { return c.connection.Close() }
func (c *Client) Fetch(ctx context.Context, input *v1.FetchRequest) (*v1.FetchResponse, error) {
	return c.rpc.FetchAndExtract(ctx, input)
}
func (c *Client) Prepare(ctx context.Context, input *v1.PrepareSessionRequest) (*v1.PrepareSessionResponse, error) {
	return c.rpc.PrepareSession(ctx, input)
}
func (c *Client) Healthy(ctx context.Context, capability string) bool {
	child, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	response, err := c.health.Check(child, &grpc_health_v1.HealthCheckRequest{Service: capability})
	return err == nil && response.Status == grpc_health_v1.HealthCheckResponse_SERVING
}
