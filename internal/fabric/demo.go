package fabric

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/learn"
	"github.com/Catatonic-Phobos/OMLS/internal/power"
	"github.com/Catatonic-Phobos/OMLS/internal/schedule"
	omlsv1 "github.com/Catatonic-Phobos/OMLS/proto/omls/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workUnit struct {
	ID           string
	Function     string
	Iterations   int64
	Index        int32
	Count        int32
	NodeID       string
	EnvelopeYAML []byte
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
			WorkId:       unit.ID,
			Function:     unit.Function,
			Iterations:   unit.Iterations,
			WorkerIndex:  unit.Index,
			WorkerCount:  unit.Count,
			EnvelopeYaml: unit.EnvelopeYAML,
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
	envYAML := req.GetEnvelopeYaml()
	policyValue := strings.ToLower(strings.TrimSpace(req.GetPolicy()))
	gpuMode := strings.HasPrefix(policyValue, "gpu-serial")
	placeMode := false
	if gpuMode && len(envYAML) > 0 {
		return nil, status.Error(codes.InvalidArgument, "resource envelopes are not supported for GPU work yet")
	}
	if gpuMode {
		policyValue = strings.TrimPrefix(policyValue, "gpu-serial")
		policyValue = strings.TrimPrefix(policyValue, ":")
	}
	switch policyValue {
	case "place", "exclusive":
		placeMode = true
		policyValue = learn.PolicyAdaptive
	}
	policyName := learn.NormalizePolicy(policyValue)
	function := "parallel_workers"
	requires := []string{"compute", "parallelizable"}
	if gpuMode {
		function = "gpu_serial"
		requires = []string{"graphics", "compute", "parallelizable"}
	}
	if placeMode && gpuMode {
		return nil, status.Error(codes.InvalidArgument, "exclusive placement is CPU-only for now")
	}
	envelopeCost := learn.EnvelopeCostFromYAML(envYAML)
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
	if s.store != nil {
		if seed, err := s.store.SeedPlaneStats(); err == nil {
			for id, st := range seed {
				plane.Seed(id, st.Duration, st.TempDelta, st.Samples)
			}
		}
	}
	var powerNote string
	if s.power != nil && len(envYAML) > 0 {
		intensity := power.IntensityFromEnvelopeYAML(envYAML)
		st := s.power.State()
		b := power.EnvelopeBudgetHint("run-demo", intensity, st.TotalBudgetW, 0)
		ack := s.power.Handle(power.Message{Type: power.MsgSetBudget, SchemaVersion: power.SchemaVersion, Budget: &b})
		if ack.OK {
			powerNote = fmt.Sprintf("power: budget %.1fW peak %.1fW on %s (envelope intensity=%.2f)", b.Watts, b.PeakWatts, b.RailID, intensity)
		} else {
			powerNote = "power: budget attach failed: " + ack.Error
		}
	} else if s.power != nil {
		st := s.power.State()
		powerNote = fmt.Sprintf("power: simulator online total_budget=%.0fW draw=%.1fW rails=%d", st.TotalBudgetW, st.TotalDrawW, len(st.Rails))
	}
	var alloc schedule.Allocation
	var placements []schedule.Placement
	var policy string
	resp := &omlsv1.RunDemoResponse{}

	for round := 1; round <= roundsN; round++ {
		snap := s.reg.Snapshot()
		cands, cap, err := planCandidates(snap.Nodes, workers, s.localHostname, ceiling, requires, function)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
		}

		funcByNode := map[string]string{}
		if placeMode {
			half := workers / 2
			if half < 1 {
				half = 1
			}
			other := workers - half
			if other < 1 {
				other = 1
			}
			placements, err = schedule.PlaceExclusive(snap.Nodes, []schedule.FuncRequest{
				{Function: "parallel_workers", Requires: requires, Workers: half},
				{Function: "memory_touch", Requires: requires, Workers: other},
			}, schedule.Options{LocalHostname: s.localHostname, ThermalCeiling: ceiling})
			if err != nil {
				return nil, status.Errorf(codes.FailedPrecondition, "place: %v", err)
			}
			alloc = schedule.Allocation{}
			for _, p := range placements {
				alloc[p.NodeID] += p.Workers
				funcByNode[p.NodeID] = p.Function
			}
			policy = fmt.Sprintf("exclusive function placement across distinct nodes (policy=place functions=%d)", len(placements))
			if powerNote != "" {
				policy = policy + "; " + powerNote
			}
		} else if round == 1 {
			alloc, _, err = schedule.Plan(snap.Nodes, schedule.Request{
				Function: function,
				Requires: requires,
				Workers:  workers,
			}, schedule.Options{LocalHostname: s.localHostname, ThermalCeiling: ceiling})
			if err != nil {
				return nil, status.Errorf(codes.FailedPrecondition, "schedule: %v", err)
			}
			policy = fmt.Sprintf("initial proportional split by scheduler score (device=%s policy=%s)", map[bool]string{true: "gpu-serial", false: "cpu"}[gpuMode], policyName)
			if powerNote != "" {
				policy = policy + "; " + powerNote
			}
		} else if policyName == learn.PolicyAdaptive {
			signals := signalsFromSnapshot(snap.Nodes, cap, s.localHostname)
			alloc, policy = plane.AdaptiveAdjust(alloc, signals, learn.AdaptiveOptions{
				Weights:          learn.DefaultAdaptiveWeights(),
				ThermalCeiling:   ceiling,
				CooldownRounds:   2,
				MinScoreDelta:    0.12,
				MaxMovesPerRound: 1,
				EnvelopeCost:     envelopeCost,
				Round:            round,
			})
		} else {
			tempNow, tempKnown := tempsFromSnapshot(snap.Nodes)
			alloc, policy = plane.Adjust(alloc, cap, tempNow, tempKnown)
		}

		gpuIndices := map[string][]int{}
		if gpuMode {
			gpuIndices = openCLIndices(snap.Nodes)
		}
		var units []workUnit
		if placeMode {
			units = enqueuePlacements(placements, workIters, envYAML)
		} else {
			units = enqueueAlloc(alloc, workIters, workers, envYAML, function, gpuMode, gpuIndices)
		}
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
		nodeExec := map[string]int{}

		for got < expect {
			select {
			case <-dctx.Done():
				return nil, status.Errorf(codes.DeadlineExceeded, "demo timed out waiting for results (%d/%d)", got, expect)
			case res := <-s.demo.results:
				got++
				if res.Error != "" {
					if gpuMode {
						return nil, status.Errorf(codes.FailedPrecondition, "GPU work %s failed on node %s: %s", res.WorkID, res.NodeID, res.Error)
					}
					continue
				}
				nodeExec[res.NodeID]++
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
			if s.store != nil && !gpuMode {
				host, virt := "", ""
				for _, node := range snap.Nodes {
					if node.ID == id {
						host, virt = node.Hostname, node.Virt
						break
					}
				}
				_ = s.store.ObserveWork(id, host, virt, "cpu0", avg, td, known)
			}
		}

		candByID := map[string]schedule.Candidate{}
		for _, c := range cands {
			candByID[c.NodeID] = c
		}
		placeByID := map[string]schedule.Placement{}
		for _, p := range placements {
			placeByID[p.NodeID] = p
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
		plannedHosts := int32(0)
		for id, w := range alloc {
			if w == 0 {
				continue
			}
			plannedHosts++
			c := candByID[id]
			host, virt, score, reason := c.Hostname, c.Virt, c.Score, c.Reason
			fn := function
			if p, ok := placeByID[id]; ok {
				host, virt, score, reason = p.Hostname, p.Virt, p.Score, p.Reason
				fn = p.Function
			} else if fnName := funcByNode[id]; fnName != "" {
				fn = fnName
			}
			if host == "" {
				for _, n := range snap.Nodes {
					if n.ID == id {
						host, virt = n.Hostname, n.Virt
						break
					}
				}
			}
			exec := int32(nodeExec[id])
			demoRound.Allocations = append(demoRound.Allocations, &omlsv1.NodeAllocation{
				NodeId:   id,
				Hostname: host,
				Virt:     virt,
				Workers:  int32(w),
				Score:    score,
				Reason:   reason,
				Executed: exec,
				Function: fn,
			})
		}
		demoRound.PlannedHosts = plannedHosts
		demoRound.ExecutedHosts = int32(len(nodeExec))
		if demoRound.ExecutedHosts < demoRound.PlannedHosts {
			demoRound.PolicyNote += fmt.Sprintf("; WARNING: planned_hosts=%d executed_hosts=%d — division was not fully realized",
				demoRound.PlannedHosts, demoRound.ExecutedHosts)
		}
		resp.Rounds = append(resp.Rounds, demoRound)
	}

	resp.Summary = summarizeDemo(resp)
	if s.power != nil {
		st := s.power.State()
		resp.Summary += fmt.Sprintf("; power_draw=%.1fW budgets=%d", st.TotalDrawW, len(st.Budgets))
	}
	return resp, nil
}

func planCandidates(nodes []graph.NodeEntry, workers int, localHost string, ceiling float64, requires []string, function string) ([]schedule.Candidate, map[string]int, error) {
	_, cands, err := schedule.Plan(nodes, schedule.Request{
		Function: function,
		Requires: requires,
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

func signalsFromSnapshot(nodes []graph.NodeEntry, cap map[string]int, localHost string) map[string]learn.NodeSignals {
	out := map[string]learn.NodeSignals{}
	for _, n := range nodes {
		sig := learn.NodeSignals{
			Virt:     n.Virt,
			IsLocal:  localHost != "" && n.Hostname == localHost,
			Capacity: cap[n.ID],
		}
		if n.Telemetry != nil {
			sig.Load = n.Telemetry.CPULoad
			sig.LoadKnown = true
			if n.Telemetry.TemperatureKnown {
				sig.TempC = n.Telemetry.TemperatureC
				sig.TempKnown = true
			}
		}
		out[n.ID] = sig
	}
	return out
}

func enqueueAlloc(alloc schedule.Allocation, workIters int64, totalWorkers int, envYAML []byte, function string, deviceSerial bool, deviceIndices map[string][]int) []workUnit {
	out := []workUnit{}
	idx := int32(0)
	for nodeID, n := range alloc {
		prefix := nodeID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		for i := 0; i < n; i++ {
			workerIndex := idx
			if deviceSerial {
				workerIndex = int32(i)
				if i < len(deviceIndices[nodeID]) {
					workerIndex = int32(deviceIndices[nodeID][i])
				}
			}
			out = append(out, workUnit{
				ID:           fmt.Sprintf("w-%s-%d", prefix, idx),
				Function:     function,
				Iterations:   workIters,
				Index:        workerIndex,
				Count:        int32(totalWorkers),
				NodeID:       nodeID,
				EnvelopeYAML: envYAML,
			})
			idx++
		}
	}
	return out
}

func enqueuePlacements(placements []schedule.Placement, workIters int64, envYAML []byte) []workUnit {
	out := []workUnit{}
	idx := int32(0)
	total := 0
	for _, p := range placements {
		total += p.Workers
	}
	for _, p := range placements {
		prefix := p.NodeID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		for i := 0; i < p.Workers; i++ {
			out = append(out, workUnit{
				ID:           fmt.Sprintf("w-%s-%s-%d", prefix, p.Function, idx),
				Function:     p.Function,
				Iterations:   workIters,
				Index:        idx,
				Count:        int32(total),
				NodeID:       p.NodeID,
				EnvelopeYAML: envYAML,
			})
			idx++
		}
	}
	return out
}

func openCLIndices(nodes []graph.NodeEntry) map[string][]int {
	indices := make(map[string][]int)
	for _, node := range nodes {
		if node.Profile == nil {
			continue
		}
		for _, resource := range node.Profile.Resources {
			if resource.Class != "graphics" || resource.Attrs["compute_backend"] != "opencl" {
				continue
			}
			if index, ok := integerAttr(resource.Attrs["compute_device_index"]); ok && index >= 0 {
				indices[node.ID] = append(indices[node.ID], index)
			}
		}
	}
	return indices
}

func integerAttr(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case int32:
		return int(number), true
	case int64:
		return int(number), true
	case uint32:
		return int(number), true
	case uint64:
		return int(number), true
	case float64:
		return int(number), number == float64(int(number))
	default:
		return 0, false
	}
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
	return fmt.Sprintf("rounds=%d first=%s last=%s executed=%s planned_hosts=%d executed_hosts=%d policy=%q",
		len(resp.Rounds), fmtAlloc(first), fmtAlloc(last), fmtExecuted(last),
		last.GetPlannedHosts(), last.GetExecutedHosts(), last.GetPolicyNote())
}

func fmtAlloc(r *omlsv1.DemoRound) string {
	parts := make([]string, 0, len(r.GetAllocations()))
	for _, a := range r.GetAllocations() {
		label := shortID(a.GetNodeId())
		if fn := a.GetFunction(); fn != "" && fn != "parallel_workers" {
			label = label + "/" + fn
		}
		parts = append(parts, fmt.Sprintf("%s:%d", label, a.GetWorkers()))
	}
	if len(parts) == 0 {
		return "-"
	}
	return fmt.Sprint(parts)
}

func fmtExecuted(r *omlsv1.DemoRound) string {
	parts := make([]string, 0, len(r.GetAllocations()))
	for _, a := range r.GetAllocations() {
		parts = append(parts, fmt.Sprintf("%s:%d", shortID(a.GetNodeId()), a.GetExecuted()))
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
