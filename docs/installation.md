# Installation and packaging requirements

## Current checkout

There is no `.deb` release yet. On apt-based Linux, run `scripts/install-systemd-services.sh` from the repository to build the binary, install the Mesa OpenCL runtime when an Intel or AMD GPU needs it, install the systemd unit files, and enable/start the cluster daemon (`omls.service`). This is the current bootstrap path; its dependency setup is limited and it may request administrator authentication.

## Field upgrade: `omls update`

Machines that already have an OMLS binary with the `update` command can pull the latest **prebuilt pack** from GitHub Releases and install it without cloning the repo or installing Go:

```bash
omls update --check    # report only
omls update            # download + install + restart omls.service
```

Flow:

```text
omls update
  → GET https://api.github.com/repos/Catatonic-Phobos/OMLS/releases/latest
  → download omls-linux-<arch>.tar.gz
  → run pack install.sh
  → /usr/local/bin/omls + omls.service
```

Node identity under `/var/lib/omls` (or `~/.local/share/omls` for a manual daemon) is preserved. The pack installer stops/restarts `omls.service` when systemd is present.

### SanDisk / one-time bootstrap

USB images that only have an older “basic” binary **without** `omls update` need a single manual refresh:

1. On a build machine: `./scripts/pack-release.sh`
2. Copy `dist/omls-linux-amd64.tar.gz` to the stick (or copy the `omls` binary from the pack)
3. On the target: extract and run `./install.sh`, or replace `/usr/local/bin/omls` with the new binary

After that, `omls update` keeps the machine current.

## Publishing a GitHub pack (maintainers)

Releases must attach a downloadable asset or `omls update` fails with “no downloadable packs”.

```bash
./scripts/pack-release.sh
# produces:
#   dist/omls-linux-amd64.tar.gz
#   dist/omls-linux-amd64.tar.gz.sha256
```

Create or edit the GitHub Release (tag should match the binary version, e.g. `v1.1.0`) and **upload** `omls-linux-amd64.tar.gz` as a release asset. Mark it as the latest release so `omls update` finds it.

Example with GitHub CLI (after tagging):

```bash
git tag -a v1.1.0 -m "OMLS 1.1 pack"
git push origin v1.1.0
./scripts/pack-release.sh
gh release create v1.1.0 dist/omls-linux-amd64.tar.gz \
  --title "OMLS 1.1 — cluster + update pack" \
  --notes "Prebuilt linux/amd64 pack for omls update. Includes install.sh and omls.service."
```

If a release already exists without assets:

```bash
gh release upload v1.1.0 dist/omls-linux-amd64.tar.gz --clobber
```

Note: an older empty release marked “Latest” (for example `v1.0.0` with no assets) blocks field updates until a newer release with a pack is published, or until that release gains the tarball.

## Future compiled `.deb`

The released installer must inspect the machine through OMLS discovery and make a normal package install sufficient to activate the supported systems that OMLS detects and needs. This requirement applies to every detected subsystem, not just GPUs. The package workflow must:

1. Install the compiled OMLS binary and all required packages for detected, supported hardware and software systems. This includes applicable kernel drivers, userspace drivers, libraries, runtimes, firmware, and supporting tools.
2. Configure and activate those systems: load or enable drivers, set required system configuration, enable relevant services, and initialize each supported subsystem OMLS relies on.
3. Install and enable the appropriate OMLS systemd unit or units, then start them after installation. The agent must start after boot and reconnect to its configured master, restarting after failures.
4. Preserve machine-specific configuration across package upgrades. In single-machine mode, the local master must listen on loopback; joining a remote fabric must use the configured master address and credentials.
5. Support the package manager and driver packages for each supported distribution and hardware vendor instead of assuming that all machines use apt or the same driver package. If a required change cannot be applied without a reboot or unavailable privilege, complete the safe setup steps and clearly report the remaining activation step.

Package lifecycle scripts must be safe to rerun during upgrades and must not overwrite existing node identity, master address, or TLS credentials. Detection, dependency installation, system configuration, and service activation must be idempotent. The package must expose service status and logs through standard `systemctl` and `journalctl` commands.

The current repo script is a limited bootstrap for a local node machine; it currently handles the Intel/AMD Mesa OpenCL dependency and systemd services, not every detected subsystem. It does not yet build or publish a Debian package. The tarball pack + `omls update` path is the interim field upgrade mechanism.
