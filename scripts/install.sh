#!/usr/bin/env bash
# Install OMLS from a git checkout OR from a release pack.
# Usage (either works):
#   ./scripts/install.sh
#   ./install.sh          # inside extracted omls-linux-*.tar.gz
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
service_user="${OMLS_SERVICE_USER:-$(id -un)}"

run_root() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

resolve_repo() {
  local d="$here"
  local i
  for i in 1 2 3 4; do
    if [[ -f "$d/go.mod" && -d "$d/cmd/omls" ]]; then
      printf '%s\n' "$d"
      return 0
    fi
    d="$(dirname "$d")"
  done
  return 1
}

bin=""
unit=""
repo=""

if repo="$(resolve_repo)"; then
  echo "omls install: source tree at $repo"
  if ! command -v go >/dev/null 2>&1; then
    echo "omls install: go not found; install Go 1.22+ or use the GitHub release pack" >&2
    exit 1
  fi
  echo "omls install: building…"
  (cd "$repo" && go build -o "$repo/omls" ./cmd/omls)
  bin="$repo/omls"
  unit="$repo/scripts/systemd/omls.service.in"
elif [[ -x "$here/omls" ]]; then
  echo "omls install: using pack binary $here/omls"
  bin="$here/omls"
  if [[ -f "$here/omls.service.in" ]]; then
    unit="$here/omls.service.in"
  fi
else
  echo "omls install: no source tree and no pack binary next to this script" >&2
  echo "  expected git checkout (go.mod + cmd/omls) or release pack with ./omls" >&2
  exit 1
fi

if [[ -z "${unit:-}" || ! -f "$unit" ]]; then
  if [[ -f "$here/omls.service.in" ]]; then
    unit="$here/omls.service.in"
  else
    echo "omls install: missing omls.service.in" >&2
    exit 1
  fi
fi

if [[ ! -x "$bin" ]]; then
  echo "omls install: binary not executable: $bin" >&2
  exit 1
fi

# Optional Mesa OpenCL on apt hosts with Intel/AMD GPU (best-effort).
if [[ -f /etc/debian_version ]] && command -v apt-get >/dev/null 2>&1; then
  needs_mesa=false
  for device in /sys/bus/pci/devices/*; do
    [[ -r "$device/class" && -r "$device/vendor" ]] || continue
    class="$(<"$device/class")"
    vendor="$(<"$device/vendor")"
    if [[ "$class" == 0x03* && ( "$vendor" == 0x8086 || "$vendor" == 0x1002 ) ]]; then
      needs_mesa=true
      break
    fi
  done
  if $needs_mesa && [[ ! -e /etc/OpenCL/vendors/rusticl.icd && ! -e /usr/share/OpenCL/vendors/rusticl.icd ]]; then
    echo "omls install: installing mesa-opencl-icd…"
    run_root apt-get install -y --no-install-recommends mesa-opencl-icd || true
  fi
fi

if command -v systemctl >/dev/null 2>&1; then
  run_root systemctl stop omls.service 2>/dev/null || true
fi

echo "omls install: installing /usr/local/bin/omls"
run_root install -D -m 0755 "$bin" /usr/local/bin/omls
run_root ln -sfn /usr/local/bin/omls /usr/local/bin/omlsd

if command -v systemctl >/dev/null 2>&1; then
  sed "s/@OMLS_USER@/$service_user/g" "$unit" \
    | run_root install -D -m 0644 /dev/stdin /etc/systemd/system/omls.service
  run_root systemctl daemon-reload
  run_root systemctl disable --now omls-master.service omls-agent.service 2>/dev/null || true
  run_root systemctl enable --now omls.service
  echo "omls install: omls.service enabled"
  echo "  systemctl status omls"
  echo "  journalctl -u omls -f"
else
  echo "omls install: binary only (no systemd)"
fi

echo "omls install: done"
/usr/local/bin/omls version
echo "Cluster: omls status && omls nodes"
echo "Later upgrades: omls update"
