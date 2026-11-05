# Installation and packaging

## Install from a git checkout

Work in **one** directory (example: `$HOME/OMLS`). Do not nest another clone inside it.

```bash
cd "$HOME/OMLS"
git fetch origin
git reset --hard origin/main
./scripts/install.sh
omls version
```

`scripts/install.sh` builds from source, installs `/usr/local/bin/omls` (+ `omlsd`), enables `omls.service`, and preserves `/var/lib/omls` identity. It may request sudo.

`scripts/install-systemd-services.sh` and `scripts/pack/install.sh` (in a git tree) forward to the same script.

## Field upgrade: `omls update`

```bash
omls update --check
omls update
```

Fetches the latest GitHub Release pack (`omls-linux-<arch>.tar.gz`) and runs its installer. No Go required on the machine.

### SanDisk / first image

If the binary is older than the `update` command, install once from git (`./scripts/install.sh`) or from a release tarball (`tar xzf … && cd omls-linux-amd64 && sudo ./install.sh`). After that, use `omls update`.

## Publishing a GitHub pack

```bash
./scripts/pack-release.sh
# dist/omls-linux-amd64.tar.gz

git tag -a v1.4.1 -m "OMLS 1.4.1"
git push origin v1.4.1
gh release create v1.4.1 dist/omls-linux-amd64.tar.gz \
  --title "OMLS 1.4.1" \
  --notes "Shared task/result cache, authenticated peer storage, and experimental remote Vulkan buffer execution. Application rendering and workload offload remain incomplete."
```

Or upload onto an existing tag:

```bash
gh release upload v1.2.0 dist/omls-linux-amd64.tar.gz --clobber
```

## Future `.deb`

A compiled `.deb` remains the long-term packaging goal: detect hardware, install drivers/runtimes, enable services, preserve identity. The tarball + `omls update` path is the interim field upgrade mechanism.
