package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
)

// Status is the local daemon summary used by `omls status`.
type Status struct {
	Running       bool   `json:"running"`
	Cluster       string `json:"cluster"`
	Nodes         int    `json:"nodes"`
	Coordinator   string `json:"coordinator"`
	CoordinatorID string `json:"coordinator_id"`
	Role          string `json:"role"`
	NodeID        string `json:"node_id"`
	Hostname      string `json:"hostname"`
}

// NodeInfo is one row of `omls nodes`.
type NodeInfo struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Env    string `json:"env"`
	Status string `json:"status"`
	Addr   string `json:"addr,omitempty"`
}

// ClusterInfo is the `omls cluster` payload.
type ClusterInfo struct {
	Status          Status     `json:"status"`
	ProtocolVersion string     `json:"protocol_version"`
	Listen          string     `json:"listen"`
	Members         []NodeInfo `json:"members"`
}

// DemoRequest is forwarded to the coordinator's existing RunDemo RPC.
type DemoRequest struct {
	Workers        int32  `json:"workers"`
	Iterations     int32  `json:"iterations"`
	WorkIterations int64  `json:"work_iterations"`
	Policy         string `json:"policy"`
	TimeoutMs      int64  `json:"timeout_ms"`
	EnvelopeYAML   []byte `json:"envelope_yaml,omitempty"`
}

// DemoAllocation is one scheduler placement inside a demo round.
type DemoAllocation struct {
	NodeID   string  `json:"node_id"`
	Hostname string  `json:"hostname"`
	Workers  int32   `json:"workers"`
	Score    float64 `json:"score"`
	Virt     string  `json:"virt"`
	Reason   string  `json:"reason"`
}

// DemoRound is one scheduler pass.
type DemoRound struct {
	Round          int32              `json:"round"`
	WallMs         int64              `json:"wall_ms"`
	PolicyNote     string             `json:"policy_note"`
	Allocations    []DemoAllocation   `json:"allocations"`
	DurationEwmaMs map[string]float64 `json:"duration_ewma_ms,omitempty"`
}

// DemoResult is the coordinator's run-demo response.
type DemoResult struct {
	Rounds  []DemoRound `json:"rounds"`
	Summary string      `json:"summary"`
}

func (d *Daemon) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", d.handleStatus)
	mux.HandleFunc("/v1/nodes", d.handleNodes)
	mux.HandleFunc("/v1/cluster", d.handleCluster)
	mux.HandleFunc("/v1/graph", d.handleGraph)
	mux.HandleFunc("/v1/run-demo", d.handleDemo)
	return mux
}

func (d *Daemon) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.viewStatus())
}

func (d *Daemon) handleNodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.viewNodes())
}

func (d *Daemon) handleCluster(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.viewCluster())
}

func (d *Daemon) handleGraph(w http.ResponseWriter, r *http.Request) {
	raw, err := d.graphYAML(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (d *Daemon) handleDemo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req DemoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res, err := d.runDemo(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (d *Daemon) viewStatus() Status {
	members := d.members.Snapshot()
	coord, _ := d.members.Coordinator()
	cluster := "forming"
	if coord.NodeID != "" {
		cluster = "active"
	}
	d.mu.Lock()
	role := d.roleName
	d.mu.Unlock()
	if role == "" {
		role = "electing"
	}
	return Status{
		Running:       true,
		Cluster:       cluster,
		Nodes:         len(members),
		Coordinator:   coord.Hostname,
		CoordinatorID: coord.NodeID,
		Role:          role,
		NodeID:        d.self.NodeID,
		Hostname:      d.self.Hostname,
	}
}

func (d *Daemon) viewNodes() []NodeInfo {
	snap := d.members.Snapshot()
	out := make([]NodeInfo, 0, len(snap))
	for _, m := range snap {
		out = append(out, NodeInfo{
			NodeID: m.NodeID,
			Name:   m.Hostname,
			OS:     m.OS,
			Env:    EnvLabel(m.Virt),
			Status: m.Status,
			Addr:   m.Addr,
		})
	}
	return out
}

func (d *Daemon) viewCluster() ClusterInfo {
	return ClusterInfo{
		Status:          d.viewStatus(),
		ProtocolVersion: ProtocolVersion,
		Listen:          d.cfg.Listen,
		Members:         d.viewNodes(),
	}
}

func (d *Daemon) graphYAML(ctx context.Context) ([]byte, error) {
	d.mu.Lock()
	reg := d.reg
	role := d.roleName
	dial := d.coordDial
	d.mu.Unlock()
	if role == "coordinator" && reg != nil {
		return reg.Snapshot().MarshalYAML()
	}
	if dial == "" {
		return nil, fmt.Errorf("coordinator is not ready")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{Addr: dial, Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	resp, err := client.GetGraph(ctx, &omlsv1.GetGraphRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetGraphYaml(), nil
}

func (d *Daemon) runDemo(ctx context.Context, req DemoRequest) (DemoResult, error) {
	d.mu.Lock()
	dial := d.coordDial
	d.mu.Unlock()
	if dial == "" {
		return DemoResult{}, fmt.Errorf("coordinator is not ready")
	}
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = 180_000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
	defer cancel()
	conn, client, err := fabric.Dial(ctx, fabric.DialOptions{Addr: dial, Timeout: 5 * time.Second})
	if err != nil {
		return DemoResult{}, err
	}
	defer conn.Close()
	resp, err := client.RunDemo(ctx, &omlsv1.RunDemoRequest{
		Workers:        req.Workers,
		Iterations:     req.Iterations,
		WorkIterations: req.WorkIterations,
		TimeoutMs:      req.TimeoutMs,
		EnvelopeYaml:   req.EnvelopeYAML,
		Policy:         req.Policy,
	})
	if err != nil {
		return DemoResult{}, err
	}
	out := DemoResult{Summary: resp.GetSummary()}
	for _, round := range resp.GetRounds() {
		dr := DemoRound{
			Round:          round.GetRound(),
			WallMs:         round.GetWallMs(),
			PolicyNote:     round.GetPolicyNote(),
			DurationEwmaMs: round.GetDurationEwmaMs(),
		}
		for _, a := range round.GetAllocations() {
			dr.Allocations = append(dr.Allocations, DemoAllocation{
				NodeID:   a.GetNodeId(),
				Hostname: a.GetHostname(),
				Workers:  a.GetWorkers(),
				Score:    a.GetScore(),
				Virt:     a.GetVirt(),
				Reason:   a.GetReason(),
			})
		}
		out.Rounds = append(out.Rounds, dr)
	}
	return out, nil
}

func (d *Daemon) serveAPI(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(d.cfg.SocketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(d.cfg.SocketPath)
	ln, err := net.Listen("unix", d.cfg.SocketPath)
	if err != nil {
		return err
	}
	_ = os.Chmod(d.cfg.SocketPath, 0o600)
	srv := &http.Server{Handler: d.handler()}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		_ = os.Remove(d.cfg.SocketPath)
		return nil
	case err := <-errCh:
		_ = os.Remove(d.cfg.SocketPath)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}
