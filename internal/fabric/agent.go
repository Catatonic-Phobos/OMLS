package fabric

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/discover"
	"github.com/Catatonic-Phobos/OMLS/internal/envelope"
	"github.com/Catatonic-Phobos/OMLS/internal/gpu"
	"github.com/Catatonic-Phobos/OMLS/internal/rdl"
	"github.com/Catatonic-Phobos/OMLS/internal/telemetry"
	"github.com/Catatonic-Phobos/OMLS/internal/work"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc"
)

// AgentConfig drives Register → Advertise → Heartbeat (+ work claim) loops.
type AgentConfig struct {
	MasterAddr   string
	Client       omlsv1.FabricClient
	Conn         *grpc.ClientConn // closed by Run when set
	Interval     time.Duration
	WorkPoll     time.Duration
	SysRoot      string
	AgentVersion string
	NodeID       string // overrides the discovered id when a cluster identity is persisted
	Once         bool   // register+advertise+one heartbeat then exit
}

// Run discovers the local node, registers with the master, and heartbeats.
// While running it also claims and executes demo work units.
func Run(ctx context.Context, cfg AgentConfig) error {
	if cfg.Client == nil {
		return fmt.Errorf("fabric client is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.WorkPoll <= 0 {
		cfg.WorkPoll = 200 * time.Millisecond
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
	applyNodeID(&doc, cfg.NodeID)
	nodeID := doc.Node.ID

	reg, err := cfg.Client.Register(ctx, &omlsv1.RegisterRequest{
		NodeId:       nodeID,
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

	if err := advertise(ctx, cfg.Client, nodeID, session, &doc); err != nil {
		return err
	}

	sendHeartbeat := func() error {
		tel := telemetry.Sample(cfg.SysRoot)
		hb, err := cfg.Client.Heartbeat(ctx, &omlsv1.HeartbeatRequest{
			NodeId:    nodeID,
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
			applyNodeID(&doc, cfg.NodeID)
			nodeID = doc.Node.ID
			if err := advertise(ctx, cfg.Client, nodeID, session, &doc); err != nil {
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

	hbTicker := time.NewTicker(cfg.Interval)
	defer hbTicker.Stop()
	workTicker := time.NewTicker(cfg.WorkPoll)
	defer workTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-hbTicker.C:
			if err := sendHeartbeat(); err != nil {
				return err
			}
		case <-workTicker.C:
			if err := claimAndRun(ctx, cfg, nodeID, session); err != nil {
				return err
			}
		}
	}
}

func claimAndRun(ctx context.Context, cfg AgentConfig, nodeID, session string) error {
	for {
		claim, err := cfg.Client.ClaimWork(ctx, &omlsv1.ClaimWorkRequest{
			NodeId:    nodeID,
			SessionId: session,
		})
		if err != nil {
			return fmt.Errorf("claim work: %w", err)
		}
		if !claim.GetHasWork() || claim.GetUnit() == nil {
			return nil
		}
		unit := claim.GetUnit()
		before := telemetry.Sample(cfg.SysRoot)
		var dur time.Duration
		var burnErr error
		if unit.GetFunction() == "gpu_serial" {
			deviceIndex := int(unit.GetWorkerIndex())
			devices, devErr := gpu.Devices()
			if devErr != nil {
				burnErr = devErr
			} else if deviceIndex < 0 || deviceIndex >= len(devices) {
				burnErr = fmt.Errorf("OpenCL GPU index %d unavailable (found %d)", deviceIndex, len(devices))
			} else {
				device := devices[deviceIndex]
				dur, burnErr = gpu.Burn(deviceIndex, unit.GetIterations())
				if burnErr == nil {
					fmt.Fprintf(os.Stderr, "omls agent: GPU work %s complete device=%q operations=%d duration=%s\n",
						unit.GetWorkId(), device.Name, unit.GetIterations(), dur.Round(time.Millisecond))
				}
			}
		} else {
			pacer, stopEnv, _ := startEnvelope(ctx, unit.GetEnvelopeYaml(), cfg.SysRoot)
			dur, burnErr = work.Burn(ctx, unit.GetIterations(), pacer)
			if stopEnv != nil {
				stopEnv()
			}
		}
		after := telemetry.Sample(cfg.SysRoot)
		tempKnown := before.TemperatureKnown && after.TemperatureKnown
		errText := ""
		if burnErr != nil && ctx.Err() == nil {
			errText = burnErr.Error()
		}
		_, err = cfg.Client.ReportWork(ctx, &omlsv1.ReportWorkRequest{
			NodeId:           nodeID,
			SessionId:        session,
			WorkId:           unit.GetWorkId(),
			DurationMs:       dur.Milliseconds(),
			TempBeforeC:      before.TemperatureC,
			TempAfterC:       after.TemperatureC,
			TemperatureKnown: tempKnown,
			Error:            errText,
		})
		if err != nil {
			return fmt.Errorf("report work: %w", err)
		}
		// Keep draining the queue while the demo is active.
	}
}

func startEnvelope(ctx context.Context, raw []byte, sysRoot string) (work.Pacer, func(), []string) {
	var env envelope.Envelope
	var err error
	if len(raw) == 0 {
		env = envelope.Default()
	} else {
		env, err = envelope.ParseYAML(raw)
		if err != nil {
			env = envelope.Default()
		}
	}
	backends, warns := envelope.OpenBackends()
	if err != nil {
		warns = append(warns, "envelope yaml: "+err.Error()+"; using balanced")
	}
	ctrl := &envelope.Controller{
		Env:      env,
		Backends: backends,
		TempFn: func() (float64, bool) {
			tel := telemetry.Sample(sysRoot)
			return tel.TemperatureC, tel.TemperatureKnown
		},
	}
	if _, err := ctrl.Start(ctx); err != nil {
		warns = append(warns, "envelope start: "+err.Error())
		return nil, nil, warns
	}
	var pacer work.Pacer
	for _, b := range backends {
		if d, ok := b.(*envelope.DutyCycle); ok {
			pacer = d
			break
		}
	}
	stop := func() { _, _ = ctrl.Stop() }
	return pacer, stop, warns
}

func applyNodeID(doc *rdl.Document, id string) {
	if doc == nil || id == "" {
		return
	}
	doc.Node.ID = id
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
