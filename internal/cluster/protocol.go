// Package cluster implements Phase 8 multi-worker gRPC registration,
// heartbeats, placement scheduling, and gateway request proxying.
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
)

const clusterServiceName = "cellbox.cluster.v1.ClusterService"

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

type jsonCodec struct{}

func (jsonCodec) Name() string { return "json" }
func (jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
func (jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// RegisterRequest registers a worker node with the cluster gateway.
type RegisterRequest struct {
	WorkerID      string `json:"worker_id"`
	AdvertiseURL  string `json:"advertise_url"`
	GRPCAddr      string `json:"grpc_addr,omitempty"`
	TotalCPUMilli int64  `json:"total_cpu_milli"`
	TotalMemoryMB int64  `json:"total_memory_mb"`
	TotalPIDs     int64  `json:"total_pids"`
}

// RegisterResponse acknowledges worker registration.
type RegisterResponse struct {
	Accepted bool   `json:"accepted"`
	State    string `json:"state"`
}

// HeartbeatRequest reports current worker capacity and active session count.
type HeartbeatRequest struct {
	WorkerID       string `json:"worker_id"`
	AdvertiseURL   string `json:"advertise_url"`
	GRPCAddr       string `json:"grpc_addr,omitempty"`
	TotalCPUMilli  int64  `json:"total_cpu_milli"`
	TotalMemoryMB  int64  `json:"total_memory_mb"`
	TotalPIDs      int64  `json:"total_pids"`
	UsedCPUMilli   int64  `json:"used_cpu_milli"`
	UsedMemoryMB   int64  `json:"used_memory_mb"`
	UsedPIDs       int64  `json:"used_pids"`
	ActiveSessions int    `json:"active_sessions"`
}

// HeartbeatResponse returns the gateway's authoritative worker state.
type HeartbeatResponse struct {
	State string `json:"state"`
}

// DrainRequest requests draining a worker node.
type DrainRequest struct {
	WorkerID string `json:"worker_id"`
}

// DrainResponse acknowledges a drain request.
type DrainResponse struct {
	State string `json:"state"`
}

// ClusterServiceServer handles worker registration, heartbeats, and drain RPCs.
type ClusterServiceServer interface {
	RegisterWorker(context.Context, *RegisterRequest) (*RegisterResponse, error)
	Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error)
	DrainWorker(context.Context, *DrainRequest) (*DrainResponse, error)
}

// ClusterServiceClient invokes ClusterService RPCs against a gateway.
type ClusterServiceClient interface {
	RegisterWorker(context.Context, *RegisterRequest, ...grpc.CallOption) (*RegisterResponse, error)
	Heartbeat(context.Context, *HeartbeatRequest, ...grpc.CallOption) (*HeartbeatResponse, error)
	DrainWorker(context.Context, *DrainRequest, ...grpc.CallOption) (*DrainResponse, error)
}

type clusterClient struct {
	cc grpc.ClientConnInterface
}

// NewClusterClient wraps a gRPC connection as a ClusterServiceClient.
func NewClusterClient(cc grpc.ClientConnInterface) ClusterServiceClient {
	return &clusterClient{cc: cc}
}

// DialGateway opens an insecure gRPC client connection using the JSON codec.
func DialGateway(target string, opts ...grpc.DialOption) (ClusterServiceClient, *grpc.ClientConn, error) {
	baseOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")),
	}
	baseOpts = append(baseOpts, opts...)
	conn, err := grpc.NewClient(target, baseOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("dial cluster gateway %q: %w", target, err)
	}
	return NewClusterClient(conn), conn, nil
}

func (c *clusterClient) RegisterWorker(ctx context.Context, req *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error) {
	out := new(RegisterResponse)
	if err := c.cc.Invoke(ctx, "/"+clusterServiceName+"/RegisterWorker", req, out, append(opts, grpc.CallContentSubtype("json"))...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *clusterClient) Heartbeat(ctx context.Context, req *HeartbeatRequest, opts ...grpc.CallOption) (*HeartbeatResponse, error) {
	out := new(HeartbeatResponse)
	if err := c.cc.Invoke(ctx, "/"+clusterServiceName+"/Heartbeat", req, out, append(opts, grpc.CallContentSubtype("json"))...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *clusterClient) DrainWorker(ctx context.Context, req *DrainRequest, opts ...grpc.CallOption) (*DrainResponse, error) {
	out := new(DrainResponse)
	if err := c.cc.Invoke(ctx, "/"+clusterServiceName+"/DrainWorker", req, out, append(opts, grpc.CallContentSubtype("json"))...); err != nil {
		return nil, err
	}
	return out, nil
}

// RegisterClusterService registers svc on a gRPC server.
func RegisterClusterService(s grpc.ServiceRegistrar, svc ClusterServiceServer) {
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: clusterServiceName,
		HandlerType: (*ClusterServiceServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "RegisterWorker",
				Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
					in := new(RegisterRequest)
					if err := dec(in); err != nil {
						return nil, err
					}
					if interceptor == nil {
						return srv.(ClusterServiceServer).RegisterWorker(ctx, in)
					}
					info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + clusterServiceName + "/RegisterWorker"}
					return interceptor(ctx, in, info, func(ctx context.Context, req any) (any, error) {
						return srv.(ClusterServiceServer).RegisterWorker(ctx, req.(*RegisterRequest))
					})
				},
			},
			{
				MethodName: "Heartbeat",
				Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
					in := new(HeartbeatRequest)
					if err := dec(in); err != nil {
						return nil, err
					}
					if interceptor == nil {
						return srv.(ClusterServiceServer).Heartbeat(ctx, in)
					}
					info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + clusterServiceName + "/Heartbeat"}
					return interceptor(ctx, in, info, func(ctx context.Context, req any) (any, error) {
						return srv.(ClusterServiceServer).Heartbeat(ctx, req.(*HeartbeatRequest))
					})
				},
			},
			{
				MethodName: "DrainWorker",
				Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
					in := new(DrainRequest)
					if err := dec(in); err != nil {
						return nil, err
					}
					if interceptor == nil {
						return srv.(ClusterServiceServer).DrainWorker(ctx, in)
					}
					info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + clusterServiceName + "/DrainWorker"}
					return interceptor(ctx, in, info, func(ctx context.Context, req any) (any, error) {
						return srv.(ClusterServiceServer).DrainWorker(ctx, req.(*DrainRequest))
					})
				},
			},
		},
	}, svc)
}

// NewGRPCServer builds a gRPC server configured with the JSON codec and ClusterService.
func NewGRPCServer(svc ClusterServiceServer, opts ...grpc.ServerOption) *grpc.Server {
	baseOpts := []grpc.ServerOption{grpc.ForceServerCodec(jsonCodec{})}
	baseOpts = append(baseOpts, opts...)
	srv := grpc.NewServer(baseOpts...)
	RegisterClusterService(srv, svc)
	return srv
}

// RunHeartbeatLoop periodically sends worker heartbeats until ctx is canceled.
func RunHeartbeatLoop(ctx context.Context, client ClusterServiceClient, interval time.Duration, build func() HeartbeatRequest) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			req := build()
			hbCtx, cancel := context.WithTimeout(ctx, interval)
			_, _ = client.Heartbeat(hbCtx, &req)
			cancel()
		}
	}
}
