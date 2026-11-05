# Remote graphics backend integration

The application process stays on the coordinator; the target executor in this
lab is STUDIO's GTX 1050. This is graphics API remoting, not moving the whole
application to STUDIO. Audio routing and clock ownership are unchanged.

## Implemented host boundary

`render/gfxstream-host/main.cpp` initializes the native Gfxstream host library,
selects an explicit Vulkan ICD, queries the device the decoder actually chose,
and refuses a CPU device or mismatched requested device name. The adapter offers:

* `--probe`: structured hardware/backend readiness information, with application
  ICD and presentation readiness explicitly false.
* `--stdio`: one native Gfxstream command/reply channel over stdin/stdout. An
  OMLS CLI SSH command provides authentication and transport. Upstream logging
  is redirected away from protocol stdout. Pipe/channel backpressure bounds
  the adapter's chunk buffers; no image data or commands are synthesized.
* `--mux`: multiple channels share one initialized renderer and one graphics
  process namespace. The authenticated SSH connection carries bounded `OMGR/1`
  frames; one private local Unix socket accepts the guest's per-thread streams.

Channels accept the native wire protocol, not arbitrary Vulkan function names,
JSON drawing commands or screenshots. The local experimental Mesa ICD now
provides compatible calls through a Unix socket and the multiplexed session.
This is not yet a complete windowed Vulkan implementation.

```
./dist/omls-render render probe --node STUDIO --expect "GTX 1050"
./dist/omls-render render channel --node STUDIO
./dist/omls-render render session --node STUDIO --expect "GTX 1050"
python3 render/mesa-guest/run-headless.py --expect "GTX 1050" -- vulkaninfo --summary
```

`probe` is an initialization test, not a rendering benchmark. `channel` is a
development interface for a protocol-compatible frontend; it is not a game
launcher. The CLI refuses unsupported launch commands rather than silently
falling back to local rendering and reporting remote success.

The live STUDIO validation initialized the decoder on `NVIDIA GeForce GTX 1050`
with driver `580.178.04`. A native `rcGetRendererVersion` request (opcode 10000)
sent through SSH returned version 1 and the channel closed successfully. Raw
results and diagnostics are stored under `artifacts/render-integration/`.
`device_local_heap_bytes` is the sum of device-local heaps reported by Vulkan;
it is not VRAM occupancy or a substitute for utilization telemetry.

## Validated guest path

The client source is Mesa `38377311965f94287e6b90f6b264861582cb5428`, built
privately with the reviewed patch under `render/mesa-guest/`. System ICDs and
Steam launch settings are unchanged. The local `vulkaninfo --summary` exited
successfully and reported `Goldfish GFXStream (NVIDIA GeForce GTX 1050)`.

An actual 6,310,536-byte Sims asset was uploaded into host-visible Vulkan
memory, copied between buffers by a remotely submitted `vkCmdCopyBuffer`, then
invalidated/read back to the local application. Returned SHA-256 matches the
original. Separate runs verified nonzero offset + `VK_WHOLE_SIZE`, rejection of
overflowing mapped ranges, and command recording/submission on different
threads/channels. A further run created 80 temporary client threads, beyond the
64-live-channel limit, to validate channel reuse and thread cleanup.

Source-level traces on STUDIO contain the actual `vkCmdCopyBuffer`,
`vkQueueSubmit` and `vkInvalidateMappedMemoryRanges` decoder calls. These are
GPU buffer transfer validations, not rendered game frames or FPS benchmarks.
The measured upload/copy/readback intervals were about 3–6 seconds for 6.31 MB;
that includes transport, synchronization and optional tracing, and must not be
presented as interactive rendering performance.

The network client uses private anonymous staging RAM and explicit Vulkan
flush/invalidate serialization. It removes `HOST_COHERENT` from exposed memory
types instead of falsely claiming physical coherence across machines. Full
coherent-memory semantics remain to be implemented. Applications requiring
coherent host-visible memory may therefore be incompatible. The existing
`application_icd_ready` / `presentation_ready` fields remain false until the
complete application path is supported; they are not a claim that the new
experimental headless ICD file does not exist.

`OMGR/1` frames have a 16-byte little-endian header (`OMGR`, uint16 version,
uint16 type, uint32 channel, uint32 payload length). Payloads are capped at
1 MiB, live channels at 64, and per-channel reply queues at four frames.
Open/data/close and replies are demultiplexed on the coordinator. Heartbeats
are sent every three seconds, the coordinator expires an unresponsive session
after about 25 seconds, and the native host applies 30-second idle/I/O waits.
The watchdog runs independently of the potentially blocked data writer.
Normal socket closure releases a channel; application owner exit or session
cancellation closes all channels and aborts its leased temporary task.

## Build and dependencies

Gfxstream upstream checkout is under `sharing/development/gfxstream`. The
inspected upstream revision was `94240f814c0097a63027ae680ea4bec7b0cb25f7`.
The decoder ABI and future frontend must use a compatible pinned revision.
This external source tree is excluded from the OMLS repository via `sharing/`.

```
bash render/gfxstream-host/setup.sh "$HOME/OMLS"
```

The setup script builds all three decoders (GLES, Vulkan, composer); the checked
revision's Vulkan-only build refers to a missing `gles_compat.h`. Vendor X11,
XCB, OpenGL and DRM include paths are supplied explicitly, avoiding changes to
system headers or drivers. If system Meson is unavailable, a pure Python
`mesonbuild` package can be placed under `sharing/development/tools/`. It is
used by both configure and Ninja's regeneration commands. The native binary
is atomically installed at `sharing/runtime/omls-gfxstream-host`; the backend
shared library remains in the development build with an explicit runpath.

The lab build runs on STUDIO with two compilation workers and reduced scheduling
priority. No global Vulkan ICD, Steam launch configuration, audio configuration,
or existing OMLS daemon executable is replaced by this build.

## Required before testing an application

1. The experimental headless client needs broader Vulkan feature/extension
   validation before it can be considered generally application-compatible.
2. Per-thread channels share a session and namespace now; further stress and
   synchronization testing is needed for complex rendering engines.
3. Explicit noncoherent uploads/readbacks are implemented. Coherent mapping,
   external memory and external synchronization still need network-compatible
   implementations. A file descriptor or CPU pointer from STUDIO cannot be
   mapped as local shared memory on the notebook.
4. WSI/swapchain presentation must return the remote frame to the application's
   local window with bounded queues. The present adapter does not implement
   that path. Frame encoding and decoding cost must be measured.
5. A test must demonstrate commands and rendering on STUDIO, a locally running
   application process, frames displayed locally, and measured frame times.
   Hardware enumeration or a copied screenshot does not satisfy that test.

There is no claim of a working remote Sims renderer at this stage. Saving or
restarting a game does not remove these missing implementation requirements.

## Relationship to the persistent result cache

Reusable work such as a compatible shader compilation or texture preparation
can use `sharing/cache/graphics` and task manifests. Dynamic frames are not
reused simply because a filename or texture has been seen before: the complete
relevant state would need to match. Do not write every transient frame into the
persistent cache by default. The renderer's live VRAM resources, its bounded
frame-delivery queues, and the persistent task-result cache have different
lifetimes and budgets.

References: [Gfxstream source and build](https://github.com/google/gfxstream),
[Mesa Venus memory requirements](https://docs.mesa3d.org/drivers/venus.html),
[Proton DXVK](https://github.com/ValveSoftware/dxvk).
