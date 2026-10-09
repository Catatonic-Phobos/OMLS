# OMLS operational layers

`main` is always the **cumulative functional stack**. Each version below is a **layer**: additive capability with a clear ownership boundary. Layers can be revisited or swapped later without rewriting the whole system.

```text
1.3 RAM cache reservation (memory or graphics slice on a peer)
  ↑
1.2 Honest membership + exclusive function placement
  ↑
1.1 Cluster autodiscovery (omlsd, mDNS, elected coordinator)
  ↑
1.0 Integration milestone (stable surface)
  ↑
0.9 Physical Power Fabric / MCU stub
  ↑
0.8 Driver sandbox / VFIO-UIO stub
  ↑
0.7 Community hardware profiles
  ↑
0.6 Adaptive scheduling (multi-signal + hysteresis)
  ↑
0.5 Behavior Profiles / telemetry persistence
  ↑
0.4 Resource Envelopes (ADSR control)
  ↑
0.3 Scheduler + Learning Plane (EWMA) + run-demo
  ↑
0.2 Fabric (gRPC + mTLS) + Resource Graph
  ↑
0.1 Discover + RDL Machine Profile
```

Policy: land each layer on `main` so the tree stays runnable; keep a PR (or commit message `feat(0.N): …`) per layer so diffs stay reviewable. See [project preferences](project-preferences.md).

---

## Layer map (code ownership)

| Layer | Role | Primary packages / surfaces |
|---|---|---|
| **0.1** | Discover hardware → describe | `internal/discover`, `internal/rdl`, `omls agent discover` | [note](layers/0.1-discover.md) |
| **0.2** | Multi-node control plane | `internal/fabric` (register/heartbeat/graph), `internal/graph`, `proto/`, `omls master serve\|graph\|health`, `omls agent run` | [note](layers/0.2-fabric.md) |
| **0.3** | Schedule work + learn split | `internal/schedule`, `internal/learn`, `internal/work`, fabric `ClaimWork`/`ReportWork`/`RunDemo`, `omls master run-demo` | [note](layers/0.3-scheduler.md) |
| **0.4** | Shape CPU intensity over time | `internal/envelope`, agent envelope apply, `envelope_yaml` on work units | [note](layers/0.4-envelopes.md) |
| **0.5** | Persist observed behavior | `internal/behavior`, `ListProfiles`/`GetProfile`, master `--data-dir`, `omls master profiles` | [note](layers/0.5-behavior.md) |
| **0.6** | Adaptive multi-signal policy | `internal/learn` adaptive blend + hysteresis, `RunDemoRequest.policy`, `--policy adaptive` | [note](layers/0.6-adaptive.md) |
| **0.7** | Community hardware priors | `internal/community`, `examples/community/`, `omls community`, master `--community-dir` | [note](layers/0.7-community.md) |
| **0.8** | Driver sandbox stub | `internal/sandbox`, RDL `sandbox` class, `omls agent sandbox probe\|list\|claim` | [note](layers/0.8-sandbox.md) |
| **0.9** | Power fabric simulator | `internal/power`, `omls power`, RunDemo envelope→budget | [note](layers/0.9-power.md) |
| **1.0** | Integration milestone | version `1.0.0`, `TestStackSmoke1_0`, stable product surface | [note](layers/1.0-milestone.md) |
| **1.1** | Home cluster bootstrap | `internal/cluster`, `omls daemon`, `omls status\|nodes\|cluster\|graph`, mDNS `_omls._tcp`, elected coordinator over the existing fabric | [note](layers/1.1-cluster.md) |
| **1.2** | Honest join + place functions | `schedule.PlaceExclusive`, fabric `executed`/`place` policy, `joined` vs `discovered` status | [note](layers/1.2-honest-placement.md) |
| **1.3** | RAM reservation on a peer | `internal/cache`, `omls cache serve\|place\|status`, daemon listen `:7444` | [note](layers/1.3-cache.md) |

---

## Dependency rules

1. A higher layer may call a lower layer; never the reverse for core contracts.
2. Missing signals (temp, cgroup, cpufreq) degrade to neutral / warning — never crash.
3. In-memory graph (0.2) remains the live view; disk Behavior Profiles (0.5) are the durable learning prior.
4. Envelopes (0.4) are optional annotations on work; scheduler/learn (0.3/0.6) still decide *where* work goes.
5. Adaptive policy (0.6) may use envelope intensity as a soft cost hint; it does not replace envelope apply on the agent.
6. Community profiles (0.7) are soft priors only — local Behavior observations always win.
7. Sandbox (0.8) never loads kernel modules or binds devices; missing VFIO/UIO is a warning, not a failure.
8. Power fabric (0.9) is simulated software budgets only — no physical MCU control.
9. 1.0 freezes the userspace loop as the supported product surface; Language Plane / real MCU / migration remain future/research.
