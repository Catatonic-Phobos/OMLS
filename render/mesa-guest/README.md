# Experimental local Vulkan ICD

This is a private, opt-in Mesa Gfxstream frontend for the OMLS SSH graphics
session. It currently supports headless queries and explicit noncoherent buffer
transfers. It is not ready for games or windowed presentation.

## Inputs and build

- Mesa source: `38377311965f94287e6b90f6b264861582cb5428` under
  `sharing/development/mesa`.
- `mesa.patch`: transport, private staging memory, range validation and
  per-thread stream lifetime changes. It is scoped to `src/gfxstream`.
- Meson 1.12.1 is installed privately under
  `sharing/development/python-packages`; Mako and packaging come from this
  machine's existing Python installation.
- The lab extracted Ubuntu's `libdrm-dev` 2.4.125 into
  `sharing/development/deps` using `apt-get download` and `dpkg-deb -x`. Its
  pkg-config prefix points there and its library symlinks resolve to the
  already-installed runtime. No system package/driver was replaced.

```
bash render/mesa-guest/setup.sh /home/phobos/OMLS
```

The script verifies the source revision, applies the patch only when it is not
already applied, configures a headless-only build, and builds with one worker
at reduced scheduling priority. It does not use `git reset` or install Mesa
globally. Mesa's local AGENTS.md instructions apply to source modifications;
no upstream merge request or commit was created.

## Use

```
python3 render/mesa-guest/run-headless.py --expect "GTX 1050" -- vulkaninfo --summary
```

The wrapper starts `dist/omls-render render session`, waits for the hardware
handshake, sets `OMLS_RENDER_SOCKET` and the private ICD path for its child only,
then closes the session when the child exits. The wrapper registers itself as
the session owner so an unexpected wrapper exit also cancels the session.
`--trace` enables native decoder call logging on STUDIO. `--timeout SECONDS`
bounds the local application runtime. No Steam application is automatically
restarted and no system Vulkan environment is changed.

The transport is a Linux Unix socket carrying the native wire stream per
thread. The OMLS relay supplies channel framing over one authenticated SSH
connection to the host adapter. The host registers one shared graphics-process
namespace, while the guest releases a thread's encoders and stream on TLS
destruction. This prevents completed worker threads from retaining all 64
channel slots.

Network memory is private local staging RAM. Host-visible types are advertised
as noncoherent and explicit flush/invalidate copies the bytes. The patch also
checks mapped-range arithmetic without overflow and handles `VK_WHOLE_SIZE`
relative to nonzero offsets. Coherent mapping, external fd interop, WSI and
arbitrary engine compatibility are not implemented.
Window/presentation and external-handle extensions inherited from upstream are
masked in the experimental mode, so applications do not see unsupported local
fd/import/export capabilities as available.

## Real-asset validation

```
c++ -std=c++17 -O2 -pthread -Isharing/development/mesa/include \
  render/mesa-guest/transfer-file.cpp -l:libvulkan.so.1 -o dist/omls-vulkan-transfer
python3 render/mesa-guest/run-headless.py --expect "GTX 1050" --trace -- \
  ./dist/omls-vulkan-transfer INPUT_ASSET OUTPUT_READBACK --offset-whole --threaded --churn
```

`transfer-file` deliberately bounds input to 32 MiB. It creates Vulkan buffers,
uploads real file bytes, records a GPU buffer copy, submits it and waits for a
fence, downloads and compares the result, then frees its Vulkan resources.
`--threaded` submits work from a different thread than resource creation.
`--churn` creates 80 temporary query threads to test channel reclamation.
The overflow validation check must fail before any invalid range reaches the
host. This validates memory/transport and shared handles, not rasterization or
game rendering. Evidence is under `artifacts/render-integration/`.
