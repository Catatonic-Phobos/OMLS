# OMLS

**Operational Machine Learning System** — stock Linux userspace control plane that discovers hardware, describes it as a Machine Profile (RDL), and coordinates resources across nodes over a gRPC fabric.

This repository is **independent** of other company stacks.

## Status

`main` is always the **cumulative functional stack**. Versions are additive **layers** — see [`docs/omls-layers.md`](docs/omls-layers.md).

| Version | Scope | Layer note |
|---|---|---|
| **0.1** | `omls agent discover` → Machine Profile (RDL v0) | base |
| **0.2** | Multi-node fabric (gRPC + mTLS) + in-memory Resource Graph | |
| **0.3** | Resource Graph scheduler + EWMA learning demo (`run-demo`) | [delivery](docs/layers/0.3-scheduler.md) |
| **0.4** | Resource Envelopes (attack/peak/sustain/release) | [delivery](docs/layers/0.4-envelopes.md) |
| **0.5** | Behavior Profiles + telemetry persistence | [delivery](docs/layers/0.5-behavior.md) |
| **0.6** | Adaptive multi-signal scheduling + hysteresis | [delivery](docs/layers/0.6-adaptive.md) |
| **0.7** | Community hardware profiles + priors | [delivery](docs/layers/0.7-community.md) |
| **0.8** | Driver sandbox / VFIO-UIO userspace stub | [delivery](docs/layers/0.8-sandbox.md) |
| **0.9** | Power Fabric / MCU protocol simulator | [delivery](docs/layers/0.9-power.md) |
| **1.0** | Integration milestone — stable adaptive stack | [delivery](docs/layers/1.0-milestone.md) |
| **1.1** (this tree) | Cluster autodiscovery — `omlsd`, LAN membership, elected coordinator | [delivery](docs/layers/1.1-cluster.md) |

No kernel fork. No Popcorn. Userspace on stock Linux only. Version string: **`1.1.0`**.

## Build

Requires Go 1.22+.

```bash
go build -o omls ./cmd/omls
```

Regenerate gRPC stubs (optional; checked in):

```bash
./scripts/gen-proto.sh
```

## Discover (0.1)

```bash
./omls agent discover --out machine-profile.yaml
# JSON also works:
./omls agent discover --out machine-profile.json
```

Collectors (best-effort; missing pieces become **warnings**, never a crash):

| Area | Sources |
|---|---|
| Node ID | `/etc/machine-id`, else D-Bus machine-id, else hostname hash |
| OS / virt | `/etc/os-release`, `/proc/version`; tags `wsl`, `kvm`, `docker`, `bare`, … |
| CPU | `/proc/cpuinfo`, `/sys/devices/system/cpu` (CPUFreq governors when present) |
| Memory | `/proc/meminfo` |
| Storage | `/sys/block` |
| Network | `/sys/class/net` + addresses |
| PCI / GPU | `/sys/bus/pci/devices` — GPUs via PCI display class `03xxxx` only (no vendor SDKs) |
| USB | `/sys/bus/usb/devices` |
| Thermal | `/sys/class/thermal`, `/sys/class/hwmon` |
| Power | `/sys/class/powercap` (RAPL) when present |

WSL nodes are tagged `node.virt: wsl`. Limited PCI / hwmon / powercap on WSL is expected; discovery continues with warnings.

## Fabric (0.2)

Two processes, one binary:

```text
omls master serve   ← Resource Graph
omls agent run      ← discover + register + heartbeat
```

### Dev certs (mTLS)

```bash
./scripts/gen-dev-certs.sh ./dev-certs
```

### Local lab (plaintext, `--insecure` only)

```bash
# terminal 1
./omls master serve --listen 127.0.0.1:7443 --insecure --data-dir ./omls-data

# terminal 2 (node A)
./omls agent run --master 127.0.0.1:7443 --insecure

# terminal 3 (operator)
./omls master health --master 127.0.0.1:7443 --insecure
./omls master graph --master 127.0.0.1:7443 --insecure --out graph.yaml
```

### mTLS (preferred)

```bash
./omls master serve --listen 0.0.0.0:7443 \
  --ca dev-certs/ca.crt --cert dev-certs/master.crt --key dev-certs/master.key

./omls agent run --master master.example:7443 \
  --ca dev-certs/ca.crt --cert dev-certs/agent.crt --key dev-certs/agent.key \
  --server-name localhost

./omls master graph --master master.example:7443 \
  --ca dev-certs/ca.crt --cert dev-certs/master.crt --key dev-certs/master.key \
  --server-name localhost --out graph.yaml
```

`--insecure` disables TLS entirely. Lab / localhost only.

Heartbeat silence beyond `--heartbeat-timeout` (default 15s) marks the node **unavailable** in the graph; the profile is retained.

## Scheduler demo (0.3)

With a master and one or more `omls agent run` processes:

```bash
# terminal 1
./omls master serve --listen 127.0.0.1:7443 --insecure

# terminal 2 (+ optional more nodes)
./omls agent run --master 127.0.0.1:7443 --insecure

# terminal 3
./omls master run-demo --master 127.0.0.1:7443 --insecure --workers 8 --iterations 3
```

The demo schedules `parallel_workers` across available compute nodes, agents burn CPU for each work unit, and the Learning Plane (EWMA of duration / temp delta) adjusts the next split with printed reasons. Missing thermal signals stay neutral. Rebalancing needs ≥2 available nodes.

### GPU work (serial per agent)

`run-demo --device gpu` schedules work only onto graphics devices that the agent can open through an OpenCL GPU runtime. It sends one work unit per GPU and each agent executes its queue sequentially in discovered GPU order. PCI discovery alone does not make a GPU schedulable. On apt-based systems, OMLS automatically installs `mesa-opencl-icd` when it detects an Intel or AMD GPU without the Mesa runtime, enables the matching Rusticl driver, and repeats discovery. The package manager may request administrator authentication. Verify `compute_backend: opencl` in the profile before starting the demo.

```bash
./omls agent discover --out machine-profile.yaml
# the agent also performs GPU runtime setup before registering
./omls agent run --master 127.0.0.1:7443 --insecure
./omls master run-demo --master 127.0.0.1:7443 --insecure \
  --device gpu --workers 2 --iterations 3 --work-iterations 30000000
```

GPU work is a synthetic OpenCL integer workload. Increase `--work-iterations` for more work per device. Resource Envelopes are not supported in GPU mode yet.

## Cluster (1.1)

One resident process per computer. It discovers hardware, keeps a stable `node_id`, and finds other OMLS nodes on the LAN with mDNS (`_omls._tcp.local`). No master address and no manual join.

```bash
omls daemon
# another terminal, same machine or any peer
omls status
omls nodes
omls cluster
omls graph
omls run-demo --workers 4 --iterations 2 --policy adaptive
```

`omls master serve` and `omls agent run --master HOST:PORT` stay available for debugging. Normal use does not need them. The daemon elects a temporary coordinator (lowest ready `node_id`). If that machine disappears, another node takes the role and the existing scheduler sees the nodes that are still up.

Identity is stored in the daemon data directory (`~/.local/share/omls/identity.yaml`, or `/var/lib/omls` for the systemd unit) and is reused after reboot.

### Start OMLS at boot with systemd

The repository currently has no `.deb` installer. For this checkout, the setup script builds OMLS, installs the Intel/AMD Mesa OpenCL runtime if needed, and enables `omls.service` at boot. The unit runs as the invoking user. Package and service installation may request administrator authentication.

```bash
./scripts/install-systemd-services.sh
systemctl status omls
journalctl -u omls -f
omls status
omls nodes
```

The older `omls-master` and `omls-agent` units are disabled by that script so they do not bind the same port. Their unit templates remain for manual lab use.

### Update from GitHub (`omls update`)

Field machines (USB stick, home PCs) can upgrade without cloning the repo:

```bash
omls update --check
omls update
```

This downloads the latest `omls-linux-<arch>.tar.gz` from [GitHub Releases](https://github.com/Catatonic-Phobos/OMLS/releases) and runs the pack installer. Node identity is preserved. Releases must attach that tarball — build it with `./scripts/pack-release.sh`. A SanDisk image that predates this command needs **one** manual binary/pack install first; after that, `omls update` is enough. Details: [installation.md](docs/installation.md).

**Release requirement:** the future compiled `.deb` installer must inspect the systems OMLS detects and automatically install and activate every required supported driver, library, service, and system configuration—not only GPU components. It must also install and enable the OMLS service(s) so they start at boot, while preserving per-machine configuration. See [installation and packaging requirements](docs/installation.md).

## Resource Envelopes (0.4)

Workloads can declare an ADSR-style **Resource Envelope** instead of only “run at 100%”:

```bash
./omls agent envelope show --preset eco
./omls agent envelope validate --file examples/envelopes/high-burst.yaml
./omls agent envelope apply --preset eco --duration 2s

# attach envelope to the scheduler demo
./omls master run-demo --master 127.0.0.1:7443 --insecure --preset eco --workers 4
```

Backends (best-effort, never crash if missing):

| Backend | Mechanism |
|---|---|
| `duty-cycle` | userspace pace/sleep (always available) |
| `cgroup-cpu` | cgroup v2 `cpu.max` when writable |
| `cpufreq` | `scaling_max_freq` when writable |

If temperature crosses `limits.thermal_max_c`, the controller clamps the level and records a warning.

## Behavior Profiles (0.5)

The master persists per-node **Behavior Profiles** and a short telemetry history under `--data-dir` (default `omls-data/`):

```text
omls-data/
  profiles/<node>.yaml     # EWMA duration/temp, confidence, recent samples
  telemetry/<node>.jsonl   # append-only heartbeat/stream history
```

Heartbeats and `StreamTelemetry` append samples; `run-demo` ObserveWork updates duration/temp-delta EWMAs and seeds the Learning Plane on the next demo.

```bash
./omls master serve --listen 127.0.0.1:7443 --insecure --data-dir ./omls-data
# … agents + optional run-demo …

./omls master profiles list --master 127.0.0.1:7443 --insecure
./omls master profiles show --master 127.0.0.1:7443 --insecure --node <node-id>
./omls master profiles show --master 127.0.0.1:7443 --insecure --node <node-id> \
  --out behavior.yaml --telemetry-out samples.jsonl --telemetry-tail 50
```

gRPC: `ListProfiles` / `GetProfile`. Confidence grows with samples (`1 - e^(-n/20)`).

## Adaptive scheduling (0.6)

`run-demo` can use the legacy EWMA policy or a multi-signal **adaptive** policy:

```bash
./omls master run-demo --master 127.0.0.1:7443 --insecure \
  --workers 8 --iterations 5 --policy adaptive --preset eco
```

Adaptive blends duration EWMA, load, temperature headroom, locality/virt, optional envelope intensity, and spare capacity. After a blend move it holds for a cooldown (default 2 rounds) so allocation does not thrash; thermal escape still overrides cooldown. Policy notes print per-node scores.

## Community profiles (0.7)

Shared YAML snippets under `examples/community/` (or `community/` / `profiles/`) carry match heuristics and soft EWMA priors with provenance:

```bash
./omls community list --dir examples/community
./omls community show amd64-wsl --dir examples/community
./omls community import examples/community/amd64-kvm-generic.yaml --dir ./community

./omls master serve --listen 127.0.0.1:7443 --insecure \
  --data-dir ./omls-data --community-dir examples/community
```

When an agent advertises its Machine Profile, the master best-effort matches arch/virt/model and seeds the Behavior store **only if** that node has no local observations yet.

## Driver sandbox (0.8)

Probe for VFIO/UIO and simulate sandbox claims (never binds real drivers):

```bash
./omls agent sandbox probe
./omls agent sandbox list
./omls agent sandbox claim sandbox0   # only if a backend was detected
./omls agent discover --sandbox --out machine-profile.yaml
```

Typical cloud VMs print `backend=none` with a warning — that is expected and non-fatal.

## Power Fabric (0.9)

In-process MCU simulator with rails and soft budgets (no physical hardware):

```bash
./omls power show
./omls power hello
./omls power budget --watts 40 --consumer lab
./omls power budget --preset eco --consumer demo
```

When `run-demo` is given `--preset` / `--envelope`, the master attaches a soft power budget derived from envelope intensity and mentions it in the policy note.

## 1.0 milestone

`omls version` / fabric Health report `1.1.0` on this tree (1.0.0 was the integration milestone). The supported loop is:

```text
DISCOVER → DESCRIBE → REGISTER → SCHEDULE → ENVELOPE → OBSERVE → ADJUST
         (+ community priors, sandbox probe, power budgets)
```

In-process smoke: `go test ./internal/fabric/ -run TestStackSmoke1_0`.

**Documented future (not required):** Language Plane / Ollama, real MCU switching, Popcorn-like migration, real VFIO bind. See [`docs/layers/1.0-milestone.md`](docs/layers/1.0-milestone.md).

## Root vs non-root

Most of 0.1/0.2 works as a normal user:

- **Usually readable without root:** `/proc/*`, many `/sys/class/net`, `/sys/block` size/attrs, `/etc/machine-id`, PCI vendor/device/class, basic USB ids.
- **May need root or capabilities on some hosts:** certain hwmon/powercap energy counters, DMI product fields, some device model strings under `/sys/block/*/device/`.

If a path is unreadable, OMLS omits that detail and records a warning. Running as root is optional for a useful profile; it is not required to start.

## RDL v0

See [`schemas/rdl-v0.schema.json`](schemas/rdl-v0.schema.json) and [`docs/omls-plan-0.1-0.3.md`](docs/omls-plan-0.1-0.3.md).

## Docs

- [`docs/omls-layers.md`](docs/omls-layers.md) — operational layer hierarchy (0.1→1.0)
- [`docs/layers/`](docs/layers/) — per-layer delivery notes
- [`docs/omls-vision.md`](docs/omls-vision.md) — full architecture vision
- [`docs/omls-plan-0.1-0.3.md`](docs/omls-plan-0.1-0.3.md) — implementation plan for 0.1–0.3
- [`docs/omls-handoff-catatonic.md`](docs/omls-handoff-catatonic.md) — handoff notes

## License

MIT — see [`LICENSE`](LICENSE).
