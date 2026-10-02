package fabric_test

import (
	"context"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/fabric"
	"github.com/Catatonic-Phobos/OMLS/internal/graph"
	"github.com/Catatonic-Phobos/OMLS/internal/power"
)

func TestServerPowerSimulator(t *testing.T) {
	reg := graph.New(15 * time.Second)
	srv := fabric.NewServer(reg, time.Second)
	if srv.Power() == nil {
		t.Fatal("expected power simulator")
	}
	ack := srv.Power().Handle(power.Message{
		Type: power.MsgSetBudget,
		Budget: &power.Budget{
			ConsumerID: "t",
			RailID:     "rail-5v",
			Watts:      5,
			Source:     "manual",
		},
	})
	if !ack.OK {
		t.Fatal(ack.Error)
	}
	st := srv.Power().State()
	if len(st.Budgets) != 1 {
		t.Fatalf("%v", st.Budgets)
	}
	_ = context.Background()
}
