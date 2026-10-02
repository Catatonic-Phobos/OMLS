// Package fabric implements the OMLS 0.2 gRPC control plane.
package fabric

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const Version = "0.3.0"

// Server hosts the Fabric API against an in-memory Resource Graph.
type Server struct {
	omlsv1.UnimplementedFabricServer
	reg            *graph.Registry
	heartbeatEvery time.Duration
	version        string
	demo           *demoState
	localHostname  string
}

// NewServer wraps a registry.
func NewServer(reg *graph.Registry, heartbeatEvery time.Duration) *Server {
	if heartbeatEvery <= 0 {
		heartbeatEvery = 5 * time.Second
	}
	host, _ := os.Hostname()
	return &Server{
		reg:            reg,
		heartbeatEvery: heartbeatEvery,
		version:        Version,
		demo:           newDemoState(),
		localHostname:  host,
	}
}

// Registry exposes the backing graph (for tests / local dumps).
func (s *Server) Registry() *graph.Registry { return s.reg }

func (s *Server) Register(ctx context.Context, req *omlsv1.RegisterRequest) (*omlsv1.RegisterResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	session := newSessionID()
	s.reg.Register(req.GetNodeId(), req.GetHostname(), req.GetVirt(), req.GetAgentVersion(), session)
	return &omlsv1.RegisterResponse{
		SessionId:           session,
		HeartbeatIntervalMs: s.heartbeatEvery.Milliseconds(),
	}, nil
}

func (s *Server) Heartbeat(ctx context.Context, req *omlsv1.HeartbeatRequest) (*omlsv1.HeartbeatResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	var tel *graph.Telemetry
	if t := req.GetTelemetry(); t != nil {
		tel = protoTelemetry(t)
	}
	ok, reAdv := s.reg.Heartbeat(req.GetNodeId(), req.GetSessionId(), tel)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "node not registered or session mismatch")
	}
	return &omlsv1.HeartbeatResponse{Ok: true, ReAdvertise: reAdv}, nil
}

func (s *Server) AdvertiseProfile(ctx context.Context, req *omlsv1.AdvertiseProfileRequest) (*omlsv1.AdvertiseProfileResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	doc, err := parseProfile(req.GetFormat(), req.GetProfile())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "profile: %v", err)
	}
	if err := s.reg.Advertise(req.GetNodeId(), req.GetSessionId(), doc); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}
	return &omlsv1.AdvertiseProfileResponse{Ok: true, ResourceCount: int32(len(doc.Resources))}, nil
}

func (s *Server) StreamTelemetry(stream omlsv1.Fabric_StreamTelemetryServer) error {
	accepted := int32(0)
	for {
		sample, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&omlsv1.TelemetryAck{Accepted: accepted})
		}
		if err != nil {
			return err
		}
		if sample.GetNodeId() == "" || sample.GetSnapshot() == nil {
			continue
		}
		tel := *protoTelemetry(sample.GetSnapshot())
		if err := s.reg.RecordTelemetry(sample.GetNodeId(), sample.GetSessionId(), tel); err != nil {
			return status.Errorf(codes.FailedPrecondition, "%v", err)
		}
		accepted++
	}
}

func (s *Server) Health(ctx context.Context, _ *omlsv1.HealthRequest) (*omlsv1.HealthResponse, error) {
	total, avail := s.reg.Counts()
	return &omlsv1.HealthResponse{
		Status:         "ok",
		Version:        s.version,
		NodeCount:      int32(total),
		AvailableNodes: int32(avail),
	}, nil
}

func (s *Server) GetGraph(ctx context.Context, _ *omlsv1.GetGraphRequest) (*omlsv1.GetGraphResponse, error) {
	snap := s.reg.Snapshot()
	raw, err := snap.MarshalYAML()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal graph: %v", err)
	}
	return &omlsv1.GetGraphResponse{GraphYaml: raw}, nil
}

func parseProfile(format string, raw []byte) (*rdl.Document, error) {
	switch format {
	case "", "yaml", "yml":
		return rdl.ParseYAML(raw)
	case "json":
		return rdl.ParseJSON(raw)
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func protoTelemetry(t *omlsv1.TelemetrySnapshot) *graph.Telemetry {
	obs := time.Now().UTC()
	if t.GetObservedUnixMs() > 0 {
		obs = time.UnixMilli(t.GetObservedUnixMs()).UTC()
	}
	return &graph.Telemetry{
		CPULoad:           t.GetCpuLoad(),
		MemAvailableBytes: t.GetMemAvailableBytes(),
		MemTotalBytes:     t.GetMemTotalBytes(),
		TemperatureC:      t.GetTemperatureC(),
		TemperatureKnown:  t.GetTemperatureKnown(),
		ObservedAt:        obs,
	}
}

func newSessionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ServeOptions configures the gRPC listener.
type ServeOptions struct {
	Listen   string
	TLS      credentials.TransportCredentials // nil => insecure (dev-only)
	Registry *graph.Registry
	Interval time.Duration
}

// ListenAndServe starts the fabric master until ctx is cancelled.
func ListenAndServe(ctx context.Context, opt ServeOptions) error {
	if opt.Listen == "" {
		opt.Listen = ":7443"
	}
	if opt.Registry == nil {
		return fmt.Errorf("registry is required")
	}
	lis, err := net.Listen("tcp", opt.Listen)
	if err != nil {
		return err
	}
	defer lis.Close()

	var serverOpts []grpc.ServerOption
	if opt.TLS != nil {
		serverOpts = append(serverOpts, grpc.Creds(opt.TLS))
	}
	gs := grpc.NewServer(serverOpts...)
	omlsv1.RegisterFabricServer(gs, NewServer(opt.Registry, opt.Interval))

	errCh := make(chan error, 1)
	go func() {
		errCh <- gs.Serve(lis)
	}()

	sweep := time.NewTicker(time.Second)
	defer sweep.Stop()

	for {
		select {
		case <-ctx.Done():
			gs.GracefulStop()
			<-errCh
			return nil
		case err := <-errCh:
			return err
		case <-sweep.C:
			opt.Registry.Sweep()
		}
	}
}

// DialOptions configures an agent/operator client.
type DialOptions struct {
	Addr    string
	TLS     credentials.TransportCredentials // nil => insecure
	Timeout time.Duration
}

// Dial connects to a fabric master.
func Dial(ctx context.Context, opt DialOptions) (*grpc.ClientConn, omlsv1.FabricClient, error) {
	if opt.Addr == "" {
		return nil, nil, fmt.Errorf("master address is required")
	}
	creds := opt.TLS
	if creds == nil {
		creds = insecure.NewCredentials()
	}
	dctx := ctx
	var cancel context.CancelFunc
	if opt.Timeout > 0 {
		dctx, cancel = context.WithTimeout(ctx, opt.Timeout)
		defer cancel()
	}
	conn, err := grpc.DialContext(dctx, opt.Addr, //nolint:staticcheck // DialContext still common on go1.22 grpc
		grpc.WithTransportCredentials(creds),
		grpc.WithBlock(), //nolint:staticcheck
	)
	if err != nil {
		return nil, nil, err
	}
	return conn, omlsv1.NewFabricClient(conn), nil
}
