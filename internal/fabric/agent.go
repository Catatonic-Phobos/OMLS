package fabric

import (
	"context"
	"fmt"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/discover"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"github.com/Catatonic-Phobos/OMLS/internal/telemetry"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc"
)

// AgentConfig drives Register → Advertise → Heartbeat loops.
type AgentConfig struct {
	MasterAddr   string
	Client       omlsv1.FabricClient
	Conn         *grpc.ClientConn // closed by Run when set
	Interval     time.Duration
	SysRoot      string
	AgentVersion string
	Once         bool // register+advertise+one heartbeat then exit
}

// Run discovers the local node, registers with the master, and heartbeats.
func Run(ctx context.Context, cfg AgentConfig) error {
	if cfg.Client == nil {
		return fmt.Errorf("fabric client is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.AgentVersion == "" {
		cfg.AgentVersion = Version
	}
	if cfg.Conn != nil {
		defer cfg.Conn.Close()
	}

	res, err := discover.Discover(discover.Options{SysRoot: cfg.SysRoot})
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	doc := res.Document

	reg, err := cfg.Client.Register(ctx, &omlsv1.RegisterRequest{
		NodeId:       doc.Node.ID,
		Hostname:     doc.Node.Hostname,
		Virt:         doc.Node.Virt,
		AgentVersion: cfg.AgentVersion,
	})
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	session := reg.GetSessionId()
	if ms := reg.GetHeartbeatIntervalMs(); ms > 0 {
		cfg.Interval = time.Duration(ms) * time.Millisecond
	}

	if err := advertise(ctx, cfg.Client, doc.Node.ID, session, &doc); err != nil {
		return err
	}

	sendHeartbeat := func() error {
		tel := telemetry.Sample(cfg.SysRoot)
		hb, err := cfg.Client.Heartbeat(ctx, &omlsv1.HeartbeatRequest{
			NodeId:    doc.Node.ID,
			SessionId: session,
			Telemetry: &omlsv1.TelemetrySnapshot{
				CpuLoad:           tel.CPULoad,
				MemAvailableBytes: tel.MemAvailableBytes,
				MemTotalBytes:     tel.MemTotalBytes,
				TemperatureC:      tel.TemperatureC,
				TemperatureKnown:  tel.TemperatureKnown,
				ObservedUnixMs:    tel.ObservedAt.UnixMilli(),
			},
		})
		if err != nil {
			return fmt.Errorf("heartbeat: %w", err)
		}
		if hb.GetReAdvertise() {
			fresh, derr := discover.Discover(discover.Options{SysRoot: cfg.SysRoot})
			if derr != nil {
				return fmt.Errorf("re-discover: %w", derr)
			}
			doc = fresh.Document
			if err := advertise(ctx, cfg.Client, doc.Node.ID, session, &doc); err != nil {
				return err
			}
		}
		return nil
	}

	if err := sendHeartbeat(); err != nil {
		return err
	}
	if cfg.Once {
		return nil
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := sendHeartbeat(); err != nil {
				return err
			}
		}
	}
}

func advertise(ctx context.Context, client omlsv1.FabricClient, nodeID, session string, doc *rdl.Document) error {
	raw, err := doc.MarshalYAML()
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	resp, err := client.AdvertiseProfile(ctx, &omlsv1.AdvertiseProfileRequest{
		NodeId:    nodeID,
		SessionId: session,
		Format:    "yaml",
		Profile:   raw,
	})
	if err != nil {
		return fmt.Errorf("advertise: %w", err)
	}
	if !resp.GetOk() {
		return fmt.Errorf("advertise rejected")
	}
	return nil
}
