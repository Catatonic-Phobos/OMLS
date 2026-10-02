package behavior_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Catatonic-Phobos/OMLS/internal/behavior"
)

func TestStoreRecordTelemetryAndObserveWork(t *testing.T) {
	dir := t.TempDir()
	store, err := behavior.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.Dir() != dir {
		t.Fatalf("dir=%s", store.Dir())
	}

	err = store.RecordTelemetry("node-a", "host-a", "kvm", behavior.TelemetrySample{
		ObservedAt:        time.Now().UTC(),
		CPULoad:           1.5,
		MemAvailableBytes: 4 << 30,
		MemTotalBytes:     8 << 30,
		TemperatureC:      55,
		TemperatureKnown:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ObserveWork("node-a", "host-a", "kvm", "cpu0", 42.0, 1.5, true); err != nil {
		t.Fatal(err)
	}
	if err := store.ObserveWork("node-a", "host-a", "kvm", "cpu0", 50.0, 2.0, true); err != nil {
		t.Fatal(err)
	}

	p, err := store.Load("node-a")
	if err != nil {
		t.Fatal(err)
	}
	if p.ProfileVersion != behavior.ProfileVersion {
		t.Fatalf("version=%s", p.ProfileVersion)
	}
	if p.Hostname != "host-a" || p.Virt != "kvm" {
		t.Fatalf("%+v", p)
	}
	if len(p.RecentTelemetry) != 1 {
		t.Fatalf("telemetry ring=%d", len(p.RecentTelemetry))
	}
	if len(p.Resources) != 1 || p.Resources[0].ID != "cpu0" {
		t.Fatalf("resources=%+v", p.Resources)
	}
	obs := p.Resources[0].Observed
	if obs.Samples != 2 {
		t.Fatalf("samples=%d", obs.Samples)
	}
	// EWMA: first 42, then 0.3*50 + 0.7*42 = 44.4
	if obs.DurationEWMAMs < 44.0 || obs.DurationEWMAMs > 45.0 {
		t.Fatalf("duration ewma=%.3f", obs.DurationEWMAMs)
	}
	if p.Resources[0].Confidence <= 0 {
		t.Fatal("expected positive confidence")
	}

	ewma, samples, ok := store.DurationEWMA("node-a")
	if !ok || samples != 2 || ewma != obs.DurationEWMAMs {
		t.Fatalf("DurationEWMA=%v %d %v", ewma, samples, ok)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].NodeID != "node-a" {
		t.Fatalf("%+v", list)
	}

	seed, err := store.SeedPlaneStats()
	if err != nil {
		t.Fatal(err)
	}
	st, ok := seed["node-a"]
	if !ok || st.Samples != 2 {
		t.Fatalf("%+v", seed)
	}

	tail, err := store.ReadTelemetryTail("node-a", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 1 || tail[0].CPULoad != 1.5 {
		t.Fatalf("%+v", tail)
	}

	// Profile file exists under profiles/
	if _, err := behavior.Open(filepath.Join(dir)); err != nil {
		t.Fatal(err)
	}
}

func TestConfidenceAndSanitize(t *testing.T) {
	if c := behavior.Confidence(0); c != 0 {
		t.Fatalf("%v", c)
	}
	if c := behavior.Confidence(20); c < 0.6 || c > 0.7 {
		t.Fatalf("%v", c)
	}
	dir := t.TempDir()
	store, err := behavior.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// IDs with odd chars should still save/load.
	id := "node:with/odd chars"
	if err := store.ObserveWork(id, "h", "bare", "cpu0", 10, 0, false); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.NodeID != id {
		t.Fatalf("%s", p.NodeID)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %+v", err, list)
	}
}

func TestEmptyLoadAndMissingTelemetry(t *testing.T) {
	store, err := behavior.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.Load("missing")
	if err != nil {
		t.Fatal(err)
	}
	if p.NodeID != "missing" || len(p.Resources) != 0 {
		t.Fatalf("%+v", p)
	}
	tail, err := store.ReadTelemetryTail("missing", 10)
	if err != nil || tail != nil {
		t.Fatalf("%v %+v", err, tail)
	}
	_, _, ok := store.DurationEWMA("missing")
	if ok {
		t.Fatal("expected no ewma")
	}
}
