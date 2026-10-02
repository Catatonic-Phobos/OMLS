package fabric

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/learn"
	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workUnit struct {
	ID         string
	Function   string
	Iterations int64
	Index      int32
	Count      int32
	NodeID     string
}

type workResult struct {
	NodeID     string
	WorkID     string
	DurationMs int64
	TempBefore float64
	TempAfter  float64
	TempKnown  bool
	Error      string
}

type demoState struct {
	mu      sync.Mutex
	active  bool
	queue   map[string][]workUnit
	pending map[string]workUnit
	results chan workResult
}

func newDemoState() *demoState {
	return &demoState{
		queue:   map[string][]workUnit{},
		pending: map[string]workUnit{},
		results: make(chan workResult, 256),
	}
}

func (s *Server) ClaimWork(ctx context.Context, req *omlsv1.ClaimWorkRequest) (*omlsv1.ClaimWorkResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	s.demo.mu.Lock()
	defer s.demo.mu.Unlock()
	if !s.demo.active {
		return &omlsv1.ClaimWorkResponse{HasWork: false}, nil
	}
	q := s.demo.queue[req.GetNodeId()]
	if len(q) == 0 {
		return &omlsv1.ClaimWorkResponse{HasWork: false}, nil
	}
	unit := q[0]
	s.demo.queue[req.GetNodeId()] = q[1:]
	s.demo.pending[unit.ID] = unit
	return &omlsv1.ClaimWorkResponse{
		HasWork: true,
		Unit: &omlsv1.WorkUnit{
			WorkId:      unit.ID,
			Function:    unit.Function,
			Iterations:  unit.Iterations,
			WorkerIndex: unit.Index,
			WorkerCount: unit.Count,
		},
	}, nil
}

func (s *Server) ReportWork(ctx context.Context, req *omlsv1.ReportWorkRequest) (*omlsv1.ReportWorkResponse, error) {
	if req.GetNodeId() == "" || req.GetWorkId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id and work_id are required")
	}
	s.demo.mu.Lock()
	_, ok := s.demo.pending[req.GetWorkId()]
	if ok {
		delete(s.demo.pending, req.GetWorkId())
	}
	active := s.demo.active
	s.demo.mu.Unlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "unknown work_id")
	}
	if !active {
		return &omlsv1.ReportWorkResponse{Ok: true}, nil
	}
	select {
	case s.demo.results <- workResult{
		NodeID:     req.GetNodeId(),
		WorkID:     req.GetWorkId(),
		DurationMs: req.GetDurationMs(),
		TempBefore: req.GetTempBeforeC(),
		TempAfter:  req.GetTempAfterC(),
		TempKnown:  req.GetTemperatureKnown(),
		Error:      req.GetError(),
	}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &omlsv1.ReportWorkResponse{Ok: true}, nil
}

func (s *Server) RunDemo(ctx context.Context, req *omlsv1.RunDemoRequest) (*omlsv1.RunDemoResponse, error) {
	workers := int(req.GetWorkers())
	if workers < 1 {
		workers = 8
	}
	roundsN := int(req.GetIterations())
	if roundsN < 1 {
		roundsN = 3
	}
	workIters := req.GetWorkIterations()
	if workIters < 1 {
		workIters = 3_000_000
	}
	ceiling := req.GetThermalCeilingC()
	if ceiling <= 0 {
		ceiling = 85
	}
	timeout := time.Duration(req.GetTimeoutMs()) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	s.demo.mu.Lock()
	if s.demo.active {
		s.demo.mu.Unlock()
		return nil, status.Error(codes.FailedPrecondition, "a demo is already running")
	}
	s.demo.active = true
	s.demo.queue = map[string][]workUnit{}
	s.demo.pending = map[string]workUnit{}
	drainResults(s.demo.results)
	s.demo.mu.Unlock()
	defer func() {
		s.demo.mu.Lock()
		s.demo.active = false
		s.demo.queue = map[string][]workUnit{}
		s.demo.pending = map[string]workUnit{}
		s.demo.mu.Unlock()
	}()

	plane := learn.New(0.3, ceiling)
	var alloc schedule.Allocation
	var policy string
	resp := &omlsv1.RunDemoResponse{}

	for round := 1; round <= roundsN; round++ {
		snap := s.reg.Snapshot()
		cands, cap, err := planCandidates(snap.Nodes, workers, s.localHostname, ceiling)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
		}

		if round == 1 {
			alloc, _, err = schedule.Plan(snap.Nodes, schedule.Request{
				Function: "parallel_workers",
				Requires: []string{"compute", "parallelizable"},
				Workers:  workers,
			}, schedule.Options{LocalHostname: s.localHostname, ThermalCeiling: ceiling})
			if err != nil {
				return nil, status.Errorf(codes.FailedPrecondition, "schedule: %v", err)
			}
			policy = "initial proportional split by scheduler score"
		} else {
			tempNow, tempKnown := tempsFromSnapshot(snap.Nodes)
			alloc, policy = plane.Adjust(alloc, cap, tempNow, tempKnown)
		}

		units := enqueueAlloc(alloc, workIters, workers)
		s.demo.mu.Lock()
		s.demo.queue = map[string][]workUnit{}
		s.demo.pending = map[string]workUnit{}
		for _, u := range units {
			s.demo.queue[u.NodeID] = append(s.demo.queue[u.NodeID], u)
		}
		expect := len(units)
		s.demo.mu.Unlock()

		if expect == 0 {
			return nil, status.Error(codes.FailedPrecondition, "allocation assigned zero workers")
		}

		start := time.Now()
		got := 0
		nodeDurSum := map[string]float64{}
		nodeDurN := map[string]int{}
		nodeTempDelta := map[string]float64{}
		nodeTempN := map[string]int{}

		for got < expect {
			select {
			case <-dctx.Done():
				return nil, status.Errorf(codes.DeadlineExceeded, "demo timed out waiting for results (%d/%d)", got, expect)
			case res := <-s.demo.results:
				got++
				if res.Error != "" {
					continue
				}
				nodeDurSum[res.NodeID] += float64(res.DurationMs)
				nodeDurN[res.NodeID]++
				if res.TempKnown {
					nodeTempDelta[res.NodeID] += res.TempAfter - res.TempBefore
					nodeTempN[res.NodeID]++
				}
			}
		}

		for id, n := range nodeDurN {
			avg := nodeDurSum[id] / float64(n)
			td := 0.0
			known := false
			if nodeTempN[id] > 0 {
				td = nodeTempDelta[id] / float64(nodeTempN[id])
				known = true
			}
			plane.Observe(id, avg, td, known)
		}

		candByID := map[string]schedule.Candidate{}
		for _, c := range cands {
			candByID[c.NodeID] = c
		}
		st := plane.Snapshot()
		demoRound := &omlsv1.DemoRound{
			Round:          int32(round),
			PolicyNote:     policy,
			WallMs:         time.Since(start).Milliseconds(),
			DurationEwmaMs: map[string]float64{},
			TempDeltaEwma:  map[string]float64{},
		}
		for id, sstat := range st {
			demoRound.DurationEwmaMs[id] = sstat.DurationEWMA
			demoRound.TempDeltaEwma[id] = sstat.TempDeltaEWMA
		}
		for id, w := range alloc {
			if w == 0 {
				continue
			}
			c := candByID[id]
			host, virt := c.Hostname, c.Virt
			if host == "" {
				for _, n := range snap.Nodes {
					if n.ID == id {
						host, virt = n.Hostname, n.Virt
						break
					}
				}
			}
			demoRound.Allocations = append(demoRound.Allocations, &omlsv1.NodeAllocation{
				NodeId:   id,
				Hostname: host,
				Virt:     virt,
				Workers:  int32(w),
				Score:    c.Score,
				Reason:   c.Reason,
			})
		}
		resp.Rounds = append(resp.Rounds, demoRound)
	}

	resp.Summary = summarizeDemo(resp)
	return resp, nil
}

func planCandidates(nodes []graph.NodeEntry, workers int, localHost string, ceiling float64) ([]schedule.Candidate, map[string]int, error) {
	_, cands, err := schedule.Plan(nodes, schedule.Request{
		Function: "parallel_workers",
		Requires: []string{"compute", "parallelizable"},
		Workers:  workers,
	}, schedule.Options{LocalHostname: localHost, ThermalCeiling: ceiling})
	if err != nil {
		return nil, nil, err
	}
	cap := map[string]int{}
	for _, c := range cands {
		cap[c.NodeID] = c.Capacity
	}
	return cands, cap, nil
}

func tempsFromSnapshot(nodes []graph.NodeEntry) (map[string]float64, map[string]bool) {
	tempNow := map[string]float64{}
	tempKnown := map[string]bool{}
	for _, n := range nodes {
		if n.Telemetry != nil && n.Telemetry.TemperatureKnown {
			tempNow[n.ID] = n.Telemetry.TemperatureC
			tempKnown[n.ID] = true
		}
	}
	return tempNow, tempKnown
}

func enqueueAlloc(alloc schedule.Allocation, workIters int64, totalWorkers int) []workUnit {
	out := []workUnit{}
	idx := int32(0)
	for nodeID, n := range alloc {
		prefix := nodeID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		for i := 0; i < n; i++ {
			out = append(out, workUnit{
				ID:         fmt.Sprintf("w-%s-%d", prefix, idx),
				Function:   "parallel_workers",
				Iterations: workIters,
				Index:      idx,
				Count:      int32(totalWorkers),
				NodeID:     nodeID,
			})
			idx++
		}
	}
	return out
}

func drainResults(ch chan workResult) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func summarizeDemo(resp *omlsv1.RunDemoResponse) string {
	if len(resp.Rounds) == 0 {
		return "no rounds"
	}
	first := resp.Rounds[0]
	last := resp.Rounds[len(resp.Rounds)-1]
	return fmt.Sprintf("rounds=%d first=%s last=%s policy=%q",
		len(resp.Rounds), fmtAlloc(first), fmtAlloc(last), last.GetPolicyNote())
}

func fmtAlloc(r *omlsv1.DemoRound) string {
	parts := make([]string, 0, len(r.GetAllocations()))
	for _, a := range r.GetAllocations() {
		parts = append(parts, fmt.Sprintf("%s:%d", shortID(a.GetNodeId()), a.GetWorkers()))
	}
	if len(parts) == 0 {
		return "-"
	}
	return fmt.Sprint(parts)
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
