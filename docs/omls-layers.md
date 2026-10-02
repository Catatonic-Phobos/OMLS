# OMLS operational layers

`main` is always the **cumulative functional stack**. Each version below is a **layer**: additive capability with a clear ownership boundary. Layers can be revisited or swapped later without rewriting the whole system.

```text
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

---

## Dependency rules

1. A higher layer may call a lower layer; never the reverse for core contracts.
2. Missing signals (temp, cgroup, cpufreq) degrade to neutral / warning — never crash.
3. In-memory graph (0.2) remains the live view; disk Behavior Profiles (0.5) are the durable learning prior.
4. Envelopes (0.4) are optional annotations on work; scheduler/learn (0.3/0.6) still decide *where* work goes.
5. Adaptive policy (0.6) may use envelope intensity as a soft cost hint; it does not replace envelope apply on the agent.
6. Community profiles (0.7) are soft priors only — local Behavior observations always win.
7. Sandbox (0.8) never loads kernel modules or binds devices; missing VFIO/UIO is a warning, not a failure.
