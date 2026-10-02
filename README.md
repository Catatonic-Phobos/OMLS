# OMLS

Operational Machine Learning System.

OMLS 0.1 is a local inventory agent. `omls agent discover` reads a Linux node from stock userspace and writes a [Resource Description Language](docs/rdl-v0.1.md) 0.1 document (JSON).

## Scope

| Decision | 0.1 |
| --- | --- |
| Where it runs | Linux stock userspace (`/proc`, `/sys`, `/etc`) |
| Interface | Go command `omls agent discover` |
| License | MIT |
| Output | RDL 0.1 node inventory |

Out of scope for 0.1: kernel changes, Popcorn Linux, KEEP-Up, and anything past discovery (scheduling, model execution, a control plane).

## Build

Requires Go 1.22 or newer on Linux.

```sh
go test ./...
go build -o bin/omls ./cmd/omls
./bin/omls agent discover
```

`--root` reads `proc`, `sys`, and `etc` from another directory. Kernel release and architecture still come from the running kernel, which is the node the agent is executing on. Distribution fields come from `etc/os-release` under that root.

```sh
./bin/omls agent discover --root /path/to/snapshot --output node.json
./bin/omls version
```

The command exits 0 on success, 1 on an inventory error, and 2 on usage errors.
