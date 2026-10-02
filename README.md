# OMLS

**Operational Machine Learning System** — stock Linux userspace control plane that discovers hardware, describes it as a Machine Profile (RDL), and (in later releases) coordinates resources across nodes.

This repository is **independent** of other company stacks. OMLS 0.1 is discovery + RDL only.

## Status

| Version | Scope |
|---|---|
| **0.1** (this tree) | `omls agent discover` → Machine Profile (RDL v0) |
| 0.2 | Multi-node fabric (gRPC + mTLS) — not started |
| 0.3 | Resource Graph scheduler + first statistical learning loop — not started |

No kernel fork. No Popcorn. Userspace on stock Linux only.

## Build

Requires Go 1.22+.

```bash
go build -o omls ./cmd/omls
```

## Discover

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

## Root vs non-root

Most of 0.1 works as a normal user:

- **Usually readable without root:** `/proc/*`, many `/sys/class/net`, `/sys/block` size/attrs, `/etc/machine-id`, PCI vendor/device/class, basic USB ids.
- **May need root or capabilities on some hosts:** certain hwmon/powercap energy counters, DMI product fields, some device model strings under `/sys/block/*/device/`.

If a path is unreadable, OMLS omits that detail and records a warning. Running as root is optional for a useful profile; it is not required to start.

## RDL v0

Schema: [`schemas/rdl-v0.schema.json`](schemas/rdl-v0.schema.json)  
Go types + validation: [`internal/rdl`](internal/rdl)

```yaml
rdl_version: "0.1"
node:
  id: "<stable-id>"
  hostname: "..."
  os: { family: linux, pretty: "..." }
  virt: bare   # or wsl, kvm, ...
resources: []
transports: []
limits: []
behavior: []   # empty in 0.1
```

## Test

```bash
go test ./...
```

## License

MIT — see [LICENSE](LICENSE). Experimental phase; legal posture may be revisited later.
