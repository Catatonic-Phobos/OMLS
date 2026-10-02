# OMLS Plan — 0.1 → 0.3

Status: **approved base decisions** · ready to implement after review  
Date: 2026-10-02

## Locked decisions

1. **No new kernel.** No Popcorn/Stramash as runtime base. No Linux fork in 0.1–1.0.
2. **Stock Linux userspace:** `omls-agent` + fabric + `omls-master`.
3. **Learning:** statistics / EWMA / regression first; ML models and local LLM later.
4. Kernel path only if userspace hits a proven limit: `userspace → eBPF → module → scheduler hooks → optional fork`.

Vision source: [OMLS Vision](cursor.agent://project/docs/omls-vision.md)

---

## Goal of this slice

Prove the OMLS loop on **two heterogeneous Linux nodes**:

```text
DISCOVER → DESCRIBE → REGISTER → SCHEDULE → OBSERVE → ADJUST
```

without touching kernel code.

Success looks like:

- Each node publishes a **Machine Profile** (RDL).
- Master builds a **Resource Graph**.
- A parallel compute workload is distributed by capability + load + locality.
- After a few runs, allocation shifts from crude defaults toward observed efficiency/thermal behavior (still statistical, not neural).

---

## Out of scope (defer)

| Topic | Earliest |
|---|---|
| Resource Envelopes (attack/peak/sustain/release) | 0.4 |
| Full Behavior Profiles persistence | 0.5 |
| Adaptive scheduling beyond simple stats | 0.6 |
| Community hardware profiles | 0.7 |
| Driver sandbox / VFIO-UIO | 0.8 |
| Physical Power Fabric / MCU | 0.9+ |
| Ollama / Language Plane | after 0.5 infra |
| Popcorn-like cross-ISA process migration | research track only |

---

## Architecture for 0.1–0.3

```text
                 omls-master
                      │
                Resource Graph
                      │
              Fabric (gRPC + mTLS)
                 ┌────┴────┐
                 │         │
           omls-agent  omls-agent
           Node A      Node B
           (Debian)    (Mint / other)
```

**Process model (decision for v0):** one Go module/repo with two commands:

- `omls agent` — local discovery, telemetry, workload worker
- `omls master` — graph, admission, scheduling, learning-plane stubs

Same binary, different roles. Simpler than two repos until the API stabilizes.

---

## OMLS 0.1 — Hardware discovery

### Deliverables

1. Go module `omls` with `cmd/omls`.
2. Local collectors (best-effort, degrade gracefully if missing):
   - CPU: arch, cores, model, freq governors (`/proc`, `/sys/devices/system/cpu`)
   - Memory: total/available (`/proc/meminfo`)
   - Storage: block devices (`/sys/block`, optional lsblk)
   - Network: interfaces + addrs
   - PCI / USB inventory (sysfs)
   - Thermal / hwmon (if present)
   - CPUFreq / powercap RAPL (if present)
   - GPU: detect via PCI + vendor sysfs; **do not** require NVIDIA/AMD SDKs yet
3. **RDL v0** (YAML + JSON export):

```yaml
rdl_version: "0.1"
node:
  id: "<stable-id>"
  hostname: "..."
  os: { family: linux, pretty: "..." }
resources:
  - id: cpu0
    kind: device
    class: compute
    capabilities: [compute, parallelizable]
    attrs: { arch: x86_64, cores: 8 }
  - id: mem0
    kind: device
    class: memory
    capabilities: [memory]
  # ...
transports:
  - id: eth0
    type: ethernet
    attrs: { ... }
limits: []
behavior: []   # empty in 0.1
```

4. CLI: `omls agent discover --out machine-profile.yaml`
5. Stable node ID: machine-id / DBus machine-id / fallback hash of hostname+machine-id.

### Acceptance

- Runs on a normal Linux user session (root only where sysfs requires it; document what needs root).
- Profile validates against a checked-in JSON Schema / Go struct.
- Missing subsystems omit sections with warnings — never crash.

---

## OMLS 0.2 — Multi-node fabric

### Deliverables

1. gRPC service definitions (`proto/`):
   - `Register` / `Heartbeat`
   - `AdvertiseProfile` (push RDL)
   - `StreamTelemetry` (cpu load, temp, mem, optional power)
   - `Health`
2. mTLS with locally generated dev CA + per-node certs (script `scripts/gen-dev-certs.sh`).
3. Agent dials master, registers, re-advertises on change (hotplug later; 0.2 = periodic refresh OK).
4. Master keeps in-memory node + resource registry; dump with `omls master graph --out graph.yaml`.
5. Config: master addr, cert paths, advertise interval.

### Acceptance

- Two hosts join one master: at least one bare-metal/VM Linux (Debian or Mint) plus optional WSL node.
- Graph shows all joined nodes and their compute/memory resources.
- Node drop → resources marked unavailable within heartbeat timeout.
- WSL nodes advertise `virt: wsl` (or equivalent) so the scheduler can apply locality/capability penalties later.

---

## OMLS 0.3 — Resource Graph scheduler + first learning loop

### Deliverables

1. **Function request** API (minimal):

```text
FUNCTION: parallel_workers
requires: [compute, parallelizable]
workers: N
```

2. Scheduler v0 scoring:
   - capability match (hard filter)
   - available workers / load
   - locality (local preferred when master colocated; else lower Ethernet penalty constant)
   - optional thermal ceiling if temp known
3. Agent **worker pool** that runs a synthetic CPU-bound task (or user command wrapper) and reports duration + samples.
4. **Learning Plane v0** (in-process, no ML libs required):
   - EWMA of task duration per node
   - EWMA of temperature delta under load
   - adjust next worker split (e.g. 6/2 → 7/1) under simple rules + clamps
5. CLI demo: `omls master run-demo --workers 8` prints allocation before/after N iterations.

### Acceptance

- Demo across unequal nodes (e.g. Debian vs Mint vs WSL) changes allocation toward the stronger/cooler / lower-latency node.
- If the strong node heats past threshold, workers rebalance.
- WSL-only constraints (limited thermal/PCI) must not crash the learning loop — missing signals = neutral weight.
- All policy changes logged with reasons (explainable; no black box).

---

## Repo layout (proposed)

```text
omls/
  cmd/omls/
  internal/
    discover/
    rdl/
    fabric/
    graph/
    schedule/
    learn/
    telemetry/
  proto/
  schemas/
  scripts/
  docs/
  README.md
```

**Repo location:** **new dedicated git repo** (locked). KEEP-Up is unrelated company infrastructure — OMLS must not live inside that workspace.

---

## Implementation sequence

```text
Week focus A: 0.1 discover + RDL schema + CLI
Week focus B: 0.2 proto + mTLS + register/graph
Week focus C: 0.3 scheduler + demo workload + EWMA loop
```

Parallelizable after 0.1 schema freeze: proto stubs can start while collectors expand.

---

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Sysfs variance across distros | Capability probes; golden fixtures from recorded sysfs trees |
| GPU detection incomplete | PCI class only in 0.1; vendor hooks later |
| mTLS friction in lab | Dev CA script; insecure localhost flag **dev-only** |
| Over-scoping into envelopes/power | Hard freeze: envelopes = 0.4+ |
| Temptation to pull Popcorn early | Explicit non-goal until fabric proves insufficient for *dispatch* (not shared memory) |

---

## Open questions

1. ~~Repo new vs KEEP-Up?~~ → **new dedicated repo** (locked).
2. ~~Repo name + GitHub?~~ → https://github.com/Catatonic-Phobos/OMLS (locked).
3. ~~First lab nodes?~~ → **Debian + Linux Mint + Windows WSL** (locked). Treat WSL as a first-class Linux node with noted limits (no real PCI passthrough by default; weaker hwmon/powercap).
4. ~~License?~~ → **MIT** for experimental phase; revisit formal legal posture later if the project sticks.

---

## Next action after plan approval

Scaffold the Go module and land **0.1 discover → Machine Profile** end-to-end on one Linux host.
