// Package power is the OMLS 0.9 Physical Power Fabric / MCU protocol stub.
// No real MCU hardware is required — an in-process simulator provides rails and budgets.
package power

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = "0.9"

// MessageType enumerates fabric protocol messages (YAML/JSON friendly).
type MessageType string

const (
	MsgHello     MessageType = "hello"
	MsgStatus    MessageType = "status"
	MsgSetBudget MessageType = "set_budget"
	MsgAck       MessageType = "ack"
	MsgTelemetry MessageType = "telemetry"
)

// Rail is one simulated power rail.
type Rail struct {
	ID           string  `json:"id" yaml:"id"`
	VoltageV     float64 `json:"voltage_v" yaml:"voltage_v"`
	CurrentA     float64 `json:"current_a" yaml:"current_a"`
	PowerW       float64 `json:"power_w" yaml:"power_w"`
	BudgetW      float64 `json:"budget_w" yaml:"budget_w"`
	TemperatureC float64 `json:"temperature_c,omitempty" yaml:"temperature_c,omitempty"`
	Domain       string  `json:"domain,omitempty" yaml:"domain,omitempty"`
}

// Budget is a soft power allocation hint for a consumer (node/workload).
type Budget struct {
	ConsumerID string  `json:"consumer_id" yaml:"consumer_id"`
	RailID     string  `json:"rail_id,omitempty" yaml:"rail_id,omitempty"`
	Watts      float64 `json:"watts" yaml:"watts"`
	PeakWatts  float64 `json:"peak_watts,omitempty" yaml:"peak_watts,omitempty"`
	Source     string  `json:"source,omitempty" yaml:"source,omitempty"` // envelope|manual|default
	Notes      string  `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// FabricState is the simulated MCU view of the power fabric.
type FabricState struct {
	SchemaVersion string    `json:"schema_version" yaml:"schema_version"`
	MCU           string    `json:"mcu" yaml:"mcu"` // "simulator"
	Online        bool      `json:"online" yaml:"online"`
	UpdatedAt     time.Time `json:"updated_at" yaml:"updated_at"`
	TotalBudgetW  float64   `json:"total_budget_w" yaml:"total_budget_w"`
	TotalDrawW    float64   `json:"total_draw_w" yaml:"total_draw_w"`
	Rails         []Rail    `json:"rails" yaml:"rails"`
	Budgets       []Budget  `json:"budgets" yaml:"budgets"`
	Warnings      []string  `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// Message is a power-fabric protocol envelope (request or response).
type Message struct {
	SchemaVersion string         `json:"schema_version" yaml:"schema_version"`
	Type          MessageType    `json:"type" yaml:"type"`
	MCU           string         `json:"mcu,omitempty" yaml:"mcu,omitempty"`
	Ts            time.Time      `json:"ts" yaml:"ts"`
	Budget        *Budget        `json:"budget,omitempty" yaml:"budget,omitempty"`
	State         *FabricState   `json:"state,omitempty" yaml:"state,omitempty"`
	OK            bool           `json:"ok,omitempty" yaml:"ok,omitempty"`
	Error         string         `json:"error,omitempty" yaml:"error,omitempty"`
	Attrs         map[string]any `json:"attrs,omitempty" yaml:"attrs,omitempty"`
}

// Simulator is an in-process MCU endpoint (no hardware).
type Simulator struct {
	mu       sync.Mutex
	state    FabricState
	seq      int
	handlers []func(Message)
}

// DefaultRails returns a small lab-style rail set.
func DefaultRails() []Rail {
	return []Rail{
		{ID: "rail-12v", VoltageV: 12, CurrentA: 2.5, PowerW: 30, BudgetW: 120, Domain: "12V", TemperatureC: 35},
		{ID: "rail-5v", VoltageV: 5, CurrentA: 1.2, PowerW: 6, BudgetW: 40, Domain: "5V", TemperatureC: 32},
		{ID: "rail-3v3", VoltageV: 3.3, CurrentA: 0.8, PowerW: 2.64, BudgetW: 20, Domain: "3V3", TemperatureC: 30},
	}
}

// NewSimulator creates an online simulated MCU with default rails.
func NewSimulator() *Simulator {
	rails := DefaultRails()
	totalB, totalD := 0.0, 0.0
	for _, r := range rails {
		totalB += r.BudgetW
		totalD += r.PowerW
	}
	return &Simulator{
		state: FabricState{
			SchemaVersion: SchemaVersion,
			MCU:           "simulator",
			Online:        true,
			UpdatedAt:     time.Now().UTC(),
			TotalBudgetW:  totalB,
			TotalDrawW:    totalD,
			Rails:         rails,
			Budgets:       []Budget{},
			Warnings:      []string{"power fabric MCU is simulated — no physical switching hardware"},
		},
	}
}

// State returns a copy of the fabric state.
func (s *Simulator) State() FabricState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state)
}

// Handle processes one protocol message and returns a response.
func (s *Simulator) Handle(req Message) Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	now := time.Now().UTC()
	resp := Message{
		SchemaVersion: SchemaVersion,
		MCU:           "simulator",
		Ts:            now,
		OK:            true,
	}
	if req.SchemaVersion != "" && req.SchemaVersion != SchemaVersion {
		resp.OK = false
		resp.Type = MsgAck
		resp.Error = fmt.Sprintf("unsupported schema_version %q (want %s)", req.SchemaVersion, SchemaVersion)
		return resp
	}
	switch req.Type {
	case MsgHello, "":
		resp.Type = MsgAck
		st := cloneState(s.state)
		resp.State = &st
		resp.Attrs = map[string]any{"seq": s.seq, "hello": true}
	case MsgStatus, MsgTelemetry:
		resp.Type = MsgStatus
		s.refreshDrawLocked(now)
		st := cloneState(s.state)
		resp.State = &st
	case MsgSetBudget:
		if req.Budget == nil {
			resp.OK = false
			resp.Type = MsgAck
			resp.Error = "budget required"
			return resp
		}
		if err := applyBudgetLocked(&s.state, *req.Budget); err != nil {
			resp.OK = false
			resp.Type = MsgAck
			resp.Error = err.Error()
			return resp
		}
		s.state.UpdatedAt = now
		resp.Type = MsgAck
		st := cloneState(s.state)
		resp.State = &st
		resp.Budget = req.Budget
	default:
		resp.OK = false
		resp.Type = MsgAck
		resp.Error = fmt.Sprintf("unknown message type %q", req.Type)
	}
	for _, h := range s.handlers {
		h(resp)
	}
	return resp
}

func applyBudgetLocked(st *FabricState, b Budget) error {
	if strings.TrimSpace(b.ConsumerID) == "" {
		return fmt.Errorf("consumer_id required")
	}
	if b.Watts < 0 {
		return fmt.Errorf("watts must be >= 0")
	}
	if b.RailID != "" {
		found := false
		for i := range st.Rails {
			if st.Rails[i].ID == b.RailID {
				found = true
				// Soft clamp: warn if over rail budget but still accept (simulator).
				if b.Watts > st.Rails[i].BudgetW {
					st.Warnings = appendUnique(st.Warnings,
						fmt.Sprintf("budget %.1fW for %s exceeds rail %s budget %.1fW (accepted as soft hint)",
							b.Watts, b.ConsumerID, b.RailID, st.Rails[i].BudgetW))
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown rail_id %q", b.RailID)
		}
	}
	replaced := false
	for i := range st.Budgets {
		if st.Budgets[i].ConsumerID == b.ConsumerID && st.Budgets[i].RailID == b.RailID {
			st.Budgets[i] = b
			replaced = true
			break
		}
	}
	if !replaced {
		st.Budgets = append(st.Budgets, b)
	}
	return nil
}

func (s *Simulator) refreshDrawLocked(now time.Time) {
	total := 0.0
	for i := range s.state.Rails {
		r := &s.state.Rails[i]
		r.PowerW = r.VoltageV * r.CurrentA
		total += r.PowerW
	}
	s.state.TotalDrawW = total
	s.state.UpdatedAt = now
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func cloneState(st FabricState) FabricState {
	out := st
	out.Rails = append([]Rail{}, st.Rails...)
	out.Budgets = append([]Budget{}, st.Budgets...)
	out.Warnings = append([]string{}, st.Warnings...)
	return out
}

// ParseMessageYAML unmarshals a protocol message.
func ParseMessageYAML(raw []byte) (Message, error) {
	var m Message
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return Message{}, err
	}
	if m.SchemaVersion == "" {
		m.SchemaVersion = SchemaVersion
	}
	return m, nil
}

// MarshalStateYAML encodes fabric state.
func MarshalStateYAML(st FabricState) ([]byte, error) {
	return yaml.Marshal(st)
}

// LoadStateFile reads a fabric state YAML (optional seed).
func LoadStateFile(path string) (FabricState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return FabricState{}, err
	}
	var st FabricState
	if err := yaml.Unmarshal(b, &st); err != nil {
		return FabricState{}, err
	}
	if st.SchemaVersion == "" {
		st.SchemaVersion = SchemaVersion
	}
	if st.MCU == "" {
		st.MCU = "simulator"
	}
	return st, nil
}

// EnvelopeBudgetHint maps envelope intensity (0..1) to a soft watt hint against total budget.
// peakScale amplifies peak_watts (default 1.4).
func EnvelopeBudgetHint(consumerID string, intensity, totalBudgetW, peakScale float64) Budget {
	if intensity < 0 {
		intensity = 0
	}
	if intensity > 1 {
		intensity = 1
	}
	if totalBudgetW <= 0 {
		totalBudgetW = 100
	}
	if peakScale <= 0 {
		peakScale = 1.4
	}
	// Use a fraction of total fabric budget as the soft consumer budget.
	w := totalBudgetW * 0.25 * (0.35 + 0.65*intensity)
	return Budget{
		ConsumerID: consumerID,
		RailID:     "rail-12v",
		Watts:      w,
		PeakWatts:  w * peakScale,
		Source:     "envelope",
		Notes:      fmt.Sprintf("from envelope intensity=%.2f", intensity),
	}
}

// IntensityFromEnvelopeYAML extracts sustain level (reuse learn's cheap parse idea).
func IntensityFromEnvelopeYAML(yamlBytes []byte) float64 {
	if len(yamlBytes) == 0 {
		return 0.55
	}
	s := strings.ToLower(string(yamlBytes))
	if i := strings.Index(s, "sustain:"); i >= 0 {
		chunk := s[i:]
		if j := strings.Index(chunk, "level:"); j >= 0 && j < 80 {
			rest := strings.TrimSpace(chunk[j+6:])
			var v float64
			if _, err := fmt.Sscanf(rest, "%f", &v); err == nil {
				if v < 0 {
					v = 0
				}
				if v > 1 {
					v = 1
				}
				return v
			}
		}
	}
	switch {
	case strings.Contains(s, "performance: eco"):
		return 0.35
	case strings.Contains(s, "performance: high"):
		return 0.85
	default:
		return 0.55
	}
}
