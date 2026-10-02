// Package fabric implements the OMLS 0.2 gRPC control plane.
package fabric

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/behavior"
	"github.com/Catatonic-Phobos/OMLS/internal/community"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/power"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

const Version = "0.9.0"

// Server hosts the Fabric API against an in-memory Resource Graph.
type Server struct {
	omlsv1.UnimplementedFabricServer
	reg            *graph.Registry
	store          *behavior.Store
	community      *community.Catalog
	power          *power.Simulator
	heartbeatEvery time.Duration
	version        string
	demo           *demoState
	localHostname  string
}

// NewServer wraps a registry. dataDir may be empty (defaults to omls-data).
func NewServer(reg *graph.Registry, heartbeatEvery time.Duration) *Server {
	return NewServerWithStore(reg, heartbeatEvery, nil)
}

// NewServerWithStore is like NewServer but uses an existing Behavior Profile store.
func NewServerWithStore(reg *graph.Registry, heartbeatEvery time.Duration, store *behavior.Store) *Server {
	return NewServerFull(reg, heartbeatEvery, store, nil)
}

// NewServerFull wires Behavior store and optional community catalog.
func NewServerFull(reg *graph.Registry, heartbeatEvery time.Duration, store *behavior.Store, cat *community.Catalog) *Server {
	if heartbeatEvery <= 0 {
		heartbeatEvery = 5 * time.Second
	}
	if store == nil {
		store, _ = behavior.Open("omls-data")
	}
	host, _ := os.Hostname()
	return &Server{
		reg:            reg,
		store:          store,
		community:      cat,
		power:          power.NewSimulator(),
		heartbeatEvery: heartbeatEvery,
		version:        Version,
		demo:           newDemoState(),
		localHostname:  host,
	}
}

// Store exposes the Behavior Profile store (tests / CLI local dumps).
func (s *Server) Store() *behavior.Store { return s.store }

// Community exposes the community catalog (may be nil).
func (s *Server) Community() *community.Catalog { return s.community }

// Power exposes the simulated power fabric MCU (0.9).
func (s *Server) Power() *power.Simulator { return s.power }

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
	if tel != nil && s.store != nil {
		host, virt := s.nodeMeta(req.GetNodeId())
		_ = s.store.RecordTelemetry(req.GetNodeId(), host, virt, behavior.TelemetrySample{
			ObservedAt:        tel.ObservedAt,
			CPULoad:           tel.CPULoad,
			MemAvailableBytes: tel.MemAvailableBytes,
			MemTotalBytes:     tel.MemTotalBytes,
			TemperatureC:      tel.TemperatureC,
			TemperatureKnown:  tel.TemperatureKnown,
		})
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
	s.maybeApplyCommunityPrior(req.GetNodeId(), doc)
	return &omlsv1.AdvertiseProfileResponse{Ok: true, ResourceCount: int32(len(doc.Resources))}, nil
}

func (s *Server) maybeApplyCommunityPrior(nodeID string, doc *rdl.Document) {
	if s.community == nil || s.store == nil || doc == nil {
		return
	}
	facts := community.FactsFromRDL(doc)
	best, score, ok := s.community.BestMatch(facts)
	if !ok || score <= 0 || best.Priors.Samples <= 0 {
		return
	}
	host, virt := doc.Node.Hostname, doc.Node.Virt
	_, _ = s.store.ApplyCommunityPrior(nodeID, host, virt, best.ID,
		best.Priors.DurationEWMAMs, best.Priors.TempDeltaEWMA, best.Priors.Samples)
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
		if s.store != nil {
			host, virt := s.nodeMeta(sample.GetNodeId())
			_ = s.store.RecordTelemetry(sample.GetNodeId(), host, virt, behavior.TelemetrySample{
				ObservedAt:        tel.ObservedAt,
				CPULoad:           tel.CPULoad,
				MemAvailableBytes: tel.MemAvailableBytes,
				MemTotalBytes:     tel.MemTotalBytes,
				TemperatureC:      tel.TemperatureC,
				TemperatureKnown:  tel.TemperatureKnown,
			})
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

func (s *Server) ListProfiles(ctx context.Context, _ *omlsv1.ListProfilesRequest) (*omlsv1.ListProfilesResponse, error) {
	if s.store == nil {
		return &omlsv1.ListProfilesResponse{}, nil
	}
	list, err := s.store.List()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list profiles: %v", err)
	}
	out := &omlsv1.ListProfilesResponse{DataDir: s.store.Dir()}
	for _, p := range list {
		sum := &omlsv1.ProfileSummary{
			NodeId:    p.NodeID,
			Hostname:  p.Hostname,
			Virt:      p.Virt,
			UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
		}
		for _, r := range p.Resources {
			if r.Class == "compute" || r.ID == "cpu0" {
				sum.Samples = int32(r.Observed.Samples)
				sum.DurationEwmaMs = r.Observed.DurationEWMAMs
				sum.Confidence = r.Confidence
				break
			}
		}
		out.Profiles = append(out.Profiles, sum)
	}
	return out, nil
}

func (s *Server) GetProfile(ctx context.Context, req *omlsv1.GetProfileRequest) (*omlsv1.GetProfileResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if s.store == nil {
		return nil, status.Error(codes.FailedPrecondition, "behavior store unavailable")
	}
	p, err := s.store.Load(req.GetNodeId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load profile: %v", err)
	}
	if p.UpdatedAt.IsZero() && len(p.Resources) == 0 && len(p.RecentTelemetry) == 0 {
		return nil, status.Errorf(codes.NotFound, "no behavior profile for %s", req.GetNodeId())
	}
	raw, err := yaml.Marshal(p)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal profile: %v", err)
	}
	tailN := int(req.GetTelemetryTail())
	samples, err := s.store.ReadTelemetryTail(req.GetNodeId(), tailN)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "telemetry: %v", err)
	}
	var jsonl []byte
	for _, sample := range samples {
		line, err := json.Marshal(sample)
		if err != nil {
			continue
		}
		jsonl = append(jsonl, line...)
		jsonl = append(jsonl, '\n')
	}
	return &omlsv1.GetProfileResponse{ProfileYaml: raw, TelemetryJsonl: jsonl}, nil
}

func (s *Server) nodeMeta(nodeID string) (hostname, virt string) {
	snap := s.reg.Snapshot()
	for _, n := range snap.Nodes {
		if n.ID == nodeID {
			return n.Hostname, n.Virt
		}
	}
	return "", ""
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
	Listen       string
	TLS          credentials.TransportCredentials // nil => insecure (dev-only)
	Registry     *graph.Registry
	Interval     time.Duration
	DataDir      string // Behavior Profiles + telemetry (default omls-data)
	Store        *behavior.Store
	CommunityDir string // optional community profile catalog (0.7)
	Community    *community.Catalog
}

// ListenAndServe starts the fabric master until ctx is cancelled.
func ListenAndServe(ctx context.Context, opt ServeOptions) error {
	if opt.Listen == "" {
		opt.Listen = ":7443"
	}
	if opt.Registry == nil {
		return fmt.Errorf("registry is required")
	}
	store := opt.Store
	if store == nil {
		var err error
		store, err = behavior.Open(opt.DataDir)
		if err != nil {
			return fmt.Errorf("behavior store: %w", err)
		}
	}
	cat := opt.Community
	if cat == nil && opt.CommunityDir != "" {
		var err error
		cat, err = community.Open(opt.CommunityDir)
		if err != nil {
			return fmt.Errorf("community catalog: %w", err)
		}
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
	omlsv1.RegisterFabricServer(gs, NewServerFull(opt.Registry, opt.Interval, store, cat))

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
