package power_test

import (
	"testing"

	"github.com/Catatonic-Phobos/OMLS/internal/power"
)

func TestSimulatorHelloAndBudget(t *testing.T) {
	sim := power.NewSimulator()
	hello := sim.Handle(power.Message{Type: power.MsgHello, SchemaVersion: power.SchemaVersion})
	if !hello.OK || hello.State == nil || !hello.State.Online {
		t.Fatalf("hello=%+v", hello)
	}
	if len(hello.State.Rails) < 3 {
		t.Fatalf("rails=%d", len(hello.State.Rails))
	}

	resp := sim.Handle(power.Message{
		Type:          power.MsgSetBudget,
		SchemaVersion: power.SchemaVersion,
		Budget: &power.Budget{
			ConsumerID: "demo",
			RailID:     "rail-12v",
			Watts:      40,
			Source:     "manual",
		},
	})
	if !resp.OK {
		t.Fatalf("set_budget: %s", resp.Error)
	}
	st := sim.State()
	if len(st.Budgets) != 1 || st.Budgets[0].Watts != 40 {
		t.Fatalf("budgets=%v", st.Budgets)
	}

	status := sim.Handle(power.Message{Type: power.MsgStatus, SchemaVersion: power.SchemaVersion})
	if status.State == nil || status.State.TotalDrawW <= 0 {
		t.Fatalf("status=%+v", status)
	}
}

func TestUnknownRailRejected(t *testing.T) {
	sim := power.NewSimulator()
	resp := sim.Handle(power.Message{
		Type: power.MsgSetBudget,
		Budget: &power.Budget{
			ConsumerID: "x",
			RailID:     "nope",
			Watts:      1,
		},
	})
	if resp.OK {
		t.Fatal("expected failure")
	}
}

func TestEnvelopeBudgetHint(t *testing.T) {
	eco := power.EnvelopeBudgetHint("c", 0.35, 180, 0)
	high := power.EnvelopeBudgetHint("c", 0.85, 180, 0)
	if eco.Watts >= high.Watts {
		t.Fatalf("eco=%v high=%v", eco.Watts, high.Watts)
	}
	if eco.Source != "envelope" || eco.RailID != "rail-12v" {
		t.Fatalf("%+v", eco)
	}
	i := power.IntensityFromEnvelopeYAML([]byte("performance: eco\nsustain:\n  level: 0.35\n"))
	if i < 0.3 || i > 0.4 {
		t.Fatalf("intensity=%v", i)
	}
}

func TestSoftOverBudgetWarning(t *testing.T) {
	sim := power.NewSimulator()
	resp := sim.Handle(power.Message{
		Type: power.MsgSetBudget,
		Budget: &power.Budget{
			ConsumerID: "gpu",
			RailID:     "rail-12v",
			Watts:      500, // over 120W rail budget
			Source:     "manual",
		},
	})
	if !resp.OK {
		t.Fatalf("should accept soft: %s", resp.Error)
	}
	found := false
	for _, w := range resp.State.Warnings {
		if w != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected over-budget warning")
	}
}
