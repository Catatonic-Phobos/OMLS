# Shared task results and independent cache lifetimes

Each machine owns a private `~/OMLS/sharing`. This is a result cache and task
handoff layer, not a distributed filesystem or pooled physical GPU memory.

```
sharing/
  tmp/<task-id>/          # kernel lease, task metadata, output.part
  cache/graphics/<sha256> # immutable completed content; no default TTL
  cache/compute/<sha256>  # immutable completed content; compute TTL
  manifests/<task-key>.json
  sharing-policy.json
  runtime/omls           # optional per-user executable
```

Existing files outside these managed categories are preserved. In particular,
the earlier `sims4/assets` transfer remains separate from managed result objects.

## Identity and compatibility

Schema-1 task JSON includes `kind`, `operation`, optional `model`, `renderer`,
`renderer_version`, `compatibility`, ordered named SHA-256 `inputs`, and a JSON
object of `parameters`. All required fields are validated. The task key hashes
canonical JSON, including nested parameter objects; JSON member ordering does
not affect identity. Input order and numeric representations remain significant.

The object's filename hashes its actual bytes. Several task manifests can
reference the same content object. The manifest records the complete spec,
content hash, size, creation, last access, hit count and optional expiration.

The caller must describe every varying input, including scene/camera/animation
state for graphics. Compiled GPU results require an appropriately specific
driver/GPU/renderer compatibility string; portable encoded images use their
own portable namespace. A task key that produces different output is rejected,
rather than silently replacing an incompatible result. Declaring a hash does
not verify a producer's external input file: the producer must verify inputs
or consume verified content objects. Untracked external file changes must never
be hidden behind an unchanged task key.

## Use

```
./dist/omls-sharing sharing init
./dist/omls-sharing sharing key --spec task.json
./dist/omls-sharing sharing put --spec task.json --source finished-result
./dist/omls-sharing sharing get --spec task.json --out result
./dist/omls-sharing sharing run --spec task.json --out result -- COMMAND ARGS
./dist/omls-sharing sharing dispatch --node STUDIO --spec task.json --out result -- COMMAND ARGS
./dist/omls-sharing sharing status
./dist/omls-sharing sharing gc
```

For `run` and `dispatch`, `parameters.argv` must contain the exact producer
command and argument array. The producer writes its completed result to
`OMLS_RESULT_PATH`; it also receives `OMLS_SHARING_ROOT`, `OMLS_TASK_KEY`, and
`OMLS_TASK_MANIFEST`. Its stdout/stderr are task logs, not result bytes. All
writers must be closed before it exits successfully. Producers must bound their
own allocations and output size. The built-in `put` streaming path enforces the
object budget and free-space reserve during writes; arbitrary external producer
programs are not disk quotas, and publication checks their size afterwards.

`dispatch` first checks A's local cache. On a miss it uses the private SSH profile
from `connections.env` to invoke B's `<sharing-root>/runtime/omls sharing run`.
B checks its own cache before invoking the producer. B verifies/publishes a
result and streams it through authenticated SSH. A stages, hashes and publishes
its own copy before materializing the consumer's file. A failed transport or
producer cannot publish partial output. The receiver root is local to each
machine; the SSH profile must match that remote root.

`--out -` sends result bytes to stdout and cache metadata to stderr. For files,
the CLI publishes the consumer output using a temporary file and rename.
Commands run with the SSH user's privileges; this interface is for trusted lab
jobs, not an unauthenticated remote-execution service. It reuses the existing SSH
host-key and persistent key setup and opens no new network listener.

## Lifetime, recovery, and pressure

- A task holds a kernel file lease, independent of its stored PID. Begin pins
  input hashes and any existing result for that key against eviction.
- Cancel/failure removes its temporary directory, preserving other valid cache
  results. CLI SIGINT, SIGTERM and SIGHUP cancel the running producer process
  group. `--owner-pid PID` also cancels a local task when its owning application
  exits, comparing process start identity to handle PID reuse. Applications
  must explicitly register ownership; an unrelated game is not auto-attached.
- On reopening a store, live leases survive and abandoned task directories are
  removed, including after forced termination. Invalid/missing-object manifests
  and orphan objects left by a crash between object and manifest publication are
  removed. Object contents are SHA-256 verified before consumption.
- Complete output is fsynced and renamed into its object path, its directory is
  synced, then the manifest is atomically written and synced. No manifest points
  to a partially published result. The producer must stop writing before commit.
- Consumers retain the store lock through integrity verification and reading;
  GC cannot evict their object mid-read. This currently serializes long reads and
  metadata updates on a machine, a deliberate initial throughput limitation.
- Compute TTL defaults to 24 hours **from publication**. Graphics has no default
  TTL. Expired compute results become misses and are collected. Other results
  are evicted by least recent access when the shared byte budget or disk reserve
  requires space; active-task inputs/results stay pinned. If pins prevent making
  room, publication fails clearly instead of evicting them.
- Default managed-result budget: **4 GiB**, minimum filesystem free reserve:
  **1 GiB**. These are stored by `sharing init` in `sharing-policy.json`; explicit
  command flags override policy for that invocation. `init` persists changes.
  The budget counts published objects, not all directories under sharing or
  arbitrary producer temporary allocations. Content objects shared by several
  manifests are counted once within a category.

`sharing maintain` recovers on startup, then collects every minute, reloading
the stored policy. `scripts/systemd/omls-sharing-maintenance.service` runs it as a
user service from `sharing/runtime/omls`; enable it on each machine for login
startup. Existing cluster and audio services do not need to restart. Direct CLI
access also recovers the cache even when maintenance was not running at shutdown.
No clean-shutdown marker is required. Task work itself is not resumed after a
crash; another request can rerun it while preserving previously valid results.

SSH dispatch keeps a task lease alive with heartbeats every 3 seconds. EOF
cancels B immediately; if the network stops delivering data without closing the
channel, B cancels after 25 seconds without a heartbeat. An owning application's
exit cancels A's dispatch and closes that channel. The remote producer process
group is stopped and its temporary directory is removed; previously published
results remain valid. Lease expiry and local owner cancellation are tested.
There is no durable retry/acknowledgement queue yet; after a failed connection,
a new request checks both caches before deciding whether to rerun the work.

## Evidence and remaining Sims work

The live lab test executes gzip compression of an actual Sims package on STUDIO,
verifies the decompressed bytes on A, then reuses A's cache. A second empty client
cache also reuses B's existing result without rerunning the producer. This is
real CPU work and transport, not GPU rendering or proof of game acceleration.
The final validation with heartbeat transport is recorded in
`artifacts/sims4-lab/sharing-lifecycle-validation.json`: remote execution took
3.530 s, a local hit took 0.098 s, and an empty local client reused the peer's
cached result in 2.414 s. The returned data decompressed to the original
6,310,536 bytes with a matching SHA-256. These are task/transfer timings, not FPS.
Maintenance was installed and enabled as a user service on both lab machines.
The primary root reported zero active tasks after completion on both.

The current Sims process remains on the notebook. No game files, mods, saves,
launch settings, graphics drivers, or audio routes were redirected by this layer.
The installed game's expansions/deltas plus mods total about 65.6 GiB, so they
need a separately chosen asset budget and import/invalidation strategy; this
4-GiB result budget is not a promise to keep all those inputs resident in RAM.

An experimental private Mesa Vulkan ICD can enumerate the GTX 1050 and pass
noncoherent buffer uploads, copies, fences, and verified readbacks through a
multiplexed SSH renderer session. It is not installed system-wide. Window
presentation, coherent-memory emulation, full extension compatibility, and game
integration remain incomplete; no remote Sims rendering is claimed. Build
sources and setup instructions are under `render/`.

Upstream references: [Gfxstream](https://github.com/google/gfxstream),
[Mesa Venus](https://docs.mesa3d.org/drivers/venus.html). Their virtualization
transports cannot be treated as network-ready shared memory simply by changing
a socket address; their memory and synchronization requirements need adaptation.
